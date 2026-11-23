package workflow

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// Locate mencari database pemilik sebuah migration (untuk rute /api/migrations/{id}).
func (s *Service) Locate(ctx context.Context, migID int64) (int64, error) {
	var db int64
	err := s.Repo.Pool().QueryRow(ctx, `SELECT database_id FROM platform_migrations WHERE id=$1`, migID).Scan(&db)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	return db, err
}

// LocateExecution mencari database dan migration pemilik sebuah eksekusi.
func (s *Service) LocateExecution(ctx context.Context, execID int64) (dbID, migID int64, err error) {
	err = s.Repo.Pool().QueryRow(ctx, `SELECT database_id, migration_id FROM platform_migration_executions WHERE id=$1`, execID).Scan(&dbID, &migID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, 0, ErrNotFound
	}
	return
}

// Usernames memetakan id pengguna ke username (untuk respons yang menampilkan nama).
func (s *Service) Usernames(ctx context.Context, ids ...int64) (map[int64]string, error) {
	out := map[int64]string{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.Repo.Pool().Query(ctx, `SELECT id, username FROM users WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[id] = name
	}
	return out, rows.Err()
}

// CanEdit: apakah pengguna boleh mengubah migration pada database ini (untuk tampilan).
func (s *Service) CanEdit(ctx context.Context, uid, dbID int64) bool {
	return s.check(ctx, uid, dbID, true) == nil
}

// ReleaseRejected dipanggil modul approval setelah menolak eksekusi: nomor urut dilepas
// bila migration belum pernah berjalan (sama seperti pembatalan).
func (s *Service) ReleaseRejected(ctx context.Context, execID int64) error {
	e, err := s.Repo.GetExecution(ctx, execID)
	if err != nil {
		return err
	}
	if e.Status == StatusRejected || e.Status == StatusCancelled {
		s.Repo.releaseOrderIfNeverRun(ctx, e)
	}
	return nil
}

// HTTPError memetakan error service ke (status, kode, pesan) untuk lapisan HTTP.
// ok=false berarti error tak dikenal (500 generik; jangan bocorkan detailnya).
func HTTPError(err error) (status int, code, msg string, ok bool) {
	switch {
	case errors.Is(err, ErrForbidden):
		return 403, "forbidden", "Anda tidak memiliki izin untuk aksi ini.", true
	case errors.Is(err, ErrNotFound):
		return 404, "not_found", "Sumber tidak ditemukan.", true
	case IsValidation(err):
		return 422, "validation_error", err.Error(), true
	case errors.Is(err, ErrImmutable), errors.Is(err, ErrConflict), errors.Is(err, ErrNotAllowed),
		errors.Is(err, ErrInvalidTransition), errors.Is(err, ErrNotNextInLine), errors.Is(err, ErrNotLastActive),
		errors.Is(err, ErrIrreversible), errors.Is(err, ErrNotActive), errors.Is(err, ErrAlreadyActive),
		errors.Is(err, ErrNotSubmitted), errors.Is(err, ErrForwardNeedsUndo):
		return 409, "invalid_status", err.Error(), true
	}
	return 500, "internal", "Terjadi kesalahan pada server.", false
}

// ValidationField menebak kolom form untuk galat validasi (untuk details 422).
func ValidationField(err error) string {
	switch {
	case errors.Is(err, ErrDownRequired):
		return "sql_down"
	case errors.Is(err, ErrReasonRequired):
		return "irreversible_reason"
	case errors.Is(err, ErrBadBatchColumn):
		return "batch_column"
	case errors.Is(err, ErrBadBatchSize):
		return "batch_size"
	case errors.Is(err, ErrBatchSQLShape), errors.Is(err, ErrBatchDownShape), errors.Is(err, ErrBatchNotReversible):
		return "sql_up"
	}
	msg := err.Error()
	if len(msg) >= 5 && msg[:5] == "judul" {
		return "title"
	}
	return "sql_up"
}
