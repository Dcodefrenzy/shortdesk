package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/shortdesk/shortdesk/internal/runtime"
)

// ErrBusy means the desk's agent is already working on a task.
var ErrBusy = errors.New("the agent is already working on a task")

// ErrNoActiveTask means there is nothing to pause, resume or stop.
var ErrNoActiveTask = errors.New("the agent is not working on a task")

// Facts about the desk image the runner relies on.
const (
	deskHome  = "/home/desk"
	pidFile   = "/tmp/shortdesk-agent.pid"
	mcpConfig = "/etc/shortdesk/mcp.json"
	// agentLog receives a copy of the agent's output; shortdesk-agent-view shows it
	// in a terminal window on the desk so people watching the screen see the agent work.
	agentLog = "/tmp/shortdesk-agent.jsonl"
)

// systemPrompt tells the agent where it is.
const systemPrompt = `You are working on a Shortdesk desk: a Linux desktop (XFCE) that a person may be watching live.
Use the browser tools to work on websites; the browser window is visible on the desk.
If you need something only a human can do (a login, a CAPTCHA, a payment, a decision), say so clearly and stop.
Websites this desk may visit are restricted. If a site fails to load because it is blocked, do not try to work
around it: run shortdesk-request-access <host> "<why you need it>" in the terminal, then stop and say what you
are waiting for. You will be told when access is granted.
Code projects live in ~/Developer, shared with every desk. Never edit a project folder there directly: get your
own working copy with cd "$(desk-project open <name>)" (or desk-project new <name> to start one, desk-project
clone <url> to clone one), work and commit there with clear messages, and commit before you finish. Your
branch is merged into the project by the person, locally. Do not push to remote repositories unless asked.
Keep other files worth keeping in ~/Files (also shared); don't delete others' work.
Save results worth keeping in ~/outputs with clear file names, so the person can open and download them:
tables as .csv, documents and reports as .md, plus images, PDFs or .html pages you make. Mention the files
in your answer. Keep your answer itself short and readable.`

// command starts Claude Code headless (bash). It opens the agent's activity window on the
// desk if it isn't on screen (checked by window title: a process search would match this
// very command), records the shell's PID, then execs Claude so the PID file points
// at Claude itself (for pause/resume/stop). Output goes to the runner and to agentLog.
const command = `cd ` + deskHome + `
xdotool search --name "Shortdesk agent" >/dev/null 2>&1 || (setsid xfce4-terminal --disable-server --title "Shortdesk agent" --geometry 110x32 \
  -x shortdesk-agent-view >/dev/null 2>&1 &)
printf '\n── New task: %s\n' "$SHORTDESK_PROMPT" >> ` + agentLog + `
desk-browser-ready || echo "Warning: the desk's browser is not ready; browser tools may fail." >&2
if [ -n "${SHORTDESK_FCC:-}" ]; then
  if ! command -v desk-fcc-ready >/dev/null; then
    echo "This desk was made before Free Claude Code was added, so it can't use your own models. Start a new desk." >&2
    exit 1
  fi
  desk-fcc-ready || exit 1
  # FCC has the model keys now; the agent doesn't need them.
  unset MODEL MODEL_FALLBACKS GEMINI_API_KEY DEEPSEEK_API_KEY GROQ_API_KEY NVIDIA_NIM_API_KEY
fi
echo $$ > ` + pidFile + `
exec claude -p "$SHORTDESK_PROMPT" \
  --output-format stream-json --verbose --dangerously-skip-permissions \
  --mcp-config ` + mcpConfig + ` --append-system-prompt "$SHORTDESK_SYSTEM_PROMPT" $SHORTDESK_CONTINUE \
  > >(tee -a ` + agentLog + `) 2> >(tee -a ` + agentLog + ` >&2)`

// Config configures the runner.
type Config struct {
	// Providers returns, for each task, the model providers to try in order. A task
	// moves to the next provider only when the current one refuses with a usage
	// limit. Credentials in Env are injected per run and never written into the desk.
	Providers func() ([]Provider, error)
	Timeout   time.Duration // per task; default 2h
}

// Provider is one way to reach a model, e.g. Claude, or the owner's own models.
type Provider struct {
	Name string
	Env  map[string]string
	// Note is added to the agent's instructions, e.g. which model it really is (Claude
	// Code tells every model it is Claude).
	Note string
	// HideCost drops Claude Code's cost estimate, which assumes Claude's prices.
	HideCost bool
}

// restFallback is how long a provider is skipped after a usage limit when its reset
// time can't be read from the message.
const restFallback = time.Hour

// resetRe reads Claude's "resets Oct 29, 11pm (Europe/London)".
var resetRe = regexp.MustCompile(`resets ([A-Z][a-z]{2} \d{1,2}), (\d{1,2}(?::\d{2})?[ap]m) \(([^)]+)\)`)

// parseReset returns when a usage limit lifts, if the message says.
func parseReset(text string, now time.Time) (time.Time, bool) {
	m := resetRe.FindStringSubmatch(text)
	if m == nil {
		return time.Time{}, false
	}
	loc, err := time.LoadLocation(m[3])
	if err != nil {
		return time.Time{}, false
	}
	clock := m[2]
	layout := "Jan 2 2006 3pm"
	if strings.Contains(clock, ":") {
		layout = "Jan 2 2006 3:04pm"
	}
	t, err := time.ParseInLocation(layout, fmt.Sprintf("%s %d %s", m[1], now.In(loc).Year(), clock), loc)
	if err != nil {
		return time.Time{}, false
	}
	if t.Before(now.Add(-24 * time.Hour)) { // "Jan 2" seen in late December
		t = t.AddDate(1, 0, 0)
	}
	return t, true
}

// harmlessNotice matches Claude Code warnings that are expected when it isn't talking
// to Claude directly; they are shown as notes, not errors.
var harmlessNotice = regexp.MustCompile(`(?i)connectors are disabled because`)

// limitRe matches a provider refusing because a usage or rate limit was hit.
var limitRe = regexp.MustCompile(`(?i)hit your .*limit|usage limit|rate limit|quota exceeded`)

// Runner runs agent tasks in desks.
type Runner struct {
	rt    runtime.Runtime
	store *Store
	cfg   Config
	log   *slog.Logger

	mu      sync.Mutex
	stopped map[string]bool      // task IDs a human stopped
	resting map[string]time.Time // provider name -> skipped until (usage limit)
	hook    func(deskID string, ev *Event)
	now     func() time.Time
}

// SetEventHook registers fn to see (and adjust) every agent event before it is stored.
func (r *Runner) SetEventHook(fn func(deskID string, ev *Event)) {
	r.mu.Lock()
	r.hook = fn
	r.mu.Unlock()
}

// NewRunner returns a Runner.
func NewRunner(rt runtime.Runtime, store *Store, cfg Config, log *slog.Logger) *Runner {
	if cfg.Timeout == 0 {
		cfg.Timeout = 2 * time.Hour
	}
	return &Runner{rt: rt, store: store, cfg: cfg, log: log, stopped: map[string]bool{},
		resting: map[string]time.Time{}, now: time.Now}
}

// Store returns the runner's store.
func (r *Runner) Store() *Store { return r.store }

// Start begins a task in the desk and returns immediately; progress arrives as events.
// Follow-up tasks continue the desk's previous conversation.
func (r *Runner) Start(deskID, prompt string) (Task, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return Task{}, errors.New("prompt is empty")
	}
	task, first, ok := r.store.Begin(deskID, prompt)
	if !ok {
		return Task{}, ErrBusy
	}
	go r.run(task, !first)
	return task, nil
}

func (r *Runner) run(task Task, cont bool) {
	ctx, cancel := context.WithTimeout(context.Background(), r.cfg.Timeout)
	defer cancel()

	providers := []Provider{{Name: "the desk's own login"}}
	if r.cfg.Providers != nil {
		ps, err := r.cfg.Providers()
		if err != nil {
			r.store.Append(task.DeskID, Event{TaskID: task.ID, Kind: KindError, Text: err.Error()})
			r.store.SetStatus(task.DeskID, task.ID, StatusFailed)
			return
		}
		providers = ps
	}

	// Skip providers still resting after a usage limit, unless that leaves none.
	var skipped []string
	if len(providers) > 1 {
		var ready []Provider
		for _, p := range providers {
			if until, ok := r.restingUntil(p.Name); ok {
				skipped = append(skipped, fmt.Sprintf("%s is out of usage until %s", p.Name, until.Local().Format("Jan 2, 3:04pm")))
				continue
			}
			ready = append(ready, p)
		}
		if len(ready) > 0 {
			providers = ready
		} else {
			skipped = nil
		}
	}

	for i, p := range providers {
		if len(skipped) > 0 && i == 0 {
			r.store.Append(task.DeskID, Event{TaskID: task.ID, Kind: KindLog,
				Text: strings.Join(skipped, "; ") + ". Using " + p.Name + "."})
		} else if len(providers) > 1 {
			r.store.Append(task.DeskID, Event{TaskID: task.ID, Kind: KindLog, Text: "Using " + p.Name})
		}
		out := r.attempt(ctx, task, cont, p)
		if out.limited && !out.stopped {
			r.rest(p.Name, out.limitText)
			if i+1 < len(providers) {
				r.store.Append(task.DeskID, Event{TaskID: task.ID, Kind: KindLog,
					Text: p.Name + " hit its usage limit. Switching to " + providers[i+1].Name + "."})
				continue
			}
		}
		r.finish(ctx, task, out)
		return
	}
}

// rest skips a provider until its usage limit lifts.
func (r *Runner) rest(name, limitText string) {
	until, ok := parseReset(limitText, r.now())
	if !ok {
		until = r.now().Add(restFallback)
	}
	r.mu.Lock()
	r.resting[name] = until
	r.mu.Unlock()
}

func (r *Runner) restingUntil(name string) (time.Time, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	until, ok := r.resting[name]
	if !ok || !r.now().Before(until) {
		delete(r.resting, name)
		return time.Time{}, false
	}
	return until, true
}

// outcome is how one attempt at a task ended.
type outcome struct {
	err         error
	stopped     bool // a human stopped it
	sawResult   bool
	resultError bool
	limited     bool   // the provider refused with a usage limit
	limitText   string // its message, which may say when the limit lifts
}

// attempt runs the agent once with one provider, streaming its events.
func (r *Runner) attempt(ctx context.Context, task Task, cont bool, p Provider) outcome {
	env := map[string]string{
		"HOME":                    deskHome,
		"DISPLAY":                 ":0",
		"SHORTDESK_PROMPT":        task.Prompt,
		"SHORTDESK_SYSTEM_PROMPT": strings.TrimSpace(systemPrompt + "\n" + p.Note),
	}
	if cont {
		env["SHORTDESK_CONTINUE"] = "--continue"
	}
	for k, v := range p.Env {
		env[k] = v
	}

	var out outcome
	var mu sync.Mutex
	emit := func(line string, stderr bool) {
		for _, ev := range ParseClaudeLine(line) {
			if stderr && ev.Kind == KindLog && !harmlessNotice.MatchString(ev.Text) {
				ev.Kind = KindError
			}
			if p.HideCost && ev.Kind == KindResult {
				ev.CostUSD = 0
			}
			mu.Lock()
			if ev.Kind == KindResult {
				out.sawResult, out.resultError = true, ev.IsError
			}
			if (ev.Kind == KindResult && ev.IsError) || ev.Kind == KindError || ev.Kind == KindText || ev.Kind == KindLog {
				if limitRe.MatchString(ev.Text) {
					out.limited = true
					out.limitText = ev.Text
				}
			}
			mu.Unlock()
			ev.TaskID = task.ID
			r.mu.Lock()
			hook := r.hook
			r.mu.Unlock()
			if hook != nil {
				hook(task.DeskID, &ev)
			}
			r.store.Append(task.DeskID, ev)
		}
	}
	stdout := newLineWriter(func(l string) { emit(l, false) })
	stderr := newLineWriter(func(l string) { emit(l, true) })

	out.err = r.rt.Exec(ctx, task.DeskID, runtime.ExecRequest{Command: command, Env: env, Stdout: stdout, Stderr: stderr})
	stdout.Flush()
	stderr.Flush()

	r.mu.Lock()
	out.stopped = r.stopped[task.ID]
	r.mu.Unlock()
	// A limit only counts when the task actually failed.
	out.limited = out.limited && (out.err != nil || out.resultError)
	return out
}

// finish records the task's final status.
func (r *Runner) finish(ctx context.Context, task Task, out outcome) {
	r.mu.Lock()
	delete(r.stopped, task.ID)
	r.mu.Unlock()

	switch {
	case out.stopped:
		r.store.SetStatus(task.DeskID, task.ID, StatusStopped)
	case out.err != nil && out.resultError:
		// The agent already reported why it failed; its exit status adds nothing.
		r.store.SetStatus(task.DeskID, task.ID, StatusFailed)
	case out.err != nil:
		r.store.Append(task.DeskID, Event{TaskID: task.ID, Kind: KindError, Text: describeExit(out.err, ctx)})
		r.store.SetStatus(task.DeskID, task.ID, StatusFailed)
	case !out.sawResult || out.resultError:
		r.store.SetStatus(task.DeskID, task.ID, StatusFailed)
	default:
		r.store.SetStatus(task.DeskID, task.ID, StatusDone)
	}
}

func describeExit(err error, ctx context.Context) string {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "The task ran out of time and was stopped."
	}
	var exit *runtime.ExitError
	if errors.As(err, &exit) {
		return fmt.Sprintf("The agent exited with status %d.", exit.Code)
	}
	return "Could not run the agent: " + err.Error()
}

// Pause freezes the agent so a human can use the desk. Its browser and other
// programs keep running; only the agent stops acting.
func (r *Runner) Pause(ctx context.Context, deskID string) error {
	return r.signal(ctx, deskID, StatusRunning, "kill -STOP $(cat "+pidFile+")", StatusPaused)
}

// Resume lets a paused agent continue exactly where it stopped.
func (r *Runner) Resume(ctx context.Context, deskID string) error {
	return r.signal(ctx, deskID, StatusPaused, "kill -CONT $(cat "+pidFile+")", StatusRunning)
}

// Stop ends the agent's current task. A paused process must be continued to act on TERM.
func (r *Runner) Stop(ctx context.Context, deskID string) error {
	t, ok := r.store.Active(deskID)
	if !ok {
		return ErrNoActiveTask
	}
	r.mu.Lock()
	r.stopped[t.ID] = true
	r.mu.Unlock()
	return r.rt.Exec(ctx, deskID, runtime.ExecRequest{
		Command: "p=$(cat " + pidFile + ") && kill -TERM $p && kill -CONT $p",
	})
}

func (r *Runner) signal(ctx context.Context, deskID string, from Status, cmd string, to Status) error {
	t, ok := r.store.Active(deskID)
	if !ok {
		return ErrNoActiveTask
	}
	if t.Status != from {
		return fmt.Errorf("the agent is %s", t.Status)
	}
	var errOut bytes.Buffer
	if err := r.rt.Exec(ctx, deskID, runtime.ExecRequest{Command: cmd, Stderr: &errOut}); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(errOut.String()))
	}
	r.store.SetStatus(deskID, t.ID, to)
	return nil
}

// lineWriter calls fn for each complete line written to it.
type lineWriter struct {
	mu  sync.Mutex
	buf []byte
	fn  func(string)
}

var _ io.Writer = (*lineWriter)(nil)

func newLineWriter(fn func(string)) *lineWriter { return &lineWriter{fn: fn} }

func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			return len(p), nil
		}
		line := string(w.buf[:i])
		w.buf = w.buf[i+1:]
		w.fn(line)
	}
}

// Flush emits any trailing partial line.
func (w *lineWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.buf) > 0 {
		w.fn(string(w.buf))
		w.buf = nil
	}
}
