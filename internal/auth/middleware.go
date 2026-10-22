package auth

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/authz"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/users"
)

const SessionCookie = "dbmp_session"

const PreCSRFCookie = "dbmp_csrf"

type ctxKey struct{}

type Identity struct {
	User *users.User
	Session *Session
	CSRF string
	Expired bool
}

type Manager struct {
	Sessions *SessionStore
	Users *users.Store
	Secure bool
	Unauthorized func(w http.ResponseWriter, r *http.Request, expired bool)
	Forbidden func(w http.ResponseWriter, r *http.Request)
	Fail func(w http.ResponseWriter, r *http.Request, err error)
}

func FromContext(ctx context.Context) *Identity {
	if id, ok := ctx.Value(ctxKey{}).(*Identity); ok {
		return id
	}
	return &Identity{}
}

func CurrentUser(ctx context.Context) (*users.User, bool) {
	id := FromContext(ctx)
	return id.User, id.User != nil
}

func CurrentPrincipal(ctx context.Context) (authz.Principal, bool) {
	u, ok := CurrentUser(ctx)
	if !ok {
		return authz.Principal{}, false
	}
	return authz.Principal{UserID: u.ID, IsAdmin: u.IsAdmin}, true
}

func CSRFToken(ctx context.Context) string {
	return FromContext(ctx).CSRF
}

func (m *Manager) SetSessionCookie(w http.ResponseWriter, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{Name: SessionCookie, Value: token, Path: "/", Expires: expires, HttpOnly: true, Secure: m.Secure, SameSite: http.SameSiteLaxMode})
}

func (m *Manager) ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: SessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: m.Secure, SameSite: http.SameSiteLaxMode})
}

func (m *Manager) LoadSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		id := &Identity{}
		if c, err := r.Cookie(SessionCookie); err == nil && c.Value != "" {
			if err := m.resolve(ctx, w, c.Value, id); err != nil {
				m.Fail(w, r, err)
				return
			}
		}
		if id.User == nil {
			token, err := m.preCSRF(w, r)
			if err != nil {
				m.Fail(w, r, err)
				return
			}
			id.CSRF = token
		}
		ctx = context.WithValue(ctx, ctxKey{}, id)
		r = r.WithContext(ctx)
		if !IsSafeMethod(r.Method) && !ValidCSRF(id.CSRF, r) {
			m.Forbidden(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (m *Manager) resolve(ctx context.Context, w http.ResponseWriter, token string, id *Identity) error {
	sess, err := m.Sessions.Lookup(ctx, token)
	if errors.Is(err, ErrSessionExpired) || errors.Is(err, ErrNoSession) {
		m.ClearSessionCookie(w)
		id.Expired = true
		return nil
	}
	if err != nil {
		return err
	}
	u, err := m.Users.GetByID(ctx, sess.UserID)
	if err != nil && !errors.Is(err, users.ErrNotFound) {
		return err
	}
	if err != nil || !u.IsActive {
		m.Sessions.Revoke(ctx, sess.ID)
		m.ClearSessionCookie(w)
		id.Expired = true
		return nil
	}
	if NeedsRotation(sess, m.Sessions.Now(), m.Sessions.Config()) {
		fresh, ok, err := m.Sessions.Rotate(ctx, sess, token)
		if err != nil {
			return err
		}
		if ok {
			m.SetSessionCookie(w, fresh, sess.ExpiresAt)
		}
	}
	if err := m.Sessions.Touch(ctx, sess.ID); err != nil {
		return err
	}
	id.User = u
	id.Session = sess
	id.CSRF = sess.CSRFToken
	return nil
}

func (m *Manager) preCSRF(w http.ResponseWriter, r *http.Request) (string, error) {
	if c, err := r.Cookie(PreCSRFCookie); err == nil && len(c.Value) == 43 {
		return c.Value, nil
	}
	token, err := NewToken()
	if err != nil {
		return "", err
	}
	http.SetCookie(w, &http.Cookie{Name: PreCSRFCookie, Value: token, Path: "/", HttpOnly: true, Secure: m.Secure, SameSite: http.SameSiteStrictMode})
	return token, nil
}

func (m *Manager) RequireLogin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := FromContext(r.Context())
		if id.User == nil {
			m.Unauthorized(w, r, id.Expired)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (m *Manager) RequireAdmin(next http.Handler) http.Handler {
	return m.RequireLogin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !FromContext(r.Context()).User.IsAdmin {
			m.Forbidden(w, r)
			return
		}
		next.ServeHTTP(w, r)
	}))
}