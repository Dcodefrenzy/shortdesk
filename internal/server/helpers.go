package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/shortdesk/shortdesk/internal/agent"
	"github.com/shortdesk/shortdesk/internal/charter"
	"github.com/shortdesk/shortdesk/internal/desk"
	"github.com/shortdesk/shortdesk/internal/phone"
	"github.com/shortdesk/shortdesk/internal/runtime"
)

// Helper desks do the long jobs so the main desk stays free to talk. The main desk's
// agent hands a job over by adding a task for a helper to the Tasks app (apps helper);
// Shortdesk notices it, starts or reuses a helper desk, runs the job there, and
// reports back in the main desk's chat. Helpers post short updates in the Scratchbook;
// their full log is their own desk's chat. Idle helpers are removed after a while.
const (
	maxHelpers      = 3
	helperIdleTTL   = 10 * time.Minute
	helperPrefix    = "helper-"
	defaultJobHours = 2
)

// helperPrompt tells a helper what it's doing; the job's instructions follow.
const helperPrompt = `You are a helper desk. The main desk handed you this job:

%s

Post a short update at each milestone with: apps note "<one line>".
If you need the person (a login, a CAPTCHA, a decision), ask with
apps message "%s" "<question>" --ask "<choice>|<choice>" and stop.
Keep results in the person's apps (or ~/outputs). End with a short summary of what you did and found.`

// mainDeskNote is added to the main desk agent's instructions.
const mainDeskNote = `You are the main desk: the person talks to you. Answer quickly. For work that takes more than a
couple of minutes (browsing many pages, applying, long research, building something), hand it to a helper
desk with apps helper "<short title>" "<full instructions: everything the helper needs>", tell the person
it has started, and stop. Helpers report back in this chat by themselves. See running jobs with
apps records tasks tasks '{"where":[{"field":"runOn","op":"=","value":"helper"}]}'.`

// job is a helper task from the Tasks app.
type job struct {
	ID      string         `json:"id"`
	Version int            `json:"version"`
	Data    map[string]any `json:"data"`
}

func (j job) str(k string) string { s, _ := j.Data[k].(string); return s }

type helperState struct {
	deskID, name string
	jobID, title string // the job it's on, if any
	idleSince    time.Time
}

type helpers struct {
	s  *Server
	mu sync.Mutex
	// by desk ID
	desks map[string]*helperState
}

func newHelpers(s *Server) *helpers { return &helpers{s: s, desks: map[string]*helperState{}} }

// RunHelpers hands queued jobs to helper desks and removes idle ones, until ctx ends.
func (s *Server) RunHelpers(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		tick, cancel := context.WithTimeout(ctx, 2*time.Minute)
		s.helpers.dispatch(tick)
		s.helpers.reap(tick)
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// phoneCall calls the Phone Desk on the main desk as the owner.
func (s *Server) phoneCall(ctx context.Context, method, path string, body any, out any) error {
	main := s.mainDesk(ctx)
	if main == "" {
		return errors.New("no main desk")
	}
	ep, err := s.desks.Runtime().Endpoint(ctx, main, phonePort)
	if err != nil {
		return err
	}
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, ep.URL()+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+phone.OwnerKey(s.phone.key()))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("phone %s %s: %d %s", method, path, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func (s *Server) updateJob(ctx context.Context, j *job, patch map[string]any) error {
	var rec job
	if err := s.phoneCall(ctx, http.MethodPatch, "/api/apps/tasks/data/tasks/"+j.ID,
		map[string]any{"data": patch, "version": j.Version}, &rec); err != nil {
		return err
	}
	*j = rec
	return nil
}

func (s *Server) scratch(ctx context.Context, j job, from, text string) {
	err := s.phoneCall(ctx, http.MethodPost, "/api/apps/scratchbook/data/notes",
		map[string]any{"data": map[string]any{"task": j.ID, "title": j.str("title"), "from": from, "text": text}}, nil)
	if err != nil {
		s.log.Warn("could not write to the scratchbook", "err", err)
	}
}

// tellMain shows a line about a helper in the main desk's chat.
func (s *Server) tellMain(ctx context.Context, j job, state, text, helperDesk string) {
	main := s.mainDesk(ctx)
	if main == "" {
		return
	}
	s.agent.Store().Append(main, agent.Event{TaskID: "helper:" + j.ID, Kind: agent.KindHelper, Text: text,
		Data: map[string]string{"job": j.ID, "title": j.str("title"), "state": state, "desk": helperDesk}})
}

// dispatch starts queued helper jobs while there's room.
func (h *helpers) dispatch(ctx context.Context) {
	s := h.s
	if s.mainDesk(ctx) == "" || s.phone.State() != "running" {
		return
	}
	var queued []job
	err := s.phoneCall(ctx, http.MethodPost, "/api/apps/tasks/data/tasks/query", map[string]any{
		"where":   []map[string]any{{"field": "status", "op": "=", "value": "queued"}, {"field": "runOn", "op": "=", "value": "helper"}},
		"orderBy": []map[string]any{{"field": "_created"}},
		"limit":   10,
	}, &queued)
	if err != nil {
		s.log.Warn("could not read helper jobs", "err", err)
		return
	}
	for i := range queued {
		j := &queued[i]
		d, err := h.claim(ctx)
		if err != nil {
			if !errors.Is(err, errNoRoom) {
				s.log.Warn("could not start a helper desk", "err", err)
				_ = s.updateJob(ctx, j, map[string]any{"status": "failed", "result": "Couldn't start a helper desk: " + err.Error()})
				s.tellMain(ctx, *j, "failed", "Couldn't start a helper desk: "+err.Error(), "")
			}
			return
		}
		if err := s.updateJob(ctx, j, map[string]any{"status": "doing", "desk": d.name}); err != nil {
			h.release(d.deskID) // someone else changed it; leave it
			continue
		}
		h.startJob(ctx, d, *j)
	}
}

var errNoRoom = errors.New("all helper desks are busy")

// claim returns an idle helper, or starts a new one when there's room.
func (h *helpers) claim(ctx context.Context) (*helperState, error) {
	s := h.s
	h.mu.Lock()
	for _, d := range h.desks {
		if d.jobID == "" {
			d.jobID = "claimed"
			h.mu.Unlock()
			return d, nil
		}
	}
	if len(h.desks) >= maxHelpers {
		h.mu.Unlock()
		return nil, errNoRoom
	}
	name := ""
	for n := 1; name == ""; n++ {
		candidate := fmt.Sprintf("%s%d", helperPrefix, n)
		taken := false
		for _, d := range h.desks {
			taken = taken || d.name == candidate
		}
		if !taken {
			name = candidate
		}
	}
	placeholder := &helperState{deskID: "starting:" + name, name: name, jobID: "claimed"}
	h.desks[placeholder.deskID] = placeholder
	h.mu.Unlock()

	fail := func(err error) (*helperState, error) {
		h.mu.Lock()
		delete(h.desks, placeholder.deskID)
		h.mu.Unlock()
		return nil, err
	}
	c, err := h.helperCharter(ctx)
	if err != nil {
		return fail(err)
	}
	d, err := s.desks.Up(ctx, c, name)
	if err != nil {
		return fail(err)
	}
	ready, cancel := context.WithTimeout(ctx, s.cfg.ReadyTimeout)
	defer cancel()
	if err := s.desks.WaitReady(ready, d); err != nil {
		s.desks.Down(context.Background(), d)
		return fail(err)
	}
	h.mu.Lock()
	delete(h.desks, placeholder.deskID)
	st := &helperState{deskID: d.ID, name: d.Name, jobID: "claimed"}
	h.desks[d.ID] = st
	h.mu.Unlock()
	s.log.Info("started a helper desk", "desk", d.Name)
	return st, nil
}

// helperCharter is the main desk's charter: helpers may do what the main desk may.
func (h *helpers) helperCharter(ctx context.Context) (*charter.Charter, error) {
	main, err := h.s.desks.Find(ctx, h.s.mainDesk(ctx))
	if err != nil {
		return nil, err
	}
	charters, err := charter.LoadDir(h.s.cfg.ChartersDir)
	if err != nil {
		return nil, err
	}
	c, ok := charters[main.Charter]
	if !ok {
		return nil, fmt.Errorf("the main desk's charter %q is gone", main.Charter)
	}
	return c, nil
}

func (h *helpers) startJob(ctx context.Context, d *helperState, j job) {
	s := h.s
	hours := defaultJobHours
	if v, ok := j.Data["hours"].(float64); ok && v >= 1 && v <= 24 {
		hours = int(v)
	}
	if rn, ok := s.desks.Runtime().(runtime.Renewer); ok {
		rn.Renew(ctx, d.deskID, time.Now().Add(time.Duration(hours)*time.Hour+15*time.Minute))
	}
	h.mu.Lock()
	d.jobID, d.title, d.idleSince = j.ID, j.str("title"), time.Time{}
	h.mu.Unlock()
	prompt := fmt.Sprintf(helperPrompt, j.str("instructions"), j.str("title"))
	if _, err := s.agent.Start(d.deskID, prompt); err != nil {
		s.log.Warn("could not start the helper's job", "desk", d.name, "err", err)
		h.release(d.deskID)
		_ = s.updateJob(ctx, &j, map[string]any{"status": "failed", "result": "Couldn't start: " + err.Error()})
		return
	}
	s.scratch(ctx, j, "main desk", "Started on "+d.name+".")
	s.tellMain(ctx, j, "started", d.name+" started: "+j.str("title"), d.deskID)
}

func (h *helpers) release(deskID string) {
	h.mu.Lock()
	if d, ok := h.desks[deskID]; ok {
		d.jobID, d.title, d.idleSince = "", "", time.Now()
	}
	h.mu.Unlock()
}

// jobFor returns the job a helper desk is on.
func (h *helpers) jobFor(deskID string) (*helperState, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	d, ok := h.desks[deskID]
	if !ok || d.jobID == "" || d.jobID == "claimed" {
		return nil, false
	}
	c := *d
	return &c, true
}

// finished records a helper's result and reports it to the main desk.
func (h *helpers) finished(task agent.Task, result string, failed bool) {
	d, ok := h.jobFor(task.DeskID)
	if !ok {
		return
	}
	s := h.s
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	j := job{ID: d.jobID}
	if err := s.phoneCall(ctx, http.MethodGet, "/api/apps/tasks/data/tasks/"+d.jobID, nil, &j); err != nil {
		s.log.Warn("helper finished but its job is gone", "desk", d.name, "err", err)
	}
	status := "done"
	if failed {
		status = "failed"
	}
	if strings.TrimSpace(result) == "" {
		result = "Finished without a summary."
	}
	if j.Version != 0 {
		_ = s.updateJob(ctx, &j, map[string]any{"status": status, "result": result})
	}
	if j.Data == nil {
		j.Data = map[string]any{"title": d.title}
	}
	s.scratch(ctx, j, d.name, map[bool]string{false: "Done: ", true: "Stopped: "}[failed]+short(result, 300))
	s.tellMain(ctx, j, status, result, task.DeskID)
	h.release(task.DeskID)
}

// reap removes helpers that have had nothing to do for a while, and forgets ones
// that are gone.
func (h *helpers) reap(ctx context.Context) {
	s := h.s
	h.mu.Lock()
	var idle []*helperState
	for id, d := range h.desks {
		if strings.HasPrefix(id, "starting:") {
			continue
		}
		if d.jobID == "" && !d.idleSince.IsZero() && time.Since(d.idleSince) > helperIdleTTL {
			idle = append(idle, d)
		}
	}
	h.mu.Unlock()
	for _, d := range idle {
		dk, err := s.desks.Find(ctx, d.deskID)
		if err == nil {
			err = s.desks.Down(ctx, dk)
		}
		if err != nil && !errors.Is(err, desk.ErrNotFound) {
			s.log.Warn("could not remove an idle helper", "desk", d.name, "err", err)
			continue
		}
		s.log.Info("removed an idle helper desk", "desk", d.name)
		h.mu.Lock()
		delete(h.desks, d.deskID)
		h.mu.Unlock()
	}
}

// helperEnv tells a helper's agent which job it's on.
func (h *helpers) env(_ context.Context, deskID string) map[string]string {
	d, ok := h.jobFor(deskID)
	if !ok {
		return nil
	}
	return map[string]string{"SHORTDESK_TASK_ID": d.jobID, "SHORTDESK_TASK_TITLE": d.title}
}

func short(s string, n int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
