package server

import (
	"errors"
	"net/http"

	"github.com/shortdesk/shortdesk/internal/vault"
)

func (s *Server) vaultStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.vault.Status(r.Context()))
}

// connectVault logs Shortdesk into the owner's Vaultwarden account. The master password
// is stored write-only and used only to unlock the vault.
func (s *Server) connectVault(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.vault.Connect(r.Context(), req.Email, req.Password); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) disconnectVault(w http.ResponseWriter, r *http.Request) {
	if err := s.vault.Disconnect(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listIntegrations(w http.ResponseWriter, r *http.Request) {
	list, err := s.vault.List(r.Context())
	if err != nil {
		writeError(w, vaultStatusCode(err), err)
		return
	}
	if list == nil {
		list = []vault.Integration{}
	}
	writeJSON(w, http.StatusOK, list)
}

// addIntegration stores a login in the vault. The password and 2FA secret are never
// returned by any endpoint.
func (s *Server) addIntegration(w http.ResponseWriter, r *http.Request) {
	var req vault.NewIntegration
	if !decodeJSON(w, r, &req) {
		return
	}
	in, err := s.vault.Add(r.Context(), req)
	if err != nil {
		writeError(w, vaultStatusCode(err), err)
		return
	}
	writeJSON(w, http.StatusCreated, in)
}

func (s *Server) deleteIntegration(w http.ResponseWriter, r *http.Request) {
	if err := s.vault.Delete(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, vaultStatusCode(err), err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func vaultStatusCode(err error) int {
	if errors.Is(err, vault.ErrNotConnected) {
		return http.StatusConflict
	}
	return http.StatusBadGateway
}
