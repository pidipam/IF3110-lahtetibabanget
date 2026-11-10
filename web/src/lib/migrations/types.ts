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
