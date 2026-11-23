import { useState } from "react";
import { Link } from "react-router";
import { Badge } from "../../components/Badge";
import type { Column } from "../../components/DataTable";
import { EmptyState } from "../../components/EmptyState";
import { Loading } from "../../components/Loading";
import { PageHeader } from "../../components/PageHeader";
import { Pagination } from "../../components/Pagination";
import { ResponsiveList } from "../../components/ResponsiveList";
import { viewerPageSize, useDatabases, labelForEngine, type Database } from "../../lib/viewer";
import { ErrorView } from "../ErrorPage";

export function DatabasesPage() {
    const [page, setPage] = useState(1);
    const databases = useDatabases(page);

    const columns: Column<Database>[] = [
        {
            key: "name",
            header: "Nama",
            render: (d) => (
                <Link to={`/database/${d.id}`} className="font-medium text-indigo-700 hover:underline">
                    {d.name}
                </Link>
            ),
        },
        { key: "engine", header: "Engine", nowrap: true, render: (d) => labelForEngine(d.engine) },
        {
            key: "level",
            header: "Akses",
            nowrap: true,
            render: (d) => <Badge tone={d.level === "edit" ? "admin" : "neutral"}>{d.level === "edit" ? "View + Edit" : "View Only"}</Badge>,
        },
        {
            key: "actions",
            header: "Migration",
            nowrap: true,
            render: (d) => (
                <Link to={`/migration/database/${d.id}`} className="text-sm font-medium text-indigo-700 hover:underline">
                    Lihat
                </Link>
            ),
        },
    ];

    return (
        <div className="space-y-6">
            <PageHeader title="Database" description="Database yang bisa Anda lihat berdasarkan akses grup Anda." />
            {databases.isPending ? (
                <Loading />
            ) : databases.isError ? (
                <ErrorView code="500" onRetry={() => databases.refetch()} />
            ) : (
                <div className="space-y-4">
                    <ResponsiveList
                        caption="Daftar database yang dapat diakses"
                        columns={columns}
                        rows={databases.data.items}
                        rowKey={(d) => d.id}
                        minWidth="30rem"
                        empty={<EmptyState title="Belum ada database yang bisa diakses." description="Minta admin menambahkan Anda ke grup yang punya akses database." />}
                    />
                    <Pagination page={page} pageSize={viewerPageSize} total={databases.data.total} onChange={setPage} />
                </div>
            )}
        </div>
    );
}
