package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shortdesk/shortdesk/internal/agent"
	"github.com/shortdesk/shortdesk/internal/phone"
	"github.com/shortdesk/shortdesk/internal/runtime"
	"github.com/shortdesk/shortdesk/shortapps"
)

// withPhone runs a real Phone Desk host for the test and points the main desk's
// phone port at it.
func withPhone(t *testing.T, env *testEnv) *httptest.Server {
	t.Helper()
	h, err := phone.New(phone.Config{Dir: t.TempDir(), Secret: env.server.phone.key(), Builtin: shortapps.Builtin(),
		SDK: shortapps.SDK, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.Close() })
	srv := httptest.NewServer(h.Handler())
	t.Cleanup(srv.Close)
	screen := env.rt.EndpointFor
	host := strings.TrimPrefix(srv.URL, "http://")
	env.rt.EndpointFor = func(id string, port int) runtime.Endpoint {
		if port == phonePort {
			return runtime.Endpoint{Host: host}
		}
		return screen(id, port)
	}
	env.server.phone.setState("running")
	return srv
}

func TestHelperDesksDoJobsAndReportBack(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	main := decode[deskJSON](t, env.do(t, "POST", "/api/desks", `{"charter":"assistant","name":"main"}`, true))
	withPhone(t, env)
	s := env.server

	// Helpers answer with a summary; the main desk would say something else.
	env.rt.ExecFunc = func(r runtime.ExecRequest) string {
		if strings.Contains(r.Env["SHORTDESK_PROMPT"], "You are a helper desk") {
			if r.Env["SHORTDESK_TASK_TITLE"] != "Apply to flats" {
				t.Errorf("helper env = %v", r.Env)
			}
			return `{"type":"result","subtype":"success","result":"Applied to 5 flats in Wedding."}` + "\n"
		}
		return `{"type":"result","subtype":"success","result":"ok"}` + "\n"
	}

	// The main desk's agent hands the job over (what apps helper does).
	var queued job
	if err := s.phoneCall(ctx, http.MethodPost, "/api/apps/tasks/data/tasks", map[string]any{"data": map[string]any{
		"title": "Apply to flats", "status": "queued", "runOn": "helper", "instructions": "Apply to 5 flats in Wedding under €1,200.",
	}}, &queued); err != nil {
		t.Fatal(err)
	}

	s.helpers.dispatch(ctx)

	// The job runs on a new helper desk and is reported back.
	var done job
	deadline := time.Now().Add(5 * time.Second)
	for {
		if err := s.phoneCall(ctx, http.MethodGet, "/api/apps/tasks/data/tasks/"+queued.ID, nil, &done); err != nil {
			t.Fatal(err)
		}
		if done.str("status") == "done" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("job = %+v", done.Data)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if done.str("desk") != "helper-1" || done.str("result") != "Applied to 5 flats in Wedding." {
		t.Errorf("job = %+v", done.Data)
	}

	var notes []job
	s.phoneCall(ctx, http.MethodPost, "/api/apps/scratchbook/data/notes/query", map[string]any{"orderBy": []map[string]any{{"field": "_created"}}}, &notes)
	var texts []string
	for _, n := range notes {
		texts = append(texts, n.str("from")+": "+n.str("text"))
	}
	if strings.Join(texts, " | ") != "main desk: Started on helper-1. | helper-1: Done: Applied to 5 flats in Wedding." {
		t.Errorf("scratchbook = %q", texts)
	}

	var news []string
	for _, ev := range s.agent.Store().Events(main.ID, 0) {
		if ev.Kind == agent.KindHelper {
			news = append(news, ev.Data["state"]+": "+ev.Text)
		}
	}
	if strings.Join(news, " | ") != "started: helper-1 started: Apply to flats | done: Applied to 5 flats in Wedding." {
		t.Errorf("main chat = %q", news)
	}

	// The helper is reused for the next job, then removed once idle long enough.
	desks := decode[[]deskJSON](t, env.do(t, "GET", "/api/desks", "", true))
	if len(desks) != 2 {
		t.Fatalf("desks = %d", len(desks))
	}
	s.helpers.mu.Lock()
	for _, d := range s.helpers.desks {
		d.idleSince = time.Now().Add(-helperIdleTTL - time.Minute)
	}
	s.helpers.mu.Unlock()
	s.helpers.reap(ctx)
	if desks := decode[[]deskJSON](t, env.do(t, "GET", "/api/desks", "", true)); len(desks) != 1 || desks[0].ID != main.ID {
		t.Errorf("after reaping, desks = %+v", desks)
	}
}

func TestMainDeskIsToldToUseHelpers(t *testing.T) {
	env := newTestEnv(t)
	main := decode[deskJSON](t, env.do(t, "POST", "/api/desks", `{"charter":"assistant","name":"main"}`, true))
	other := decode[deskJSON](t, env.do(t, "POST", "/api/desks", `{"charter":"assistant","name":"other"}`, true))
	got := env.server.phone.agentEnv(context.Background(), main.ID)
	if !strings.Contains(got[agent.ExtraPromptEnv], "apps helper") || got["SHORTDESK_DESK_NAME"] != "main" {
		t.Errorf("main env = %v", got)
	}
	if v, ok := env.server.phone.agentEnv(context.Background(), other.ID)[agent.ExtraPromptEnv]; ok {
		t.Errorf("non-main desk got main instructions: %q", v)
	}
}
