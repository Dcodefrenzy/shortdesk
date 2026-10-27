package server

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
)

// loginLinkTTL is how long a one-time login link works.
const loginLinkTTL = 10 * time.Minute

// loginCodes are one-time codes for login links, so `shortdesk up` can open the
// dashboard already logged in without putting the admin token in a URL.
type loginCodes struct {
	mu    sync.Mutex
	codes map[string]time.Time // code -> expiry
}

func (c *loginCodes) issue() string {
	b := make([]byte, 24)
	rand.Read(b)
	code := hex.EncodeToString(b)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.codes == nil {
		c.codes = map[string]time.Time{}
	}
	now := time.Now()
	for k, exp := range c.codes {
		if now.After(exp) {
			delete(c.codes, k)
		}
	}
	c.codes[code] = now.Add(loginLinkTTL)
	return code
}

// redeem reports whether code is valid, and uses it up.
func (c *loginCodes) redeem(code string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	exp, ok := c.codes[code]
	delete(c.codes, code)
	return ok && time.Now().Before(exp)
}

// LoginLink returns a path that logs the browser in once, within loginLinkTTL.
func (s *Server) LoginLink() string { return "/login/link?code=" + s.codes.issue() }

// loginWithLink handles a login link: a valid code sets the session, anything else
// goes to the login page.
func (s *Server) loginWithLink(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if !s.codes.redeem(r.URL.Query().Get("code")) {
		http.Redirect(w, r, "/login/?link=expired", http.StatusSeeOther)
		return
	}
	s.setSession(w)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

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
	s.setSession(w)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) setSession(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: s.sessionValue(), Path: "/",
		HttpOnly: true, Secure: s.cfg.SecureCookies, SameSite: http.SameSiteLaxMode,
		MaxAge: 30 * 24 * 3600,
	})
}

func (s *Server) logout(w http.ResponseWriter, _ *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: s.cfg.SecureCookies, SameSite: http.SameSiteLaxMode,
	})
	w.WriteHeader(http.StatusNoContent)
}
