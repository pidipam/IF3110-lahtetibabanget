package sessionapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/auth"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/authz"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/dbconn"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/httpapi"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/users"
)

type Handler struct {
	Users *users.Store
	Sessions *auth.SessionStore
	Auth *auth.Manager
	Authz *authz.Authorizer
	Conns *dbconn.Store
	Logger *slog.Logger
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type UserDTO struct {
	ID int64 `json:"id"`
	Username string `json:"username"`
}

type DatabaseDTO struct {
	ID int64 `json:"id"`
	Name string `json:"name"`
	Engine string `json:"engine"`
	Level string `json:"level"`
}

type MeResponse struct {
	User UserDTO `json:"user"`
	IsAdmin bool `json:"is_admin"`
	CSRFToken string `json:"csrf_token"`
	Databases []DatabaseDTO `json:"databases"`
}

type CSRFResponse struct {
	CSRFToken string `json:"csrf_token"`
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, requireLogin func(http.Handler) http.Handler) {
	mux.HandleFunc("GET /api/csrf", h.csrf)
	mux.HandleFunc("POST /api/login", h.login)
	mux.Handle("POST /api/logout", requireLogin(http.HandlerFunc(h.logout)))
	mux.Handle("GET /api/me", requireLogin(http.HandlerFunc(h.me)))
}

func (h *Handler) csrf(w http.ResponseWriter, r *http.Request) {
	httpapi.WriteJSON(w, http.StatusOK, CSRFResponse{CSRFToken: auth.CSRFToken(r.Context())})
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in loginRequest
	if err := httpapi.DecodeJSON(w, r, &in); err != nil {
		httpapi.Invalid(w, "Format permintaan tidak valid.", nil)
		return
	}
	username := strings.ToLower(strings.TrimSpace(in.Username))
	if username == "" || in.Password == "" {
		httpapi.Invalid(w, "Username dan password wajib diisi.", map[string]string{"username": "wajib", "password": "wajib"})
		return
	}
	u, err := h.Users.GetByUsername(ctx, username)
	if err != nil && !errors.Is(err, users.ErrNotFound) {
		h.fail(w, r, err)
		return
	}
	if err != nil {
		auth.BurnPasswordCheck(in.Password)
	}
	if err != nil || !auth.CheckPassword(u.PasswordHash, in.Password) || !u.IsActive {
		h.Logger.Info("login failed")
		httpapi.WriteError(w, http.StatusUnauthorized, "invalid_credentials", "Username atau password salah.", nil)
		return
	}
	if id := auth.FromContext(ctx); id.Session != nil {
		h.Sessions.Revoke(ctx, id.Session.ID)
	}
	token, sess, err := h.Sessions.Create(ctx, u.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.Auth.SetSessionCookie(w, token, sess.ExpiresAt)
	h.Logger.Info("login success", "user_id", u.ID)
	resp, err := h.meOf(ctx, u, sess.CSRFToken)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	id := auth.FromContext(r.Context())
	if err := h.Sessions.Revoke(r.Context(), id.Session.ID); err != nil {
		h.fail(w, r, err)
		return
	}
	h.Auth.ClearSessionCookie(w)
	h.Logger.Info("logout", "user_id", id.User.ID)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	id := auth.FromContext(r.Context())
	resp, err := h.meOf(r.Context(), id.User, id.CSRF)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) meOf(ctx context.Context, u *users.User, csrf string) (MeResponse, error) {
	resp := MeResponse{User: UserDTO{ID: u.ID, Username: u.Username}, IsAdmin: u.IsAdmin, CSRFToken: csrf, Databases: []DatabaseDTO{}}
	levels, err := h.Authz.AccessibleDatabases(ctx, authz.Principal{UserID: u.ID, IsAdmin: u.IsAdmin})
	if err != nil {
		return resp, err
	}
	ids := make([]int64, 0, len(levels))
	for dbID := range levels {
		ids = append(ids, dbID)
	}
	conns, err := h.Conns.ListByIDs(ctx, ids)
	if err != nil {
		return resp, err
	}
	for _, c := range conns {
		resp.Databases = append(resp.Databases, DatabaseDTO{ID: c.ID, Name: c.Name, Engine: c.Engine, Level: levels[c.ID].String()})
	}
	return resp, nil
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	h.Logger.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	httpapi.Internal(w)
}
