-- Revisi spesifikasi M2 (v4): migration history mencatat SEMUA attempt, berhasil maupun gagal.
-- Satu baris per attempt: kunci baru (id), pasangan (execution_id, attempt) unik.
-- Hanya baris berstatus 'success' yang mengubah posisi aktif dan yang boleh dirujuk rollback.

ALTER TABLE platform_migration_history DROP CONSTRAINT platform_migration_history_reverted_execution_id_fkey;
ALTER TABLE platform_migration_history DROP CONSTRAINT platform_migration_history_pkey;
DROP INDEX history_revert_once;

ALTER TABLE platform_migration_history ADD COLUMN id BIGSERIAL;
ALTER TABLE platform_migration_history ADD COLUMN attempt INT NOT NULL DEFAULT 1;
ALTER TABLE platform_migration_history ADD COLUMN error_message TEXT NOT NULL DEFAULT '';
ALTER TABLE platform_migration_history ADD PRIMARY KEY (id);

ALTER TABLE platform_migration_history DROP CONSTRAINT platform_migration_history_status_check;
ALTER TABLE platform_migration_history ADD CONSTRAINT platform_migration_history_status_check
	CHECK (status IN ('success', 'failed'));
ALTER TABLE platform_migration_history ALTER COLUMN status DROP DEFAULT;

-- rujukan rollback menunjuk eksekusi (bukan baris history) yang dibatalkan
ALTER TABLE platform_migration_history ADD CONSTRAINT history_reverted_execution_fk
	FOREIGN KEY (reverted_execution_id) REFERENCES platform_migration_executions(id);

CREATE UNIQUE INDEX history_attempt_once ON platform_migration_history (execution_id, attempt);
-- satu eksekusi hanya boleh sukses sekali; satu eksekusi hanya boleh dibatalkan sekali
CREATE UNIQUE INDEX history_success_once ON platform_migration_history (execution_id) WHERE status = 'success';
CREATE UNIQUE INDEX history_revert_once ON platform_migration_history (reverted_execution_id)
	WHERE reverted_execution_id IS NOT NULL AND status = 'success';
