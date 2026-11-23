export type MigrationStatus = "Draft" | "Pending Review" | "Approved" | "Pending Queue" | "Running" | "Success" | "Failed";

export type Revision = {
    id: string;
    revision_number: number;
    sql_up: string;
    sql_down?: string;
    is_irreversible: boolean;
    irreversible_reason?: string;
    is_batch_mode: boolean;
    batch_column?: string;
    batch_size?: number;
    checksum: string;
    created_at: string;
};

export type Migration = {
    id: string;
    database_id: string;
    title: string;
    linear_order: number;
    status: MigrationStatus;
    current_revision: Revision;
    created_by: string;
    created_at: string;
    updated_at: string;
};

export type MigrationInput = {
    title: string;
    sql_up: string;
    sql_down?: string;
    is_irreversible: boolean;
    irreversible_reason?: string;
    is_batch_mode: boolean;
    batch_column?: string;
    batch_size?: number;
};

export type ExecuteResponse = {
    job_id: string;
    message: string;
};

export type UserRef = {
    id: number;
    username: string;
};

export type ApprovalDecision = "pending" | "approved" | "rejected";

export type ApprovalState = "waiting" | "blocked" | "approved" | "rejected" | "stale";

export type ApprovalGroup = {
    group_id: number;
    group_name: string;
    level: "view" | "edit";
    leader: UserRef | null;
    decision: ApprovalDecision;
    decided_by: UserRef | null;
    decided_at: string | null;
    reason?: string;
};

export type ApprovalStatus = {
    execution_id: number;
    migration_id: number;
    title: string;
    database: { id: number; name: string };
    kind: "up" | "rollback" | "forward";
    revision_number: number;
    current_revision: number;
    execution_status: string;
    requested_by: UserRef;
    created_at: string;
    state: ApprovalState;
    message: string;
    groups: ApprovalGroup[];
    can_decide: boolean;
};

export type RequiredGroup = {
    group_id: number;
    group_name: string;
    level: "view" | "edit";
    leader: UserRef | null;
};

export type RequiredApprovers = {
    database_id: number;
    ready: boolean;
    message: string;
    groups: RequiredGroup[];
};

type StatusInfo = {
    label: string;
    tone: "neutral" | "info" | "admin" | "off" | "success";
};

export const statusInfo: Record<MigrationStatus, StatusInfo> = {
    Draft: { label: "Draft", tone: "neutral" },
    "Pending Review": { label: "Menunggu review", tone: "admin" },
    Approved: { label: "Disetujui", tone: "info" },
    "Pending Queue": { label: "Dalam antrean", tone: "info" },
    Running: { label: "Berjalan", tone: "info" },
    Success: { label: "Berhasil", tone: "success" },
    Failed: { label: "Gagal", tone: "off" },
};

export function statusOf(status: string): StatusInfo {
    return statusInfo[status as MigrationStatus] ?? { label: status, tone: "neutral" };
}

export function canEdit(status: string): boolean {
    return status === "Draft" || status === "Pending Review" || status === "Approved";
}

export function canSubmit(status: string): boolean {
    return status === "Draft";
}

export function canExecute(status: string): boolean {
    return status === "Approved";
}

export type ExecutionType = "APPLY" | "ROLLBACK" | "FORWARD";

export type HistoryItem = {
    execution_id: string;
    migration_id: string;
    revision_id: string;
    execution_type: ExecutionType;
    executed_by: string;
    executed_at: string;
    sql_executed: string;
    reverted_execution_id?: string;
};

export type HistoryQuery = {
    hideRollback: boolean;
    page: number;
    pageSize: number;
};

export type HistoryPage = {
    items: HistoryItem[];
    page: number;
    page_size: number;
    total: number | null;
};

export const executionTypeInfo: Record<ExecutionType, StatusInfo> = {
    APPLY: { label: "Terapkan", tone: "info" },
    ROLLBACK: { label: "Rollback", tone: "admin" },
    FORWARD: { label: "Forward", tone: "success" },
};

export function executionTypeOf(type: string): StatusInfo {
    return executionTypeInfo[type as ExecutionType] ?? { label: type, tone: "neutral" };
}

export type JobStatus = "queued" | "running" | "success" | "failed" | "partial" | "uncertain" | "partially_reverted" | "cancelled";

export type JobAction = "rollback" | "forward" | "retry" | "resume" | "cancel";

export type Job = {
    job_id: string;
    execution_id: string;
    migration_id: string;
    database_id: string;
    title: string;
    kind: "up" | "rollback" | "forward";
    status: JobStatus;
    attempt: number;
    is_batch_mode: boolean;
    batches_committed: number;
    batches_total: number | null;
    rows_committed: number;
    rows_total: number | null;
    error_message: string;
    queued_at: string | null;
    started_at: string | null;
    finished_at: string | null;
    updated_at: string;
    allowed_actions: JobAction[];
};

export type JobActionResult = {
    message: string;
    job_id?: string;
    migration_id: string;
    needs_approval: boolean;
};

export const jobStatusInfo: Record<JobStatus, StatusInfo> = {
    queued: { label: "Dalam antrean", tone: "neutral" },
    running: { label: "Berjalan", tone: "info" },
    success: { label: "Berhasil", tone: "success" },
    failed: { label: "Gagal", tone: "off" },
    partial: { label: "Sebagian", tone: "admin" },
    uncertain: { label: "Tidak pasti", tone: "admin" },
    partially_reverted: { label: "Rollback sebagian", tone: "admin" },
    cancelled: { label: "Dibatalkan", tone: "neutral" },
};

export function jobStatusOf(status: string): StatusInfo {
    return jobStatusInfo[status as JobStatus] ?? { label: status, tone: "neutral" };
}

export const jobKindLabel: Record<Job["kind"], string> = { up: "Terapkan", rollback: "Rollback", forward: "Forward" };

export const jobActionOrder: JobAction[] = ["resume", "retry", "forward", "rollback", "cancel"];

export const activePollMs = 2000;

export const waitingPollMs = 10000;

export function pollInterval(status: string): number | false {
    switch (status) {
        case "queued":
        case "running":
            return activePollMs;
        case "partial":
        case "uncertain":
        case "partially_reverted":
            return waitingPollMs;
    }
    return false;
}
