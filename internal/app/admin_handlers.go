package app

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/auth"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/users"
)

const msgUsername = "Username harus 3-32 karakter berupa huruf kecil, angka, titik, garis bawah, atau strip."

func pathID(r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	return id, err == nil && id > 0
}

func (a *App) loadTarget(w http.ResponseWriter, r *http.Request) (*users.User, bool) {
	id, ok := pathID(r, "id")
	if !ok {
		a.notFound(w, r)
		return nil, false
	}
	u, err := a.Users.GetByID(r.Context(), id)
	if errors.Is(err, users.ErrNotFound) {
		a.notFound(w, r)
		return nil, false
	}
	if err != nil {
		a.fail(w, r, err)
		return nil, false
	}
	return u, true
}

func (a *App) adminUsers(w http.ResponseWriter, r *http.Request) {
	list, err := a.Users.List(r.Context())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.Render.Render(w, http.StatusOK, "admin_users", a.View(r, "Kelola User", map[string]any{"Users": list}))
}

func (a *App) adminUserNew(w http.ResponseWriter, r *http.Request) {
	a.Render.Render(w, http.StatusOK, "admin_user_new", a.View(r, "Tambah User", map[string]any{"Username": "", "IsAdmin": false}))
}

func (a *App) adminUserCreate(w http.ResponseWriter, r *http.Request) {
	username := normalizeUsername(r.PostFormValue("username"))
	password := r.PostFormValue("password")
	isAdmin := r.PostFormValue("is_admin") != ""
	msg := ""
	if !usernamePattern.MatchString(username) {
		msg = msgUsername
	} else if err := auth.ValidatePassword(password); err != nil {
		msg = "Password tidak valid: " + err.Error() + "."
	}
	if msg == "" {
		hash, err := auth.HashPassword(password)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		_, err = a.Users.Create(r.Context(), username, hash, isAdmin)
		if errors.Is(err, users.ErrDuplicate) {
			msg = "Username sudah dipakai."
		} else if err != nil {
			a.fail(w, r, err)
			return
		}
	}
	if msg != "" {
		v := a.View(r, "Tambah User", map[string]any{"Username": username, "IsAdmin": isAdmin})
		v.Error = msg
		a.Render.Render(w, http.StatusUnprocessableEntity, "admin_user_new", v)
		return
	}
	a.Logger.Info("admin created user", "admin_id", a.actorID(r), "username", username)
	http.Redirect(w, r, "/admin/users?ok=user_created", http.StatusSeeOther)
}

func (a *App) renderUserEdit(w http.ResponseWriter, r *http.Request, status int, target *users.User, msg string) {
	groups, err := a.Users.GroupsOfUser(r.Context(), target.ID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	v := a.View(r, "Edit User", map[string]any{"Target": target, "Groups": groups})
	v.Error = msg
	a.Render.Render(w, status, "admin_user_edit", v)
}

func (a *App) adminUserEdit(w http.ResponseWriter, r *http.Request) {
	target, ok := a.loadTarget(w, r)
	if !ok {
		return
	}
	a.renderUserEdit(w, r, http.StatusOK, target, "")
}

func (a *App) adminUserUpdate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	target, ok := a.loadTarget(w, r)
	if !ok {
		return
	}
	username := normalizeUsername(r.PostFormValue("username"))
	isAdmin := r.PostFormValue("is_admin") != ""
	isActive := r.PostFormValue("is_active") != ""
	if target.ID == a.actorID(r) && (!isAdmin || !isActive) {
		a.renderUserEdit(w, r, http.StatusUnprocessableEntity, target, "Anda tidak bisa mencabut hak admin atau menonaktifkan akun sendiri.")
		return
	}
	if !usernamePattern.MatchString(username) {
		a.renderUserEdit(w, r, http.StatusUnprocessableEntity, target, msgUsername)
		return
	}
	err := a.Users.Update(ctx, target.ID, username, isAdmin, isActive)
	if errors.Is(err, users.ErrDuplicate) {
		a.renderUserEdit(w, r, http.StatusUnprocessableEntity, target, "Username sudah dipakai.")
		return
	}
	if err != nil {
		a.fail(w, r, err)
		return
	}
	switch {
	case (target.IsActive && !isActive) || (target.IsAdmin && !isAdmin):
		err = a.Sessions.RevokeUser(ctx, target.ID)
	case !target.IsAdmin && isAdmin:
		err = a.Sessions.MarkRotateUser(ctx, target.ID)
	}
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.Logger.Info("admin updated user", "admin_id", a.actorID(r), "user_id", target.ID, "is_admin", isAdmin, "is_active", isActive)
	http.Redirect(w, r, fmt.Sprintf("/admin/users/%d?ok=user_updated", target.ID), http.StatusSeeOther)
}

func (a *App) adminUserPassword(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	target, ok := a.loadTarget(w, r)
	if !ok {
		return
	}
	password := r.PostFormValue("password")
	if err := auth.ValidatePassword(password); err != nil {
		a.renderUserEdit(w, r, http.StatusUnprocessableEntity, target, "Password tidak valid: "+err.Error()+".")
		return
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if err := a.Users.SetPassword(ctx, target.ID, hash); err != nil {
		a.fail(w, r, err)
		return
	}
	if err := a.Sessions.RevokeUser(ctx, target.ID); err != nil {
		a.fail(w, r, err)
		return
	}
	a.Logger.Info("admin reset password", "admin_id", a.actorID(r), "user_id", target.ID)
	if target.ID == a.actorID(r) {
		a.Auth.ClearSessionCookie(w)
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/admin/users/%d?ok=password_reset", target.ID), http.StatusSeeOther)
}

func (a *App) actorID(r *http.Request) int64 {
	if u, ok := auth.CurrentUser(r.Context()); ok {
		return u.ID
	}
	return 0
}

func (a *App) renderGroups(w http.ResponseWriter, r *http.Request, status int, name, description, msg string) {
	list, err := a.Users.ListGroups(r.Context())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	v := a.View(r, "Kelola Grup", map[string]any{"Groups": list, "Name": name, "Description": description})
	v.Error = msg
	a.Render.Render(w, status, "admin_groups", v)
}

func (a *App) adminGroups(w http.ResponseWriter, r *http.Request) {
	a.renderGroups(w, r, http.StatusOK, "", "", "")
}

func validGroupName(name string) bool {
	return name != "" && len(name) <= 64
}

func (a *App) adminGroupCreate(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PostFormValue("name"))
	description := strings.TrimSpace(r.PostFormValue("description"))
	if !validGroupName(name) {
		a.renderGroups(w, r, http.StatusUnprocessableEntity, name, description, "Nama grup wajib diisi, maksimal 64 karakter.")
		return
	}
	g, err := a.Users.CreateGroup(r.Context(), name, description)
	if errors.Is(err, users.ErrDuplicate) {
		a.renderGroups(w, r, http.StatusUnprocessableEntity, name, description, "Nama grup sudah dipakai.")
		return
	}
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.Logger.Info("admin created group", "admin_id", a.actorID(r), "group_id", g.ID)
	http.Redirect(w, r, fmt.Sprintf("/admin/groups/%d?ok=group_created", g.ID), http.StatusSeeOther)
}

func (a *App) loadGroup(w http.ResponseWriter, r *http.Request) (*users.Group, bool) {
	id, ok := pathID(r, "id")
	if !ok {
		a.notFound(w, r)
		return nil, false
	}
	g, err := a.Users.GetGroup(r.Context(), id)
	if errors.Is(err, users.ErrNotFound) {
		a.notFound(w, r)
		return nil, false
	}
	if err != nil {
		a.fail(w, r, err)
		return nil, false
	}
	return g, true
}

func (a *App) renderGroupEdit(w http.ResponseWriter, r *http.Request, status int, g *users.Group, msg string) {
	members, err := a.Users.Members(r.Context(), g.ID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	others, err := a.Users.NonMembers(r.Context(), g.ID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	v := a.View(r, "Kelola Grup", map[string]any{"Group": g, "Members": members, "NonMembers": others})
	v.Error = msg
	a.Render.Render(w, status, "admin_group_edit", v)
}

func (a *App) adminGroupEdit(w http.ResponseWriter, r *http.Request) {
	g, ok := a.loadGroup(w, r)
	if !ok {
		return
	}
	a.renderGroupEdit(w, r, http.StatusOK, g, "")
}

func (a *App) adminGroupUpdate(w http.ResponseWriter, r *http.Request) {
	g, ok := a.loadGroup(w, r)
	if !ok {
		return
	}
	name := strings.TrimSpace(r.PostFormValue("name"))
	description := strings.TrimSpace(r.PostFormValue("description"))
	if !validGroupName(name) {
		a.renderGroupEdit(w, r, http.StatusUnprocessableEntity, g, "Nama grup wajib diisi, maksimal 64 karakter.")
		return
	}
	err := a.Users.UpdateGroup(r.Context(), g.ID, name, description)
	if errors.Is(err, users.ErrDuplicate) {
		a.renderGroupEdit(w, r, http.StatusUnprocessableEntity, g, "Nama grup sudah dipakai.")
		return
	}
	if err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/admin/groups/%d?ok=group_updated", g.ID), http.StatusSeeOther)
}

func (a *App) adminGroupDelete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	g, ok := a.loadGroup(w, r)
	if !ok {
		return
	}
	if err := a.Sessions.RevokeGroup(ctx, g.ID); err != nil {
		a.fail(w, r, err)
		return
	}
	if err := a.Users.DeleteGroup(ctx, g.ID); err != nil {
		a.fail(w, r, err)
		return
	}
	a.Logger.Info("admin deleted group", "admin_id", a.actorID(r), "group_id", g.ID)
	http.Redirect(w, r, "/admin/groups?ok=group_deleted", http.StatusSeeOther)
}

func (a *App) adminGroupAddMember(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	g, ok := a.loadGroup(w, r)
	if !ok {
		return
	}
	userID, err := strconv.ParseInt(r.PostFormValue("user_id"), 10, 64)
	if err != nil {
		a.renderGroupEdit(w, r, http.StatusUnprocessableEntity, g, "Pilih user yang valid.")
		return
	}
	err = a.Users.AddMember(ctx, g.ID, userID)
	if errors.Is(err, users.ErrNotFound) {
		a.renderGroupEdit(w, r, http.StatusUnprocessableEntity, g, "User tidak ditemukan.")
		return
	}
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if err := a.Sessions.MarkRotateUser(ctx, userID); err != nil {
		a.fail(w, r, err)
		return
	}
	a.Logger.Info("admin added member", "admin_id", a.actorID(r), "group_id", g.ID, "user_id", userID)
	http.Redirect(w, r, fmt.Sprintf("/admin/groups/%d?ok=member_added", g.ID), http.StatusSeeOther)
}

func (a *App) adminGroupRemoveMember(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	g, ok := a.loadGroup(w, r)
	if !ok {
		return
	}
	userID, ok := pathID(r, "userID")
	if !ok {
		a.notFound(w, r)
		return
	}
	if err := a.Users.RemoveMember(ctx, g.ID, userID); err != nil {
		a.fail(w, r, err)
		return
	}
	if err := a.Sessions.RevokeUser(ctx, userID); err != nil {
		a.fail(w, r, err)
		return
	}
	a.Logger.Info("admin removed member", "admin_id", a.actorID(r), "group_id", g.ID, "user_id", userID)
	http.Redirect(w, r, fmt.Sprintf("/admin/groups/%d?ok=member_removed", g.ID), http.StatusSeeOther)
}
