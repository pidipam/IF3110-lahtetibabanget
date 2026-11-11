import { Link } from "react-router";
import { DataTable, type Column } from "../components/DataTable";
import { Badge } from "../components/Badge";
import { EmptyState } from "../components/EmptyState";
import { levelLabel, type DatabaseAccess, type Me } from "../lib/session";
import { useCurrentUser } from "../lib/currentUser";

const engineLabel: Record<string, string> = { postgres: "PostgreSQL", mysql: "MySQL", mariadb: "MariaDB" };

const columns: Column<DatabaseAccess>[] = [
    {
        key: "name",
        header: "Nama",
        render: (d) => (
            <Link to={`/migration/database/${d.id}`} className="font-medium text-indigo-700 hover:underline">
                {d.name}
            </Link>
        ),
    },
    { key: "engine", header: "Engine", nowrap: true, render: (d) => engineLabel[d.engine] ?? d.engine },
    {
        key: "level",
        header: "Akses",
        nowrap: true,
        render: (d) => <Badge tone={d.level === "edit" ? "admin" : "neutral"}>{levelLabel(d.level)}</Badge>,
    },
];

function emptyText(me: Me) {
    if (me.is_admin) {
        return "Admin Platform tidak otomatis punya akses database. Tambahkan diri Anda ke grup yang punya akses bila perlu.";
    }
    return "Minta admin menambahkan Anda ke grup yang punya akses database.";
}

export function HomePage() {
    const me = useCurrentUser();
    return (
        <div>
            <section className="hero">
                <h1 className="break-words">Halo, {me.user.username}</h1>
                <p>
                    Anda masuk sebagai <Badge tone={me.is_admin ? "admin" : "neutral"}>{me.is_admin ? "Platform Admin" : "User"}</Badge>
                </p>
            </section>
            <section className="card space-y-4 p-5 sm:p-7">
                <h2 className="text-[19px] font-semibold text-slate-900">Database Anda</h2>
                <DataTable bare caption="Database yang bisa diakses" columns={columns} rows={me.databases} rowKey={(d) => d.id} minWidth="18rem" empty={<EmptyState title="Belum ada akses database." description={emptyText(me)} />} />
            </section>
        </div>
    );
}
