CREATE TABLE platform_migration_jobs (
	id BIGSERIAL PRIMARY KEY,
	job_id TEXT NOT NULL UNIQUE,
	execution_id BIGINT NOT NULL REFERENCES platform_migration_executions(id) ON DELETE CASCADE,
	status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'done', 'failed')),
	run_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	locked_by TEXT NOT NULL DEFAULT '',
	locked_at TIMESTAMPTZ,
	lease_until TIMESTAMPTZ,
	attempts INT NOT NULL DEFAULT 0,
	last_error TEXT NOT NULL DEFAULT '',
	traceparent TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	finished_at TIMESTAMPTZ
);

CREATE INDEX platform_migration_jobs_claim_idx
	ON platform_migration_jobs (run_at, id) WHERE status = 'pending';

CREATE INDEX platform_migration_jobs_lease_idx
	ON platform_migration_jobs (lease_until) WHERE status = 'running';

CREATE INDEX platform_migration_jobs_execution_idx
	ON platform_migration_jobs (execution_id, id);
