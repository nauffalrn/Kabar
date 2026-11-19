package httpapi

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"article-service/internal/account"
	"article-service/internal/article"
)

const sessionCookie = "kabar_session"

type ctxKey struct{}

// withSession resolves the cookie once per request. Requests without a
// cookie (most reads) never touch the database here.
func (h *Handler) withSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookie)
		if err != nil || c.Value == "" {
			next.ServeHTTP(w, r)
			return
		}
		u, err := h.Accounts.Authenticate(r.Context(), c.Value)
		if err != nil {
			next.ServeHTTP(w, r) // expired or revoked: continue as a guest
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, u)))
	})
}

func currentUser(r *http.Request) (account.User, bool) {
	u, ok := r.Context().Value(ctxKey{}).(account.User)
	return u, ok
}

func actor(r *http.Request) *article.Actor {
	if u, ok := currentUser(r); ok {
		return u.Actor()
	}
	return nil
}

// withOriginCheck refuses state-changing requests sent from another site.
// Together with SameSite=Lax cookies and JSON-only bodies this blocks CSRF.
func withOriginCheck(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			if o := r.Header.Get("Origin"); o != "" {
				if u, err := url.Parse(o); err != nil || u.Host != r.Host {
					writeError(w, http.StatusForbidden, "forbidden_origin", "cross-site request refused")
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) setSession(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/",
		HttpOnly: true, Secure: h.SecureCookies, SameSite: http.SameSiteLaxMode,
		MaxAge: int(account.SessionTTL / time.Second),
	})
}

func (h *Handler) clearSession(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/",
		HttpOnly: true, Secure: h.SecureCookies, SameSite: http.SameSiteLaxMode, MaxAge: -1,
	})
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	u, token, err := h.Accounts.Register(r.Context(), req.Name, req.Email, req.Password)
	if err != nil {
		writeDomainError(w, r, h.log, err)
		return
	}
	h.setSession(w, token)
	writeJSON(w, http.StatusCreated, toUserResponse(u))
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	u, token, err := h.Accounts.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		writeDomainError(w, r, h.log, err)
		return
	}
	h.setSession(w, token)
	writeJSON(w, http.StatusOK, toUserResponse(u))
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		if err := h.Accounts.Logout(r.Context(), c.Value); err != nil {
			writeDomainError(w, r, h.log, err)
			return
		}
	}
	h.clearSession(w)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	u, ok := currentUser(r)
	if !ok {
		writeDomainError(w, r, h.log, article.ErrUnauthenticated)
		return
	}
	writeJSON(w, http.StatusOK, toUserResponse(u))
}
