CREATE TABLE db_connections (
	id BIGSERIAL PRIMARY KEY,
	name TEXT NOT NULL UNIQUE,
	host TEXT NOT NULL,
	port INTEGER NOT NULL CHECK (port BETWEEN 1 AND 65535),
	db_name TEXT NOT NULL,
	username TEXT NOT NULL,
	password_enc TEXT NOT NULL,
	sslmode TEXT NOT NULL DEFAULT 'prefer' CHECK (sslmode IN ('disable', 'allow', 'prefer', 'require', 'verify-ca', 'verify-full')),
	created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- group_db_access.db_id dibuat A tanpa foreign key karena tabel koneksi belum ada.
-- Grant ikut terhapus saat koneksi dihapus.
ALTER TABLE group_db_access
	ADD CONSTRAINT group_db_access_db_id_fkey
	FOREIGN KEY (db_id) REFERENCES db_connections(id) ON DELETE CASCADE;
