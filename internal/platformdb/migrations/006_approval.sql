ALTER TABLE groups ADD COLUMN leader_id BIGINT;

ALTER TABLE groups ADD CONSTRAINT groups_leader_is_member
	FOREIGN KEY (id, leader_id) REFERENCES group_members (group_id, user_id) ON DELETE SET NULL (leader_id);

CREATE INDEX groups_leader_idx ON groups (leader_id) WHERE leader_id IS NOT NULL;

CREATE TABLE migration_approvals (
	id BIGSERIAL PRIMARY KEY,
	execution_id BIGINT NOT NULL REFERENCES platform_migration_executions(id) ON DELETE CASCADE,
	group_id BIGINT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
	revision_number INT NOT NULL,
	approver_id BIGINT NOT NULL REFERENCES users(id),
	decision TEXT NOT NULL CHECK (decision IN ('approved', 'rejected')),
	reason TEXT NOT NULL DEFAULT '' CHECK (char_length(reason) <= 500),
	decided_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	UNIQUE (execution_id, group_id, revision_number),
	CHECK (decision = 'approved' OR btrim(reason) <> '')
);

CREATE INDEX migration_approvals_approver_idx ON migration_approvals (approver_id);

CREATE TRIGGER migration_approvals_append_only
	BEFORE UPDATE ON migration_approvals
	FOR EACH ROW EXECUTE FUNCTION platform_forbid_mutation();
