CREATE TABLE users (
	id BIGSERIAL PRIMARY KEY,
	username TEXT NOT NULL UNIQUE,
	password_hash TEXT NOT NULL,
	is_admin BOOLEAN NOT NULL DEFAULT FALSE,
	is_active BOOLEAN NOT NULL DEFAULT TRUE,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE groups (
	id BIGSERIAL PRIMARY KEY,
	name TEXT NOT NULL UNIQUE,
	description TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE group_members (
	group_id BIGINT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
	user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	PRIMARY KEY (group_id, user_id)
);

CREATE TABLE sessions (
	id BIGSERIAL PRIMARY KEY,
	user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	token_hash BYTEA NOT NULL UNIQUE,
	prev_token_hash BYTEA,
	prev_valid_until TIMESTAMPTZ,
	csrf_token TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL,
	last_seen_at TIMESTAMPTZ NOT NULL,
	rotated_at TIMESTAMPTZ NOT NULL,
	expires_at TIMESTAMPTZ NOT NULL,
	force_rotate BOOLEAN NOT NULL DEFAULT FALSE,
	revoked_at TIMESTAMPTZ
);

CREATE INDEX sessions_prev_token_hash_idx ON sessions(prev_token_hash);
CREATE INDEX sessions_user_id_idx ON sessions(user_id);

CREATE TABLE group_db_access (
	group_id BIGINT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
	db_id BIGINT NOT NULL,
	level TEXT NOT NULL CHECK (level IN ('view', 'edit')),
	PRIMARY KEY (group_id, db_id)
);