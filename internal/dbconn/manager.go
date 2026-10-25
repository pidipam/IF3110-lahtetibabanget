package dbconn

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrManagerClosed = errors.New("manager koneksi sudah ditutup")

type Source interface {
	Credentials(ctx context.Context, id int64) (Params, error)
}

type Manager struct {
	src            Source
	connectTimeout time.Duration

	mu       sync.Mutex
	pools    map[int64]*pgxpool.Pool
	sqlPools map[int64]*sql.DB
	gen      map[int64]uint64
	closed   bool
}

func NewManager(src Source) *Manager {
	return &Manager{src: src, connectTimeout: 5 * time.Second, pools: map[int64]*pgxpool.Pool{}, sqlPools: map[int64]*sql.DB{}, gen: map[int64]uint64{}}
}

// Error yang mungkin:
// ErrNotFound (koneksi tidak ada), *ConnError yang cocok dengan ErrConnectionFailed, atau error internal
func (m *Manager) Get(ctx context.Context, id int64) (*pgxpool.Pool, error) {
	for attempt := 0; attempt < 3; attempt++ {
		m.mu.Lock()
		if m.closed {
			m.mu.Unlock()
			return nil, ErrManagerClosed
		}
		if p, ok := m.pools[id]; ok {
			m.mu.Unlock()
			return p, nil
		}
		gen := m.gen[id]
		m.mu.Unlock()

		params, err := m.src.Credentials(ctx, id)
		if err != nil {
			return nil, err
		}

		if IsMySQLFamily(params.Engine) {
			return nil, ErrWrongEngine
		}

		pool, err := m.open(ctx, params)
		if err != nil {
			return nil, err
		}

		m.mu.Lock()
		if m.closed {
			m.mu.Unlock()
			pool.Close()
			return nil, ErrManagerClosed
		}
		if existing, ok := m.pools[id]; ok {
			m.mu.Unlock()
			pool.Close()
			return existing, nil
		}
		if m.gen[id] != gen {
			// Koneksi diubah atau dihapus saat pool dibuat
			m.mu.Unlock()
			pool.Close()
			continue
		}
		m.pools[id] = pool
		m.mu.Unlock()
		return pool, nil
	}
	return nil, &ConnError{Kind: KindOther, err: errors.New("koneksi berubah berulang kali saat dibuka")}
}

func (m *Manager) GetSQL(ctx context.Context, id int64) (*sql.DB, error) {
	for attempt := 0; attempt < 3; attempt++ {
		m.mu.Lock()
		if m.closed {
			m.mu.Unlock()
			return nil, ErrManagerClosed
		}
		if db, ok := m.sqlPools[id]; ok {
			m.mu.Unlock()
			return db, nil
		}
		gen := m.gen[id]
		m.mu.Unlock()

		params, err := m.src.Credentials(ctx, id)
		if err != nil {
			return nil, err
		}
		if !IsMySQLFamily(params.Engine) {
			return nil, ErrWrongEngine
		}
		db, err := m.openSQL(ctx, params)
		if err != nil {
			return nil, err
		}

		m.mu.Lock()
		if m.closed {
			m.mu.Unlock()
			db.Close()
			return nil, ErrManagerClosed
		}
		if existing, ok := m.sqlPools[id]; ok {
			m.mu.Unlock()
			db.Close()
			return existing, nil
		}
		if m.gen[id] != gen {
			m.mu.Unlock()
			db.Close()
			continue
		}
		m.sqlPools[id] = db
		m.mu.Unlock()
		return db, nil
	}
	return nil, &ConnError{Kind: KindOther, err: errors.New("koneksi berubah berulang kali saat dibuka")}
}

func (m *Manager) openSQL(ctx context.Context, p Params) (*sql.DB, error) {
	db, err := sql.Open("mysql", p.MySQLDSN())
	if err != nil {
		return nil, &ConnError{Kind: KindOther, err: err}
	}
	db.SetMaxOpenConns(4)
	db.SetConnMaxLifetime(30 * time.Minute)
	db.SetConnMaxIdleTime(5 * time.Minute)
	pctx, cancel := context.WithTimeout(ctx, m.connectTimeout)
	defer cancel()
	if err := db.PingContext(pctx); err != nil {
		db.Close()
		return nil, asConnError(err)
	}
	return db, nil
}

func (m *Manager) open(ctx context.Context, p Params) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(p.DSN())
	if err != nil {
		return nil, &ConnError{Kind: KindOther, err: err}
	}
	cfg.MaxConns = 4
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.ConnConfig.ConnectTimeout = m.connectTimeout

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, asConnError(err)
	}
	pctx, cancel := context.WithTimeout(ctx, m.connectTimeout)
	defer cancel()
	if err := pool.Ping(pctx); err != nil {
		pool.Close()
		return nil, asConnError(err)
	}
	return pool, nil
}

func asConnError(err error) error {
	if ce, ok := AsConnError(err); ok {
		return ce
	}
	kind, _ := classify(err)
	return &ConnError{Kind: kind, err: err}
}

func (m *Manager) Test(ctx context.Context, p Params) error {
	if IsMySQLFamily(p.Engine) {
		return m.testSQL(ctx, p)
	}
	cfg, err := pgx.ParseConfig(p.DSN())
	if err != nil {
		return &ConnError{Kind: KindOther, err: err}
	}
	cfg.ConnectTimeout = m.connectTimeout
	tctx, cancel := context.WithTimeout(ctx, m.connectTimeout)
	defer cancel()
	conn, err := pgx.ConnectConfig(tctx, cfg)
	if err != nil {
		return asConnError(err)
	}
	defer conn.Close(context.Background())
	if err := conn.Ping(tctx); err != nil {
		return asConnError(err)
	}
	return nil
}

func (m *Manager) testSQL(ctx context.Context, p Params) error {
	db, err := sql.Open("mysql", p.MySQLDSN())
	if err != nil {
		return &ConnError{Kind: KindOther, err: err}
	}
	defer db.Close()
	tctx, cancel := context.WithTimeout(ctx, m.connectTimeout)
	defer cancel()
	if err := db.PingContext(tctx); err != nil {
		return asConnError(err)
	}
	return nil
}

func (m *Manager) Invalidate(id int64) {
	m.mu.Lock()
	m.gen[id]++
	p := m.pools[id]
	db := m.sqlPools[id]
	delete(m.pools, id)
	delete(m.sqlPools, id)
	m.mu.Unlock()
	if p != nil {
		go p.Close()
	}
	if db != nil {
		go db.Close()
	}
}

func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	pools := m.pools
	sqlPools := m.sqlPools
	m.pools = map[int64]*pgxpool.Pool{}
	m.sqlPools = map[int64]*sql.DB{}
	m.mu.Unlock()
	for _, p := range pools {
		p.Close()
	}
	for _, db := range sqlPools {
		db.Close()
	}
}
