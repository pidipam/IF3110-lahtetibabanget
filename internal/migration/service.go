package migration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/authz"
)

// Authorizer dipenuhi oleh *authz.Authorizer
type Authorizer interface {
	Can(ctx context.Context, p authz.Principal, dbID int64, action authz.Action) error
}

type Service struct {
	repo  *Repository
	exec  *Executor
	authz Authorizer
}

func NewService(repo *Repository, exec *Executor, az Authorizer) *Service {
	return &Service{repo: repo, exec: exec, authz: az}
}

// TargetName menampilkan nama database di halaman. Butuh hak lihat database.
func (s *Service) TargetName(ctx context.Context, p authz.Principal, dbID int64) (string, error) {
	if err := s.authz.Can(ctx, p, dbID, authz.ViewDatabase); err != nil {
		return "", err
	}
	return s.exec.targets.Name(ctx, dbID)
}

// List dan Get: View Only dan View + Edit boleh.
func (s *Service) List(ctx context.Context, p authz.Principal, dbID int64) ([]Migration, error) {
	if err := s.authz.Can(ctx, p, dbID, authz.ViewMigration); err != nil {
		return nil, err
	}
	return s.repo.List(ctx, dbID)
}

func (s *Service) Get(ctx context.Context, p authz.Principal, dbID, id int64) (*Migration, error) {
	if err := s.authz.Can(ctx, p, dbID, authz.ViewMigration); err != nil {
		return nil, err
	}
	return s.repo.Get(ctx, dbID, id)
}

// Create, Update, Execute: hanya View + Edit.
func (s *Service) Create(ctx context.Context, p authz.Principal, dbID int64, title, sql string) (*Migration, error) {
	if err := s.authz.Can(ctx, p, dbID, authz.EditMigration); err != nil {
		return nil, err
	}
	title, sql, err := validateInput(title, sql)
	if err != nil {
		return nil, err
	}
	return s.repo.Create(ctx, dbID, p.UserID, title, sql)
}

func (s *Service) Update(ctx context.Context, p authz.Principal, dbID, id int64, title, sql string) error {
	if err := s.authz.Can(ctx, p, dbID, authz.EditMigration); err != nil {
		return err
	}
	title, sql, err := validateInput(title, sql)
	if err != nil {
		return err
	}
	return s.repo.UpdateDraft(ctx, dbID, id, title, sql)
}

func (s *Service) Execute(ctx context.Context, p authz.Principal, dbID, id int64) (*Migration, error) {
	if err := s.authz.Can(ctx, p, dbID, authz.EditMigration); err != nil {
		return nil, err
	}
	return s.exec.Execute(ctx, dbID, id, p.UserID)
}

func validateInput(title, sql string) (string, string, error) {
	title, err := ValidateTitle(title)
	if err != nil {
		return "", "", err
	}
	sql, err = ValidateSQL(sql)
	if err != nil {
		return "", "", err
	}
	return title, sql, nil
}

// CanEdit hanya untuk menentukan tampilan; keputusan akses tetap diambil di Create/Update/Execute.
func (s *Service) CanEdit(ctx context.Context, p authz.Principal, dbID int64) bool {
	return s.authz.Can(ctx, p, dbID, authz.EditMigration) == nil
}

var ErrChanged = errors.New("SQL berubah sejak ditinjau, tinjau ulang sebelum menjalankan")

// SQLChecksum dipakai halaman review untuk memastikan SQL yang dijalankan sama dengan yang ditinjau.
func SQLChecksum(sql string) string {
	sum := sha256.Sum256([]byte(sql))
	return hex.EncodeToString(sum[:])
}

type Review struct {
	M           *Migration
	NextVersion int
	Checksum    string
}

// Review menyiapkan data halaman tinjauan. Hanya View + Edit, hanya untuk draft.
func (s *Service) Review(ctx context.Context, p authz.Principal, dbID, id int64) (*Review, error) {
	if err := s.authz.Can(ctx, p, dbID, authz.EditMigration); err != nil {
		return nil, err
	}
	m, err := s.repo.Get(ctx, dbID, id)
	if err != nil {
		return nil, err
	}
	if !m.Status.Editable() {
		return nil, ErrNotDraft
	}
	max, err := s.repo.MaxVersion(ctx, dbID)
	if err != nil {
		return nil, err
	}
	return &Review{M: m, NextVersion: NextVersion(max), Checksum: SQLChecksum(m.SQL)}, nil
}

// ExecuteReviewed menjalankan draft hanya bila SQL-nya masih sama dengan yang ditinjau.
func (s *Service) ExecuteReviewed(ctx context.Context, p authz.Principal, dbID, id int64, checksum string) (*Migration, error) {
	if err := s.authz.Can(ctx, p, dbID, authz.EditMigration); err != nil {
		return nil, err
	}
	m, err := s.repo.Get(ctx, dbID, id)
	if err != nil {
		return nil, err
	}
	if SQLChecksum(m.SQL) != checksum {
		return nil, ErrChanged
	}
	return s.exec.Execute(ctx, dbID, id, p.UserID)
}
