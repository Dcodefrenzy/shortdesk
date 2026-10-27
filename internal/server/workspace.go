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
}

func (s *Server) getWorkspace(w http.ResponseWriter, _ *http.Request) {
	own := s.workspace.OwnModels()
	if own == nil {
		own = []string{}
	}
	r := s.workspace.Router()
	if r.Models == nil {
		r.Models = []string{}
	}
	writeJSON(w, http.StatusOK, workspaceJSON{Connections: s.workspace.Status(), Router: r, OwnModels: own})
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
