import { useState, type ReactNode } from "react";
import { Link, useParams } from "react-router";
import { Badge } from "../../components/Badge";
import { Button } from "../../components/Button";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import { Loading } from "../../components/Loading";
import { PageHeader, Section } from "../../components/PageHeader";
import { SqlEditor } from "../../components/SqlEditor";
import { isApiError } from "../../lib/api";
import { useCurrentUser } from "../../lib/currentUser";
import { useExecuteMigration, useMigration, useRequiredApprovers, useSubmitMigration } from "../../lib/migrations";
import { canEdit, canExecute, canSubmit, type Migration } from "../../lib/migrations/types";
import { toast } from "../../lib/toast";
import { ErrorView } from "../ErrorPage";
import { ApproversPanel } from "./ApproversPanel";
import { MockBanner, StatusBadge, databaseName, formatDateTime, levelFor } from "./shared";

export function MigrationReviewPage() {
    const { id = "" } = useParams();
    const me = useCurrentUser();
    const migration = useMigration(id);
    if (migration.isPending) {
        return <Loading />;
    }
    if (migration.isError) {
        const code = isApiError(migration.error) && migration.error.status === 404 ? "404" : "500";
        return <ErrorView code={code} onRetry={code === "500" ? () => migration.refetch() : undefined} />;
    }
    const level = levelFor(me, migration.data.database_id);
    if (!level) {
        return <ErrorView code="403" />;
    }
    return <Review migration={migration.data} canChange={level === "edit"} />;
}

function Detail({ label, children }: { label: string; children: ReactNode }) {
    return (
        <div className="min-w-0">
            <dt className="text-xs font-medium uppercase tracking-wide text-slate-500">{label}</dt>
            <dd className="mt-0.5 break-words text-sm text-slate-900">{children}</dd>
        </div>
    );
}

function Review({ migration: m, canChange }: { migration: Migration; canChange: boolean }) {
    const me = useCurrentUser();
    const rev = m.current_revision;
    const submit = useSubmitMigration(m.id);
    const execute = useExecuteMigration(m.id, m.database_id);
    const required = useRequiredApprovers(canChange && canSubmit(m.status) ? m.database_id : undefined);
    const [dialog, setDialog] = useState<"submit" | "execute" | null>(null);
    const [jobId, setJobId] = useState<string | null>(null);
    const dbName = databaseName(me, m.database_id);

    const onSubmit = () => {
        submit.mutate(undefined, {
            onSuccess: () => toast.success("Migration diajukan untuk review."),
            onSettled: () => setDialog(null),
        });
    };

    const onExecute = () => {
        execute.mutate(undefined, {
            onSuccess: (res) => {
                setJobId(res.job_id);
                toast.success(res.message || "Migration masuk antrean.");
            },
            onSettled: () => setDialog(null),
        });
    };

    const actions = canChange ? (
        <>
            {canEdit(m.status) && (
                <Link to={`/migration/${m.id}/ubah`} className="inline-flex min-h-10 items-center rounded-md px-4 py-2 text-sm font-semibold text-slate-800 ring-1 ring-inset ring-slate-300 hover:bg-slate-50">
                    Ubah
                </Link>
            )}
            {canSubmit(m.status) && <Button onClick={() => setDialog("submit")}>Ajukan review</Button>}
            {canExecute(m.status) && <Button onClick={() => setDialog("execute")}>Jalankan</Button>}
        </>
    ) : undefined;

    return (
        <div className="space-y-6">
            <PageHeader
                title={m.title}
                description={
                    <span className="flex flex-wrap items-center gap-2">
                        <StatusBadge status={m.status} />
                        <span>Revisi {rev.revision_number}</span>
                        <span>{dbName}</span>
                    </span>
                }
                back={{ to: `/migration/database/${m.database_id}`, label: "Daftar migration" }}
                actions={actions}
            />
            <MockBanner />
            {!canChange && <p className="text-sm text-slate-600">Anda hanya punya akses View Only, jadi hanya bisa melihat migration ini.</p>}
            {canChange && m.status === "Pending Review" && <p className="text-sm text-slate-600">Tombol Jalankan muncul setelah semua leader Grup menyetujui.</p>}
            {jobId && (
                <p role="status" className="rounded-md bg-emerald-50 px-3 py-2 text-sm text-emerald-900 ring-1 ring-inset ring-emerald-200">
                    Migration masuk antrean dengan job <span className="break-all font-mono">{jobId}</span>.
                </p>
            )}

            <Section title="SQL">
                <div className="grid gap-4 lg:grid-cols-2">
                    <div className="min-w-0 space-y-1">
                        <h3 className="text-sm font-medium text-slate-800">SQL up</h3>
                        <SqlEditor value={rev.sql_up} readOnly rows={8} aria-label="SQL up" />
                    </div>
                    <div className="min-w-0 space-y-1">
                        <h3 className="text-sm font-medium text-slate-800">SQL down</h3>
                        {rev.is_irreversible ? (
                            <div className="rounded-md bg-red-50 px-3 py-3 text-sm text-red-800 ring-1 ring-inset ring-red-200">
                                <p className="font-medium">Irreversible, tidak bisa di-rollback.</p>
                                <p className="mt-1 break-words">Alasan: {rev.irreversible_reason}</p>
                            </div>
                        ) : (
                            <SqlEditor value={rev.sql_down ?? ""} readOnly rows={8} aria-label="SQL down" />
                        )}
                    </div>
                </div>
            </Section>

            <div className="grid gap-6 lg:grid-cols-2">
                <Section title="Detail">
                    <dl className="grid gap-4 sm:grid-cols-2">
                        <Detail label="Mode">
                            {rev.is_batch_mode ? (
                                <span className="inline-flex flex-wrap gap-1">
                                    <Badge>Batch</Badge>
                                    <span>
                                        kolom <span className="font-mono">{rev.batch_column}</span>, {rev.batch_size?.toLocaleString("id-ID")} baris per batch
                                    </span>
                                </span>
                            ) : (
                                "Normal"
                            )}
                        </Detail>
                        <Detail label="Dibuat oleh">{m.created_by}</Detail>
                        <Detail label="Revisi dibuat">{formatDateTime(rev.created_at)}</Detail>
                        <Detail label="Terakhir diubah">{formatDateTime(m.updated_at)}</Detail>
                        <div className="sm:col-span-2">
                            <Detail label="Checksum">
                                <span className="break-all font-mono text-xs">{rev.checksum}</span>
                            </Detail>
                        </div>
                    </dl>
                </Section>
                <ApproversPanel migration={m} />
            </div>

            <ConfirmDialog open={dialog === "submit"} title="Ajukan review?" confirmLabel="Ya, ajukan" loading={submit.isPending} onConfirm={onSubmit} onClose={() => setDialog(null)}>
                <p>Revisi {rev.revision_number} akan dikirim ke leader Grup untuk disetujui.</p>
                {required.data && !required.data.ready && <p className="text-amber-800">{required.data.message}</p>}
                {required.data && required.data.groups.length > 0 && <p>Approver: {required.data.groups.map((g) => (g.leader ? `${g.leader.username} (Grup ${g.group_name})` : `Grup ${g.group_name} tanpa leader`)).join(", ")}.</p>}
            </ConfirmDialog>
            <ConfirmDialog open={dialog === "execute"} title="Jalankan migration?" confirmLabel="Ya, jalankan" danger loading={execute.isPending} onConfirm={onExecute} onClose={() => setDialog(null)}>
                <p>
                    Revisi {rev.revision_number} akan dijalankan di database <strong>{dbName}</strong>.
                </p>
                {rev.is_irreversible ? <p className="text-red-700">Migration ini irreversible dan tidak bisa di-rollback.</p> : <p>Perubahan bisa dibatalkan nanti dengan SQL down.</p>}
            </ConfirmDialog>
        </div>
    );
}
