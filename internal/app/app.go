package app

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/auth"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/authz"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/migration"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/users"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/web"
)

type Options struct {
	DB               *pgxpool.Pool
	Session          auth.SessionConfig
	Secure           bool
	Logger           *slog.Logger
	Targets          migration.Targets
	StatementTimeout time.Duration
}

type App struct {
	Users         *users.Store
	Sessions      *auth.SessionStore
	Authz         *authz.Authorizer
	Auth          *auth.Manager
	Render        *web.Renderer
	Logger        *slog.Logger
	mux           *http.ServeMux
	handler       http.Handler
	Migrations    *migration.Service
	migrationExec *migration.Executor
}

var notices = map[string]string{"user_created": "User berhasil dibuat.", "user_updated": "User berhasil diperbarui.", "password_reset": "Password berhasil direset dan semua sesi user tersebut diakhiri.", "group_created": "Grup berhasil dibuat.", "group_updated": "Grup berhasil diperbarui.", "group_deleted": "Grup berhasil dihapus.", "member_added": "Anggota berhasil ditambahkan.", "member_removed": "Anggota berhasil dikeluarkan."}

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

	if opt.Targets != nil {
		repo := migration.NewRepository(opt.DB)
		a.migrationExec = migration.NewExecutor(opt.DB, repo, opt.Targets, opt.StatementTimeout)
		a.Migrations = migration.NewService(repo, a.migrationExec, a.Authz)
	}

	a.routes()
	a.handler = securityHeaders(a.logRequests(a.Auth.LoadSession(a.mux)))
	return a, nil
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

// RecoverMigrations menandai migration yang tertinggal berstatus running sebagai failed.
func (a *App) RecoverMigrations(ctx context.Context) (int64, error) {
	if a.migrationExec == nil {
		return 0, nil
	}
	return a.migrationExec.RecoverStuck(ctx)
}
