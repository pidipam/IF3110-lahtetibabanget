package dbconn

import (
	"context"
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

	mu     sync.Mutex
	pools  map[int64]*pgxpool.Pool
	gen    map[int64]uint64
	closed bool
}

func NewManager(src Source) *Manager {
	return &Manager{src: src, connectTimeout: 5 * time.Second, pools: map[int64]*pgxpool.Pool{}, gen: map[int64]uint64{}}
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

func (m *Manager) Invalidate(id int64) {
	m.mu.Lock()
	m.gen[id]++
	p := m.pools[id]
	delete(m.pools, id)
	m.mu.Unlock()
	if p != nil {
		go p.Close()
	}
}

func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	pools := m.pools
	m.pools = map[int64]*pgxpool.Pool{}
	m.mu.Unlock()
	for _, p := range pools {
		p.Close()
	}
}
