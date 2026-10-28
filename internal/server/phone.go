package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/http/httputil"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shortdesk/shortdesk/internal/desk"
	"github.com/shortdesk/shortdesk/internal/phone"
	"github.com/shortdesk/shortdesk/internal/runtime"
)

// The Phone Desk is the software that serves the person's apps. It runs on the main
// desk (shortdesk phone-host, from the desk image), keeps the apps and their data in
// the main desk's home, and is reached:
//   - by the person, through Shortdesk at /phone/api/… and /phone/apps/… (Shortdesk
//     checks their login and calls with the owner key; cookies never reach the desk);
//   - by agents, directly: on the main desk at 127.0.0.1, from helper desks at the
//     main desk's address on the desk network, with a key signed per task.
const (
	phonePort      = 7071
	phoneDir       = "/home/desk/.phone"
	phoneSecretKey = "phone_secret"
	agentKeyTTL    = 3 * time.Hour
)

// startPhone runs on the main desk: it starts the Phone Desk unless it already answers.
const startPhone = `if curl -fsS -m 2 http://127.0.0.1:` + "7071" + `/healthz >/dev/null 2>&1; then echo running; exit 0; fi
if ! command -v shortdesk >/dev/null; then echo missing; exit 0; fi
mkdir -p ` + phoneDir + `
nohup shortdesk phone-host --addr :` + "7071" + ` --dir ` + phoneDir + ` >/tmp/shortdesk-phone.log 2>&1 &
for i in 1 2 3 4 5 6 7 8 9 10; do
  if curl -fsS -m 1 http://127.0.0.1:` + "7071" + `/healthz >/dev/null 2>&1; then echo started; exit 0; fi
  sleep 0.5
done
echo failed; tail -5 /tmp/shortdesk-phone.log`

type phoneDesk struct {
	s     *Server
	proxy *httputil.ReverseProxy

	mu     sync.Mutex
	secret []byte
	state  string // running | missing (old desk image) | failed | "" (not checked)
}

type phoneCtxKey struct{}

var errPhoneStarting = errors.New("phone desk is starting")

func newPhoneDesk(s *Server) *phoneDesk {
	p := &phoneDesk{s: s}
	p.proxy = &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			ep := pr.In.Context().Value(phoneCtxKey{}).(runtime.Endpoint)
			pr.Out.URL.Scheme = "http"
			pr.Out.URL.Host = ep.Host
			pr.Out.URL.Path = ep.PathPrefix + strings.TrimPrefix(pr.In.URL.Path, "/phone")
			pr.Out.URL.RawPath = ""
			pr.Out.Host = ep.Host
			pr.Out.Header.Del("Cookie")
			pr.Out.Header.Del("Authorization")
			if strings.HasPrefix(pr.In.URL.Path, "/phone/api/") {
				pr.Out.Header.Set("Authorization", "Bearer "+phone.OwnerKey(p.key()))
			}
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			s.log.Warn("phone desk unreachable", "path", r.URL.Path, "err", err)
			p.unavailable(w, r, "Your phone isn't answering. The main desk may be starting; try again in a minute.")
		},
		// While a new main desk settles, its sandbox's proxy answers 502/503 itself:
		// show the same friendly page instead of a bare error.
		ModifyResponse: func(resp *http.Response) error {
			if resp.StatusCode == http.StatusBadGateway || resp.StatusCode == http.StatusServiceUnavailable {
				if !strings.Contains(resp.Header.Get("Content-Type"), "json") {
					return errPhoneStarting
				}
			}
			return nil
		},
	}
	return p
}

// key returns the secret the Phone Desk and Shortdesk share, made on first use.
func (p *phoneDesk) key() []byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.secret != nil {
		return p.secret
	}
	v := p.s.workspace.Secret(phoneSecretKey)
	if v == "" {
		b := make([]byte, 32)
		rand.Read(b)
		v = hex.EncodeToString(b)
		if err := p.s.workspace.SetSecret(phoneSecretKey, v); err != nil {
			p.s.log.Warn("could not save the phone key", "err", err)
		}
	}
	p.secret = []byte(v)
	return p.secret
}

// mainDeskLease is how far ahead the main desk's expiry is kept: it never expires while
// Shortdesk runs, and lasts this long if Shortdesk is stopped.
const mainDeskLease = 7 * 24 * time.Hour

// keepAlive pushes the main desk's expiry forward once it's within a day of the lease.
func (p *phoneDesk) keepAlive(ctx context.Context, id string) {
	rn, ok := p.s.desks.Runtime().(runtime.Renewer)
	if !ok {
		return
	}
	d, err := p.s.desks.Find(ctx, id)
	if err != nil || d.ExpiresAt.IsZero() || time.Until(d.ExpiresAt) > mainDeskLease-24*time.Hour {
		return
	}
	if _, err := rn.Renew(ctx, id, time.Now().Add(mainDeskLease)); err != nil {
		p.s.log.Warn("could not keep the main desk alive", "desk", d.Name, "err", err)
		return
	}
	p.s.log.Info("kept the main desk alive", "desk", d.Name, "until", time.Now().Add(mainDeskLease).Format(time.DateTime))
}

// ensure keeps the main desk alive and starts the Phone Desk on it if it isn't running.
func (p *phoneDesk) ensure(ctx context.Context) {
	main := p.s.mainDesk(ctx)
	if main == "" {
		p.setState("")
		return
	}
	p.keepAlive(ctx, main)
	var out bytes.Buffer
	err := p.s.desks.Runtime().Exec(ctx, main, runtime.ExecRequest{
		Command: startPhone,
		Env:     map[string]string{phone.SecretEnv: string(p.key()), "HOME": "/home/desk"},
		Stdout:  &out,
	})
	state := strings.Fields(out.String())
	switch {
	case err != nil:
		p.s.log.Warn("could not check the phone desk", "desk", main, "err", err)
	case len(state) > 0 && state[0] == "started":
		p.s.log.Info("started the phone desk", "desk", main)
		p.setState("running")
	case len(state) > 0:
		if state[0] == "failed" {
			p.s.log.Warn("the phone desk didn't start", "desk", main, "log", out.String())
		}
		p.setState(state[0])
	}
}

func (p *phoneDesk) setState(s string) {
	p.mu.Lock()
	p.state = s
	p.mu.Unlock()
}

func (p *phoneDesk) State() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state
}

// KeepPhone keeps whichever desk is the main desk alive, with the Phone Desk running.
func (s *Server) KeepPhone(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		check, cancel := context.WithTimeout(ctx, 30*time.Second)
		s.phone.ensure(check)
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// agentEnv gives an agent its way to the person's apps for one task.
func (p *phoneDesk) agentEnv(ctx context.Context, deskID string) map[string]string {
	main := p.s.mainDesk(ctx)
	if main == "" {
		return nil
	}
	url := "http://127.0.0.1:" + strconv.Itoa(phonePort)
	if deskID != main {
		pn, ok := p.s.desks.Runtime().(runtime.PrivateNetwork)
		if !ok {
			return nil
		}
		addr, err := pn.PrivateAddr(ctx, main)
		if err != nil {
			p.s.log.Warn("helper desk can't find the phone desk", "desk", deskID, "err", err)
			return nil
		}
		url = "http://" + addr + ":" + strconv.Itoa(phonePort)
	}
	d, err := p.s.desks.Find(ctx, deskID)
	if err != nil {
		return nil
	}
	return map[string]string{
		"SHORTDESK_PHONE_URL": url,
		"SHORTDESK_PHONE_KEY": phone.AgentKey(p.key(), d.Name, agentKeyTTL, time.Now()),
	}
}

// ServeHTTP forwards /phone/… to the Phone Desk on the main desk: the phone itself and
// its API (logged in), and app pages (signed paths).
func (p *phoneDesk) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	main := p.s.mainDesk(r.Context())
	if main == "" {
		p.unavailable(w, r, "Start your main desk first: it runs your phone.")
		return
	}
	ep, err := p.s.desks.Runtime().Endpoint(r.Context(), main, phonePort)
	if err != nil {
		if !errors.Is(err, desk.ErrNotFound) {
			p.s.log.Warn("phone desk address", "err", err)
		}
		p.unavailable(w, r, "Your phone isn't answering. The main desk may be starting; try again in a minute.")
		return
	}
	p.proxy.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), phoneCtxKey{}, ep)))
}

// unavailable answers when the phone can't be reached: a small page for people, JSON
// for the phone's own requests.
func (p *phoneDesk) unavailable(w http.ResponseWriter, r *http.Request, msg string) {
	if strings.HasPrefix(r.URL.Path, "/phone/api/") || strings.HasPrefix(r.URL.Path, "/phone/_next/") {
		writeError(w, http.StatusServiceUnavailable, errors.New(msg))
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Retry-After", "20")
	w.WriteHeader(http.StatusServiceUnavailable)
	fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<meta http-equiv="refresh" content="20"><title>Your phone</title>
<body style="margin:0;display:grid;place-items:center;min-height:100vh;font:15px system-ui;background:#0e1113;color:#e7eaec;text-align:center">
<div style="max-width:300px;padding:24px"><div style="font-size:40px">📱</div><p>%s</p></div>`, html.EscapeString(msg))
}

// phoneStatus tells the phone page whether apps can be shown.
func (s *Server) phoneStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"mainDesk": s.mainDesk(r.Context()), "state": s.phone.State()})
}
