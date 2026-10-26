package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/shortdesk/shortdesk/internal/desk"
	"github.com/shortdesk/shortdesk/internal/logins"
)

func (s *Server) listLogins(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.logins.Saved())
}

// changeLogin handles POST /api/desks/{id}/logins/{kind}/{name}/{revoke|allow}.
func (s *Server) changeLogin(w http.ResponseWriter, r *http.Request) {
	d, ok := s.findDesk(w, r)
	if !ok {
		return
	}
	key := logins.Key{Kind: r.PathValue("kind"), Name: r.PathValue("name")}
	var err error
	switch r.PathValue("action") {
	case "revoke":
		err = s.logins.Revoke(r.Context(), d.ID, key)
	case "allow":
		err = s.logins.Allow(r.Context(), d.ID, key)
	default:
		writeError(w, http.StatusNotFound, fmt.Errorf("unknown action %q", r.PathValue("action")))
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// captureFrom saves new logins from one desk and gives them to the other desks.
func (s *Server) captureFrom(ctx context.Context, d desk.Desk) error {
	all, err := s.desks.List(ctx)
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(all))
	for _, o := range all {
		if o.State == "Running" || o.State == "running" {
			ids = append(ids, o.ID)
		}
	}
	return s.logins.Capture(ctx, d.ID, d.Name, ids)
}

// CaptureLogins looks for new logins on every running desk every interval, until ctx ends.
func (s *Server) CaptureLogins(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		desks, err := s.desks.List(ctx)
		if err != nil {
			s.log.Warn("login capture: list desks", "err", err)
			continue
		}
		for _, d := range desks {
			if d.State != "Running" && d.State != "running" {
				continue
			}
			c, cancel := context.WithTimeout(ctx, 2*time.Minute)
			if err := s.captureFrom(c, d); err != nil {
				s.log.Debug("login capture skipped desk", "desk", d.Name, "err", err)
			}
			cancel()
		}
	}
}
