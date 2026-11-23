import { useState } from "react";
import { Link } from "react-router";
import { Badge } from "../../components/Badge";
import { Button } from "../../components/Button";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import { Textarea } from "../../components/Form";
import { EmptyState } from "../../components/EmptyState";
import { Loading } from "../../components/Loading";
import { PageHeader } from "../../components/PageHeader";
import { Pagination } from "../../components/Pagination";
import { isApiError } from "../../lib/api";
import {
    approvalPageSize,
    decisionKindLabel,
    decisionLabel,
    decisionTone,
    stateInfo,
    useApprovalInbox,
    useApproveMigration,
    useRejectMigration,
    type Approval,
} from "../../lib/approvals";
import { toast } from "../../lib/toast";
import { ErrorView } from "../ErrorPage";

function formatDate(iso: string): string {
    return new Date(iso).toLocaleString("id-ID", { day: "numeric", month: "short", year: "numeric", hour: "2-digit", minute: "2-digit" });
}

export function ApprovalsPage() {
    const [page, setPage] = useState(1);
    const inbox = useApprovalInbox(page);
    const approve = useApproveMigration();
    const reject = useRejectMigration();
    const [approving, setApproving] = useState<Approval | null>(null);
    const [rejecting, setRejecting] = useState<Approval | null>(null);
    const [reason, setReason] = useState("");
    const [reasonError, setReasonError] = useState<string | null>(null);
    const [formError, setFormError] = useState<string | null>(null);

    const close = () => {
        setApproving(null);
        setRejecting(null);
        setReason("");
        setReasonError(null);
        setFormError(null);
    };

    const doApprove = () => {
        if (!approving) {
            return;
        }
        approve.mutate(
            { id: approving.migration_id, revisionNumber: approving.revision_number },
            {
                onSuccess: () => {
                    toast.success("Persetujuan tercatat.");
                    close();
                },
                onError: (err) => setFormError(isApiError(err) ? err.message : "Persetujuan gagal."),
            },
        );
    };

    const doReject = () => {
        if (!rejecting) {
            return;
        }
        const value = reason.trim();
        if (!value) {
            setReasonError("Alasan penolakan wajib diisi.");
            return;
        }
        if (value.length > 500) {
            setReasonError("Alasan penolakan maksimal 500 karakter.");
            return;
        }
        reject.mutate(
            { id: rejecting.migration_id, revisionNumber: rejecting.revision_number, reason: value },
            {
                onSuccess: () => {
                    toast.success("Penolakan tercatat.");
                    close();
                },
                onError: (err) => setFormError(isApiError(err) ? err.message : "Penolakan gagal."),
            },
        );
    };

    return (
        <div className="space-y-6">
            <PageHeader title="Persetujuan" description="Migration yang menunggu keputusan dari grup yang Anda pimpin." />
            {formError && !approving && !rejecting && <div role="alert" className="alert-error">{formError}</div>}
            {inbox.isPending ? (
                <Loading />
            ) : inbox.isError ? (
                <ErrorView code="500" onRetry={() => inbox.refetch()} />
            ) : (
                <div className="space-y-4">
                    {inbox.data.items.length === 0 ? (
                        <EmptyState title="Tidak ada persetujuan menunggu." description="Permintaan baru dari grup yang Anda pimpin akan muncul di sini." />
                    ) : (
                        inbox.data.items.map((item) => (
                            <section key={item.execution_id} className="card min-w-0 space-y-4 p-5 sm:p-6">
                                <div className="flex flex-wrap items-start justify-between gap-3">
                                    <div className="min-w-0">
                                        <h2 className="break-words text-lg font-semibold text-slate-900">{item.title}</h2>
                                        <p className="mt-1 flex flex-wrap items-center gap-2 text-sm text-slate-500">
                                            <Badge tone="info">{decisionKindLabel[item.kind] ?? item.kind}</Badge>
                                            <span>Revisi {item.revision_number}</span>
                                            <span>{item.database.name}</span>
                                            <span>{formatDate(item.created_at)}</span>
                                        </p>
                                    </div>
                                    <Badge tone={item.state === "rejected" || item.state === "stale" ? "off" : item.state === "approved" ? "success" : "admin"}>
                                        {stateInfo(item.state)}
                                    </Badge>
                                </div>
                                <p className="text-sm text-slate-600">
                                    Diajukan oleh {item.requested_by.username}. {item.message}
                                </p>
                                {item.state === "stale" && (
                                    <p className="rounded-md bg-amber-50 px-3 py-2 text-sm text-amber-900 ring-1 ring-inset ring-amber-200">
                                        SQL sudah berubah setelah permintaan ini dibuat. Minta pengaju mengirim revisi baru.
                                    </p>
                                )}
                                <ul aria-label="Keputusan tiap grup" className="divide-y divide-slate-100 rounded-lg ring-1 ring-slate-200">
                                    {item.groups.map((g) => (
                                        <li key={g.group_id} className="flex flex-col gap-2 px-3 py-3 sm:flex-row sm:items-center sm:justify-between">
                                            <div className="min-w-0">
                                                <p className="break-words text-sm font-medium text-slate-900">Grup {g.group_name}</p>
                                                <p className="text-xs text-slate-500">
                                                    Leader: {g.leader?.username ?? "belum ditetapkan"}
                                                    {g.decided_by && ` · diputuskan oleh ${g.decided_by.username}`}
                                                </p>
                                                {g.reason && <p className="mt-1 break-words text-xs text-slate-600">Alasan: {g.reason}</p>}
                                            </div>
                                            <Badge tone={decisionTone(g.decision)}>{decisionLabel(g.decision)}</Badge>
                                        </li>
                                    ))}
                                </ul>
                                <div className="flex flex-wrap items-center justify-end gap-2">
                                    <Link to={`/migration/${item.migration_id}`} className="btn btn-secondary">
                                        Lihat detail
                                    </Link>
                                    {item.can_decide ? (
                                        <>
                                            <Button variant="danger" onClick={() => { setReason(""); setReasonError(null); setFormError(null); setRejecting(item); }}>
                                                Tolak
                                            </Button>
                                            <Button onClick={() => { setFormError(null); setApproving(item); }}>Setuju</Button>
                                        </>
                                    ) : (
                                        <p className="text-sm text-slate-500">Belum ada keputusan yang menunggu dari Anda.</p>
                                    )}
                                </div>
                            </section>
                        ))
                    )}
                    <Pagination page={page} pageSize={approvalPageSize} total={inbox.data.total} onChange={setPage} />
                </div>
            )}
            <ConfirmDialog
                open={approving !== null}
                title="Setujui migration?"
                confirmLabel="Ya, setuju"
                loading={approve.isPending}
                onConfirm={doApprove}
                onClose={close}
            >
                <p>Revisi {approving?.revision_number} dari <strong>{approving?.title}</strong> akan disetujui atas nama grup Anda.</p>
                {formError && <p role="alert" className="alert-error">{formError}</p>}
            </ConfirmDialog>
            <ConfirmDialog
                open={rejecting !== null}
                title="Tolak migration?"
                confirmLabel="Ya, tolak"
                danger
                loading={reject.isPending}
                onConfirm={doReject}
                onClose={close}
            >
                <p>Revisi {rejecting?.revision_number} dari <strong>{rejecting?.title}</strong> akan ditolak.</p>
                <label htmlFor="alasan-penolakan" className="field-label">Alasan penolakan</label>
                <Textarea
                    id="alasan-penolakan"
                    name="reason"
                    rows={4}
                    maxLength={500}
                    value={reason}
                    aria-invalid={reasonError ? true : undefined}
                    onChange={(e) => { setReason(e.target.value); setReasonError(null); }}
                    autoFocus
                />
                {reasonError && <p role="alert" className="text-xs text-red-600">{reasonError}</p>}
                {formError && <p role="alert" className="alert-error">{formError}</p>}
            </ConfirmDialog>
        </div>
    );
}
