package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/shortdesk/shortdesk/internal/agent"
)

const keepAliveEvery = 20 * time.Second

func (s *Server) startTask(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Prompt string `json:"prompt"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	d, ok := s.findDesk(w, r)
	if !ok {
		return
	}
	// While the agent works, the message waits its turn instead of being refused.
	t, queued, err := s.agent.Send(d.ID, req.Prompt)
	switch {
	case err != nil:
		writeError(w, http.StatusBadRequest, err)
	case queued:
		writeJSON(w, http.StatusAccepted, map[string]any{"queued": true, "waiting": s.agent.Queued(d.ID)})
	default:
		writeJSON(w, http.StatusAccepted, t)
	}
}

// controlAgent handles POST /api/desks/{id}/agent/{action}: pause, resume or stop.
func (s *Server) controlAgent(w http.ResponseWriter, r *http.Request) {
	d, ok := s.findDesk(w, r)
	if !ok {
		return
	}
	var err error
	switch action := r.PathValue("action"); action {
	case "pause":
		err = s.agent.Pause(r.Context(), d.ID)
	case "resume":
		err = s.agent.Resume(r.Context(), d.ID)
	case "stop":
		err = s.agent.Stop(r.Context(), d.ID)
	default:
		writeError(w, http.StatusNotFound, fmt.Errorf("unknown action %q", action))
		return
	}
	switch {
	case errors.Is(err, agent.ErrNoActiveTask):
		writeError(w, http.StatusConflict, err)
	case err != nil:
		writeError(w, http.StatusBadGateway, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// streamEvents is a Server-Sent Events stream of a desk's chat: past events after
// Last-Event-ID (or ?after=), then live ones.
func (s *Server) streamEvents(w http.ResponseWriter, r *http.Request) {
	d, ok := s.findDesk(w, r)
	if !ok {
		return
	}
	after, _ := strconv.Atoi(r.Header.Get("Last-Event-ID"))
	if v := r.URL.Query().Get("after"); v != "" {
		after, _ = strconv.Atoi(v)
	}

	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	past, live, cancel := s.agent.Store().Subscribe(d.ID, after)
	defer cancel()
	send := func(ev agent.Event) error {
		b, err := json.Marshal(ev)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "id: %d\ndata: %s\n\n", ev.Seq, b); err != nil {
			return err
		}
		return rc.Flush()
	}
	for _, ev := range past {
		if send(ev) != nil {
			return
		}
	}
	if rc.Flush() != nil {
		return
	}

	ping := time.NewTicker(keepAliveEvery)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-live:
			if send(ev) != nil {
				return
			}
		case <-ping.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil || rc.Flush() != nil {
				return
			}
		}
	}
}
