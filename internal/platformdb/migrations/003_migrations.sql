CREATE TABLE migrations (
	id BIGSERIAL PRIMARY KEY,
    db_id BIGINT NOT NULL REFERENCES db_connections(id) ON DELETE RESTRICT,
	version INT,
	title TEXT NOT NULL,
	sql_text TEXT NOT NULL,
	status TEXT NOT NULL CHECK (status IN ('draft', 'running', 'success', 'failed')),
	created_by BIGINT NOT NULL REFERENCES users(id),
	executed_by BIGINT REFERENCES users(id),
	created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	started_at TIMESTAMPTZ,
	finished_at TIMESTAMPTZ,
	error_message TEXT NOT NULL DEFAULT '',
	CHECK ((status = 'draft') = (version IS NULL)),
	UNIQUE (db_id, version)
);

CREATE INDEX migrations_db_idx ON migrations (db_id, created_at DESC);