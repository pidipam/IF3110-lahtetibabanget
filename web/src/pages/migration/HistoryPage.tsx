import { useCallback } from "react";
import { Link, useParams, useSearchParams } from "react-router";
import { Badge } from "../../components/Badge";
import type { Column } from "../../components/DataTable";
import { EmptyState } from "../../components/EmptyState";
import { Checkbox } from "../../components/Form";
import { Loading } from "../../components/Loading";
import { PageHeader } from "../../components/PageHeader";
import { Pagination } from "../../components/Pagination";
import { ResponsiveList } from "../../components/ResponsiveList";
import { useCurrentUser } from "../../lib/currentUser";
import { historyPageSize, useHistory, useMigrations } from "../../lib/migrations";
import { executionTypeOf, type HistoryItem } from "../../lib/migrations/types";
import { levelLabel } from "../../lib/session";
import { ErrorView } from "../ErrorPage";
import { DatabaseTabs } from "./DatabaseTabs";
import { MockBanner, databaseName, formatDateTime, levelFor } from "./shared";

export function ExecutionTypeBadge({ type }: { type: string }) {
    const info = executionTypeOf(type);
    return <Badge tone={info.tone}>{info.label}</Badge>;
}

export function useMigrationTitles(dbId: string) {
    const migrations = useMigrations(dbId);
    return useCallback((id: string) => migrations.data?.find((m) => m.id === id)?.title ?? `Migration #${id}`, [migrations.data]);
}

function useHistoryParams() {
    const [params, setParams] = useSearchParams();
    const hideRollback = params.get("hide_rollback") === "1";
    const raw = Number(params.get("page"));
    const page = Number.isInteger(raw) && raw > 0 ? raw : 1;
    const setHide = (hide: boolean) =>
        setParams(
            (prev) => {
                const next = new URLSearchParams(prev);
                next.delete("page");
                if (hide) {
                    next.set("hide_rollback", "1");
                } else {
                    next.delete("hide_rollback");
                }
                return next;
            },
            { replace: true },
        );
    const setPage = (p: number) =>
        setParams((prev) => {
            const next = new URLSearchParams(prev);
            if (p > 1) {
                next.set("page", String(p));
            } else {
                next.delete("page");
            }
            return next;
        });
    return { hideRollback, page, setHide, setPage };
}

export function HistoryPage() {
    const { dbId = "" } = useParams();
    const me = useCurrentUser();
    const level = levelFor(me, dbId);
    const { hideRollback, page, setHide, setPage } = useHistoryParams();
    const history = useHistory(dbId, { hideRollback, page, pageSize: historyPageSize });
    const titleOf = useMigrationTitles(dbId);

    if (!level) {
        return <ErrorView code="403" />;
    }

    const items = history.data?.items ?? [];
    const revertedBy = new Map(items.filter((h) => h.reverted_execution_id).map((h) => [h.reverted_execution_id!, h.execution_id]));
    const detail = (id: string) => `/migration/database/${dbId}/riwayat/${id}`;

    const columns: Column<HistoryItem>[] = [
        {
            key: "migration",
            header: "Migration",
            render: (h) => (
                <Link to={detail(h.execution_id)} className="font-medium text-indigo-700 hover:underline">
                    {titleOf(h.migration_id)}
                </Link>
            ),
        },
        { key: "type", header: "Jenis", render: (h) => <ExecutionTypeBadge type={h.execution_type} /> },
        { key: "time", header: "Waktu", nowrap: true, render: (h) => formatDateTime(h.executed_at) },
        { key: "by", header: "Pelaksana", render: (h) => h.executed_by },
        {
            key: "note",
            header: "Keterangan",
            render: (h) => {
                if (h.reverted_execution_id) {
                    return (
                        <Link to={detail(h.reverted_execution_id)} className="text-indigo-700 hover:underline">
                            Membatalkan eksekusi #{h.reverted_execution_id}
                        </Link>
                    );
                }
                const by = revertedBy.get(h.execution_id);
                if (by) {
                    return (
                        <Link to={detail(by)} className="text-amber-800 hover:underline">
                            Dibatalkan oleh rollback #{by}
                        </Link>
                    );
                }
                return <span className="text-slate-400">#{h.execution_id}</span>;
            },
        },
    ];

    return (
        <div className="space-y-6">
            <PageHeader
                title="Riwayat eksekusi"
                description={
                    <span className="flex flex-wrap items-center gap-2">
                        {databaseName(me, dbId)}
                        <Badge tone={level === "edit" ? "info" : "neutral"}>{levelLabel(level)}</Badge>
                    </span>
                }
                back={me.databases.length > 1 ? { to: "/migration", label: "Pilih database" } : undefined}
            />
            <DatabaseTabs dbId={dbId} />
            <MockBanner />
            <Checkbox label="Hide rollback" hint="Sembunyikan rollback dan eksekusi yang dibatalkannya." checked={hideRollback} onChange={(e) => setHide(e.target.checked)} />
            {history.isPending ? (
                <Loading />
            ) : history.isError ? (
                <ErrorView code="500" onRetry={() => history.refetch()} />
            ) : (
                <div className={`space-y-4 ${history.isPlaceholderData ? "opacity-60" : ""}`} aria-busy={history.isPlaceholderData || undefined}>
                    <ResponsiveList
                        caption="Riwayat eksekusi"
                        columns={columns}
                        rows={items}
                        rowKey={(h) => h.execution_id}
                        minWidth="52rem"
                        empty={<EmptyState title={hideRollback ? "Tidak ada eksekusi yang masih berlaku." : "Belum ada eksekusi."} description={hideRollback ? "Matikan Hide rollback untuk melihat semua eksekusi." : undefined} />}
                    />
                    {history.data.total !== null && <Pagination page={page} pageSize={history.data.page_size || historyPageSize} total={history.data.total} onChange={setPage} />}
                </div>
            )}
        </div>
    );
}
