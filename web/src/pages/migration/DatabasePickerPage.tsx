import { Link, Navigate } from "react-router";
import { Badge } from "../../components/Badge";
import { EmptyState } from "../../components/EmptyState";
import { PageHeader } from "../../components/PageHeader";
import { useCurrentUser } from "../../lib/currentUser";
import { levelLabel } from "../../lib/session";

const engineLabel: Record<string, string> = { postgres: "PostgreSQL", mysql: "MySQL", mariadb: "MariaDB" };

export function DatabasePickerPage() {
    const me = useCurrentUser();
    if (me.databases.length === 1) {
        return <Navigate to={`/migration/database/${me.databases[0].id}`} replace />;
    }
    return (
        <div className="space-y-6">
            <PageHeader title="Migration" description="Pilih database untuk melihat atau membuat migration." />
            {me.databases.length === 0 ? (
                <EmptyState title="Belum ada akses database." description="Minta admin menambahkan Anda ke Grup yang punya akses database." />
            ) : (
                <ul aria-label="Database" className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
                    {me.databases.map((d) => (
                        <li key={d.id}>
                            <Link to={`/migration/database/${d.id}`} className="flex h-full flex-col gap-2 rounded-lg bg-white p-4 ring-1 ring-slate-200 hover:ring-indigo-300 focus-visible:outline-2 focus-visible:outline-indigo-600">
                                <span className="break-words font-semibold text-slate-900">{d.name}</span>
                                <span className="flex flex-wrap items-center gap-2 text-sm text-slate-600">
                                    {engineLabel[d.engine] ?? d.engine}
                                    <Badge tone={d.level === "edit" ? "info" : "neutral"}>{levelLabel(d.level)}</Badge>
                                </span>
                            </Link>
                        </li>
                    ))}
                </ul>
            )}
        </div>
    );
}
