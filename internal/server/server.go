// Package server is Shortdesk's HTTP API, used by the dashboard. It also proxies
// each desk's screen, so desks are only reachable through Shortdesk after login.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/shortdesk/shortdesk/internal/access"
	"github.com/shortdesk/shortdesk/internal/agent"
	"github.com/shortdesk/shortdesk/internal/charter"
	"github.com/shortdesk/shortdesk/internal/desk"
	"github.com/shortdesk/shortdesk/internal/logins"
	"github.com/shortdesk/shortdesk/internal/projects"
	"github.com/shortdesk/shortdesk/internal/vault"
	"github.com/shortdesk/shortdesk/internal/workspace"
)

// Config configures the server.
type Config struct {
	// Token is the admin token for logging in (single-user for now).
	Token string
	// ChartersDir holds the charters desks can be started from. It is re-read on
	// every request, so edits apply without a restart.
	ChartersDir string
	// WebDir, if set, is served at / (the built dashboard); otherwise Web is.
	WebDir string
	Web    fs.FS
	// AccessFile keeps per-desk website rules across restarts; empty keeps them in memory.
	AccessFile string
	// SecureCookies marks the session cookie Secure (set when served over HTTPS).
	SecureCookies bool
	// ReadyTimeout bounds how long creating a desk waits for its screen.
	ReadyTimeout time.Duration
}

// Server serves the API and desk screens.
type Server struct {
	cfg       Config
	desks     *desk.Manager
	agent     *agent.Runner
	access    *access.Manager
	workspace *workspace.Store
	logins    *logins.Manager
	projects  *projects.Manager
	vault     *vault.Vault
	viewer    *viewerProxy
	phone     *phoneDesk
	codes     loginCodes
	log       *slog.Logger
}

// New returns a Server.
func New(cfg Config, desks *desk.Manager, runner *agent.Runner, ws *workspace.Store, lg *logins.Manager, v *vault.Vault, log *slog.Logger) *Server {
	if cfg.ReadyTimeout == 0 {
		cfg.ReadyTimeout = 90 * time.Second
	}
	s := &Server{
		cfg: cfg, desks: desks, agent: runner, workspace: ws, log: log,
		access:   access.New(desks, runner, cfg.ChartersDir, log),
		logins:   lg,
		projects: projects.New(desks.Runtime()),
		vault:    v,
		viewer:   newViewerProxy(desks, log),
	}
	s.phone = newPhoneDesk(s)
	runner.AddPrepare(s.phone.agentEnv)
	if cfg.AccessFile != "" {
		if err := s.access.Persist(cfg.AccessFile); err != nil {
			log.Warn("could not load saved website rules", "err", err)
		}
	}
	return s
}

// Handler returns the server's routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "runtime": s.desks.Runtime().Name()})
	})
	mux.HandleFunc("POST /api/login", s.login)
	mux.HandleFunc("GET /login/link", s.loginWithLink)
	mux.HandleFunc("POST /api/logout", s.logout)

	authed := http.NewServeMux()
	authed.HandleFunc("GET /api/session", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]bool{"authenticated": true})
	})
	authed.HandleFunc("GET /api/charters", s.listCharters)
	authed.HandleFunc("GET /api/phone", s.phoneStatus)
	authed.Handle("/phone/api/", s.phone)
	authed.HandleFunc("GET /api/desks", s.listDesks)
	authed.HandleFunc("POST /api/desks", s.createDesk)
	authed.HandleFunc("GET /api/desks/{id}", s.getDesk)
	authed.HandleFunc("DELETE /api/desks/{id}", s.deleteDesk)
	authed.HandleFunc("GET /api/desks/{id}/clipboard", s.getClipboard)
	authed.HandleFunc("POST /api/desks/{id}/clipboard", s.setClipboard)
	authed.HandleFunc("POST /api/desks/{id}/tasks", s.startTask)
	authed.HandleFunc("POST /api/desks/{id}/agent/{action}", s.controlAgent)
	authed.HandleFunc("GET /api/desks/{id}/events", s.streamEvents)
	authed.HandleFunc("GET /api/desks/{id}/settings", s.getSettings)
	authed.HandleFunc("GET /api/desks/{id}/outputs", s.listOutputs)
	authed.HandleFunc("GET /api/desks/{id}/projects", s.listProjects)
	authed.HandleFunc("GET /api/desks/{id}/projects/{name}/diff", s.projectDiff)
	authed.HandleFunc("POST /api/desks/{id}/projects/{name}/merge", s.mergeProject)
	authed.HandleFunc("GET /api/desks/{id}/outputs/{path...}", s.getOutput)
	authed.HandleFunc("POST /api/desks/{id}/network", s.addNetworkRule)
	authed.HandleFunc("DELETE /api/desks/{id}/network/{host}", s.removeNetworkRule)
	authed.HandleFunc("PUT /api/desks/{id}/network/allow-all", s.setAllowAll)
	authed.HandleFunc("POST /api/desks/{id}/access/{requestId}", s.decideAccess)
	authed.HandleFunc("GET /api/logins", s.listLogins)
	authed.HandleFunc("GET /api/vault", s.vaultStatus)
	authed.HandleFunc("POST /api/vault/connect", s.connectVault)
	authed.HandleFunc("POST /api/vault/disconnect", s.disconnectVault)
	authed.HandleFunc("GET /api/integrations", s.listIntegrations)
	authed.HandleFunc("POST /api/integrations", s.addIntegration)
	authed.HandleFunc("DELETE /api/integrations/{id}", s.deleteIntegration)
	authed.HandleFunc("POST /api/desks/{id}/logins/{kind}/{name}/{action}", s.changeLogin)
	authed.HandleFunc("GET /api/workspace", s.getWorkspace)
	authed.HandleFunc("PUT /api/workspace/connections/{name}", s.setConnection)
	authed.HandleFunc("PUT /api/workspace/router", s.setRouter)
	authed.HandleFunc("PUT /api/workspace/main-desk", s.setMainDesk)
	authed.Handle("/desks/{id}/view/{rest...}", s.viewer)

	protected := s.requireAuth(authed)
	mux.Handle("/api/", protected)
	mux.Handle("/desks/", protected)
	mux.Handle("/phone/api/", protected)
	// App pages load through short-lived signed paths the Phone Desk checks itself.
	mux.Handle("GET /phone/apps/", s.phone)
	switch {
	case s.cfg.WebDir != "":
		mux.Handle("/", http.FileServer(http.Dir(s.cfg.WebDir)))
	case s.cfg.Web != nil:
		mux.Handle("/", http.FileServer(http.FS(s.cfg.Web)))
	}
	return logRequests(s.log, mux)
}

// deskJSON is a desk as the API returns it.
type deskJSON struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Charter   string     `json:"charter"`
	Tenant    string     `json:"tenant,omitempty"`
	State     string     `json:"state"`
	CreatedAt time.Time  `json:"createdAt"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	// Agent is the desk's agent state: idle, working or paused.
	Agent agent.State `json:"agent"`
	// ViewerURL is relative to this server: the desk's screen, through the proxy.
	ViewerURL string `json:"viewerUrl"`
	// Tasks is how many tasks the desk has been given.
	Tasks int `json:"tasks"`
}

func (s *Server) toJSON(d desk.Desk) deskJSON {
	j := deskJSON{
		ID: d.ID, Name: d.Name, Charter: d.Charter, Tenant: d.Tenant, State: d.State,
		CreatedAt: d.CreatedAt, ViewerURL: viewerPath(d.ID), Agent: s.agent.Store().State(d.ID),
		Tasks: s.agent.Store().TaskCount(d.ID),
	}
	if !d.ExpiresAt.IsZero() {
		j.ExpiresAt = &d.ExpiresAt
	}
	return j
}

type charterJSON struct {
	Name         string   `json:"name"`
	Tenant       string   `json:"tenant,omitempty"`
	Version      int      `json:"version,omitempty"`
	Image        string   `json:"image"`
	CPU          string   `json:"cpu"`
	Memory       string   `json:"memory"`
	Lifetime     string   `json:"lifetime"`
	AllowedHosts []string `json:"allowedHosts"`
}

func (s *Server) listCharters(w http.ResponseWriter, _ *http.Request) {
	charters, err := charter.LoadDir(s.cfg.ChartersDir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	out := make([]charterJSON, 0, len(charters))
	for _, c := range charters {
		out = append(out, toCharterJSON(c))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) listDesks(w http.ResponseWriter, r *http.Request) {
	desks, err := s.desks.List(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	out := make([]deskJSON, 0, len(desks))
	for _, d := range desks {
		out = append(out, s.toJSON(d))
	}
	writeJSON(w, http.StatusOK, out)
}

// createDesk starts a desk from a charter in ChartersDir (by name, never a path)
// and waits until its screen is ready.
func (s *Server) createDesk(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Charter string `json:"charter"`
		Name    string `json:"name"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	charters, err := charter.LoadDir(s.cfg.ChartersDir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	c, ok := charters[req.Charter]
	if !ok {
		writeError(w, http.StatusBadRequest, errors.New("unknown charter "+quote(req.Charter)))
		return
	}
	d, err := s.desks.Up(r.Context(), c, req.Name)
	switch {
	case errors.Is(err, desk.ErrNameTaken):
		writeError(w, http.StatusConflict, err)
		return
	case err != nil:
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.ReadyTimeout)
	defer cancel()
	if err := s.desks.WaitReady(ctx, d); err != nil {
		s.log.Warn("desk created but not ready", "desk", d.Name, "err", err)
	}
	// The desk's name names its git branches (desk/<name>) in shared projects.
	if err := s.projects.SetDeskName(ctx, d.ID, d.Name); err != nil {
		s.log.Warn("could not set desk name", "desk", d.Name, "err", err)
	}
	// New desks start logged in to everything saved.
	if err := s.logins.ApplyAll(ctx, d.ID); err != nil {
		s.log.Warn("could not give saved logins to new desk", "desk", d.Name, "err", err)
	}
	writeJSON(w, http.StatusCreated, s.toJSON(d))
}

func (s *Server) getDesk(w http.ResponseWriter, r *http.Request) {
	if d, ok := s.findDesk(w, r); ok {
		writeJSON(w, http.StatusOK, s.toJSON(d))
	}
}

func (s *Server) deleteDesk(w http.ResponseWriter, r *http.Request) {
	d, ok := s.findDesk(w, r)
	if !ok {
		return
	}
	// Keep any logins made on this desk before it goes away.
	if err := s.captureFrom(r.Context(), d); err != nil {
		s.log.Warn("could not save logins before removing desk", "desk", d.Name, "err", err)
	}
	if err := s.desks.Down(r.Context(), d); err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	s.viewer.forget(d.ID)
	s.agent.Store().Forget(d.ID)
	s.access.Forget(d.ID)
	s.logins.Forget(d.ID)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getClipboard(w http.ResponseWriter, r *http.Request) {
	d, ok := s.findDesk(w, r)
	if !ok {
		return
	}
	text, err := s.desks.Copy(r.Context(), d)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"text": text})
}

func (s *Server) setClipboard(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text string `json:"text"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	d, ok := s.findDesk(w, r)
	if !ok {
		return
	}
	if err := s.desks.Paste(r.Context(), d, req.Text); err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// findDesk looks up the {id} path value among Shortdesk's desks, writing 404 if absent.
func (s *Server) findDesk(w http.ResponseWriter, r *http.Request) (desk.Desk, bool) {
	id := r.PathValue("id")
	d, err := s.desks.Find(r.Context(), id)
	if err == nil && d.ID == id {
		return d, true
	}
	if err == nil || errors.Is(err, desk.ErrNotFound) {
		writeError(w, http.StatusNotFound, errors.New("desk not found"))
	} else {
		writeError(w, http.StatusBadGateway, err)
	}
	return desk.Desk{}, false
}

// decodeJSON requires a JSON body, which also stops cross-site form posts.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, errors.New("Content-Type must be application/json"))
		return false
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func quote(s string) string { return `"` + s + `"` }

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Unwrap lets http.ResponseController reach the underlying writer (websocket hijacking).
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func logRequests(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		// Desk screens load many assets; only log their errors.
		if strings.HasPrefix(r.URL.Path, "/desks/") && rec.status < 400 {
			return
		}
		log.Info("request", "method", r.Method, "path", r.URL.Path, "status", rec.status, "took", time.Since(start).Round(time.Millisecond))
	})
}
