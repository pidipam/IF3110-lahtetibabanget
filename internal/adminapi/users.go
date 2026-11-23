package adminapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/auth"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/httpapi"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/users"
)

type createUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	IsAdmin bool `json:"is_admin"`
}

type updateUserRequest struct {
	Username *string `json:"username"`
	IsAdmin *bool `json:"is_admin"`
	IsActive *bool `json:"is_active"`
}

type passwordRequest struct {
	Password string `json:"password"`
}

func (h *Handler) listUsers(w http.ResponseWriter, r *http.Request) {
	p, ok := httpapi.ParsePage(w, r)
	if !ok {
		return
	}
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	list, total, err := h.Users.ListPage(r.Context(), q, p.PageSize, p.Offset())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, httpapi.NewPage(toUserDTOs(list), p, total))
}

func (h *Handler) createUser(w http.ResponseWriter, r *http.Request) {
	var in createUserRequest
	if err := httpapi.DecodeJSON(w, r, &in); err != nil {
		httpapi.Invalid(w, msgBadBody, nil)
		return
	}
	username := normalizeUsername(in.Username)
	details := map[string]string{}
	if !validUsername(username) {
		details["username"] = msgUsername
	}
	if msg := passwordProblem(in.Password); msg != "" {
		details["password"] = msg
	}
	if len(details) > 0 {
		httpapi.Invalid(w, "Data user tidak valid.", details)
		return
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	u, err := h.Users.Create(r.Context(), username, hash, in.IsAdmin)
	if errors.Is(err, users.ErrDuplicate) {
		conflict(w, "username", "Username sudah dipakai.")
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.Logger.Info("admin created user", "admin_id", actorID(r), "user_id", u.ID, "is_admin", u.IsAdmin)
	httpapi.WriteJSON(w, http.StatusCreated, toUserDTO(*u))
}

func (h *Handler) getUser(w http.ResponseWriter, r *http.Request) {
	u, ok := h.loadUser(w, r)
	if !ok {
		return
	}
	h.writeUserDetail(w, r, u)
}

func (h *Handler) writeUserDetail(w http.ResponseWriter, r *http.Request, u *users.User) {
	groups, err := h.Users.GroupsOfUser(r.Context(), u.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	out := UserDetail{UserDTO: toUserDTO(*u), Groups: make([]GroupRef, 0, len(groups))}
	for _, g := range groups {
		out.Groups = append(out.Groups, GroupRef{ID: g.ID, Name: g.Name})
	}
	httpapi.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) updateUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	target, ok := h.loadUser(w, r)
	if !ok {
		return
	}
	var in updateUserRequest
	if err := httpapi.DecodeJSON(w, r, &in); err != nil {
		httpapi.Invalid(w, msgBadBody, nil)
		return
	}
	username, isAdmin, isActive := target.Username, target.IsAdmin, target.IsActive
	if in.Username != nil {
		username = normalizeUsername(*in.Username)
	}
	if in.IsAdmin != nil {
		isAdmin = *in.IsAdmin
	}
	if in.IsActive != nil {
		isActive = *in.IsActive
	}
	if target.ID == actorID(r) && (!isAdmin || !isActive) {
		httpapi.WriteError(w, http.StatusUnprocessableEntity, "self_lockout", "Anda tidak bisa mencabut hak admin atau menonaktifkan akun sendiri.", nil)
		return
	}
	if !validUsername(username) {
		httpapi.Invalid(w, "Data user tidak valid.", map[string]string{"username": msgUsername})
		return
	}
	err := h.Users.Update(ctx, target.ID, username, isAdmin, isActive)
	if errors.Is(err, users.ErrDuplicate) {
		conflict(w, "username", "Username sudah dipakai.")
		return
	}
	if errors.Is(err, users.ErrNotFound) {
		httpapi.NotFound(w)
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	switch userEffect(target.IsAdmin, target.IsActive, isAdmin, isActive) {
	case effectRevoke:
		err = h.Sessions.RevokeUser(ctx, target.ID)
	case effectRotate:
		err = h.Sessions.MarkRotateUser(ctx, target.ID)
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.Logger.Info("admin updated user", "admin_id", actorID(r), "user_id", target.ID, "is_admin", isAdmin, "is_active", isActive)
	target.Username, target.IsAdmin, target.IsActive = username, isAdmin, isActive
	h.writeUserDetail(w, r, target)
}

func (h *Handler) resetPassword(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	target, ok := h.loadUser(w, r)
	if !ok {
		return
	}
	var in passwordRequest
	if err := httpapi.DecodeJSON(w, r, &in); err != nil {
		httpapi.Invalid(w, msgBadBody, nil)
		return
	}
	if msg := passwordProblem(in.Password); msg != "" {
		httpapi.Invalid(w, "Password tidak valid.", map[string]string{"password": msg})
		return
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if err := h.Users.SetPassword(ctx, target.ID, hash); err != nil {
		h.fail(w, r, err)
		return
	}
	if err := h.Sessions.RevokeUser(ctx, target.ID); err != nil {
		h.fail(w, r, err)
		return
	}
	h.Logger.Info("admin reset password", "admin_id", actorID(r), "user_id", target.ID)
	if target.ID == actorID(r) {
		h.Auth.ClearSessionCookie(w)
	}
	w.WriteHeader(http.StatusNoContent)
}
