package migration

import (
	"context"

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
