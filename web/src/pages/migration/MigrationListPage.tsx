import { Link, useParams } from "react-router";
import { Badge } from "../../components/Badge";
import type { Column } from "../../components/DataTable";
import { EmptyState } from "../../components/EmptyState";
import { Loading } from "../../components/Loading";
import { PageHeader } from "../../components/PageHeader";
import { ResponsiveList } from "../../components/ResponsiveList";
import { useCurrentUser } from "../../lib/currentUser";
import { useMigrations } from "../../lib/migrations";
import type { Migration } from "../../lib/migrations/types";
import { levelLabel } from "../../lib/session";
import { ErrorView } from "../ErrorPage";
import { DatabaseTabs } from "./DatabaseTabs";
import { MockBanner, StatusBadge, databaseName, formatDateTime, levelFor } from "./shared";

const columns: Column<Migration>[] = [
    {
        key: "title",
        header: "Judul",
        render: (m) => (
            <Link to={`/migration/${m.id}`} className="font-medium text-indigo-700 hover:underline">
                {m.title}
            </Link>
        ),
    },
    { key: "status", header: "Status", render: (m) => <StatusBadge status={m.status} /> },
    { key: "revision", header: "Revisi", nowrap: true, render: (m) => `Revisi ${m.current_revision.revision_number}` },
    {
        key: "mode",
        header: "Mode",
        render: (m) => (
            <span className="inline-flex flex-wrap justify-end gap-1">
                <Badge>{m.current_revision.is_batch_mode ? "Batch" : "Normal"}</Badge>
                {m.current_revision.is_irreversible && <Badge tone="off">Irreversible</Badge>}
            </span>
        ),
    },
    { key: "updated", header: "Diubah", nowrap: true, render: (m) => formatDateTime(m.updated_at) },
];

export function MigrationListPage() {
    const { dbId = "" } = useParams();
    const me = useCurrentUser();
    const level = levelFor(me, dbId);
    const migrations = useMigrations(dbId);

    if (!level) {
        return <ErrorView code="403" />;
    }

    return (
        <div className="space-y-6">
            <PageHeader
                title="Migration"
                description={
                    <span className="flex flex-wrap items-center gap-2">
                        {databaseName(me, dbId)}
                        <Badge tone={level === "edit" ? "info" : "neutral"}>{levelLabel(level)}</Badge>
                    </span>
                }
                back={me.databases.length > 1 ? { to: "/migration", label: "Pilih database" } : undefined}
                actions={
                    level === "edit" ? (
                        <Link to={`/migration/database/${dbId}/baru`} className="btn btn-primary">
                            Buat migration
                        </Link>
                    ) : undefined
                }
            />
            <DatabaseTabs dbId={dbId} />
            <MockBanner />
            {migrations.isPending ? (
                <Loading />
            ) : migrations.isError ? (
                <ErrorView code="500" onRetry={() => migrations.refetch()} />
            ) : (
                <ResponsiveList
                    caption="Daftar migration"
                    columns={columns}
                    rows={migrations.data}
                    rowKey={(m) => m.id}
                    minWidth="44rem"
                    empty={<EmptyState title="Belum ada migration." description={level === "edit" ? "Buat migration pertama untuk database ini." : "Belum ada migration yang dibuat untuk database ini."} />}
                />
            )}
        </div>
    );
}
