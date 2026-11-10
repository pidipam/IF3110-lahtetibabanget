import { Badge } from "../../components/Badge";
import { Loading } from "../../components/Loading";
import { Section } from "../../components/PageHeader";
import { useApprovalStatus, useRequiredApprovers } from "../../lib/migrations";
import type { ApprovalDecision, ApprovalState, Migration, UserRef } from "../../lib/migrations/types";
import { formatDateTime } from "./shared";

const decisionInfo: Record<ApprovalDecision, { label: string; tone: "neutral" | "success" | "off" }> = {
    pending: { label: "Menunggu", tone: "neutral" },
    approved: { label: "Disetujui", tone: "success" },
    rejected: { label: "Ditolak", tone: "off" },
};

const stateStyle: Record<ApprovalState, string> = {
    waiting: "bg-slate-50 text-slate-800 ring-slate-200",
    blocked: "bg-amber-50 text-amber-900 ring-amber-200",
    approved: "bg-emerald-50 text-emerald-900 ring-emerald-200",
    rejected: "bg-red-50 text-red-800 ring-red-200",
    stale: "bg-amber-50 text-amber-900 ring-amber-200",
};

const levelText = { view: "View Only", edit: "View + Edit" };

type Row = {
    id: number;
    name: string;
    level: "view" | "edit";
    leader: UserRef | null;
    decision?: ApprovalDecision;
    decidedBy?: UserRef | null;
    decidedAt?: string | null;
    reason?: string;
};

function Message({ tone, children }: { tone: ApprovalState; children: string }) {
    return (
        <p role="status" className={`rounded-md px-3 py-2 text-sm ring-1 ring-inset ${stateStyle[tone]}`}>
            {children}
        </p>
    );
}

function GroupList({ rows }: { rows: Row[] }) {
    if (rows.length === 0) {
        return null;
    }
    return (
        <ul aria-label="Approver per Grup" className="divide-y divide-slate-100 rounded-lg ring-1 ring-slate-200">
            {rows.map((r) => (
                <li key={r.id} className="flex flex-col gap-1 px-3 py-3 sm:flex-row sm:items-start sm:justify-between">
                    <div className="min-w-0">
                        <p className="break-words font-medium text-slate-900">Grup {r.name}</p>
                        <p className="text-sm text-slate-600">
                            {r.leader ? `Leader: ${r.leader.username}` : <span className="text-amber-800">Belum ada leader</span>}
                            <span className="text-slate-400"> ({levelText[r.level]})</span>
                        </p>
                        {r.decidedBy && r.decidedAt && (
                            <p className="text-xs text-slate-500">
                                Diputuskan {r.decidedBy.username}, {formatDateTime(r.decidedAt)}
                            </p>
                        )}
                        {r.reason && <p className="mt-1 break-words text-sm text-red-700">Alasan: {r.reason}</p>}
                    </div>
                    {r.decision && (
                        <div className="shrink-0">
                            <Badge tone={decisionInfo[r.decision].tone}>{decisionInfo[r.decision].label}</Badge>
                        </div>
                    )}
                </li>
            ))}
        </ul>
    );
}

export function ApproversPanel({ migration }: { migration: Migration }) {
    const draft = migration.status === "Draft";
    const required = useRequiredApprovers(draft ? migration.database_id : undefined);
    const approval = useApprovalStatus(migration.id, !draft);

    if (draft) {
        return (
            <Section title="Approver yang dibutuhkan" description="Leader setiap Grup yang punya akses ke database ini harus menyetujui sebelum migration bisa dijalankan.">
                {required.isPending ? (
                    <Loading label="Memuat approver..." />
                ) : required.isError ? (
                    <p className="text-sm text-red-700">Daftar approver gagal dimuat.</p>
                ) : (
                    <>
                        <Message tone={required.data.ready ? "waiting" : "blocked"}>{required.data.message}</Message>
                        <GroupList rows={required.data.groups.map((g) => ({ id: g.group_id, name: g.group_name, level: g.level, leader: g.leader }))} />
                    </>
                )}
            </Section>
        );
    }

    return (
        <Section title="Status persetujuan">
            {approval.isPending ? (
                <Loading label="Memuat status persetujuan..." />
            ) : approval.isError ? (
                <p className="text-sm text-red-700">Status persetujuan gagal dimuat.</p>
            ) : approval.data === null ? (
                <p className="text-sm text-slate-600">Belum ada permintaan review untuk revisi ini.</p>
            ) : (
                <>
                    <Message tone={approval.data.state}>{approval.data.message}</Message>
                    <GroupList rows={approval.data.groups.map((g) => ({ id: g.group_id, name: g.group_name, level: g.level, leader: g.leader, decision: g.decision, decidedBy: g.decided_by, decidedAt: g.decided_at, reason: g.reason }))} />
                </>
            )}
        </Section>
    );
}
