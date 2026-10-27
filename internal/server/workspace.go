package server

import (
	"context"
	"net/http"

	"github.com/shortdesk/shortdesk/internal/workspace"
)

type workspaceJSON struct {
	Connections []workspace.ConnectionStatus `json:"connections"`
	Router      workspace.Router             `json:"router"`
	// OwnModels is the order actually used after Claude (the router's list or defaults).
	OwnModels []string `json:"ownModels"`
	// MainDesk is the desk the owner chats with: the one picked, or else the oldest
	// desk; empty when there are no desks.
	MainDesk string `json:"mainDesk"`
}

func (s *Server) getWorkspace(w http.ResponseWriter, req *http.Request) {
	own := s.workspace.OwnModels()
	if own == nil {
		own = []string{}
	}
	r := s.workspace.Router()
	if r.Models == nil {
		r.Models = []string{}
	}
	writeJSON(w, http.StatusOK, workspaceJSON{Connections: s.workspace.Status(), Router: r, OwnModels: own,
		MainDesk: s.mainDesk(req.Context())})
}

// mainDesk returns the desk the owner chats with: the one they picked while it exists,
// otherwise the oldest desk.
func (s *Server) mainDesk(ctx context.Context) string {
	desks, err := s.desks.List(ctx)
	if err != nil || len(desks) == 0 {
		return ""
	}
	picked := s.workspace.MainDesk()
	oldest := desks[0]
	for _, d := range desks {
		if d.ID == picked {
			return d.ID
		}
		if d.CreatedAt.Before(oldest.CreatedAt) {
			oldest = d
		}
	}
	return oldest.ID
}

// setMainDesk picks the desk the owner chats with: {"id": "<desk id>"}.
func (s *Server) setMainDesk(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	d, err := s.desks.Find(r.Context(), req.ID)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if err := s.workspace.SetMainDesk(d.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) setRouter(w http.ResponseWriter, r *http.Request) {
	var req workspace.Router
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.workspace.SetRouter(req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.modelsChanged(r.Context())
	w.WriteHeader(http.StatusNoContent)
}

// setConnection stores a connection's secret; an empty value removes it. Secrets are
// never returned by the API.
func (s *Server) setConnection(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Value string `json:"value"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.workspace.SetSecret(r.PathValue("name"), req.Value); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.modelsChanged(r.Context())
	w.WriteHeader(http.StatusNoContent)
}

// RefreshDesks re-applies every running desk's website rules, e.g. at startup for desks
// created before the router's models changed.
func (s *Server) RefreshDesks(ctx context.Context) { s.modelsChanged(ctx) }

// modelsChanged lets running desks reach the router's models as they are now. A desk
// that can't be updated keeps its old rules; the change is logged, not fatal.
func (s *Server) modelsChanged(ctx context.Context) {
	if err := s.access.ApplyAll(ctx); err != nil {
		s.log.Warn("could not update desks' website rules for the router's models", "err", err)
	}
}
