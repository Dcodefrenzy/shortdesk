// Package phone is the Phone Desk's host: it keeps every ShortApp (installed as a
// folder with app.json and web/), gives each one its own data through one API, and
// serves the phone the person sees. Agents build apps on their own desks and install
// them here; they and the person then read and change app data through the API.
//
// The host trusts no one by default. Shortdesk calls it with the owner key (for the
// person, after checking their login); agents call it with a key Shortdesk signs for
// each task. App pages load through short-lived signed paths and talk to their data
// only through the phone page around them, never directly.
package phone

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/shortdesk/shortdesk/internal/appdata"
)

// Config configures a Host.
type Config struct {
	Dir     string // where apps and their data live (a volume that outlives the desk)
	Secret  []byte // shared with Shortdesk; signs every key
	Builtin fs.FS  // apps every phone starts with: <id>/app.json, <id>/web/…
	Shell   fs.FS  // the phone page and its assets (the built dashboard)
	SDK     []byte // shortapps.js, served next to every app's page
	Log     *slog.Logger
}

// Host serves the Phone Desk.
type Host struct {
	cfg  Config
	apps apps
	data appdata.Store
	now  func() time.Time
}

// AppLinkTTL is how long an opened app's files stay loadable.
const AppLinkTTL = 12 * time.Hour

// New opens the host's apps and data, and installs built-in apps that are missing or
// out of date (unless they were changed since).
func New(cfg Config) (*Host, error) {
	if len(cfg.Secret) < 16 {
		return nil, errors.New("phone: the shared secret is missing or too short")
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	data, err := appdata.OpenSQLite(filepath.Join(cfg.Dir, "data"))
	if err != nil {
		return nil, err
	}
	h := &Host{cfg: cfg, apps: apps{dir: filepath.Join(cfg.Dir, "apps")}, data: data, now: time.Now}
	if cfg.Builtin != nil {
		if err := h.installBuiltin(); err != nil {
			return nil, err
		}
	}
	return h, nil
}

func (h *Host) Close() error { return h.data.Close() }

func (h *Host) installBuiltin() error {
	entries, err := fs.ReadDir(h.cfg.Builtin, ".")
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sub, _ := fs.Sub(h.cfg.Builtin, e.Name())
		raw, err := fs.ReadFile(sub, "app.json")
		if err != nil {
			continue
		}
		var m Manifest
		if json.Unmarshal(raw, &m) != nil {
			continue
		}
		have, err := h.apps.get(m.ID)
		if err == nil && (!have.Builtin || have.Version == m.Version) {
			continue // up to date, or changed by its owner: leave it
		}
		if _, err := h.apps.installFS(sub); err != nil {
			return fmt.Errorf("built-in app %s: %w", e.Name(), err)
		}
		h.cfg.Log.Info("installed built-in app", "app", m.ID, "version", m.Version)
	}
	return nil
}

// Handler returns the host's routes.
func (h *Host) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })

	api := http.NewServeMux()
	api.HandleFunc("GET /api/apps", h.listApps)
	api.HandleFunc("POST /api/apps", h.installApp)
	api.HandleFunc("DELETE /api/apps/{app}", h.ownerOnly(h.removeApp))
	api.HandleFunc("POST /api/apps/{app}/undo", h.undoApp)
	api.HandleFunc("POST /api/apps/{app}/open", h.openApp)
	api.HandleFunc("GET /api/apps/{app}/data/{coll}", h.queryRecords)
	api.HandleFunc("POST /api/apps/{app}/data/{coll}/query", h.queryRecords)
	api.HandleFunc("POST /api/apps/{app}/data/{coll}", h.addRecord)
	api.HandleFunc("GET /api/apps/{app}/data/{coll}/{id}", h.getRecord)
	api.HandleFunc("PATCH /api/apps/{app}/data/{coll}/{id}", h.updateRecord)
	api.HandleFunc("DELETE /api/apps/{app}/data/{coll}/{id}", h.deleteRecord)
	api.HandleFunc("GET /api/apps/{app}/history", h.history)
	mux.Handle("/api/", h.authed(api))

	mux.HandleFunc("GET /apps/{app}/{token}/{file...}", h.serveAppFile)
	mux.HandleFunc("/", h.serveShell)
	return mux
}

type callerKey struct{}

func (h *Host) authed(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		c, err := parseKey(h.cfg.Secret, key, h.now())
		if err != nil {
			writeError(w, http.StatusUnauthorized, errors.New("this needs a phone key"))
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), callerKey{}, c)))
	})
}

func callerOf(r *http.Request) caller { c, _ := r.Context().Value(callerKey{}).(caller); return c }

func (h *Host) ownerOnly(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if callerOf(r).role != roleOwner {
			writeError(w, http.StatusForbidden, errors.New("only you can do that, not an agent"))
			return
		}
		fn(w, r)
	}
}

func (h *Host) listApps(w http.ResponseWriter, _ *http.Request) {
	list, err := h.apps.list()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (h *Host) installApp(w http.ResponseWriter, r *http.Request) {
	m, err := h.apps.installTar(r.Body)
	if err != nil {
		writeError(w, status(err), err)
		return
	}
	h.cfg.Log.Info("installed app", "app", m.ID, "version", m.Version, "by", callerOf(r).actor)
	app, _ := h.apps.get(m.ID)
	writeJSON(w, http.StatusOK, app)
}

func (h *Host) removeApp(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("app")
	if _, err := h.apps.get(id); err != nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("no app %q", id))
		return
	}
	if err := h.apps.remove(id); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := h.data.DropApp(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Host) undoApp(w http.ResponseWriter, r *http.Request) {
	if err := h.apps.undo(r.PathValue("app")); err != nil {
		writeError(w, status(err), err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// openApp returns where the app's page loads from, for the next AppLinkTTL.
func (h *Host) openApp(w http.ResponseWriter, r *http.Request) {
	app, err := h.apps.get(r.PathValue("app"))
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("no app %q", r.PathValue("app")))
		return
	}
	tok := appToken(h.cfg.Secret, app.ID, AppLinkTTL, h.now())
	writeJSON(w, http.StatusOK, map[string]any{"app": app, "path": "apps/" + app.ID + "/" + tok + "/"})
}

// appData returns an installed app's data, checking that it keeps coll.
func (h *Host) appData(w http.ResponseWriter, r *http.Request) (appdata.AppData, string, bool) {
	app, err := h.apps.get(r.PathValue("app"))
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("no app %q", r.PathValue("app")))
		return nil, "", false
	}
	coll := r.PathValue("coll")
	if coll != "" && !app.keeps(coll) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("%s keeps %s, not %q", app.Name, strings.Join(app.Collections, ", "), coll))
		return nil, "", false
	}
	d, err := h.data.App(r.Context(), app.ID)
	if err != nil {
		writeError(w, status(err), err)
		return nil, "", false
	}
	return d, coll, true
}

func (h *Host) queryRecords(w http.ResponseWriter, r *http.Request) {
	d, coll, ok := h.appData(w, r)
	if !ok {
		return
	}
	var q struct {
		Where   []appdata.Cond  `json:"where"`
		OrderBy []appdata.Order `json:"orderBy"`
		Limit   int             `json:"limit"`
		Offset  int             `json:"offset"`
	}
	if r.Method == http.MethodPost && !decode(w, r, &q) {
		return
	}
	if r.Method == http.MethodGet {
		q.Limit, _ = strconv.Atoi(r.URL.Query().Get("limit"))
	}
	recs, err := d.Query(r.Context(), coll, appdata.Query{Where: q.Where, OrderBy: q.OrderBy, Limit: q.Limit, Offset: q.Offset})
	if err != nil {
		writeError(w, status(err), err)
		return
	}
	writeJSON(w, http.StatusOK, recs)
}

func (h *Host) addRecord(w http.ResponseWriter, r *http.Request) {
	d, coll, ok := h.appData(w, r)
	if !ok {
		return
	}
	var req struct {
		ID   string         `json:"id"`
		Data map[string]any `json:"data"`
	}
	if !decode(w, r, &req) {
		return
	}
	rec, err := d.Insert(r.Context(), coll, req.ID, req.Data, callerOf(r).actor)
	if err != nil {
		writeError(w, status(err), err)
		return
	}
	writeJSON(w, http.StatusCreated, rec)
}

func (h *Host) getRecord(w http.ResponseWriter, r *http.Request) {
	d, coll, ok := h.appData(w, r)
	if !ok {
		return
	}
	rec, err := d.Get(r.Context(), coll, r.PathValue("id"))
	if err != nil {
		writeError(w, status(err), err)
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

func (h *Host) updateRecord(w http.ResponseWriter, r *http.Request) {
	d, coll, ok := h.appData(w, r)
	if !ok {
		return
	}
	var req struct {
		Data    map[string]any `json:"data"`
		Version int            `json:"version"`
	}
	if !decode(w, r, &req) {
		return
	}
	rec, err := d.Update(r.Context(), coll, r.PathValue("id"), req.Data, req.Version, callerOf(r).actor)
	if err != nil {
		writeError(w, status(err), err)
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

func (h *Host) deleteRecord(w http.ResponseWriter, r *http.Request) {
	d, coll, ok := h.appData(w, r)
	if !ok {
		return
	}
	version, _ := strconv.Atoi(r.URL.Query().Get("version"))
	if err := d.Delete(r.Context(), coll, r.PathValue("id"), version, callerOf(r).actor); err != nil {
		writeError(w, status(err), err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Host) history(w http.ResponseWriter, r *http.Request) {
	d, _, ok := h.appData(w, r)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	changes, err := d.History(r.Context(), r.URL.Query().Get("coll"), r.URL.Query().Get("id"), limit)
	if err != nil {
		writeError(w, status(err), err)
		return
	}
	writeJSON(w, http.StatusOK, changes)
}

// serveAppFile serves an app's own files. The page runs sandboxed: it can't reach any
// API or see cookies, and talks to its data only through the phone page around it.
func (h *Host) serveAppFile(w http.ResponseWriter, r *http.Request) {
	id, tok := r.PathValue("app"), r.PathValue("token")
	if !checkAppToken(h.cfg.Secret, id, tok, h.now()) {
		http.Error(w, "This link has expired. Open the app again from your phone.", http.StatusForbidden)
		return
	}
	file := r.PathValue("file")
	if file == "" || strings.HasSuffix(file, "/") {
		file += "index.html"
	}
	name, ok := cleanName(file)
	if !ok {
		http.NotFound(w, r)
		return
	}
	p := filepath.Join(h.apps.path(id), "web", filepath.FromSlash(name))
	info, err := os.Stat(p)
	sdk := name == "shortapps.js" && err != nil && h.cfg.SDK != nil
	if !sdk && (err != nil || info.IsDir()) {
		http.NotFound(w, r)
		return
	}
	hd := w.Header()
	hd.Set("Content-Security-Policy", "sandbox allow-scripts allow-forms allow-popups allow-modals; "+
		"default-src 'self' 'unsafe-inline' data: blob:; connect-src 'none'; frame-ancestors 'self'")
	hd.Set("X-Content-Type-Options", "nosniff")
	hd.Set("Referrer-Policy", "no-referrer")
	hd.Set("Cache-Control", "private, max-age=300")
	if sdk {
		hd.Set("Content-Type", "text/javascript; charset=utf-8")
		w.Write(h.cfg.SDK)
		return
	}
	http.ServeFile(w, r, p)
}

// serveShell serves the phone page and its assets, whatever path the phone is opened
// at (/, /?app=calories): the page itself decides what to show.
func (h *Host) serveShell(w http.ResponseWriter, r *http.Request) {
	if h.cfg.Shell == nil {
		http.Error(w, "no phone page in this build", http.StatusNotFound)
		return
	}
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name == "" || !strings.Contains(path.Base(name), ".") {
		name = "phone/index.html"
	}
	if _, err := fs.Stat(h.cfg.Shell, name); err != nil {
		http.NotFound(w, r)
		return
	}
	http.ServeFileFS(w, r, h.cfg.Shell, name)
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.UseNumber()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid JSON: %w", err))
		return false
	}
	return true
}

func status(err error) int {
	switch {
	case errors.Is(err, appdata.ErrNotFound), errors.Is(err, os.ErrNotExist):
		return http.StatusNotFound
	case errors.Is(err, appdata.ErrConflict), errors.Is(err, appdata.ErrExists):
		return http.StatusConflict
	case errors.Is(err, appdata.ErrInvalid), errors.Is(err, errBadApp):
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}
