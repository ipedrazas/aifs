package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/ipedrazas/aifs/api/internal/auth"
)

type sessionKey struct{}

// SessionFrom returns the session attached by the auth middleware.
func SessionFrom(ctx context.Context) (auth.Session, bool) {
	s, ok := ctx.Value(sessionKey{}).(auth.Session)
	return s, ok
}

type sessionHandlers struct {
	d          Deps
	cookieName string
}

func newSessionHandlers(d Deps) *sessionHandlers {
	name := "aifs_session"
	if d.CookieSecure {
		// The __Host- prefix makes the browser refuse the cookie unless it is
		// Secure, has Path=/ and no Domain.
		name = "__Host-aifs_session"
	}
	return &sessionHandlers{d: d, cookieName: name}
}

// LoginRequest is the body of POST /api/v1/session.
type LoginRequest struct {
	Password string `json:"password"`
}

// SessionResponse describes the current session.
type SessionResponse struct {
	Authenticated bool      `json:"authenticated"`
	ExpiresAt     time.Time `json:"expiresAt"`
}

func (h *sessionHandlers) login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil || req.Password == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "a password is required")
		return
	}

	token, sess, err := h.d.Auth.Login(r.Context(), clientIP(r), req.Password)
	var limited *auth.RateLimitedError
	switch {
	case errors.As(err, &limited):
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(limited.RetryAfter.Seconds()))))
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many login attempts; try again later")
		return
	case errors.Is(err, auth.ErrInvalidPassword):
		writeError(w, http.StatusUnauthorized, "invalid_password", "incorrect password")
		return
	case err != nil:
		h.d.Logger.ErrorContext(r.Context(), "login", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not create a session")
		return
	}

	http.SetCookie(w, h.cookie(token, sess.ExpiresAt))
	writeJSON(w, http.StatusOK, SessionResponse{Authenticated: true, ExpiresAt: sess.ExpiresAt.UTC()})
}

func (h *sessionHandlers) get(w http.ResponseWriter, r *http.Request) {
	sess, _ := SessionFrom(r.Context())
	writeJSON(w, http.StatusOK, SessionResponse{Authenticated: true, ExpiresAt: sess.ExpiresAt.UTC()})
}

func (h *sessionHandlers) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(h.cookieName); err == nil {
		if err := h.d.Auth.Logout(r.Context(), c.Value); err != nil {
			h.d.Logger.ErrorContext(r.Context(), "logout", "err", err)
			writeError(w, http.StatusInternalServerError, "internal", "could not end the session")
			return
		}
	}
	http.SetCookie(w, h.cookie("", time.Time{}))
	w.WriteHeader(http.StatusNoContent)
}

// require rejects requests without a live session with 401.
func (h *sessionHandlers) require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var token string
		if c, err := r.Cookie(h.cookieName); err == nil {
			token = c.Value
		}
		sess, err := h.d.Auth.Authenticate(r.Context(), token)
		if errors.Is(err, auth.ErrNoSession) {
			writeError(w, http.StatusUnauthorized, "unauthenticated", "log in to continue")
			return
		}
		if err != nil {
			h.d.Logger.ErrorContext(r.Context(), "authenticate", "err", err)
			writeError(w, http.StatusInternalServerError, "internal", "could not check the session")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessionKey{}, sess)))
	})
}

// cookie builds the session cookie; an empty token deletes it.
func (h *sessionHandlers) cookie(token string, expires time.Time) *http.Cookie {
	// Secure is on by default; operators may turn it off for plain-HTTP LAN
	// deployments (AIFS_COOKIE_SECURE=false).
	c := &http.Cookie{ //nolint:gosec // Secure is configurable, see above
		Name:     h.cookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   h.d.CookieSecure,
		SameSite: http.SameSiteStrictMode,
	}
	if token == "" {
		c.MaxAge = -1
	} else {
		c.Expires = expires
		c.MaxAge = int(time.Until(expires).Seconds())
	}
	return c
}

// clientIP keys login rate limiting. Behind a reverse proxy every client
// shares the proxy's address, which still limits the total attempt rate.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
