package app

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/approval"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/auth"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/authz"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/dbconn"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/httpapi"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/migration"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/migration/exec"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/migration/spaapi"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/migration/workflow"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/users"
	webui "github.com/pidipam/IF3110-lahtetibabanget/web"
)

type Options struct {
	DB            *pgxpool.Pool
	Session       auth.SessionConfig
	Secure        bool
	Logger        *slog.Logger
	EncryptionKey []byte
	Queue         workflow.Queue // antrean Vara; nil = antrean lokal sementara
}

type App struct {
	Users         *users.Store
	Sessions      *auth.SessionStore
	Authz         *authz.Authorizer
	Auth          *auth.Manager
	Logger        *slog.Logger
	Conns         *dbconn.Store
	Manager       *dbconn.Manager
	GrantStore    *dbconn.GrantStore
	Grants        *dbconn.GrantService
	mux           *http.ServeMux
	handler       http.Handler
	migrationExec *migration.Executor
	Approvals *approval.Service
	Workflow      *workflow.Service
	wfHandler     *exec.Handler
	wfAPI         *spaapi.Handler
}

func New(opt Options) (*App, error) {
	logger := opt.Logger
	if logger == nil {
		logger = slog.Default()
	}
	a := &App{Users: users.NewStore(opt.DB), Sessions: auth.NewSessionStore(opt.DB, opt.Session), Authz: authz.New(authz.NewPGStore(opt.DB)), Logger: logger, mux: http.NewServeMux()}
	a.Auth = &auth.Manager{Sessions: a.Sessions, Users: a.Users, Secure: opt.Secure, Unauthorized: a.unauthorized, Forbidden: a.forbidden, Fail: a.fail}
	cipher, err := dbconn.NewCipher(opt.EncryptionKey)
	if err != nil {
		return nil, err
	}
	a.Conns = dbconn.NewStore(opt.DB, cipher)
	a.Manager = dbconn.NewManager(a.Conns)
	a.GrantStore = dbconn.NewGrantStore(opt.DB)
	a.Grants = &dbconn.GrantService{Repo: a.GrantStore, Sessions: a.Sessions}
	repo := migration.NewRepository(opt.DB)
	targets := managerTargets{
		db:     opt.DB,
		get:    func(ctx context.Context, id int64) (*pgxpool.Pool, error) { return a.Manager.Get(ctx, id) },
		getSQL: a.Manager.GetSQL,
	}
	a.migrationExec = migration.NewExecutor(opt.DB, repo, targets, 0)
	a.Approvals = approval.NewService(opt.DB, a.Authz)
	a.initWorkflow(opt, targets)
	a.routes()
	a.handler = securityHeaders(a.logRequests(withSPA(webui.Handler(), a.Auth.LoadSession(a.mux))))
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
		a.Logger.Info("request", "method", r.Method, "path", httpapi.LogPath(r), "status", sw.status, "duration", time.Since(start))
	})
}

func withSPA(spa, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		safe := r.Method == http.MethodGet || r.Method == http.MethodHead
		if safe && r.URL.Path == strings.TrimSuffix(webui.Base, "/") {
			http.Redirect(w, r, webui.Base, http.StatusMovedPermanently)
			return
		}
		if safe && strings.HasPrefix(r.URL.Path, webui.Base) {
			spa.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
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
