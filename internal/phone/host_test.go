package phone

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/shortdesk/shortdesk/shortapps"
)

var secret = []byte("test-secret-0123456789abcdef")

type env struct {
	t    *testing.T
	host *Host
	srv  *httptest.Server
}

func newEnv(t *testing.T) *env {
	t.Helper()
	shell := fstest.MapFS{"phone/index.html": {Data: []byte("<p>phone</p>")}, "_next/app.js": {Data: []byte("js")}}
	h, err := New(Config{Dir: t.TempDir(), Secret: secret, Builtin: shortapps.Builtin(), Shell: shell, SDK: shortapps.SDK,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.Close() })
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)
	return &env{t: t, host: h, srv: srv}
}

func (e *env) do(method, path, key, body string) (*http.Response, string) {
	e.t.Helper()
	req, _ := http.NewRequest(method, e.srv.URL+path, strings.NewReader(body))
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, string(b)
}

func pkg(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg})
		tw.Write([]byte(body))
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func TestBuiltinAppsAreInstalled(t *testing.T) {
	e := newEnv(t)
	_, body := e.do("GET", "/api/apps", OwnerKey(secret), "")
	var apps []App
	json.Unmarshal([]byte(body), &apps)
	names := map[string]bool{}
	for _, a := range apps {
		names[a.ID] = a.Builtin
	}
	if !names["messages"] || !names["tasks"] {
		t.Fatalf("apps = %s", body)
	}
}

func TestKeysAreRequired(t *testing.T) {
	e := newEnv(t)
	now := time.Now()
	for name, key := range map[string]string{
		"none":          "",
		"made up":       "owner.0000",
		"other secret":  OwnerKey([]byte("another-secret-0123456789")),
		"expired agent": AgentKey(secret, "one", -time.Minute, now),
		"forged desk":   strings.Replace(AgentKey(secret, "one", time.Hour, now), "agent.one.", "agent.two.", 1),
	} {
		if resp, _ := e.do("GET", "/api/apps", key, ""); resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s: status %d, want 401", name, resp.StatusCode)
		}
	}
}

func TestAgentAndOwnerShareAppData(t *testing.T) {
	e := newEnv(t)
	agent := AgentKey(secret, "one", time.Hour, time.Now())
	owner := OwnerKey(secret)

	resp, body := e.do("POST", "/api/apps/tasks/data/tasks", agent, `{"data":{"title":"Apply to flats","status":"doing"}}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("agent add = %d %s", resp.StatusCode, body)
	}
	var rec struct {
		ID        string         `json:"id"`
		Version   int            `json:"version"`
		CreatedBy string         `json:"createdBy"`
		Data      map[string]any `json:"data"`
	}
	json.Unmarshal([]byte(body), &rec)
	if rec.CreatedBy != "agent on one" {
		t.Errorf("created by %q", rec.CreatedBy)
	}

	// The person moves it on; the agent's stale write is refused.
	if resp, body := e.do("PATCH", "/api/apps/tasks/data/tasks/"+rec.ID, owner, `{"data":{"status":"done"},"version":1}`); resp.StatusCode != 200 {
		t.Fatalf("owner update = %d %s", resp.StatusCode, body)
	}
	if resp, _ := e.do("PATCH", "/api/apps/tasks/data/tasks/"+rec.ID, agent, `{"data":{"status":"todo"},"version":1}`); resp.StatusCode != http.StatusConflict {
		t.Errorf("stale agent update = %d, want 409", resp.StatusCode)
	}

	_, body = e.do("POST", "/api/apps/tasks/data/tasks/query", agent, `{"where":[{"field":"status","op":"=","value":"done"}]}`)
	if !strings.Contains(body, "Apply to flats") {
		t.Errorf("query = %s", body)
	}
	if resp, _ := e.do("POST", "/api/apps/tasks/data/secrets", agent, `{"data":{}}`); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("undeclared collection = %d, want 400", resp.StatusCode)
	}
	if resp, _ := e.do("GET", "/api/apps/nope/data/x", agent, ""); resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown app = %d, want 404", resp.StatusCode)
	}
	_, body = e.do("GET", "/api/apps/tasks/history?coll=tasks&id="+rec.ID, owner, "")
	if !strings.Contains(body, `"actor":"you"`) || !strings.Contains(body, `"actor":"agent on one"`) {
		t.Errorf("history = %s", body)
	}
	// Agents can't delete apps.
	if resp, _ := e.do("DELETE", "/api/apps/tasks", agent, ""); resp.StatusCode != http.StatusForbidden {
		t.Errorf("agent delete app = %d, want 403", resp.StatusCode)
	}
}

func TestInstallUpdateUndo(t *testing.T) {
	e := newEnv(t)
	agent := AgentKey(secret, "one", time.Hour, time.Now())
	app := func(version, page string) []byte {
		return pkg(t, map[string]string{
			"app.json":       `{"id":"calories","name":"Calories","icon":"🍽️","color":"#ffd66b","description":"Meals and calories.","version":"` + version + `","collections":["meals"]}`,
			"web/index.html": page,
			"web/app.js":     "console.log(1)",
		})
	}
	if resp, body := e.do("POST", "/api/apps", agent, string(app("1.0.0", "v1"))); resp.StatusCode != 200 {
		t.Fatalf("install = %d %s", resp.StatusCode, body)
	}
	e.do("POST", "/api/apps/calories/data/meals", agent, `{"data":{"food":"carbonara","kcal":650}}`)
	if resp, body := e.do("POST", "/api/apps", agent, string(app("1.1.0", "v2"))); resp.StatusCode != 200 {
		t.Fatalf("update = %d %s", resp.StatusCode, body)
	}

	page := func() string {
		_, body := e.do("POST", "/api/apps/calories/open", OwnerKey(secret), "")
		var o struct{ Path string }
		json.Unmarshal([]byte(body), &o)
		_, page := e.do("GET", "/"+o.Path, "", "")
		return page
	}
	if got := page(); got != "v2" {
		t.Errorf("page after update = %q", got)
	}
	// Data survives an update.
	if _, body := e.do("GET", "/api/apps/calories/data/meals", agent, ""); !strings.Contains(body, "carbonara") {
		t.Errorf("meals after update = %s", body)
	}
	if resp, _ := e.do("POST", "/api/apps/calories/undo", agent, ""); resp.StatusCode != http.StatusNoContent {
		t.Errorf("undo = %d", resp.StatusCode)
	}
	if got := page(); got != "v1" {
		t.Errorf("page after undo = %q", got)
	}

	for name, files := range map[string]map[string]string{
		"no app.json":   {"web/index.html": "x"},
		"no page":       {"app.json": `{"id":"x","name":"X","icon":"x","version":"1"}`},
		"bad id":        {"app.json": `{"id":"../x","name":"X","icon":"x","version":"1"}`, "web/index.html": "x"},
		"escaping path": {"app.json": `{"id":"x","name":"X","icon":"x","version":"1"}`, "web/index.html": "x", "../../evil": "x"},
	} {
		if resp, _ := e.do("POST", "/api/apps", agent, string(pkg(t, files))); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", name, resp.StatusCode)
		}
	}
}

func TestAppPagesAreWalledOff(t *testing.T) {
	e := newEnv(t)
	_, body := e.do("POST", "/api/apps/tasks/open", OwnerKey(secret), "")
	var o struct{ Path string }
	json.Unmarshal([]byte(body), &o)
	if !strings.HasPrefix(o.Path, "apps/tasks/") {
		t.Fatalf("open = %s", body)
	}
	resp, page := e.do("GET", "/"+o.Path, "", "")
	if resp.StatusCode != 200 || !strings.Contains(page, "Tasks") {
		t.Fatalf("page = %d", resp.StatusCode)
	}
	csp := resp.Header.Get("Content-Security-Policy")
	if !strings.Contains(csp, "sandbox") || !strings.Contains(csp, "connect-src 'none'") {
		t.Errorf("CSP = %q", csp)
	}
	if resp, js := e.do("GET", "/"+o.Path+"shortapps.js", "", ""); resp.StatusCode != 200 || !strings.Contains(js, "window.shortapps") {
		t.Errorf("sdk = %d", resp.StatusCode)
	}
	// Another app's token doesn't open this one, and forged ones fail.
	tok := strings.Split(o.Path, "/")[2]
	if resp, _ := e.do("GET", "/apps/messages/"+tok+"/", "", ""); resp.StatusCode != http.StatusForbidden {
		t.Errorf("token reused for another app = %d", resp.StatusCode)
	}
	if resp, _ := e.do("GET", "/apps/tasks/tasks.9999999999.forged/", "", ""); resp.StatusCode != http.StatusForbidden {
		t.Errorf("forged token = %d", resp.StatusCode)
	}
	if resp, _ := e.do("GET", "/"+o.Path+"../../../app.json", "", ""); resp.StatusCode == 200 {
		t.Errorf("escaped the web folder")
	}
}

func TestShellIsServed(t *testing.T) {
	e := newEnv(t)
	for _, p := range []string{"/", "/?app=tasks", "/a/calories"} {
		if _, body := e.do("GET", p, "", ""); body != "<p>phone</p>" {
			t.Errorf("%s = %q", p, body)
		}
	}
	if _, body := e.do("GET", "/_next/app.js", "", ""); body != "js" {
		t.Errorf("asset = %q", body)
	}
}
