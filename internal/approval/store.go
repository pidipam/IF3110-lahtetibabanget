package approval

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/authz"
)

type querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

const executionColumns = "e.id, e.migration_id, e.database_id, m.title, c.name, e.revision_number, m.current_revision, e.kind, e.status, e.requested_by, r.username, e.reject_reason, e.created_at"

const executionFrom = " FROM platform_migration_executions e JOIN platform_migrations m ON m.id = e.migration_id JOIN db_connections c ON c.id = e.database_id JOIN users r ON r.id = e.requested_by "

func scanExecution(row pgx.Row) (*Execution, error) {
	e := &Execution{}
	err := row.Scan(&e.ID, &e.MigrationID, &e.DatabaseID, &e.Title, &e.DatabaseName, &e.RevisionNumber, &e.CurrentRevision, &e.Kind, &e.Status, &e.RequestedBy, &e.RequestedByName, &e.RejectReason, &e.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return e, err
}

func scanExecutions(rows pgx.Rows, err error) ([]Execution, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Execution
	for rows.Next() {
		e, err := scanExecution(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

func loadExecution(ctx context.Context, q querier, id int64) (*Execution, error) {
	return scanExecution(q.QueryRow(ctx, "SELECT "+executionColumns+executionFrom+"WHERE e.id = $1", id))
}

func lockPending(ctx context.Context, q querier, migrationID int64) (*Execution, error) {
	return scanExecution(q.QueryRow(ctx, "SELECT "+executionColumns+executionFrom+"WHERE e.migration_id = $1 AND e.status = 'pending_review' FOR UPDATE OF e", migrationID))
}

func latestExecution(ctx context.Context, q querier, migrationID int64) (*Execution, error) {
	return scanExecution(q.QueryRow(ctx, "SELECT "+executionColumns+executionFrom+"WHERE e.migration_id = $1 ORDER BY e.id DESC LIMIT 1", migrationID))
}

func migrationExists(ctx context.Context, q querier, migrationID int64) (bool, error) {
	var ok bool
	err := q.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM platform_migrations WHERE id = $1)", migrationID).Scan(&ok)
	return ok, err
}

func loadGroups(ctx context.Context, q querier, dbID int64) ([]Group, error) {
	rows, err := q.Query(ctx, "SELECT g.id, g.name, a.level, COALESCE(g.leader_id, 0), COALESCE(u.username, ''), COALESCE(u.is_active, FALSE) FROM group_db_access a JOIN groups g ON g.id = a.group_id LEFT JOIN users u ON u.id = g.leader_id WHERE a.db_id = $1 ORDER BY g.name, g.id", dbID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Group
	for rows.Next() {
		var g Group
		var level string
		if err := rows.Scan(&g.ID, &g.Name, &level, &g.LeaderID, &g.LeaderName, &g.LeaderActive); err != nil {
			return nil, err
		}
		g.Level = authz.ParseLevel(level)
		out = append(out, g)
	}
	return out, rows.Err()
}

func loadVotes(ctx context.Context, q querier, execID int64) ([]Vote, error) {
	rows, err := q.Query(ctx, "SELECT v.group_id, v.revision_number, v.approver_id, u.username, v.decision, v.reason, v.decided_at FROM migration_approvals v JOIN users u ON u.id = v.approver_id WHERE v.execution_id = $1 ORDER BY v.decided_at, v.id", execID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Vote
	for rows.Next() {
		var v Vote
		var d string
		if err := rows.Scan(&v.GroupID, &v.RevisionNumber, &v.ApproverID, &v.ApproverName, &d, &v.Reason, &v.DecidedAt); err != nil {
			return nil, err
		}
		v.Decision = Decision(d)
		out = append(out, v)
	}
	return out, rows.Err()
}

func evaluateWith(ctx context.Context, q querier, exec *Execution) (Status, error) {
	groups, err := loadGroups(ctx, q, exec.DatabaseID)
	if err != nil {
		return Status{}, err
	}
	votes, err := loadVotes(ctx, q, exec.ID)
	if err != nil {
		return Status{}, err
	}
	return Evaluate(*exec, groups, votes), nil
}

func insertVote(ctx context.Context, q querier, exec *Execution, groupID, approverID int64, d Decision, reason string) error {
	_, err := q.Exec(ctx, "INSERT INTO migration_approvals (execution_id, group_id, revision_number, approver_id, decision, reason) VALUES ($1, $2, $3, $4, $5, $6)", exec.ID, groupID, exec.RevisionNumber, approverID, string(d), reason)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrAlreadyDecided
	}
	return err
}

func setStatus(ctx context.Context, q querier, execID int64, from []string, to, reason string) (bool, error) {
	tag, err := q.Exec(ctx, "UPDATE platform_migration_executions SET status = $2, reject_reason = CASE WHEN $2 = 'rejected' THEN $3 ELSE reject_reason END, updated_at = now() WHERE id = $1 AND status = ANY($4)", execID, to, reason, from)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

const pendingFrom = executionFrom + "WHERE e.status = 'pending_review' AND "

const mineFilter = "e.requested_by <> $1 AND (e.kind = 'rollback' OR e.revision_number = m.current_revision) AND EXISTS (SELECT 1 FROM groups g JOIN group_db_access a ON a.group_id = g.id JOIN users u ON u.id = g.leader_id WHERE a.db_id = e.database_id AND g.leader_id = $1 AND u.is_active AND NOT EXISTS (SELECT 1 FROM migration_approvals v WHERE v.execution_id = e.id AND v.group_id = g.id AND v.revision_number = e.revision_number))"

const visibleFilter = "EXISTS (SELECT 1 FROM group_db_access a JOIN group_members gm ON gm.group_id = a.group_id JOIN users u ON u.id = gm.user_id WHERE a.db_id = e.database_id AND gm.user_id = $1 AND u.is_active)"

func listPending(ctx context.Context, q querier, userID int64, mine bool, limit, offset int) ([]Execution, int, error) {
	filter := visibleFilter
	if mine {
		filter = mineFilter
	}
	var total int
	if err := q.QueryRow(ctx, "SELECT count(*)"+pendingFrom+filter, userID).Scan(&total); err != nil {
		return nil, 0, err
	}
	list, err := scanExecutions(q.Query(ctx, "SELECT "+executionColumns+pendingFrom+filter+" ORDER BY e.created_at, e.id LIMIT $2 OFFSET $3", userID, limit, offset))
	return list, total, err
}
