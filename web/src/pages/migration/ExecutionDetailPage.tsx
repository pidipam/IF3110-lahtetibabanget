import type { ReactNode } from "react";
import { Link, useParams } from "react-router";
import { Button } from "../../components/Button";
import { Loading } from "../../components/Loading";
import { PageHeader, Section } from "../../components/PageHeader";
import { SqlEditor } from "../../components/SqlEditor";
import { isApiError } from "../../lib/api";
import { useCurrentUser } from "../../lib/currentUser";
import { useHistory, useHistoryItem } from "../../lib/migrations";
import { toast } from "../../lib/toast";
import { ErrorView } from "../ErrorPage";
import { ExecutionTypeBadge, useMigrationTitles } from "./HistoryPage";
import { MockBanner, databaseName, formatDateTime, levelFor } from "./shared";

function Detail({ label, children }: { label: string; children: ReactNode }) {
    return (
        <div className="min-w-0">
            <dt className="text-xs font-medium uppercase tracking-wide text-slate-500">{label}</dt>
            <dd className="mt-0.5 break-words text-sm text-slate-900">{children}</dd>
        </div>
    );
}

export function ExecutionDetailPage() {
    const { dbId = "", executionId = "" } = useParams();
    const me = useCurrentUser();
    const level = levelFor(me, dbId);
    const item = useHistoryItem(dbId, executionId);
    const all = useHistory(dbId, { hideRollback: false, page: 1, pageSize: 100 });
    const titleOf = useMigrationTitles(dbId);

    if (!level) {
        return <ErrorView code="403" />;
    }
    if (item.isPending) {
        return <Loading />;
    }
    if (item.isError) {
        const code = isApiError(item.error) && item.error.status === 404 ? "404" : "500";
        return <ErrorView code={code} onRetry={code === "500" ? () => item.refetch() : undefined} />;
    }

    const h = item.data;
    const base = `/migration/database/${dbId}/riwayat`;
    const revertedBy = all.data?.items.find((x) => x.reverted_execution_id === h.execution_id);

    const copy = async () => {
        try {
            await navigator.clipboard.writeText(h.sql_executed);
            toast.success("SQL disalin.");
        } catch {
            toast.error("SQL gagal disalin. Pilih teksnya lalu salin manual.");
        }
    };

    return (
        <div className="space-y-6">
            <PageHeader
                title={`Eksekusi #${h.execution_id}`}
                description={
                    <span className="flex flex-wrap items-center gap-2">
                        <ExecutionTypeBadge type={h.execution_type} />
                        <span>{databaseName(me, dbId)}</span>
                    </span>
                }
                back={{ to: base, label: "Riwayat eksekusi" }}
            />
            <MockBanner />
            {revertedBy && (
                <p role="note" className="rounded-md bg-amber-50 px-3 py-2 text-sm text-amber-900 ring-1 ring-inset ring-amber-200">
                    Eksekusi ini sudah dibatalkan oleh{" "}
                    <Link to={`${base}/${revertedBy.execution_id}`} className="font-medium underline">
                        rollback #{revertedBy.execution_id}
                    </Link>
                    .
                </p>
            )}

            <Section title="Detail">
                <dl className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
                    <Detail label="Migration">
                        <Link to={`/migration/${h.migration_id}`} className="text-indigo-700 hover:underline">
                            {titleOf(h.migration_id)}
                        </Link>
                    </Detail>
                    <Detail label="Waktu">{formatDateTime(h.executed_at)}</Detail>
                    <Detail label="Pelaksana">{h.executed_by}</Detail>
                    <Detail label="ID revisi">
                        <span className="font-mono">{h.revision_id}</span>
                    </Detail>
                    {h.reverted_execution_id && (
                        <Detail label="Membatalkan">
                            <Link to={`${base}/${h.reverted_execution_id}`} className="text-indigo-700 hover:underline">
                                Eksekusi #{h.reverted_execution_id}
                            </Link>
                        </Detail>
                    )}
                </dl>
            </Section>

            <Section title="SQL yang dijalankan" description="Salinan SQL persis seperti yang dikirim ke database saat eksekusi ini.">
                <SqlEditor value={h.sql_executed} readOnly rows={Math.min(Math.max(h.sql_executed.split("\n").length, 6), 24)} aria-label="SQL yang dijalankan" />
                <div className="flex justify-end">
                    <Button variant="secondary" onClick={copy}>
                        Salin SQL
                    </Button>
                </div>
            </Section>
        </div>
    );
}
