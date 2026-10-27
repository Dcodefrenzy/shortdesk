package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shortdesk/shortdesk/internal/agent"
	"github.com/shortdesk/shortdesk/internal/desk"
	"github.com/shortdesk/shortdesk/internal/logins"
	"github.com/shortdesk/shortdesk/internal/runtime"
	"github.com/shortdesk/shortdesk/internal/runtime/runtimetest"
	"github.com/shortdesk/shortdesk/internal/vault"
	"github.com/shortdesk/shortdesk/internal/workspace"
)

const token = "test-token"

type testEnv struct {
	server *Server
	srv    *httptest.Server
	rt     *runtimetest.FakeWithPolicy
	screen *httptest.Server // stands in for every desk's screen
	seen   chan *http.Request
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	env := &testEnv{seen: make(chan *http.Request, 16)}
	env.screen = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		env.seen <- r
		io.WriteString(w, "screen:"+r.URL.Path)
	}))
	t.Cleanup(env.screen.Close)
	screenHost := strings.TrimPrefix(env.screen.URL, "http://")
	env.rt = &runtimetest.FakeWithPolicy{Fake: runtimetest.Fake{
		Caps: runtime.Capabilities{EgressPolicy: true},
		EndpointFor: func(string, int) runtime.Endpoint {
			return runtime.Endpoint{Host: screenHost, PathPrefix: "/proxy/6080"}
		},
	}}

	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "assistant.yaml"), []byte(
		"charter: assistant\npermissions: {network: {allow: ['claude.ai']}}"), 0o600)

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	ws, err := workspace.Open(filepath.Join(dir, "workspace.json"))
	if err != nil {
		t.Fatal(err)
	}
	runner := agent.NewRunner(env.rt, agent.NewStore(), agent.Config{Providers: ws.Providers}, log)
	lg, err := logins.New(env.rt, filepath.Join(dir, "logins"), log)
	if err != nil {
		t.Fatal(err)
	}
	s := New(Config{Token: token, ChartersDir: dir, ReadyTimeout: 5 * time.Second},
		desk.NewManager(env.rt), runner, ws, lg, vault.New(filepath.Join(dir, "vault"), ws), log)
	env.server = s
	env.srv = httptest.NewServer(s.Handler())
	t.Cleanup(env.srv.Close)
	return env
}

func (e *testEnv) do(t *testing.T, method, path, body string, auth bool) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(method, e.srv.URL+path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if auth {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func decode[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	var v T
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestHealthIsPublic(t *testing.T) {
	env := newTestEnv(t)
	if resp := env.do(t, "GET", "/api/health", "", false); resp.StatusCode != 200 {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

func TestEverythingElseNeedsLogin(t *testing.T) {
	env := newTestEnv(t)
	for _, path := range []string{"/api/desks", "/api/charters", "/api/session", "/desks/box-1/view/"} {
		if resp := env.do(t, "GET", path, "", false); resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("GET %s without login = %d, want 401", path, resp.StatusCode)
		}
	}
}

func TestLoginCookie(t *testing.T) {
	env := newTestEnv(t)
	if resp := env.do(t, "POST", "/api/login", `{"token":"wrong"}`, false); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong token = %d, want 401", resp.StatusCode)
	}
	resp := env.do(t, "POST", "/api/login", `{"token":"`+token+`"}`, false)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("login = %d", resp.StatusCode)
	}
	var cookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookie {
			cookie = c
		}
	}
	if cookie == nil || !cookie.HttpOnly || strings.Contains(cookie.Value, token) {
		t.Fatalf("bad session cookie: %+v", cookie)
	}
	req, _ := http.NewRequest("GET", env.srv.URL+"/api/session", nil)
	req.AddCookie(cookie)
	r2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	r2.Body.Close()
	if r2.StatusCode != 200 {
		t.Errorf("session with cookie = %d", r2.StatusCode)
	}
}

func TestPostRequiresJSON(t *testing.T) {
	env := newTestEnv(t)
	req, _ := http.NewRequest("POST", env.srv.URL+"/api/desks", strings.NewReader("charter=assistant"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("form post = %d, want 415", resp.StatusCode)
	}
}

func TestCharters(t *testing.T) {
	env := newTestEnv(t)
	got := decode[[]charterJSON](t, env.do(t, "GET", "/api/charters", "", true))
	if len(got) != 1 || got[0].Name != "assistant" || got[0].AllowedHosts[0] != "claude.ai" {
		t.Errorf("charters = %+v", got)
	}
}

func TestCreateListDelete(t *testing.T) {
	env := newTestEnv(t)

	if resp := env.do(t, "POST", "/api/desks", `{"charter":"../../etc/passwd"}`, true); resp.StatusCode != 400 {
		t.Errorf("unknown charter = %d, want 400", resp.StatusCode)
	}

	resp := env.do(t, "POST", "/api/desks", `{"charter":"assistant","name":"ayo"}`, true)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create = %d", resp.StatusCode)
	}
	created := decode[deskJSON](t, resp)
	if created.Name != "ayo" || created.Charter != "assistant" {
		t.Errorf("created = %+v", created)
	}
	u, err := url.Parse(created.ViewerURL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.Path != "/desks/"+created.ID+"/view/" || q.Get("path") != "desks/"+created.ID+"/view/websockify" {
		t.Errorf("viewerUrl = %q", created.ViewerURL)
	}
	if q.Get("clipboard_seamless") != "true" || q.Get("clipboard_up") != "true" || q.Get("clipboard_down") != "true" {
		t.Errorf("clipboard sync not enabled for the embedded viewer: %q", created.ViewerURL)
	}
	if got := env.rt.Created[0].AllowedHosts; len(got) != 1 || got[0] != "claude.ai" {
		t.Errorf("allowlist not passed to runtime: %v", got)
	}

	if resp := env.do(t, "POST", "/api/desks", `{"charter":"assistant","name":"ayo"}`, true); resp.StatusCode != http.StatusConflict {
		t.Errorf("duplicate = %d, want 409", resp.StatusCode)
	}

	list := decode[[]deskJSON](t, env.do(t, "GET", "/api/desks", "", true))
	if len(list) != 1 || list[0].ID != created.ID {
		t.Errorf("list = %+v", list)
	}

	if resp := env.do(t, "DELETE", "/api/desks/"+created.ID, "", true); resp.StatusCode != http.StatusNoContent {
		t.Errorf("delete = %d", resp.StatusCode)
	}
	if resp := env.do(t, "GET", "/api/desks/"+created.ID, "", true); resp.StatusCode != 404 {
		t.Errorf("get after delete = %d, want 404", resp.StatusCode)
	}
}

func TestViewerProxyForwardsAndStripsCredentials(t *testing.T) {
	env := newTestEnv(t)
	created := decode[deskJSON](t, env.do(t, "POST", "/api/desks", `{"charter":"assistant"}`, true))
	drain(env.seen) // readiness checks during create

	req, _ := http.NewRequest("GET", env.srv.URL+"/desks/"+created.ID+"/view/assets/app.js", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: "secret"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if string(body) != "screen:/proxy/6080/assets/app.js" {
		t.Errorf("proxied to %q", body)
	}
	got := <-env.seen
	if got.Header.Get("Authorization") != "" || got.Header.Get("Cookie") != "" {
		t.Errorf("credentials leaked to desk: auth=%q cookie=%q", got.Header.Get("Authorization"), got.Header.Get("Cookie"))
	}
}

func TestViewerProxyOnlyReachesManagedDesks(t *testing.T) {
	env := newTestEnv(t)
	env.rt.Boxes = append(env.rt.Boxes, runtime.Box{ID: "foreign", Labels: map[string]string{"other": "x"}})
	if resp := env.do(t, "GET", "/desks/foreign/view/", "", true); resp.StatusCode != 404 {
		t.Errorf("foreign box = %d, want 404", resp.StatusCode)
	}
}

func drain(c chan *http.Request) {
	for {
		select {
		case <-c:
		default:
			return
		}
	}
}

func TestTaskFlowAndEventStream(t *testing.T) {
	env := newTestEnv(t)
	env.rt.ExecOutput = `{"type":"assistant","message":{"content":[{"type":"text","text":"On it"}]}}
{"type":"result","subtype":"success","result":"All done"}
`
	created := decode[deskJSON](t, env.do(t, "POST", "/api/desks", `{"charter":"assistant"}`, true))
	if created.Agent != agent.StateIdle {
		t.Errorf("new desk agent = %q, want idle", created.Agent)
	}

	if resp := env.do(t, "POST", "/api/desks/"+created.ID+"/tasks", `{"prompt":"find flights"}`, true); resp.StatusCode != http.StatusAccepted {
		t.Fatalf("start task = %d", resp.StatusCode)
	}
	if resp := env.do(t, "POST", "/api/desks/"+created.ID+"/agent/dance", "", true); resp.StatusCode != 404 {
		t.Errorf("unknown action = %d, want 404", resp.StatusCode)
	}

	req, _ := http.NewRequest("GET", env.srv.URL+"/api/desks/"+created.ID+"/events", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type = %q", ct)
	}

	var seen []string
	buf := make([]byte, 0, 4096)
	chunk := make([]byte, 1024)
	deadline := time.Now().Add(3 * time.Second)
	for !strings.Contains(string(buf), `"text":"done"`) {
		if time.Now().After(deadline) {
			t.Fatalf("stream never reached done; got:\n%s", buf)
		}
		n, err := resp.Body.Read(chunk)
		buf = append(buf, chunk[:n]...)
		if err != nil {
			break
		}
	}
	for _, line := range strings.Split(string(buf), "\n") {
		if data, ok := strings.CutPrefix(line, "data: "); ok {
			var ev agent.Event
			json.Unmarshal([]byte(data), &ev)
			seen = append(seen, string(ev.Kind)+":"+ev.Text)
		}
	}
	want := "prompt:find flights,status:running,text:On it,result:All done,status:done"
	if got := strings.Join(seen, ","); got != want {
		t.Errorf("stream = %s\nwant     %s", got, want)
	}
}

func TestSettingsAndNetworkRules(t *testing.T) {
	env := newTestEnv(t)
	d := decode[deskJSON](t, env.do(t, "POST", "/api/desks", `{"charter":"assistant"}`, true))

	if resp := env.do(t, "POST", "/api/desks/"+d.ID+"/network", `{"host":"booking.com"}`, true); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("add rule = %d", resp.StatusCode)
	}
	if resp := env.do(t, "POST", "/api/desks/"+d.ID+"/network", `{"host":"not a host"}`, true); resp.StatusCode != 400 {
		t.Errorf("bad host = %d, want 400", resp.StatusCode)
	}
	st := decode[settingsJSON](t, env.do(t, "GET", "/api/desks/"+d.ID+"/settings", "", true))
	if st.Charter == nil || st.Charter.Name != "assistant" || !st.Network.Editable || len(st.Apps) == 0 {
		t.Errorf("settings = %+v", st)
	}
	if len(st.Network.Hosts) != 2 || st.Network.Hosts[1].Host != "booking.com" || st.Network.Hosts[1].Source != "settings" {
		t.Errorf("hosts = %+v", st.Network.Hosts)
	}
	if got := strings.Join(env.rt.Policy(d.ID), ","); got != "claude.ai,booking.com" {
		t.Errorf("applied = %s", got)
	}
	if resp := env.do(t, "DELETE", "/api/desks/"+d.ID+"/network/booking.com", "", true); resp.StatusCode != http.StatusNoContent {
		t.Errorf("remove rule = %d", resp.StatusCode)
	}
	if resp := env.do(t, "POST", "/api/desks/"+d.ID+"/access/nope", `{"decision":"desk"}`, true); resp.StatusCode != 404 {
		t.Errorf("unknown request = %d, want 404", resp.StatusCode)
	}
}

func TestWorkspaceConnectionsAreWriteOnly(t *testing.T) {
	env := newTestEnv(t)
	if resp := env.do(t, "PUT", "/api/workspace/connections/anthropic_api_key", `{"value":"sk-ant-secret-9876"}`, true); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("set = %d", resp.StatusCode)
	}
	resp := env.do(t, "GET", "/api/workspace", "", true)
	body, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(body), "sk-ant-secret") {
		t.Fatalf("secret returned by the API: %s", body)
	}
	if strings.Contains(string(body), "openrouter") {
		t.Errorf("OpenRouter still offered: %s", body)
	}
	if resp := env.do(t, "PUT", "/api/workspace/connections/openrouter_api_key", `{"value":"x"}`, true); resp.StatusCode != 400 {
		t.Errorf("openrouter connection accepted: %d", resp.StatusCode)
	}
}

func TestLoginEndpointsValidate(t *testing.T) {
	env := newTestEnv(t)
	d := decode[deskJSON](t, env.do(t, "POST", "/api/desks", `{"charter":"assistant"}`, true))
	if resp := env.do(t, "POST", "/api/desks/"+d.ID+"/logins/site/..%2Fetc/allow", "", true); resp.StatusCode == http.StatusNoContent {
		t.Error("invalid login name accepted")
	}
	if resp := env.do(t, "POST", "/api/desks/"+d.ID+"/logins/app/claude-code/dance", "", true); resp.StatusCode != 404 {
		t.Errorf("unknown action = %d", resp.StatusCode)
	}
	if resp := env.do(t, "GET", "/api/logins", "", true); resp.StatusCode != 200 {
		t.Errorf("list logins = %d", resp.StatusCode)
	}
}

func TestIntegrationsNeedAConnectedVault(t *testing.T) {
	env := newTestEnv(t)
	if resp := env.do(t, "GET", "/api/integrations", "", true); resp.StatusCode != http.StatusConflict {
		t.Errorf("list without vault = %d, want 409", resp.StatusCode)
	}
	if resp := env.do(t, "POST", "/api/vault/connect", `{"email":"","password":""}`, true); resp.StatusCode != 400 {
		t.Errorf("empty connect = %d, want 400", resp.StatusCode)
	}
}

func TestCleanOutputPath(t *testing.T) {
	for in, want := range map[string]string{"flights.csv": "flights.csv", "a/b.md": "a/b.md", "./x.png": "x.png"} {
		if got, err := cleanOutputPath(in); err != nil || got != want {
			t.Errorf("cleanOutputPath(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "../etc/passwd", "a/../../x", ".ssh/id_rsa", "a/.hidden", "."} {
		if _, err := cleanOutputPath(bad); err == nil {
			t.Errorf("cleanOutputPath(%q) accepted", bad)
		}
	}
}

func TestOutputsAreServedSandboxed(t *testing.T) {
	env := newTestEnv(t)
	d := decode[deskJSON](t, env.do(t, "POST", "/api/desks", `{"charter":"assistant"}`, true))
	env.rt.Files.Upload(context.Background(), d.ID, outputsDir+"/page.html", strings.NewReader("<script>alert(1)</script>"))
	env.rt.Files.Upload(context.Background(), d.ID, outputsDir+"/flights.csv", strings.NewReader("a,b\n1,2\n"))

	resp := env.do(t, "GET", "/api/desks/"+d.ID+"/outputs/page.html", "", true)
	if csp := resp.Header.Get("Content-Security-Policy"); !strings.HasPrefix(csp, "sandbox") {
		t.Errorf("CSP = %q, want sandbox", csp)
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment") {
		t.Errorf("html served %q, want attachment", cd)
	}
	resp = env.do(t, "GET", "/api/desks/"+d.ID+"/outputs/flights.csv", "", true)
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Errorf("csv type = %q", ct)
	}
	if resp := env.do(t, "GET", "/api/desks/"+d.ID+"/outputs/..%2Fsecret", "", true); resp.StatusCode == 200 {
		t.Error("path outside outputs served")
	}
}

func TestLoginLinkWorksOnce(t *testing.T) {
	env := newTestEnv(t)
	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	visit := func(path string) *http.Response {
		t.Helper()
		resp, err := noRedirect.Get(env.srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp
	}

	link := env.server.LoginLink()
	resp := visit(link)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/" || len(resp.Cookies()) != 1 {
		t.Fatalf("first use: %d %s cookies=%d", resp.StatusCode, resp.Header.Get("Location"), len(resp.Cookies()))
	}
	session := resp.Cookies()[0]
	req, _ := http.NewRequest("GET", env.srv.URL+"/api/session", nil)
	req.AddCookie(session)
	if r, _ := http.DefaultClient.Do(req); r.StatusCode != http.StatusOK {
		t.Errorf("session from link not accepted: %d", r.StatusCode)
	}

	for _, path := range []string{link, "/login/link?code=nope", "/login/link"} {
		if resp := visit(path); resp.Header.Get("Location") != "/login/?link=expired" || len(resp.Cookies()) != 0 {
			t.Errorf("%s: %s cookies=%d", path, resp.Header.Get("Location"), len(resp.Cookies()))
		}
	}

	var c loginCodes
	code := c.issue()
	c.codes[code] = time.Now().Add(-time.Second)
	if c.redeem(code) {
		t.Error("expired code accepted")
	}
}

// A fresh install has no saved logins; the dashboard needs empty lists, not null.
func TestSettingsListsAreNeverNull(t *testing.T) {
	env := newTestEnv(t)
	d := decode[deskJSON](t, env.do(t, "POST", "/api/desks", `{"charter":"assistant"}`, true))
	st := decode[map[string]json.RawMessage](t, env.do(t, "GET", "/api/desks/"+d.ID+"/settings", "", true))
	var network map[string]json.RawMessage
	json.Unmarshal(st["network"], &network)
	for name, raw := range map[string]json.RawMessage{
		"apps": st["apps"], "shared": st["shared"], "logins": st["logins"],
		"integrations": st["integrations"], "network.hosts": network["hosts"],
	} {
		if !strings.HasPrefix(string(raw), "[") {
			t.Errorf("%s = %s, want a list", name, raw)
		}
	}
}

func TestMainDesk(t *testing.T) {
	env := newTestEnv(t)
	main := func() string {
		return decode[workspaceJSON](t, env.do(t, "GET", "/api/workspace", "", true)).MainDesk
	}
	if got := main(); got != "" {
		t.Fatalf("main desk with no desks = %q", got)
	}
	a := decode[deskJSON](t, env.do(t, "POST", "/api/desks", `{"charter":"assistant","name":"first"}`, true))
	time.Sleep(10 * time.Millisecond)
	b := decode[deskJSON](t, env.do(t, "POST", "/api/desks", `{"charter":"assistant","name":"second"}`, true))
	if got := main(); got != a.ID {
		t.Errorf("default main desk = %q, want the oldest %q", got, a.ID)
	}
	if resp := env.do(t, "PUT", "/api/workspace/main-desk", `{"id":"`+b.ID+`"}`, true); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("pick main desk = %d", resp.StatusCode)
	}
	if got := main(); got != b.ID {
		t.Errorf("picked main desk = %q, want %q", got, b.ID)
	}
	if resp := env.do(t, "PUT", "/api/workspace/main-desk", `{"id":"nope"}`, true); resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown desk = %d, want 404", resp.StatusCode)
	}
	env.do(t, "DELETE", "/api/desks/"+b.ID, "", true)
	if got := main(); got != a.ID {
		t.Errorf("after removing the main desk = %q, want %q", got, a.ID)
	}
}
