ALTER TABLE db_connections
	ADD COLUMN engine TEXT NOT NULL DEFAULT 'postgres'
	CHECK (engine IN ('postgres', 'mysql', 'mariadb'));