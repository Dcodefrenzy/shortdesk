// Package opensandbox implements runtime.Runtime on an OpenSandbox server
// (https://github.com/opensandbox-group/OpenSandbox).
//
// Two APIs are used: the lifecycle API on the server (create, list, delete, endpoints,
// network policy) and the execd API inside each sandbox (running commands).
package opensandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/shortdesk/shortdesk/internal/runtime"
)

// execdPort is where OpenSandbox's execd daemon listens inside every sandbox.
const execdPort = 44772

// Config configures the OpenSandbox runtime.
type Config struct {
	URL    string // lifecycle API base, e.g. http://127.0.0.1:8090
	APIKey string
	// LocalDocker means the server runs sandboxes on this machine's Docker, so an
	// interactive shell can be opened with `docker exec` on the sandbox container.
	LocalDocker bool
	// EndpointHost is how OpenSandbox names this machine in desk addresses when it runs in
	// a container ("host.docker.internal" on Docker Desktop); it's replaced by 127.0.0.1.
	EndpointHost string
	HTTPClient   *http.Client
}

// Runtime talks to an OpenSandbox server.
type Runtime struct {
	cfg  Config
	http *http.Client
}

var _ runtime.Runtime = (*Runtime)(nil)
var _ runtime.Shell = (*Runtime)(nil)

// New returns an OpenSandbox runtime.
func New(cfg Config) (*Runtime, error) {
	if cfg.URL == "" {
		return nil, errors.New("opensandbox: URL is required")
	}
	cfg.URL = strings.TrimRight(cfg.URL, "/")
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 2 * time.Minute}
	}
	return &Runtime{cfg: cfg, http: hc}, nil
}

func (r *Runtime) Name() string { return "opensandbox" }

func (r *Runtime) Capabilities() runtime.Capabilities {
	return runtime.Capabilities{EgressPolicy: true}
}

type imageSpec struct {
	URI string `json:"uri"`
}

type networkRule struct {
	Action string `json:"action"`
	Target string `json:"target"`
}

type networkPolicy struct {
	DefaultAction string        `json:"defaultAction"`
	Egress        []networkRule `json:"egress"`
}

type createRequest struct {
	Image          imageSpec         `json:"image"`
	Entrypoint     []string          `json:"entrypoint,omitempty"`
	Timeout        int               `json:"timeout,omitempty"`
	ResourceLimits map[string]string `json:"resourceLimits,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
	NetworkPolicy  *networkPolicy    `json:"networkPolicy,omitempty"`
	Volumes        []volume          `json:"volumes,omitempty"`
}

type volume struct {
	Name string `json:"name"`
	PVC  struct {
		ClaimName         string `json:"claimName"`
		CreateIfNotExists bool   `json:"createIfNotExists"`
	} `json:"pvc"`
	MountPath string `json:"mountPath"`
}

type sandbox struct {
	ID     string `json:"id"`
	Status struct {
		State   string `json:"state"`
		Message string `json:"message"`
	} `json:"status"`
	Metadata  map[string]string `json:"metadata"`
	CreatedAt time.Time         `json:"createdAt"`
	ExpiresAt *time.Time        `json:"expiresAt"`
}

func (s sandbox) box() runtime.Box {
	b := runtime.Box{ID: s.ID, State: s.Status.State, Labels: s.Metadata, CreatedAt: s.CreatedAt}
	if s.ExpiresAt != nil {
		b.ExpiresAt = *s.ExpiresAt
	}
	return b
}

// Create starts a sandbox. When spec.AllowedHosts is set, all other outbound traffic is denied.
func (r *Runtime) Create(ctx context.Context, spec runtime.Spec) (runtime.Box, error) {
	req := createRequest{
		Image:      imageSpec{URI: spec.Image},
		Entrypoint: spec.Entrypoint,
		Env:        spec.Env,
		Metadata:   spec.Labels,
		ResourceLimits: map[string]string{
			"cpu":    spec.CPU,
			"memory": spec.Memory,
		},
	}
	if spec.Lifetime > 0 {
		req.Timeout = max(int(spec.Lifetime.Seconds()), 60)
	}
	for i, m := range spec.Mounts {
		v := volume{Name: fmt.Sprintf("shared-%d", i), MountPath: m.Path}
		v.PVC.ClaimName, v.PVC.CreateIfNotExists = m.Volume, true
		req.Volumes = append(req.Volumes, v)
	}
	if spec.AllowedHosts != nil {
		req.NetworkPolicy = &networkPolicy{DefaultAction: "deny"}
		for _, h := range spec.AllowedHosts {
			req.NetworkPolicy.Egress = append(req.NetworkPolicy.Egress, networkRule{Action: "allow", Target: h})
		}
	}
	var out sandbox
	if err := r.do(ctx, http.MethodPost, "/v1/sandboxes", req, &out); err != nil {
		return runtime.Box{}, fmt.Errorf("create sandbox: %w", err)
	}
	return out.box(), nil
}

func (r *Runtime) Get(ctx context.Context, id string) (runtime.Box, error) {
	var out sandbox
	if err := r.do(ctx, http.MethodGet, "/v1/sandboxes/"+url.PathEscape(id), nil, &out); err != nil {
		return runtime.Box{}, err
	}
	return out.box(), nil
}

// List pages through all sandboxes and filters by labels client-side.
func (r *Runtime) List(ctx context.Context, labels map[string]string) ([]runtime.Box, error) {
	const pageSize = 100
	var boxes []runtime.Box
	for page := 1; ; page++ {
		var out struct {
			Items []sandbox `json:"items"`
		}
		path := fmt.Sprintf("/v1/sandboxes?page=%d&pageSize=%d", page, pageSize)
		if err := r.do(ctx, http.MethodGet, path, nil, &out); err != nil {
			return nil, fmt.Errorf("list sandboxes: %w", err)
		}
		for _, s := range out.Items {
			if hasLabels(s.Metadata, labels) {
				boxes = append(boxes, s.box())
			}
		}
		if len(out.Items) < pageSize {
			return boxes, nil
		}
	}
}

func hasLabels(have, want map[string]string) bool {
	for k, v := range want {
		if have[k] != v {
			return false
		}
	}
	return true
}

func (r *Runtime) Delete(ctx context.Context, id string) error {
	return r.do(ctx, http.MethodDelete, "/v1/sandboxes/"+url.PathEscape(id), nil, nil)
}

// Endpoint returns how to reach a port inside the sandbox, e.g. 127.0.0.1:56109/proxy/6080.
func (r *Runtime) Endpoint(ctx context.Context, id string, port int) (runtime.Endpoint, error) {
	var out struct {
		Endpoint string `json:"endpoint"`
	}
	path := fmt.Sprintf("/v1/sandboxes/%s/endpoints/%d", url.PathEscape(id), port)
	if err := r.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return runtime.Endpoint{}, fmt.Errorf("endpoint for port %d: %w", port, err)
	}
	host, prefix, _ := strings.Cut(out.Endpoint, "/")
	if prefix != "" {
		prefix = "/" + prefix
	}
	if h := r.cfg.EndpointHost; h != "" && strings.HasPrefix(host, h+":") {
		host = "127.0.0.1" + strings.TrimPrefix(host, h)
	}
	return runtime.Endpoint{Host: host, PathPrefix: prefix}, nil
}

// ShellCommand opens a terminal via docker exec. Only valid when the server's
// sandboxes run on this machine's Docker.
func (r *Runtime) ShellCommand(id string) ([]string, error) {
	if !r.cfg.LocalDocker {
		return nil, errors.New("interactive shell needs the OpenSandbox server on this machine (set SHORTDESK_OPENSANDBOX_LOCAL_DOCKER=true)")
	}
	return []string{"docker", "exec", "-it", "sandbox-" + id, "bash", "-l"}, nil
}

// apiError is a non-2xx response from the lifecycle API.
type apiError struct {
	Status int
	Body   string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("opensandbox: HTTP %d: %s", e.Status, strings.TrimSpace(e.Body))
}

func (r *Runtime) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, r.cfg.URL+path, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if r.cfg.APIKey != "" {
		req.Header.Set("OPEN-SANDBOX-API-KEY", r.cfg.APIKey)
	}
	resp, err := r.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return runtime.ErrNotFound
	}
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &apiError{Status: resp.StatusCode, Body: string(b)}
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

var _ runtime.NetworkPolicy = (*Runtime)(nil)

// SetAllowedHosts replaces a running sandbox's egress allowlist.
func (r *Runtime) SetAllowedHosts(ctx context.Context, id string, hosts []string) error {
	p := networkPolicy{DefaultAction: "deny", Egress: []networkRule{}}
	if slices.Equal(hosts, runtime.AllowAllHosts) {
		p.DefaultAction = "allow"
		hosts = nil
	}
	for _, h := range hosts {
		p.Egress = append(p.Egress, networkRule{Action: "allow", Target: h})
	}
	if err := r.do(ctx, http.MethodPut, "/v1/sandboxes/"+url.PathEscape(id)+"/networkpolicy", p, nil); err != nil {
		return fmt.Errorf("update network policy: %w", err)
	}
	return nil
}

var _ runtime.PrivateNetwork = (*Runtime)(nil)

// PrivateAddr returns the sandbox's address on the local Docker network. A sandbox's
// network belongs to its egress sidecar, so that container holds the address.
func (r *Runtime) PrivateAddr(ctx context.Context, id string) (string, error) {
	if !r.cfg.LocalDocker {
		return "", errors.New("desks can only reach each other when OpenSandbox runs on this machine's Docker")
	}
	out, err := exec.CommandContext(ctx, "docker", "inspect", "-f",
		"{{range .NetworkSettings.Networks}}{{.IPAddress}} {{end}}", "sandbox-egress-"+id).Output()
	if err != nil {
		return "", fmt.Errorf("desk %s address: %w", id, err)
	}
	for _, ip := range strings.Fields(string(out)) {
		return ip, nil
	}
	return "", fmt.Errorf("desk %s has no network address", id)
}

var _ runtime.Renewer = (*Runtime)(nil)

// Renew moves the sandbox's expiry (the server's creation-time limit doesn't apply).
func (r *Runtime) Renew(ctx context.Context, id string, expiresAt time.Time) (time.Time, error) {
	var out struct {
		ExpiresAt time.Time `json:"expiresAt"`
	}
	err := r.do(ctx, http.MethodPost, "/v1/sandboxes/"+url.PathEscape(id)+"/renew-expiration",
		map[string]string{"expiresAt": expiresAt.UTC().Format(time.RFC3339)}, &out)
	return out.ExpiresAt, err
}
