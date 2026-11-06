-- Tabel Migrasi Utama (urutan migrasi linear)
CREATE TABLE IF NOT EXISTS platform_migrations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    database_id UUID NOT NULL REFERENCES db_connections(id) ON DELETE CASCADE,
    title VARCHAR(255) NOT NULL,
    linear_order INT NOT NULL,
    status VARCHAR(50) NOT NULL DEFAULT 'Draft', -- Draft, Pending Review, Approved, Pending Queue, Running, Success, Failed
    active_revision_id UUID, -- Menunjuk ke revisi yang disetujui / aktif
    created_by UUID NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(database_id, linear_order)
);

-- Tabel Revisi (Setiap perubahan SQL up/down membuat revisi baru, immutable jika sudah dieksekusi)
CREATE TABLE IF NOT EXISTS platform_migration_revisions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    migration_id UUID NOT NULL REFERENCES platform_migrations(id) ON DELETE CASCADE,
    revision_number INT NOT NULL,
    sql_up TEXT NOT NULL,
    sql_down TEXT,
    is_irreversible BOOLEAN NOT NULL DEFAULT FALSE,
    irreversible_reason TEXT,
    is_batch_mode BOOLEAN NOT NULL DEFAULT FALSE,
    batch_column VARCHAR(128),
    batch_size INT,
    checksum VARCHAR(64) NOT NULL, -- SHA256 checksum dari isi SQL
    created_by UUID NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(migration_id, revision_number)
);

-- Pasang FKEY active_revision_id setelah tabel revisi dibuat
ALTER TABLE platform_migrations 
ADD CONSTRAINT fk_active_revision 
FOREIGN KEY (active_revision_id) REFERENCES platform_migration_revisions(id);

-- Tabel Migration History (Hanya mencatat commit yang SUKSES)
CREATE TABLE IF NOT EXISTS platform_migration_history (
    execution_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    migration_id UUID NOT NULL REFERENCES platform_migrations(id),
    revision_id UUID NOT NULL REFERENCES platform_migration_revisions(id),
    database_id UUID NOT NULL REFERENCES db_connections(id),
    executed_by UUID NOT NULL REFERENCES users(id),
    executed_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    execution_type VARCHAR(20) NOT NULL, -- 'APPLY', 'ROLLBACK', 'FORWARD'
    sql_executed TEXT NOT NULL,
    reverted_execution_id UUID REFERENCES platform_migration_history(execution_id) -- Untuk Hide Rollback
);

-- Tabel Distributed Lock per Database (LINTAS WORKER)
CREATE TABLE IF NOT EXISTS platform_database_locks (
    database_id UUID PRIMARY KEY REFERENCES db_connections(id) ON DELETE CASCADE,
    worker_id VARCHAR(100) NOT NULL,
    execution_id UUID,
    acquired_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    lease_until TIMESTAMPTZ NOT NULL,
    status VARCHAR(50) NOT NULL DEFAULT 'LOCKED' -- LOCKED, PARTIAL, UNCERTAIN
);