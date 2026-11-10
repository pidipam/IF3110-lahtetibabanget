package adminapi

import (
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/auth"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/authz"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/dbconn"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/httpapi"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/users"
)

const MaxGroupName = 64

const MaxGroupDescription = 500

const msgUsername = "Username harus 3-32 karakter berupa huruf kecil, angka, titik, garis bawah, atau strip."

const msgGroupName = "Nama grup wajib diisi, maksimal 64 karakter."

const msgGroupDescription = "Deskripsi grup maksimal 500 karakter."

const msgBadBody = "Format permintaan tidak valid."

var usernamePattern = regexp.MustCompile(`^[a-z0-9_.-]{3,32}$`)

type Handler struct {
	Users *users.Store
	Sessions *auth.SessionStore
	Auth *auth.Manager
	Grants *dbconn.GrantService
	GrantStore *dbconn.GrantStore
	Conns *dbconn.Store
	Logger *slog.Logger
}

type UserDTO struct {
	ID int64 `json:"id"`
	Username string `json:"username"`
	IsAdmin bool `json:"is_admin"`
	IsActive bool `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
}

type GroupRef struct {
	ID int64 `json:"id"`
	Name string `json:"name"`
}

type UserDetail struct {
	UserDTO
	Groups []GroupRef `json:"groups"`
}

type GroupDTO struct {
	ID int64 `json:"id"`
	Name string `json:"name"`
	Description string `json:"description"`
	MemberCount int `json:"member_count"`
	CreatedAt time.Time `json:"created_at"`
}

type AccessDTO struct {
	DBID int64 `json:"db_id"`
	Name string `json:"name"`
	Engine string `json:"engine"`
	Level string `json:"level"`
}

type GroupDetail struct {
	GroupDTO
	Members []UserDTO `json:"members"`
	Access []AccessDTO `json:"access"`
}

type sessionEffect int

const (
	effectNone sessionEffect = iota
	effectRotate
	effectRevoke
)

func (h *Handler) RegisterRoutes(mux *http.ServeMux, requireAdmin func(http.Handler) http.Handler) {
	handle := func(pattern string, fn http.HandlerFunc) { mux.Handle(pattern, requireAdmin(fn)) }
	handle("GET /api/admin/users", h.listUsers)
	handle("POST /api/admin/users", h.createUser)
	handle("GET /api/admin/users/{id}", h.getUser)
	handle("PATCH /api/admin/users/{id}", h.updateUser)
	handle("POST /api/admin/users/{id}/password", h.resetPassword)
	handle("GET /api/admin/groups", h.listGroups)
	handle("POST /api/admin/groups", h.createGroup)
	handle("GET /api/admin/groups/{id}", h.getGroup)
	handle("PATCH /api/admin/groups/{id}", h.updateGroup)
	handle("DELETE /api/admin/groups/{id}", h.deleteGroup)
	handle("GET /api/admin/groups/{id}/candidates", h.candidates)
	handle("POST /api/admin/groups/{id}/members", h.addMember)
	handle("DELETE /api/admin/groups/{id}/members/{userID}", h.removeMember)
	handle("GET /api/admin/groups/{id}/access", h.getAccess)
	handle("PUT /api/admin/groups/{id}/access", h.putAccess)
}

func toUserDTO(u users.User) UserDTO {
	return UserDTO{ID: u.ID, Username: u.Username, IsAdmin: u.IsAdmin, IsActive: u.IsActive, CreatedAt: u.CreatedAt.UTC()}
}

func toGroupDTO(g users.Group) GroupDTO {
	return GroupDTO{ID: g.ID, Name: g.Name, Description: g.Description, MemberCount: g.MemberCount, CreatedAt: g.CreatedAt.UTC()}
}

func toUserDTOs(list []users.User) []UserDTO {
	out := make([]UserDTO, 0, len(list))
	for _, u := range list {
		out = append(out, toUserDTO(u))
	}
	return out
}

func normalizeUsername(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func validUsername(s string) bool {
	return usernamePattern.MatchString(s)
}

func passwordProblem(password string) string {
	if err := auth.ValidatePassword(password); err != nil {
		return "Password tidak valid: " + err.Error() + "."
	}
	return ""
}

func groupNameProblem(name string) string {
	if name == "" || len([]rune(name)) > MaxGroupName {
		return msgGroupName
	}
	return ""
}

func groupDescriptionProblem(description string) string {
	if len([]rune(description)) > MaxGroupDescription {
		return msgGroupDescription
	}
	return ""
}

func userEffect(wasAdmin, wasActive, isAdmin, isActive bool) sessionEffect {
	switch {
	case (wasActive && !isActive) || (wasAdmin && !isAdmin):
		return effectRevoke
	case !wasAdmin && isAdmin:
		return effectRotate
	}
	return effectNone
}

func parseLevel(s string) (authz.Level, bool) {
	switch s {
	case "none":
		return authz.LevelNone, true
	case "view":
		return authz.LevelView, true
	case "edit":
		return authz.LevelEdit, true
	}
	return authz.LevelNone, false
}

func actorID(r *http.Request) int64 {
	if u, ok := auth.CurrentUser(r.Context()); ok {
		return u.ID
	}
	return 0
}

func conflict(w http.ResponseWriter, field, message string) {
	httpapi.WriteError(w, http.StatusConflict, "conflict", message, map[string]string{field: message})
}

func (h *Handler) loadUser(w http.ResponseWriter, r *http.Request) (*users.User, bool) {
	id, ok := httpapi.PathID(r, "id")
	if !ok {
		httpapi.NotFound(w)
		return nil, false
	}
	u, err := h.Users.GetByID(r.Context(), id)
	if errors.Is(err, users.ErrNotFound) {
		httpapi.NotFound(w)
		return nil, false
	}
	if err != nil {
		h.fail(w, r, err)
		return nil, false
	}
	return u, true
}

func (h *Handler) loadGroup(w http.ResponseWriter, r *http.Request) (*users.Group, bool) {
	id, ok := httpapi.PathID(r, "id")
	if !ok {
		httpapi.NotFound(w)
		return nil, false
	}
	g, err := h.Users.GetGroup(r.Context(), id)
	if errors.Is(err, users.ErrNotFound) {
		httpapi.NotFound(w)
		return nil, false
	}
	if err != nil {
		h.fail(w, r, err)
		return nil, false
	}
	return g, true
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	h.Logger.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	httpapi.Internal(w)
}
