package server

import (
	"errors"
	"net/http"

	"github.com/shortdesk/shortdesk/internal/projects"
)

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	d, ok := s.findDesk(w, r)
	if !ok {
		return
	}
	list, err := s.projects.List(r.Context(), d.ID)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) projectDiff(w http.ResponseWriter, r *http.Request) {
	d, ok := s.findDesk(w, r)
	if !ok {
		return
	}
	diff, err := s.projects.Diff(r.Context(), d.ID, r.PathValue("name"))
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"diff": diff})
}

// mergeProject merges this desk's branch into the project's main branch, locally.
func (s *Server) mergeProject(w http.ResponseWriter, r *http.Request) {
	d, ok := s.findDesk(w, r)
	if !ok {
		return
	}
	err := s.projects.Merge(r.Context(), d.ID, r.PathValue("name"))
	switch {
	case errors.Is(err, projects.ErrConflict), errors.Is(err, projects.ErrNotClean):
		writeError(w, http.StatusConflict, err)
	case err != nil:
		writeError(w, http.StatusBadGateway, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
