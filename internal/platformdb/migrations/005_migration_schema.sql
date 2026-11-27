-- id memakai BIGINT agar cocok dengan users(id) dan db_connections(id).

CREATE TABLE platform_migrations (
	id BIGSERIAL PRIMARY KEY,
	database_id BIGINT NOT NULL REFERENCES db_connections(id) ON DELETE RESTRICT,
	title TEXT NOT NULL CHECK (char_length(title) BETWEEN 1 AND 100),
	linear_order INT CHECK (linear_order > 0),          -- NULL sampai diajukan
	current_revision INT NOT NULL DEFAULT 1,
	created_by BIGINT NOT NULL REFERENCES users(id),
	created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	UNIQUE (database_id, linear_order)
);

CREATE TABLE platform_migration_revisions (
	migration_id BIGINT NOT NULL REFERENCES platform_migrations(id) ON DELETE RESTRICT,
	revision_number INT NOT NULL,
	sql_up TEXT NOT NULL,
	sql_down TEXT NOT NULL DEFAULT '',
	is_irreversible BOOLEAN NOT NULL DEFAULT FALSE,
	irreversible_reason TEXT NOT NULL DEFAULT '',
	is_batch_mode BOOLEAN NOT NULL DEFAULT FALSE,
	batch_column TEXT NOT NULL DEFAULT '',
	batch_size INT NOT NULL DEFAULT 0,
	checksum CHAR(64) NOT NULL,
	created_by BIGINT NOT NULL REFERENCES users(id),
	created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	PRIMARY KEY (migration_id, revision_number),
	CHECK (is_irreversible OR sql_down <> ''),
	CHECK (NOT is_irreversible OR irreversible_reason <> ''),
	CHECK (NOT is_batch_mode OR (batch_column ~ '^[a-z_][a-z0-9_]{0,62}$' AND batch_size BETWEEN 1 AND 100000))
);

CREATE FUNCTION platform_forbid_mutation() RETURNS trigger AS $$
BEGIN
	RAISE EXCEPTION '% bersifat append-only', TG_TABLE_NAME USING ERRCODE = 'restrict_violation';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER platform_migration_revisions_append_only
	BEFORE UPDATE OR DELETE ON platform_migration_revisions
	FOR EACH ROW EXECUTE FUNCTION platform_forbid_mutation();

CREATE TABLE platform_migration_executions (
	id BIGSERIAL PRIMARY KEY,                            -- execution ID
	migration_id BIGINT NOT NULL REFERENCES platform_migrations(id) ON DELETE RESTRICT,
	database_id BIGINT NOT NULL REFERENCES db_connections(id) ON DELETE RESTRICT,
	revision_number INT NOT NULL,
	kind TEXT NOT NULL CHECK (kind IN ('up', 'rollback', 'forward')),
	status TEXT NOT NULL CHECK (status IN ('pending_review', 'approved', 'queued', 'running', 'success',
		'failed', 'partial', 'uncertain', 'partially_reverted', 'rejected', 'cancelled')),
	undoes_execution_id BIGINT REFERENCES platform_migration_executions(id),
	requested_by BIGINT NOT NULL REFERENCES users(id),
	executed_sql TEXT NOT NULL DEFAULT '',               -- salinan SQL yang benar-benar dijalankan
	attempt INT NOT NULL DEFAULT 0,
	job_id TEXT NOT NULL DEFAULT '',
	reject_reason TEXT NOT NULL DEFAULT '',
	error_message TEXT NOT NULL DEFAULT '',
	batches_committed INT NOT NULL DEFAULT 0,
	rows_committed BIGINT NOT NULL DEFAULT 0,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	queued_at TIMESTAMPTZ, started_at TIMESTAMPTZ, finished_at TIMESTAMPTZ,
	FOREIGN KEY (migration_id, revision_number) REFERENCES platform_migration_revisions (migration_id, revision_number),
	CHECK ((kind = 'rollback') = (undoes_execution_id IS NOT NULL))
);

-- Satu database hanya menjalankan satu migration; yang lain menunggu di antrean.
CREATE UNIQUE INDEX one_busy_execution_per_database ON platform_migration_executions (database_id)
	WHERE status IN ('running', 'partial', 'uncertain', 'partially_reverted');
-- Satu migration hanya punya satu permintaan terbuka pada satu waktu.
CREATE UNIQUE INDEX one_open_execution_per_migration ON platform_migration_executions (migration_id)
	WHERE status IN ('pending_review', 'approved', 'queued', 'running', 'partial', 'uncertain', 'partially_reverted');
CREATE INDEX executions_database_idx ON platform_migration_executions (database_id, id DESC);

CREATE TABLE platform_migration_history (              -- hanya commit yang berhasil
	execution_id BIGINT PRIMARY KEY REFERENCES platform_migration_executions(id),
	migration_id BIGINT NOT NULL REFERENCES platform_migrations(id),
	database_id BIGINT NOT NULL REFERENCES db_connections(id),
	revision_number INT NOT NULL,
	execution_type TEXT NOT NULL CHECK (execution_type IN ('up', 'rollback', 'forward')),
	status TEXT NOT NULL DEFAULT 'success' CHECK (status = 'success'),
	executed_by BIGINT NOT NULL REFERENCES users(id),
	executed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	sql_executed TEXT NOT NULL,
	reverted_execution_id BIGINT REFERENCES platform_migration_history(execution_id),
	FOREIGN KEY (migration_id, revision_number) REFERENCES platform_migration_revisions (migration_id, revision_number),
	CHECK ((execution_type = 'rollback') = (reverted_execution_id IS NOT NULL))
);
CREATE UNIQUE INDEX history_revert_once ON platform_migration_history (reverted_execution_id)
	WHERE reverted_execution_id IS NOT NULL;           -- satu eksekusi hanya dibatalkan sekali
CREATE INDEX history_database_idx ON platform_migration_history (database_id, executed_at);

CREATE TRIGGER platform_migration_history_append_only
	BEFORE UPDATE OR DELETE ON platform_migration_history
	FOR EACH ROW EXECUTE FUNCTION platform_forbid_mutation();

CREATE TABLE platform_database_locks (
	database_id BIGINT PRIMARY KEY REFERENCES db_connections(id) ON DELETE CASCADE,
	execution_id BIGINT NOT NULL REFERENCES platform_migration_executions(id),
	worker_id TEXT NOT NULL,
	acquired_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	heartbeat_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	lease_until TIMESTAMPTZ NOT NULL,
	status TEXT NOT NULL DEFAULT 'locked' CHECK (status IN ('locked', 'partial', 'uncertain'))
);