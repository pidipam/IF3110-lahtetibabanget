package migration

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrBusy = errors.New("ada migration lain yang sedang berjalan pada database ini, coba lagi sebentar")

const (
	DefaultStatementTimeout       = 30 * time.Second
	lockBase                int64 = 7_310_100_000_000 // dasar key advisory lock; ditambah db_id
)

type Executor struct {
	db      *pgxpool.Pool // platform DB
	repo    *Repository
	targets Targets
	Timeout time.Duration
}

func NewExecutor(db *pgxpool.Pool, repo *Repository, targets Targets, timeout time.Duration) *Executor {
	if timeout <= 0 {
		timeout = DefaultStatementTimeout
	}
	return &Executor{db: db, repo: repo, targets: targets, Timeout: timeout}
}

// Execute menjalankan satu draft. Hasil eksekusi yang gagal BUKAN error Go:
// migration dikembalikan dengan status failed dan error_message terisi.
// Error Go dikembalikan untuk masalah lain (bukan draft, sibuk, target mati, dsb).
func (e *Executor) Execute(ctx context.Context, dbID, id, userID int64) (*Migration, error) {
	m, err := e.repo.Get(ctx, dbID, id)
	if err != nil {
		return nil, err
	}
	if !m.Status.Editable() {
		return nil, ErrNotDraft
	}

	// 1. Lock per database di platform DB (berlaku juga bila ada lebih dari satu instance aplikasi).
	lock, err := e.db.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	key := lockBase + dbID
	var got bool
	if err := lock.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", key).Scan(&got); err != nil {
		lock.Release()
		return nil, err
	}
	if !got {
		lock.Release()
		return nil, ErrBusy
	}
	defer unlock(lock, key)

	// 2. Pastikan target bisa dihubungi SEBELUM nomor versi dipakai.
	sess, err := e.openSession(ctx, dbID)
	if err != nil {
		return nil, err
	}
	defer sess.Close()

	// 3. Tetapkan versi di bawah lock dan tandai running. SQL yang dijalankan
	//    diambil dari baris yang sama (RETURNING) agar sama persis dengan yang tercatat.
	var maxV int
	if err := lock.QueryRow(ctx, "SELECT COALESCE(MAX(version), 0) FROM migrations WHERE db_id = $1", dbID).Scan(&maxV); err != nil {
		return nil, err
	}
	var sqlText string
	err = lock.QueryRow(ctx, `UPDATE migrations SET status = 'running', version = $3, executed_by = $4,
		started_at = now(), updated_at = now(), error_message = ''
		WHERE id = $1 AND db_id = $2 AND status = 'draft' RETURNING sql_text`,
		id, dbID, NextVersion(maxV), userID).Scan(&sqlText)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotDraft
	}
	if err != nil {
		return nil, err
	}

	// 4. Eksekusi di transaction target: commit bila sukses, rollback bila gagal.
	execErr := sess.Run(ctx, sqlText, e.Timeout)
	status, msg := StatusSuccess, ""
	if execErr != nil {
		status, msg = StatusFailed, truncate(execErr.Error(), 2000)
	}

	// 5. Catat hasil di platform DB. Context tidak ikut batal bila browser terputus.
	_, err = lock.Exec(context.WithoutCancel(ctx), `UPDATE migrations SET status = $2, error_message = $3,
		finished_at = now(), updated_at = now() WHERE id = $1 AND status = 'running'`,
		id, string(status), msg)
	if err != nil {
		return nil, fmt.Errorf("hasil migration gagal dicatat: %w", err)
	}
	return e.repo.Get(context.WithoutCancel(ctx), dbID, id)
}

// RecoverStuck dipanggil saat startup: migration yang tertinggal berstatus
// running berarti proses berhenti di tengah jalan.
func (e *Executor) RecoverStuck(ctx context.Context) (int64, error) {
	tag, err := e.db.Exec(ctx, `UPDATE migrations SET status = 'failed', finished_at = now(), updated_at = now(),
		error_message = 'Eksekusi terputus sebelum hasil tercatat. Periksa kondisi database target.'
		WHERE status = 'running'`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// unlock melepas advisory lock. Bila gagal, koneksi diputus supaya lock
// tidak ikut kembali ke pool (lock sesi otomatis lepas saat koneksi putus).
func unlock(lock *pgxpool.Conn, key int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var ok bool
	if err := lock.QueryRow(ctx, "SELECT pg_advisory_unlock($1)", key).Scan(&ok); err != nil || !ok {
		lock.Hijack().Close(ctx)
		return
	}
	lock.Release()
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
