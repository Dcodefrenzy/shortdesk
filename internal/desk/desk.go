// Package desk turns charters into running desks on a runtime and manages them:
// naming, listing, viewer links, clipboard and teardown.
package desk

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/shortdesk/shortdesk/internal/charter"
	"github.com/shortdesk/shortdesk/internal/runtime"
)

// Facts about the desk image (images/desk).
const (
	ViewerPort = 6080
	Entrypoint = "/usr/local/bin/start-desk"
	display    = ":0"
)

// Labels Shortdesk puts on every box it creates.
const (
	LabelManaged = "shortdesk.managed"
	LabelName    = "shortdesk.name"
	LabelCharter = "shortdesk.charter"
	LabelTenant  = "shortdesk.tenant"
)

// Errors callers can check for.
var (
	ErrNotFound  = errors.New("desk not found")
	ErrNameTaken = errors.New("desk name already in use")
)

// Desk is a running (or stopped) desk.
type Desk struct {
	ID        string
	Name      string
	Charter   string
	Tenant    string
	State     string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// Manager manages desks on one runtime.
type Manager struct {
	rt     runtime.Runtime
	http   *http.Client
	shared []runtime.Mount
}

// SharedFolders every desk gets: storage volumes managed by the runtime (not folders
// on anyone's computer), the same files on every desk, so new desks start with them.
var SharedFolders = []runtime.Mount{
	{Volume: "shortdesk-developer", Path: "/home/desk/Developer"}, // code projects
	{Volume: "shortdesk-files", Path: "/home/desk/Files"},         // documents, downloads, anything to keep
}

// ShareFolders attaches SharedFolders to every desk created after this call.
func (m *Manager) ShareFolders() { m.shared = SharedFolders }

// Shared returns the folders every new desk gets.
func (m *Manager) Shared() []runtime.Mount { return m.shared }

// NewManager returns a Manager for rt.
func NewManager(rt runtime.Runtime) *Manager {
	return &Manager{rt: rt, http: &http.Client{Timeout: 5 * time.Second}}
}

// Runtime returns the manager's runtime.
func (m *Manager) Runtime() runtime.Runtime { return m.rt }

// Up creates a desk from a charter. An empty name gets a generated one.
func (m *Manager) Up(ctx context.Context, c *charter.Charter, name string) (Desk, error) {
	if !c.Permissions.Network.Unrestricted() && !m.rt.Capabilities().EgressPolicy {
		return Desk{}, fmt.Errorf("charter %q restricts network access, but the %s runtime cannot enforce that; use the opensandbox runtime or allow [\"*\"]",
			c.Name, m.rt.Name())
	}
	if name == "" {
		suffix, err := randomSuffix()
		if err != nil {
			return Desk{}, err
		}
		name = c.Name + "-" + suffix
	}
	if _, err := m.Find(ctx, name); err == nil {
		return Desk{}, fmt.Errorf("%w: %q", ErrNameTaken, name)
	} else if !errors.Is(err, ErrNotFound) {
		return Desk{}, err
	}

	spec := specFor(c, name)
	spec.Mounts = m.shared
	box, err := m.rt.Create(ctx, spec)
	if err != nil {
		return Desk{}, err
	}
	return fromBox(box), nil
}

// specFor translates a charter into a runtime spec.
func specFor(c *charter.Charter, name string) runtime.Spec {
	spec := runtime.Spec{
		Image:      c.Desk.Image,
		Entrypoint: []string{Entrypoint},
		Env:        c.Desk.Env,
		CPU:        c.Desk.Resources.CPU,
		Memory:     c.Desk.Resources.Memory,
		Lifetime:   time.Duration(c.Limits.DeskLifetime),
		Ports:      []int{ViewerPort},
		Labels: map[string]string{
			LabelManaged: "true",
			LabelName:    name,
			LabelCharter: c.Name,
		},
	}
	if c.Tenant != "" {
		spec.Labels[LabelTenant] = c.Tenant
	}
	if !c.Permissions.Network.Unrestricted() {
		spec.AllowedHosts = c.Permissions.Network.Allow
	}
	return spec
}

// List returns all desks, newest first.
func (m *Manager) List(ctx context.Context) ([]Desk, error) {
	boxes, err := m.rt.List(ctx, map[string]string{LabelManaged: "true"})
	if err != nil {
		return nil, err
	}
	desks := make([]Desk, 0, len(boxes))
	for _, b := range boxes {
		desks = append(desks, fromBox(b))
	}
	sort.Slice(desks, func(i, j int) bool { return desks[i].CreatedAt.After(desks[j].CreatedAt) })
	return desks, nil
}

// Find returns the desk with the given name or ID. An empty ref means "the only desk".
func (m *Manager) Find(ctx context.Context, ref string) (Desk, error) {
	desks, err := m.List(ctx)
	if err != nil {
		return Desk{}, err
	}
	if ref == "" {
		switch len(desks) {
		case 0:
			return Desk{}, ErrNotFound
		case 1:
			return desks[0], nil
		default:
			return Desk{}, fmt.Errorf("%d desks running; name one (see `shortdesk desk list`)", len(desks))
		}
	}
	for _, d := range desks {
		if d.Name == ref || d.ID == ref {
			return d, nil
		}
	}
	return Desk{}, fmt.Errorf("%w: %q", ErrNotFound, ref)
}

// Down deletes a desk.
func (m *Manager) Down(ctx context.Context, d Desk) error {
	return m.rt.Delete(ctx, d.ID)
}

// ViewerURL returns the browser link to a desk's screen. KasmVNC's viewer connects its
// websocket to ?path= relative to the host root, so the endpoint's path prefix
// has to be passed along.
func (m *Manager) ViewerURL(ctx context.Context, d Desk) (string, error) {
	ep, err := m.rt.Endpoint(ctx, d.ID, ViewerPort)
	if err != nil {
		return "", err
	}
	wsPath := strings.TrimPrefix(ep.PathPrefix+"/websockify", "/")
	return ep.URL() + "/?" + url.Values{"path": {wsPath}}.Encode(), nil
}

// WaitReady blocks until the desk's viewer answers or ctx ends.
func (m *Manager) WaitReady(ctx context.Context, d Desk) error {
	ep, err := m.rt.Endpoint(ctx, d.ID, ViewerPort)
	if err != nil {
		return err
	}
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ep.URL()+"/", nil)
		if resp, err := m.http.Do(req); err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("desk %s not ready: %w", d.Name, ctx.Err())
		case <-tick.C:
		}
	}
}

// Paste puts text on the desk's clipboard (and primary selection, for middle-click).
// Text travels base64-encoded so newlines and special characters survive exactly.
func (m *Manager) Paste(ctx context.Context, d Desk, text string) error {
	return m.rt.Exec(ctx, d.ID, runtime.ExecRequest{
		Command: `f=$(mktemp) && printf %s "$SHORTDESK_CLIP" | base64 -d > "$f" &&
xclip -selection clipboard -i "$f" && xclip -selection primary -i "$f"; s=$?; rm -f "$f"; exit $s`,
		Env: map[string]string{"SHORTDESK_CLIP": base64.StdEncoding.EncodeToString([]byte(text)), "DISPLAY": display},
	})
}

// Copy returns the desk's clipboard text.
func (m *Manager) Copy(ctx context.Context, d Desk) (string, error) {
	var out bytes.Buffer
	err := m.rt.Exec(ctx, d.ID, runtime.ExecRequest{
		Command: `xclip -selection clipboard -o 2>/dev/null | base64 -w0`,
		Env:     map[string]string{"DISPLAY": display},
		Stdout:  &out,
	})
	if err != nil {
		return "", err
	}
	text, err := base64.StdEncoding.DecodeString(strings.TrimSpace(out.String()))
	if err != nil {
		return "", fmt.Errorf("decode desk clipboard: %w", err)
	}
	return string(text), nil
}

func fromBox(b runtime.Box) Desk {
	return Desk{
		ID:        b.ID,
		Name:      b.Labels[LabelName],
		Charter:   b.Labels[LabelCharter],
		Tenant:    b.Labels[LabelTenant],
		State:     b.State,
		CreatedAt: b.CreatedAt,
		ExpiresAt: b.ExpiresAt,
	}
}

func randomSuffix() (string, error) {
	b := make([]byte, 2)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
