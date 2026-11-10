package adminapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/authz"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/dbconn"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/httpapi"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/users"
)

type createGroupRequest struct {
	Name string `json:"name"`
	Description string `json:"description"`
}

type updateGroupRequest struct {
	Name *string `json:"name"`
	Description *string `json:"description"`
}

type memberRequest struct {
	UserID int64 `json:"user_id"`
}

type accessItem struct {
	DBID int64 `json:"db_id"`
	Level string `json:"level"`
}

type accessRequest struct {
	Access []accessItem `json:"access"`
}

type accessChange struct {
	DBID int64
	Level authz.Level
}

func groupProblems(name, description string) map[string]string {
	details := map[string]string{}
	if msg := groupNameProblem(name); msg != "" {
		details["name"] = msg
	}
	if msg := groupDescriptionProblem(description); msg != "" {
		details["description"] = msg
	}
	return details
}

func validateAccess(items []accessItem, known map[int64]bool) ([]accessChange, map[string]string) {
	details := map[string]string{}
	if len(items) == 0 {
		details["access"] = "Isi minimal satu perubahan akses."
		return nil, details
	}
	seen := map[int64]bool{}
	changes := make([]accessChange, 0, len(items))
	for i, it := range items {
		key := fmt.Sprintf("access[%d]", i)
		level, ok := parseLevel(it.Level)
		switch {
		case !known[it.DBID]:
			details[key+".db_id"] = "Database tidak ditemukan."
		case seen[it.DBID]:
			details[key+".db_id"] = "Database disebut lebih dari sekali."
		case !ok:
			details[key+".level"] = "Level harus none, view, atau edit."
		}
		seen[it.DBID] = true
		changes = append(changes, accessChange{DBID: it.DBID, Level: level})
	}
	if len(details) > 0 {
		return nil, details
	}
	return changes, nil
}

func (h *Handler) listGroups(w http.ResponseWriter, r *http.Request) {
	p, ok := httpapi.ParsePage(w, r)
	if !ok {
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	list, total, err := h.Users.ListGroupsPage(r.Context(), q, p.PageSize, p.Offset())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	items := make([]GroupDTO, 0, len(list))
	for _, g := range list {
		items = append(items, toGroupDTO(g))
	}
	httpapi.WriteJSON(w, http.StatusOK, httpapi.NewPage(items, p, total))
}

func (h *Handler) createGroup(w http.ResponseWriter, r *http.Request) {
	var in createGroupRequest
	if err := httpapi.DecodeJSON(w, r, &in); err != nil {
		httpapi.Invalid(w, msgBadBody, nil)
		return
	}
	name, description := strings.TrimSpace(in.Name), strings.TrimSpace(in.Description)
	if details := groupProblems(name, description); len(details) > 0 {
		httpapi.Invalid(w, "Data grup tidak valid.", details)
		return
	}
	g, err := h.Users.CreateGroup(r.Context(), name, description)
	if errors.Is(err, users.ErrDuplicate) {
		conflict(w, "name", "Nama grup sudah dipakai.")
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.Logger.Info("admin created group", "admin_id", actorID(r), "group_id", g.ID)
	httpapi.WriteJSON(w, http.StatusCreated, toGroupDTO(*g))
}

func (h *Handler) getGroup(w http.ResponseWriter, r *http.Request) {
	g, ok := h.loadGroup(w, r)
	if !ok {
		return
	}
	h.writeGroupDetail(w, r, g)
}

func (h *Handler) writeGroupDetail(w http.ResponseWriter, r *http.Request, g *users.Group) {
	members, err := h.Users.Members(r.Context(), g.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	access, err := h.accessOf(r.Context(), g.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	g.MemberCount = len(members)
	httpapi.WriteJSON(w, http.StatusOK, GroupDetail{GroupDTO: toGroupDTO(*g), Members: toUserDTOs(members), Access: access})
}

func (h *Handler) updateGroup(w http.ResponseWriter, r *http.Request) {
	g, ok := h.loadGroup(w, r)
	if !ok {
		return
	}
	var in updateGroupRequest
	if err := httpapi.DecodeJSON(w, r, &in); err != nil {
		httpapi.Invalid(w, msgBadBody, nil)
		return
	}
	name, description := g.Name, g.Description
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
	}
	if in.Description != nil {
		description = strings.TrimSpace(*in.Description)
	}
	if details := groupProblems(name, description); len(details) > 0 {
		httpapi.Invalid(w, "Data grup tidak valid.", details)
		return
	}
	err := h.Users.UpdateGroup(r.Context(), g.ID, name, description)
	if errors.Is(err, users.ErrDuplicate) {
		conflict(w, "name", "Nama grup sudah dipakai.")
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
	h.Logger.Info("admin updated group", "admin_id", actorID(r), "group_id", g.ID)
	g.Name, g.Description = name, description
	h.writeGroupDetail(w, r, g)
}

func (h *Handler) deleteGroup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	g, ok := h.loadGroup(w, r)
	if !ok {
		return
	}
	if err := h.Sessions.RevokeGroup(ctx, g.ID); err != nil {
		h.fail(w, r, err)
		return
	}
	if err := h.Users.DeleteGroup(ctx, g.ID); err != nil && !errors.Is(err, users.ErrNotFound) {
		h.fail(w, r, err)
		return
	}
	h.Logger.Info("admin deleted group", "admin_id", actorID(r), "group_id", g.ID)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) candidates(w http.ResponseWriter, r *http.Request) {
	g, ok := h.loadGroup(w, r)
	if !ok {
		return
	}
	list, err := h.Users.NonMembers(r.Context(), g.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string][]UserDTO{"items": toUserDTOs(list)})
}

func (h *Handler) addMember(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	g, ok := h.loadGroup(w, r)
	if !ok {
		return
	}
	var in memberRequest
	if err := httpapi.DecodeJSON(w, r, &in); err != nil {
		httpapi.Invalid(w, msgBadBody, nil)
		return
	}
	u, err := h.Users.GetByID(ctx, in.UserID)
	if errors.Is(err, users.ErrNotFound) {
		httpapi.Invalid(w, "User tidak valid.", map[string]string{"user_id": "User tidak ditemukan."})
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if !u.IsActive {
		httpapi.Invalid(w, "User tidak valid.", map[string]string{"user_id": "User nonaktif tidak bisa ditambahkan ke grup."})
		return
	}
	err = h.Users.AddMember(ctx, g.ID, u.ID)
	if errors.Is(err, users.ErrNotFound) {
		httpapi.NotFound(w)
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if err := h.Sessions.MarkRotateUser(ctx, u.ID); err != nil {
		h.fail(w, r, err)
		return
	}
	h.Logger.Info("admin added member", "admin_id", actorID(r), "group_id", g.ID, "user_id", u.ID)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) removeMember(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	g, ok := h.loadGroup(w, r)
	if !ok {
		return
	}
	userID, ok := httpapi.PathID(r, "userID")
	if !ok {
		httpapi.NotFound(w)
		return
	}
	removed, err := h.Users.DeleteMember(ctx, g.ID, userID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if !removed {
		httpapi.NotFound(w)
		return
	}
	if err := h.Sessions.RevokeUser(ctx, userID); err != nil {
		h.fail(w, r, err)
		return
	}
	h.Logger.Info("admin removed member", "admin_id", actorID(r), "group_id", g.ID, "user_id", userID)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) accessOf(ctx context.Context, groupID int64) ([]AccessDTO, error) {
	conns, err := h.Conns.List(ctx)
	if err != nil {
		return nil, err
	}
	grants, err := h.GrantStore.ListByGroup(ctx, groupID)
	if err != nil {
		return nil, err
	}
	levels := map[int64]authz.Level{}
	for _, gr := range grants {
		levels[gr.ConnID] = gr.Level
	}
	out := make([]AccessDTO, 0, len(conns))
	for _, c := range conns {
		out = append(out, AccessDTO{DBID: c.ID, Name: c.Name, Engine: c.Engine, Level: levels[c.ID].String()})
	}
	return out, nil
}

func (h *Handler) getAccess(w http.ResponseWriter, r *http.Request) {
	g, ok := h.loadGroup(w, r)
	if !ok {
		return
	}
	access, err := h.accessOf(r.Context(), g.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string][]AccessDTO{"items": access})
}

func (h *Handler) putAccess(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	g, ok := h.loadGroup(w, r)
	if !ok {
		return
	}
	var in accessRequest
	if err := httpapi.DecodeJSON(w, r, &in); err != nil {
		httpapi.Invalid(w, msgBadBody, nil)
		return
	}
	conns, err := h.Conns.List(ctx)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	known := map[int64]bool{}
	for _, c := range conns {
		known[c.ID] = true
	}
	changes, details := validateAccess(in.Access, known)
	if details != nil {
		httpapi.Invalid(w, "Perubahan akses tidak valid.", details)
		return
	}
	for _, c := range changes {
		if c.Level == authz.LevelNone {
			err = h.Grants.RemoveAccess(ctx, g.ID, c.DBID)
		} else {
			err = h.Grants.SetAccess(ctx, g.ID, c.DBID, c.Level)
		}
		if errors.Is(err, dbconn.ErrNotFound) {
			httpapi.Invalid(w, "Perubahan akses tidak valid.", map[string]string{"access": "Database atau grup sudah tidak ada."})
			return
		}
		if err != nil {
			h.fail(w, r, err)
			return
		}
		h.Logger.Info("admin set group access", "admin_id", actorID(r), "group_id", g.ID, "db_id", c.DBID, "level", c.Level.String())
	}
	access, err := h.accessOf(ctx, g.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string][]AccessDTO{"items": access})
}
