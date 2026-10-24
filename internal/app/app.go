package app

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/auth"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/authz"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/dbconn"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/users"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/web"
)

type Options struct {
	DB *pgxpool.Pool
	Session auth.SessionConfig
	Secure bool
	Logger *slog.Logger
	EncryptionKey []byte
}

type App struct {
	Users *users.Store
	Sessions *auth.SessionStore
	Authz *authz.Authorizer
	Auth *auth.Manager
	Render *web.Renderer
	Logger *slog.Logger
	Conns *dbconn.Store
	Manager *dbconn.Manager
	GrantStore *dbconn.GrantStore
	Grants *dbconn.GrantService
	mux *http.ServeMux
	handler http.Handler
}
var notices = map[string]string{"user_created": "User berhasil dibuat.", "user_updated": "User berhasil diperbarui.", "password_reset": "Password berhasil direset dan semua sesi user tersebut diakhiri.", "group_created": "Grup berhasil dibuat.", "group_updated": "Grup berhasil diperbarui.", "group_deleted": "Grup berhasil dihapus.", "member_added": "Anggota berhasil ditambahkan.", "member_removed": "Anggota berhasil dikeluarkan.", "connection_created": "Koneksi database berhasil dibuat.", "connection_updated": "Koneksi database berhasil diperbarui.", "connection_deleted": "Koneksi database berhasil dihapus.", "access_updated": "Akses database grup berhasil diperbarui."}

func New(opt Options) (*App, error) {
	rd, err := web.NewRenderer()
	if err != nil {
		return nil, err
	}
	logger := opt.Logger
	if logger == nil {
		logger = slog.Default()
	}
	a := &App{Users: users.NewStore(opt.DB), Sessions: auth.NewSessionStore(opt.DB, opt.Session), Authz: authz.New(authz.NewPGStore(opt.DB)), Render: rd, Logger: logger, mux: http.NewServeMux()}
	a.Auth = &auth.Manager{Sessions: a.Sessions, Users: a.Users, Secure: opt.Secure, Unauthorized: a.unauthorized, Forbidden: a.forbidden, Fail: a.fail}
	cipher, err := dbconn.NewCipher(opt.EncryptionKey)
	if err != nil {
		return nil, err
	}
	a.Conns = dbconn.NewStore(opt.DB, cipher)
	a.Manager = dbconn.NewManager(a.Conns)
	a.GrantStore = dbconn.NewGrantStore(opt.DB)
	a.Grants = &dbconn.GrantService{Repo: a.GrantStore, Sessions: a.Sessions}
	a.routes()
	a.handler = securityHeaders(a.logRequests(a.Auth.LoadSession(a.mux)))
	return a, nil
}

func (a *App) Close() {
	a.Manager.Close()
}

func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.handler.ServeHTTP(w, r)
}

func (a *App) Handle(pattern string, h http.Handler) {
	a.mux.Handle(pattern, h)
}

func (a *App) View(r *http.Request, title string, data map[string]any) web.View {
	id := auth.FromContext(r.Context())
	if data == nil {
		data = map[string]any{}
	}
	return web.View{Title: title, Nav: navOf(r.URL.Path), User: id.User, CSRF: id.CSRF, Notice: notices[r.URL.Query().Get("ok")], Data: data}
}

func navOf(path string) string {
	switch {
	case path == "/":
		return "home"
	case strings.HasPrefix(path, "/admin/users"):
		return "users"
	case strings.HasPrefix(path, "/admin/groups"):
		return "groups"
	case strings.HasPrefix(path, "/admin/connections"):
		return "connections"
	case strings.HasPrefix(path, "/databases"):
		return "databases"
	}
	return ""
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (a *App) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		a.Logger.Info("request", "method", r.Method, "path", r.URL.Path, "status", sw.status, "duration", time.Since(start))
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}