package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"time"

	"github.com/shortdesk/shortdesk/internal/desk"
	"github.com/shortdesk/shortdesk/internal/runtime"
)

const endpointTTL = 30 * time.Second

// viewerPath is where a desk's screen is served, with KasmVNC's websocket path
// pointed back through the same proxy.
//
// KasmVNC turns off clipboard sync and remote resizing when it runs inside an
// iframe (it expects a parent app to handle them). The dashboard embeds it, so
// those settings are switched back on through the URL.
func viewerPath(id string) string {
	base := "/desks/" + id + "/view/"
	return base + "?" + url.Values{
		"path":               {base[1:] + "websockify"},
		"clipboard_up":       {"true"},
		"clipboard_down":     {"true"},
		"clipboard_seamless": {"true"},
		"resize":             {"remote"},
	}.Encode()
}

// viewerProxy forwards /desks/{id}/view/... (including the websocket) to the desk's
// screen. Session cookies and tokens are stripped so they never reach the desk.
type viewerProxy struct {
	desks *desk.Manager
	log   *slog.Logger
	proxy *httputil.ReverseProxy

	mu    sync.Mutex
	cache map[string]cachedEndpoint
}

type cachedEndpoint struct {
	ep      runtime.Endpoint
	fetched time.Time
}

type ctxKey struct{}

func newViewerProxy(desks *desk.Manager, log *slog.Logger) *viewerProxy {
	p := &viewerProxy{desks: desks, log: log, cache: map[string]cachedEndpoint{}}
	p.proxy = &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			ep := pr.In.Context().Value(ctxKey{}).(runtime.Endpoint)
			pr.Out.URL.Scheme = "http"
			pr.Out.URL.Host = ep.Host
			pr.Out.URL.Path = ep.PathPrefix + "/" + pr.In.PathValue("rest")
			pr.Out.URL.RawPath = ""
			pr.Out.Host = ep.Host
			pr.Out.Header.Del("Cookie")
			pr.Out.Header.Del("Authorization")
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			log.Warn("desk screen unreachable", "path", r.URL.Path, "err", err)
			writeError(w, http.StatusBadGateway, errors.New("desk screen unreachable"))
		},
	}
	return p
}

func (p *viewerProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ep, err := p.endpoint(r)
	if errors.Is(err, desk.ErrNotFound) {
		writeError(w, http.StatusNotFound, errors.New("desk not found"))
		return
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	p.proxy.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, ep)))
}

// endpoint resolves (and briefly caches) the screen endpoint of a Shortdesk desk.
// Only desks Shortdesk manages can be reached.
func (p *viewerProxy) endpoint(r *http.Request) (runtime.Endpoint, error) {
	id := r.PathValue("id")
	p.mu.Lock()
	c, ok := p.cache[id]
	p.mu.Unlock()
	if ok && time.Since(c.fetched) < endpointTTL {
		return c.ep, nil
	}
	d, err := p.desks.Find(r.Context(), id)
	if err != nil {
		return runtime.Endpoint{}, err
	}
	if d.ID != id {
		return runtime.Endpoint{}, desk.ErrNotFound
	}
	ep, err := p.desks.Runtime().Endpoint(r.Context(), id, desk.ViewerPort)
	if err != nil {
		return runtime.Endpoint{}, err
	}
	p.mu.Lock()
	p.cache[id] = cachedEndpoint{ep: ep, fetched: time.Now()}
	p.mu.Unlock()
	return ep, nil
}

func (p *viewerProxy) forget(id string) {
	p.mu.Lock()
	delete(p.cache, id)
	p.mu.Unlock()
}
