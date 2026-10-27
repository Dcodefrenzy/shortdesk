package agent

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/shortdesk/shortdesk/internal/runtime"
	"github.com/shortdesk/shortdesk/internal/runtime/runtimetest"
)

func TestParseClaudeLine(t *testing.T) {
	tests := []struct {
		name string
		line string
		want []Event
	}{
		{"blank", "   ", nil},
		{"plain text becomes log", "You've hit your weekly limit", []Event{{Kind: KindLog, Text: "You've hit your weekly limit"}}},
		{"init", `{"type":"system","subtype":"init","model":"claude-opus-5-5"}`,
			[]Event{{Kind: KindLog, Text: "Agent started"}}},
		{"text and tool", `{"type":"assistant","message":{"content":[
			{"type":"text","text":"Checking flights"},
			{"type":"tool_use","name":"browser_navigate","input":{"url":"https://www.google.com/travel/flights"}}]}}`,
			[]Event{
				{Kind: KindText, Text: "Checking flights"},
				{Kind: KindTool, Tool: "browser_navigate", Text: "https://www.google.com/travel/flights"},
			}},
		{"tool result blocks", `{"type":"user","message":{"content":[
			{"type":"tool_result","is_error":true,"content":[{"type":"text","text":"timeout"},{"type":"image"}]}]}}`,
			[]Event{{Kind: KindToolResult, Text: "timeout\n[image]", IsError: true}}},
		{"result", `{"type":"result","subtype":"success","is_error":false,"result":"BA £612 is cheapest","total_cost_usd":0.42}`,
			[]Event{{Kind: KindResult, Text: "BA £612 is cheapest", CostUSD: 0.42}}},
		{"unknown type ignored", `{"type":"stream_event"}`, nil},
		{"access request in tool output", `{"type":"user","message":{"content":[{"type":"tool_result","content":"SHORTDESK_ACCESS_REQUEST {\"host\":\"Booking.com\",\"reason\":\"hotels\"}\nRequest sent."}]}}`,
			[]Event{
				{Kind: KindToolResult, Text: "SHORTDESK_ACCESS_REQUEST {\"host\":\"Booking.com\",\"reason\":\"hotels\"}\nRequest sent."},
				{Kind: KindAccessRequest, Text: "Access requested: booking.com", Data: map[string]string{"host": "booking.com", "reason": "hotels"}},
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseClaudeLine(tt.line)
			if len(got) != len(tt.want) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
			for i := range got {
				if !reflect.DeepEqual(got[i], tt.want[i]) {
					t.Errorf("event %d = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestSummarizeInputFallsBackToSortedFields(t *testing.T) {
	if got := summarizeInput([]byte(`{"b":2,"a":1}`)); got != "a=1 b=2" {
		t.Errorf("got %q", got)
	}
}

func TestStoreOneActiveTaskPerDesk(t *testing.T) {
	s := NewStore()
	t1, first, ok := s.Begin("d", "one")
	if !ok || !first {
		t.Fatalf("first Begin: ok=%v first=%v", ok, first)
	}
	if _, _, ok := s.Begin("d", "two"); ok {
		t.Error("second task started while first is active")
	}
	if _, _, ok := s.Begin("other", "x"); !ok {
		t.Error("other desk blocked")
	}
	if s.State("d") != StateWorking {
		t.Errorf("state = %s", s.State("d"))
	}
	s.SetStatus("d", t1.ID, StatusPaused)
	if s.State("d") != StatePaused {
		t.Errorf("state = %s, want paused", s.State("d"))
	}
	s.SetStatus("d", t1.ID, StatusDone)
	s.SetStatus("d", t1.ID, StatusRunning) // finished tasks stay finished
	if s.State("d") != StateIdle {
		t.Errorf("state = %s, want idle", s.State("d"))
	}
	if _, first, ok := s.Begin("d", "two"); !ok || first {
		t.Errorf("follow-up: ok=%v first=%v", ok, first)
	}
}

func TestStoreSubscribeReplaysThenStreams(t *testing.T) {
	s := NewStore()
	s.Append("d", Event{Kind: KindLog, Text: "a"})
	s.Append("d", Event{Kind: KindLog, Text: "b"})
	past, ch, cancel := s.Subscribe("d", 1)
	defer cancel()
	if len(past) != 1 || past[0].Text != "b" || past[0].Seq != 2 {
		t.Errorf("past = %+v", past)
	}
	s.Append("d", Event{Kind: KindLog, Text: "c"})
	select {
	case ev := <-ch:
		if ev.Text != "c" || ev.Seq != 3 {
			t.Errorf("live = %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("no live event")
	}
}

const sampleRun = `{"type":"system","subtype":"init","model":"m"}
{"type":"assistant","message":{"content":[{"type":"text","text":"On it"}]}}
{"type":"result","subtype":"success","is_error":false,"result":"Done","total_cost_usd":0.01}
`

func newTestRunner(out string) (*Runner, *runtimetest.Fake) {
	rt := &runtimetest.Fake{ExecOutput: out}
	providers := func() ([]Provider, error) {
		return []Provider{{Name: "Claude", Env: map[string]string{"CLAUDE_CODE_OAUTH_TOKEN": "tok"}}}, nil
	}
	return NewRunner(rt, NewStore(), Config{Providers: providers},
		slog.New(slog.NewTextHandler(io.Discard, nil))), rt
}

func waitIdle(t *testing.T, s *Store, desk string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for s.State(desk) != StateIdle {
		if time.Now().After(deadline) {
			t.Fatal("task never finished")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestRunnerRunsTaskAndRecordsEvents(t *testing.T) {
	r, rt := newTestRunner(sampleRun)
	task, err := r.Start("d", "  find flights  ")
	if err != nil {
		t.Fatal(err)
	}
	waitIdle(t, r.Store(), "d")

	req := rt.Execs[0]
	if req.Env["SHORTDESK_PROMPT"] != "find flights" || req.Env["CLAUDE_CODE_OAUTH_TOKEN"] != "tok" {
		t.Errorf("env = %v", req.Env)
	}
	if _, ok := req.Env["SHORTDESK_CONTINUE"]; ok {
		t.Error("first task should not continue a conversation")
	}
	if !strings.Contains(req.Command, "--output-format stream-json") {
		t.Errorf("command = %q", req.Command)
	}

	var kinds []string
	for _, ev := range r.Store().Events("d", 0) {
		if ev.TaskID != task.ID {
			t.Errorf("event for wrong task: %+v", ev)
		}
		kinds = append(kinds, string(ev.Kind)+":"+ev.Text)
	}
	want := "prompt:find flights,status:running,log:Agent started,text:On it,result:Done,status:done"
	if got := strings.Join(kinds, ","); got != want {
		t.Errorf("events = %s\nwant     %s", got, want)
	}

	if _, err := r.Start("d", "and hotels"); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, r.Store(), "d")
	if rt.Execs[1].Env["SHORTDESK_CONTINUE"] != "--continue" {
		t.Error("follow-up task should continue the conversation")
	}
}

func TestRunnerFailsWithoutResult(t *testing.T) {
	r, _ := newTestRunner("You've hit your weekly limit\n")
	r.Start("d", "x")
	waitIdle(t, r.Store(), "d")
	evs := r.Store().Events("d", 0)
	if last := evs[len(evs)-1]; last.Kind != KindStatus || last.Text != string(StatusFailed) {
		t.Errorf("last event = %+v, want failed status", last)
	}
}

func TestRunnerRejectsEmptyPromptAndControlWithoutTask(t *testing.T) {
	r, _ := newTestRunner(sampleRun)
	if _, err := r.Start("d", "   "); err == nil {
		t.Error("empty prompt accepted")
	}
	if err := r.Pause(context.Background(), "d"); !errors.Is(err, ErrNoActiveTask) {
		t.Errorf("pause without task: %v", err)
	}
}

func TestLineWriterSplitsAcrossWrites(t *testing.T) {
	var got []string
	w := newLineWriter(func(l string) { got = append(got, l) })
	w.Write([]byte("ab"))
	w.Write([]byte("c\nde\nf"))
	w.Flush()
	if strings.Join(got, "|") != "abc|de|f" {
		t.Errorf("lines = %q", got)
	}
}

// limitedThenOK fails the first run with a usage limit and succeeds afterwards.
type limitedThenOK struct {
	*runtimetest.Fake
	calls int
}

func (f *limitedThenOK) Exec(ctx context.Context, id string, req runtime.ExecRequest) error {
	f.calls++
	if f.calls == 1 {
		f.Fake.ExecOutput = `{"type":"result","subtype":"error","is_error":true,"result":"You've hit your weekly limit"}` + "\n"
	} else {
		f.Fake.ExecOutput = sampleRun
	}
	return f.Fake.Exec(ctx, id, req)
}

func TestRunnerFallsBackToNextProviderOnLimit(t *testing.T) {
	rt := &limitedThenOK{Fake: &runtimetest.Fake{}}
	providers := func() ([]Provider, error) {
		return []Provider{
			{Name: "Claude", Env: map[string]string{"CLAUDE_CODE_OAUTH_TOKEN": "tok"}},
			{Name: "OpenRouter", Env: map[string]string{"ANTHROPIC_BASE_URL": "https://openrouter.ai/api"}},
		}, nil
	}
	r := NewRunner(rt, NewStore(), Config{Providers: providers}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, err := r.Start("d", "x"); err != nil {
		t.Fatal(err)
	}
	waitIdle(t, r.Store(), "d")

	if len(rt.Execs) != 2 || rt.Execs[1].Env["ANTHROPIC_BASE_URL"] != "https://openrouter.ai/api" {
		t.Fatalf("execs = %d, second env = %v", len(rt.Execs), rt.Execs[1].Env)
	}
	evs := r.Store().Events("d", 0)
	var switched bool
	for _, ev := range evs {
		if strings.Contains(ev.Text, "Switching to OpenRouter") {
			switched = true
		}
	}
	if last := evs[len(evs)-1]; !switched || last.Text != string(StatusDone) {
		t.Errorf("switched=%v last=%+v", switched, last)
	}
}

func TestRunnerDoesNotFallBackOnOtherErrors(t *testing.T) {
	rt := &runtimetest.Fake{ExecOutput: `{"type":"result","subtype":"error","is_error":true,"result":"Something else broke"}` + "\n"}
	providers := func() ([]Provider, error) {
		return []Provider{{Name: "Claude"}, {Name: "OpenRouter"}}, nil
	}
	r := NewRunner(rt, NewStore(), Config{Providers: providers}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	r.Start("d", "x")
	waitIdle(t, r.Store(), "d")
	if len(rt.Execs) != 1 {
		t.Errorf("execs = %d, want 1 (no fallback for non-limit errors)", len(rt.Execs))
	}
}

func TestParseReset(t *testing.T) {
	now := time.Date(2026, 10, 26, 20, 0, 0, 0, time.UTC)
	got, ok := parseReset("You've hit your weekly limit · resets Oct 29, 11pm (Europe/London)", now)
	if !ok {
		t.Fatal("not parsed")
	}
	want := time.Date(2026, 10, 29, 23, 0, 0, 0, time.FixedZone("GMT", 0)) // London is on GMT on Oct 29, 2026
	if !got.Equal(want) {
		t.Errorf("reset = %v, want %v", got, want)
	}
	if _, ok := parseReset("rate limit, try later", now); ok {
		t.Error("parsed a message without a reset time")
	}
	if got, ok := parseReset("resets Jan 2, 9:30am (UTC)", time.Date(2026, 12, 30, 0, 0, 0, 0, time.UTC)); !ok || got.Year() != 2027 {
		t.Errorf("year rollover: %v %v", got, ok)
	}
}

func TestRunnerSkipsProviderWhileItsLimitLasts(t *testing.T) {
	rt := &limitedThenOK{Fake: &runtimetest.Fake{}}
	providers := func() ([]Provider, error) {
		return []Provider{{Name: "Claude"}, {Name: "Own", Note: "you are model X", HideCost: true}}, nil
	}
	r := NewRunner(rt, NewStore(), Config{Providers: providers}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	r.Start("d", "first")
	waitIdle(t, r.Store(), "d")
	if len(rt.Execs) != 2 {
		t.Fatalf("first task execs = %d, want 2 (Claude, then own)", len(rt.Execs))
	}
	if !strings.Contains(rt.Execs[1].Env["SHORTDESK_SYSTEM_PROMPT"], "you are model X") {
		t.Error("provider note missing from the agent's instructions")
	}

	r.Start("d", "second")
	waitIdle(t, r.Store(), "d")
	if len(rt.Execs) != 3 {
		t.Fatalf("second task execs = %d, want 3 (Claude skipped while limited)", len(rt.Execs))
	}
	var skippedNote bool
	for _, ev := range r.Store().Events("d", 0) {
		if strings.Contains(ev.Text, "Claude is out of usage until") {
			skippedNote = true
		}
		if ev.Kind == KindResult && ev.CostUSD != 0 {
			t.Error("cost shown for a provider with HideCost")
		}
	}
	if !skippedNote {
		t.Error("no note saying Claude was skipped")
	}

	// Once the limit has passed, Claude is tried again.
	r.now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	if _, ok := r.restingUntil("Claude"); ok {
		t.Error("Claude still resting after its limit passed")
	}
}

func TestStoreSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	done, _, _ := s.Begin("desk-1", "find flights")
	s.Append("desk-1", Event{TaskID: done.ID, Kind: KindResult, Text: "€879"})
	s.SetStatus("desk-1", done.ID, StatusDone)
	running, _, _ := s.Begin("desk-1", "and hotels") // still running when Shortdesk stops

	again, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	evs := again.Events("desk-1", 0)
	if evs[0].Kind != KindPrompt || evs[0].Text != "find flights" {
		t.Fatalf("first event = %+v", evs[0])
	}
	var interrupted bool
	for _, ev := range evs {
		if ev.TaskID == running.ID && ev.Text == interruptedNote {
			interrupted = true
		}
	}
	if !interrupted {
		t.Error("interrupted task not marked")
	}
	if again.State("desk-1") != StateIdle {
		t.Errorf("state after restart = %s, want idle", again.State("desk-1"))
	}
	// New events continue the numbering, and follow-ups continue the conversation.
	_, first, ok := again.Begin("desk-1", "one more")
	if !ok || first {
		t.Errorf("begin after restart: ok=%v first=%v", ok, first)
	}
	last := again.Events("desk-1", 0)
	if last[len(last)-1].Seq <= evs[len(evs)-1].Seq {
		t.Error("sequence numbers restarted")
	}

	again.Forget("desk-1")
	if _, err := os.Stat(filepath.Join(dir, "removed", "desk-1.jsonl")); err != nil {
		t.Errorf("removed desk's chat not archived: %v", err)
	}
}

func TestRunnerExplainsKnownFailures(t *testing.T) {
	r, _ := newTestRunner(`{"type":"result","subtype":"error","is_error":true,"result":"API Error: 500 ConnectError: [Errno -2] Name or service not known"}` + "\n")
	r.Start("d", "x")
	waitIdle(t, r.Store(), "d")
	evs := r.Store().Events("d", 0)
	last := evs[len(evs)-1]
	if last.Kind != KindHelp || !strings.Contains(last.Text, "couldn't reach") {
		t.Errorf("last event = %+v, want help", last)
	}

	r2, _ := newTestRunner(sampleRun)
	r2.Start("d", "x")
	waitIdle(t, r2.Store(), "d")
	for _, ev := range r2.Store().Events("d", 0) {
		if ev.Kind == KindHelp {
			t.Errorf("help on a successful task: %+v", ev)
		}
	}
}
