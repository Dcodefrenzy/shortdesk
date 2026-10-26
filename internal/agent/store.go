package agent

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Status is where a task is in its life.
type Status string

const (
	StatusRunning Status = "running"
	StatusPaused  Status = "paused" // a human took over the desk
	StatusDone    Status = "done"
	StatusFailed  Status = "failed"
	StatusStopped Status = "stopped" // a human stopped it
)

// Active reports whether the task still has a live agent process.
func (s Status) Active() bool { return s == StatusRunning || s == StatusPaused }

// Task is one prompt sent to a desk's agent.
type Task struct {
	ID         string    `json:"id"`
	DeskID     string    `json:"deskId"`
	Prompt     string    `json:"prompt"`
	Status     Status    `json:"status"`
	CreatedAt  time.Time `json:"createdAt"`
	FinishedAt time.Time `json:"finishedAt,omitzero"`
}

// State summarizes a desk's agent for lists: idle, working or paused.
type State string

const (
	StateIdle    State = "idle"
	StateWorking State = "working"
	StatePaused  State = "paused"
)

// maxEventsPerDesk bounds memory per desk; the oldest events are dropped first.
const maxEventsPerDesk = 5000

// Store keeps tasks and chat events and fans new events out to subscribers. With a
// directory (OpenStore), every event is also appended to a per-desk file and loaded
// back on start, so chats survive restarts.
type Store struct {
	mu     sync.Mutex
	desks  map[string]*deskLog
	nowFn  func() time.Time
	idFunc func() string
	dir    string // "" = memory only
}

type deskLog struct {
	id     string
	tasks  []*Task
	events []Event
	seq    int
	subs   map[chan Event]struct{}
}

// NewStore returns an empty store.
func NewStore() *Store {
	return &Store{desks: map[string]*deskLog{}, nowFn: time.Now, idFunc: newID}
}

func (s *Store) log(deskID string) *deskLog {
	l, ok := s.desks[deskID]
	if !ok {
		l = &deskLog{id: deskID, subs: map[chan Event]struct{}{}}
		s.desks[deskID] = l
	}
	return l
}

// Begin records a new running task unless the desk already has an active one.
// It reports whether this is the desk's first task.
func (s *Store) Begin(deskID, prompt string) (t Task, first bool, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l := s.log(deskID)
	if a := l.active(); a != nil {
		return Task{}, false, false
	}
	task := &Task{ID: s.idFunc(), DeskID: deskID, Prompt: prompt, Status: StatusRunning, CreatedAt: s.nowFn()}
	first = len(l.tasks) == 0
	l.tasks = append(l.tasks, task)
	s.appendLocked(l, Event{TaskID: task.ID, Kind: KindPrompt, Text: prompt})
	s.appendLocked(l, Event{TaskID: task.ID, Kind: KindStatus, Text: string(StatusRunning)})
	return *task, first, true
}

// Active returns the desk's running or paused task.
func (s *Store) Active(deskID string) (Task, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if a := s.log(deskID).active(); a != nil {
		return *a, true
	}
	return Task{}, false
}

func (l *deskLog) active() *Task {
	if n := len(l.tasks); n > 0 && l.tasks[n-1].Status.Active() {
		return l.tasks[n-1]
	}
	return nil
}

// SetStatus changes a task's status and emits a status event. Finished tasks keep
// their final status.
func (s *Store) SetStatus(deskID, taskID string, st Status) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l := s.log(deskID)
	for _, t := range l.tasks {
		if t.ID != taskID || !t.Status.Active() || t.Status == st {
			continue
		}
		t.Status = st
		if !st.Active() {
			t.FinishedAt = s.nowFn()
		}
		s.appendLocked(l, Event{TaskID: taskID, Kind: KindStatus, Text: string(st)})
	}
}

// Append adds an event to a desk's chat.
func (s *Store) Append(deskID string, ev Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.appendLocked(s.log(deskID), ev)
}

func (s *Store) appendLocked(l *deskLog, ev Event) {
	l.seq++
	ev.Seq = l.seq
	if ev.Time.IsZero() {
		ev.Time = s.nowFn()
	}
	s.persistLocked(l.id, ev)
	l.events = append(l.events, ev)
	if len(l.events) > maxEventsPerDesk {
		l.events = l.events[len(l.events)-maxEventsPerDesk:]
	}
	for ch := range l.subs {
		select {
		case ch <- ev:
		default: // slow subscriber; it will catch up from Events on reconnect
		}
	}
}

// Events returns a desk's events with Seq greater than after.
func (s *Store) Events(deskID string, after int) []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Event
	for _, ev := range s.log(deskID).events {
		if ev.Seq > after {
			out = append(out, ev)
		}
	}
	return out
}

// Subscribe returns past events after `after` plus a channel of new ones. Call
// cancel when done.
func (s *Store) Subscribe(deskID string, after int) (past []Event, ch <-chan Event, cancel func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l := s.log(deskID)
	for _, ev := range l.events {
		if ev.Seq > after {
			past = append(past, ev)
		}
	}
	c := make(chan Event, 256)
	l.subs[c] = struct{}{}
	return past, c, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(l.subs, c)
	}
}

// State returns the desk's agent state.
func (s *Store) State(deskID string) State {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.desks[deskID]
	if !ok {
		return StateIdle
	}
	switch a := l.active(); {
	case a == nil:
		return StateIdle
	case a.Status == StatusPaused:
		return StatePaused
	default:
		return StateWorking
	}
}

// Forget drops a removed desk's chat from memory. Its file is kept, moved to the
// "removed" folder, so the history isn't lost.
func (s *Store) Forget(deskID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.desks, deskID)
	if s.dir != "" {
		os.MkdirAll(filepath.Join(s.dir, "removed"), 0o700)
		os.Rename(s.file(deskID), filepath.Join(s.dir, "removed", deskID+".jsonl"))
	}
}

func newID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// interruptedNote is recorded for tasks that were running when Shortdesk stopped.
const interruptedNote = "Shortdesk restarted while this task was running, so it was stopped."

var deskIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)

// OpenStore returns a store that saves chats under dir (one JSONL file per desk) and
// loads the ones already there. Tasks left running by a previous run are marked failed.
func OpenStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := NewStore()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".jsonl")
		if e.IsDir() || !ok || !deskIDRe.MatchString(id) {
			continue
		}
		if err := s.load(dir, id); err != nil {
			return nil, fmt.Errorf("chat %s: %w", e.Name(), err)
		}
	}
	s.dir = dir
	// Record interrupted tasks now that saving is on.
	for _, l := range s.desks {
		if a := l.active(); a != nil {
			s.appendLocked(l, Event{TaskID: a.ID, Kind: KindError, Text: interruptedNote})
			a.Status, a.FinishedAt = StatusFailed, s.nowFn()
			s.appendLocked(l, Event{TaskID: a.ID, Kind: KindStatus, Text: string(StatusFailed)})
		}
	}
	return s, nil
}

// load rebuilds one desk's events and tasks from its file.
func (s *Store) load(dir, deskID string) error {
	f, err := os.Open(filepath.Join(dir, deskID+".jsonl"))
	if err != nil {
		return err
	}
	defer f.Close()
	l := s.log(deskID)
	byID := map[string]*Task{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for sc.Scan() {
		var ev Event
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue // a torn last line from a crash
		}
		if ev.Seq > l.seq {
			l.seq = ev.Seq
		}
		l.events = append(l.events, ev)
		switch ev.Kind {
		case KindPrompt:
			t := &Task{ID: ev.TaskID, DeskID: deskID, Prompt: ev.Text, Status: StatusRunning, CreatedAt: ev.Time}
			byID[t.ID] = t
			l.tasks = append(l.tasks, t)
		case KindStatus:
			if t := byID[ev.TaskID]; t != nil {
				t.Status = Status(ev.Text)
				if !t.Status.Active() {
					t.FinishedAt = ev.Time
				}
			}
		}
	}
	if len(l.events) > maxEventsPerDesk {
		l.events = l.events[len(l.events)-maxEventsPerDesk:]
	}
	return sc.Err()
}

func (s *Store) file(deskID string) string { return filepath.Join(s.dir, deskID+".jsonl") }

// persistLocked appends one event to the desk's file. A failed write is not fatal: the
// chat still works in memory.
func (s *Store) persistLocked(deskID string, ev Event) {
	if s.dir == "" || !deskIDRe.MatchString(deskID) {
		return
	}
	b, err := json.Marshal(ev)
	if err != nil {
		return
	}
	f, err := os.OpenFile(s.file(deskID), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	f.Write(append(b, '\n'))
	f.Close()
}
