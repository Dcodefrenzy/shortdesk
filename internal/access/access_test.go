package access

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shortdesk/shortdesk/internal/agent"
	"github.com/shortdesk/shortdesk/internal/charter"
	"github.com/shortdesk/shortdesk/internal/desk"
	"github.com/shortdesk/shortdesk/internal/runtime"
	"github.com/shortdesk/shortdesk/internal/runtime/runtimetest"
)

type env struct {
	m      *Manager
	rt     *runtimetest.FakeWithPolicy
	runner *agent.Runner
	desks  *desk.Manager
	path   string
}

func newEnv(t *testing.T) env {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "a.yaml")
	os.WriteFile(path, []byte("charter: a\nversion: 1\npermissions:\n  network:\n    allow:\n      - claude.ai\n"), 0o600)
	rt := &runtimetest.FakeWithPolicy{Fake: runtimetest.Fake{Caps: runtime.Capabilities{EgressPolicy: true}}}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	desks := desk.NewManager(rt)
	runner := agent.NewRunner(rt, agent.NewStore(), agent.Config{}, log)
	return env{m: New(desks, runner, dir, log), rt: rt, runner: runner, desks: desks, path: path}
}

func (e env) up(t *testing.T, name string) desk.Desk {
	t.Helper()
	c, err := charter.Load(e.path)
	if err != nil {
		t.Fatal(err)
	}
	d, err := e.desks.Up(context.Background(), c, name)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func hosts(r Rules) string {
	var out []string
	for _, h := range r.Hosts {
		out = append(out, h.Host+"("+h.Source+")")
	}
	return strings.Join(out, ",")
}

func TestAddAndRemoveApplyLive(t *testing.T) {
	e := newEnv(t)
	d := e.up(t, "d1")
	ctx := context.Background()

	if err := e.m.Add(ctx, d, " Booking.com ", SourceSettings); err != nil {
		t.Fatal(err)
	}
	r, _ := e.m.Rules(d)
	if hosts(r) != "claude.ai(charter),booking.com(settings)" || !r.Editable {
		t.Errorf("rules = %s editable=%v", hosts(r), r.Editable)
	}
	if got := strings.Join(e.rt.Policy(d.ID), ","); got != "claude.ai,booking.com" {
		t.Errorf("applied = %s", got)
	}

	if err := e.m.Add(ctx, d, "https://x.com", SourceSettings); err == nil {
		t.Error("invalid host accepted")
	}
	if err := e.m.Remove(ctx, d, "claude.ai"); err == nil {
		t.Error("removed a charter rule from a desk")
	}
	if err := e.m.Remove(ctx, d, "booking.com"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(e.rt.Policy(d.ID), ","); got != "claude.ai" {
		t.Errorf("applied after remove = %s", got)
	}
}

func TestNotEditableWithoutLivePolicy(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.yaml"), []byte("charter: a\npermissions: {network: {allow: [claude.ai]}}"), 0o600)
	rt := &runtimetest.Fake{Caps: runtime.Capabilities{EgressPolicy: true}}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	desks := desk.NewManager(rt)
	m := New(desks, agent.NewRunner(rt, agent.NewStore(), agent.Config{}, log), dir, log)
	d := desk.Desk{ID: "x", Charter: "a"}
	if err := m.Add(context.Background(), d, "booking.com", SourceSettings); !errors.Is(err, ErrNotEditable) {
		t.Errorf("err = %v, want ErrNotEditable", err)
	}
}

// requestFromAgent runs a task whose output contains an access request and returns its ID.
func requestFromAgent(t *testing.T, e env, d desk.Desk) string {
	t.Helper()
	e.rt.ExecOutput = `{"type":"user","message":{"content":[{"type":"tool_result","content":"SHORTDESK_ACCESS_REQUEST {\"host\":\"booking.com\",\"reason\":\"hotels\"}"}]}}
{"type":"result","subtype":"success","result":"Waiting for booking.com"}
`
	if _, err := e.runner.Start(d.ID, "find hotels"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		for _, ev := range e.runner.Store().Events(d.ID, 0) {
			if ev.Kind == agent.KindAccessRequest && e.runner.Store().State(d.ID) == agent.StateIdle {
				return ev.Data["requestId"]
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("no access request recorded")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestDecideAllowOnDeskContinuesAgent(t *testing.T) {
	e := newEnv(t)
	d := e.up(t, "d1")
	id := requestFromAgent(t, e, d)
	if id == "" {
		t.Fatal("request has no id")
	}

	if err := e.m.Decide(context.Background(), d, id, AllowDesk); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(e.rt.Policy(d.ID), ","); got != "claude.ai,booking.com" {
		t.Errorf("applied = %s", got)
	}
	r, _ := e.m.Rules(d)
	if hosts(r) != "claude.ai(charter),booking.com(chat)" {
		t.Errorf("rules = %s", hosts(r))
	}
	if err := e.m.Decide(context.Background(), d, id, AllowDesk); !errors.Is(err, ErrUnknownRequest) {
		t.Errorf("second decision: %v, want ErrUnknownRequest", err)
	}

	// The agent was told and continued.
	deadline := time.Now().Add(2 * time.Second)
	for len(e.rt.Execs) < 2 {
		if time.Now().After(deadline) {
			t.Fatal("agent was not resumed")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if p := e.rt.Execs[1].Env["SHORTDESK_PROMPT"]; !strings.Contains(p, "allowed access to booking.com") {
		t.Errorf("resume prompt = %q", p)
	}
}

func TestDecideAddToCharterUpdatesFileAndAllDesks(t *testing.T) {
	e := newEnv(t)
	d1, d2 := e.up(t, "d1"), e.up(t, "d2")
	id := requestFromAgent(t, e, d1)

	if err := e.m.Decide(context.Background(), d1, id, AllowCharter); err != nil {
		t.Fatal(err)
	}
	c, err := charter.Load(e.path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(c.Permissions.Network.Allow, ",") != "claude.ai,booking.com" || c.Version != 2 {
		t.Errorf("charter allow=%v version=%d", c.Permissions.Network.Allow, c.Version)
	}
	for _, d := range []desk.Desk{d1, d2} {
		if got := strings.Join(e.rt.Policy(d.ID), ","); got != "claude.ai,booking.com" {
			t.Errorf("%s applied = %s", d.Name, got)
		}
	}
}

func TestDecideDenyChangesNothing(t *testing.T) {
	e := newEnv(t)
	d := e.up(t, "d1")
	id := requestFromAgent(t, e, d)
	if err := e.m.Decide(context.Background(), d, id, Deny); err != nil {
		t.Fatal(err)
	}
	if e.rt.Policy(d.ID) != nil {
		t.Errorf("policy changed on deny: %v", e.rt.Policy(d.ID))
	}
}

func TestModelHostsAreAllowedAndAppliedToRunningDesks(t *testing.T) {
	e := newEnv(t)
	d := e.up(t, "d1")
	ctx := context.Background()

	models := []string{"api.deepseek.com", "claude.ai"} // claude.ai is already in the charter
	e.desks.SetExtraAllowedHosts(func() []string { return models })
	r, _ := e.m.Rules(d)
	if hosts(r) != "claude.ai(charter),api.deepseek.com(models)" {
		t.Errorf("rules = %s", hosts(r))
	}
	if err := e.m.ApplyAll(ctx); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(e.rt.Policy(d.ID), ","); got != "claude.ai,api.deepseek.com" {
		t.Errorf("applied = %s", got)
	}
}

func TestAllowAllSwitch(t *testing.T) {
	e := newEnv(t)
	d := e.up(t, "d1")
	ctx := context.Background()
	if err := e.m.Add(ctx, d, "booking.com", SourceSettings); err != nil {
		t.Fatal(err)
	}

	if err := e.m.SetAllowAll(ctx, d, true); err != nil {
		t.Fatal(err)
	}
	r, _ := e.m.Rules(d)
	if !r.AllowAll || hosts(r) != "claude.ai(charter),booking.com(settings)" {
		t.Errorf("on: allowAll=%v rules=%s", r.AllowAll, hosts(r))
	}
	if got := strings.Join(e.rt.Policy(d.ID), ","); got != "*" {
		t.Errorf("on: applied = %s", got)
	}

	// Turning it off restores exactly the rules from before.
	if err := e.m.SetAllowAll(ctx, d, false); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(e.rt.Policy(d.ID), ","); got != "claude.ai,booking.com" {
		t.Errorf("off: applied = %s", got)
	}

	e.m.Forget(d.ID)
	if r, _ := e.m.Rules(d); r.AllowAll {
		t.Error("allow-all survived Forget")
	}
}

func TestPersistKeepsRulesAcrossRestarts(t *testing.T) {
	e := newEnv(t)
	d := e.up(t, "d1")
	ctx := context.Background()
	file := filepath.Join(t.TempDir(), "access.json")
	if err := e.m.Persist(file); err != nil {
		t.Fatal(err)
	}
	e.m.Add(ctx, d, "booking.com", SourceChat)
	e.m.SetAllowAll(ctx, d, true)

	// A new Manager (as after a restart) reading the same file.
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	again := New(e.desks, agent.NewRunner(e.rt, agent.NewStore(), agent.Config{}, log), filepath.Dir(e.path), log)
	if err := again.Persist(file); err != nil {
		t.Fatal(err)
	}
	r, _ := again.Rules(d)
	if !r.AllowAll || hosts(r) != "claude.ai(charter),booking.com(chat)" {
		t.Errorf("after restart: allowAll=%v rules=%s", r.AllowAll, hosts(r))
	}

	again.Forget(d.ID)
	third := New(e.desks, agent.NewRunner(e.rt, agent.NewStore(), agent.Config{}, log), filepath.Dir(e.path), log)
	third.Persist(file)
	if r, _ := third.Rules(d); r.AllowAll || hosts(r) != "claude.ai(charter)" {
		t.Errorf("after forget: allowAll=%v rules=%s", r.AllowAll, hosts(r))
	}
}
