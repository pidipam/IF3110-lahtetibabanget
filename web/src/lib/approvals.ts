import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "./api";

export type UserRef = {
    id: number;
    username: string;
};

export type ApprovalGroup = {
    group_id: number;
    group_name: string;
    level: "view" | "edit";
    leader: UserRef | null;
    decision: "pending" | "approved" | "rejected";
    decided_by: UserRef | null;
    decided_at: string | null;
    reason?: string;
};

export type Approval = {
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
    state: "waiting" | "blocked" | "approved" | "rejected" | "stale";
    message: string;
    groups: ApprovalGroup[];
    can_decide: boolean;
};

type ApprovalPage = {
    items: Approval[];
    page: number;
    page_size: number;
    total: number;
};

export const approvalPageSize = 20;

export const approvalKeys = {
    all: ["approvals"] as const,
    list: (page: number) => ["approvals", "list", page] as const,
};

export const decisionKindLabel: Record<Approval["kind"], string> = {
    up: "Terapkan",
    rollback: "Rollback",
    forward: "Forward",
};

const decisionInfo = {
    pending: { label: "Menunggu", tone: "neutral" },
    approved: { label: "Disetujui", tone: "success" },
    rejected: { label: "Ditolak", tone: "off" },
} as const;

export function decisionLabel(decision: ApprovalGroup["decision"]): string {
    return decisionInfo[decision].label;
}

export function decisionTone(decision: ApprovalGroup["decision"]): "neutral" | "success" | "off" {
    return decisionInfo[decision].tone;
}

const stateLabel: Record<Approval["state"], string> = {
    waiting: "Menunggu persetujuan",
    blocked: "Terhalang",
    approved: "Disetujui",
    rejected: "Ditolak",
    stale: "Revisi berubah",
};

export function stateInfo(state: Approval["state"]): string {
    return stateLabel[state] ?? state;
}

export function useApprovalInbox(page: number) {
    return useQuery({
        queryKey: approvalKeys.list(page),
        queryFn: () => api.get<ApprovalPage>(`/api/approvals?mine=true&page=${page}&page_size=${approvalPageSize}`),
        placeholderData: keepPreviousData,
    });
}

function useDecisionMutation(path: "approve" | "reject") {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: ({ id, revisionNumber, reason }: { id: number; revisionNumber: number; reason?: string }) =>
            api.post<Approval>(`/api/migrations/${id}/${path}`, path === "reject" ? { revision_number: revisionNumber, reason } : { revision_number: revisionNumber }),
        meta: { inlineStatuses: [409, 422] },
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: approvalKeys.all });
            qc.invalidateQueries({ queryKey: ["migrations"] });
        },
    });
}

export function useApproveMigration() {
    return useDecisionMutation("approve");
}

export function useRejectMigration() {
    return useDecisionMutation("reject");
}
