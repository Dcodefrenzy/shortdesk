package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
)

const sessionCookie = "shortdesk_session"

// requireAuth lets a request through with a valid session cookie or
// "Authorization: Bearer <token>".
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.authenticated(r) {
			writeError(w, http.StatusUnauthorized, errors.New("login required"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) authenticated(r *http.Request) bool {
	if bearer, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		return s.validToken(bearer)
	}
	c, err := r.Cookie(sessionCookie)
	return err == nil && subtle.ConstantTimeCompare([]byte(c.Value), []byte(s.sessionValue())) == 1
}

func (s *Server) validToken(t string) bool {
	return s.cfg.Token != "" && subtle.ConstantTimeCompare([]byte(t), []byte(s.cfg.Token)) == 1
}

// sessionValue is what the cookie holds: a hash of the token, so the token itself
// is never stored in the browser.
func (s *Server) sessionValue() string {
	sum := sha256.Sum256([]byte("shortdesk-session:" + s.cfg.Token))
	return hex.EncodeToString(sum[:])
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if !s.validToken(req.Token) {
		writeError(w, http.StatusUnauthorized, errors.New("wrong token"))
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: s.sessionValue(), Path: "/",
		HttpOnly: true, Secure: s.cfg.SecureCookies, SameSite: http.SameSiteLaxMode,
		MaxAge: 30 * 24 * 3600,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) logout(w http.ResponseWriter, _ *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: s.cfg.SecureCookies, SameSite: http.SameSiteLaxMode,
	})
	w.WriteHeader(http.StatusNoContent)
}
