package server

import (
	"errors"
	"net/http"
	"time"

	"github.com/shortdesk/shortdesk/internal/access"
	"github.com/shortdesk/shortdesk/internal/charter"
	"github.com/shortdesk/shortdesk/internal/desk"
	"github.com/shortdesk/shortdesk/internal/logins"
)

type sharedJSON struct {
	Volume string `json:"volume"`
	Path   string `json:"path"`
}

type settingsJSON struct {
	Desk    deskJSON     `json:"desk"`
	Charter *charterJSON `json:"charter,omitempty"`
	Network access.Rules `json:"network"`
	Apps    []desk.App   `json:"apps"`
	// Shared are the storage volumes shared into every new desk.
	Shared []sharedJSON `json:"shared"`
	// Logins are the saved logins and their status on this desk.
	Logins []logins.DeskLogin `json:"logins"`
	// Integrations are not configurable yet; the list is empty until the catalog exists.
	Integrations []string `json:"integrations"`
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	d, ok := s.findDesk(w, r)
	if !ok {
		return
	}
	out := settingsJSON{Desk: s.toJSON(d), Apps: desk.ImageApps, Integrations: []string{}, Shared: []sharedJSON{}}
	for _, m := range s.desks.Shared() {
		out.Shared = append(out.Shared, sharedJSON{Volume: m.Volume, Path: m.Path})
	}
	if charters, err := charter.LoadDir(s.cfg.ChartersDir); err == nil {
		if c, ok := charters[d.Charter]; ok {
			cj := toCharterJSON(c)
			out.Charter = &cj
		}
	}
	rules, err := s.access.Rules(d)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	out.Network = rules
	if out.Logins, err = s.logins.Desk(r.Context(), d.ID); err != nil {
		s.log.Warn("could not read desk logins", "desk", d.Name, "err", err)
		out.Logins = []logins.DeskLogin{}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) addNetworkRule(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Host string `json:"host"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	d, ok := s.findDesk(w, r)
	if !ok {
		return
	}
	if err := s.access.Add(r.Context(), d, req.Host, access.SourceSettings); err != nil {
		writeError(w, accessStatus(err), err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) removeNetworkRule(w http.ResponseWriter, r *http.Request) {
	d, ok := s.findDesk(w, r)
	if !ok {
		return
	}
	if err := s.access.Remove(r.Context(), d, r.PathValue("host")); err != nil {
		writeError(w, accessStatus(err), err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// decideAccess answers an agent's access request: {"decision": "desk"|"charter"|"deny"}.
func (s *Server) decideAccess(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Decision access.Decision `json:"decision"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	d, ok := s.findDesk(w, r)
	if !ok {
		return
	}
	if err := s.access.Decide(r.Context(), d, r.PathValue("requestId"), req.Decision); err != nil {
		writeError(w, accessStatus(err), err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func accessStatus(err error) int {
	switch {
	case errors.Is(err, access.ErrNotEditable):
		return http.StatusConflict
	case errors.Is(err, access.ErrUnknownRequest):
		return http.StatusNotFound
	default:
		return http.StatusBadRequest
	}
}

func toCharterJSON(c *charter.Charter) charterJSON {
	return charterJSON{
		Name: c.Name, Tenant: c.Tenant, Version: c.Version, Image: c.Desk.Image,
		CPU: c.Desk.Resources.CPU, Memory: c.Desk.Resources.Memory,
		Lifetime:     time.Duration(c.Limits.DeskLifetime).String(),
		AllowedHosts: c.Permissions.Network.Allow,
	}
}
