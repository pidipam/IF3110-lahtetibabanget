import { DataTable, type Column } from "../components/DataTable";
import { EmptyState } from "../components/EmptyState";
import { levelLabel, type DatabaseAccess, type Me } from "../lib/session";
import { useCurrentUser } from "../lib/currentUser";

const engineLabel: Record<string, string> = { postgres: "PostgreSQL", mysql: "MySQL", mariadb: "MariaDB" };

const columns: Column<DatabaseAccess>[] = [
    { key: "name", header: "Nama", render: (d) => <span className="font-medium">{d.name}</span> },
    { key: "engine", header: "Engine", nowrap: true, render: (d) => engineLabel[d.engine] ?? d.engine },
    {
        key: "level",
        header: "Akses",
        nowrap: true,
        render: (d) => <span className={`rounded px-2 py-0.5 text-xs font-medium ${d.level === "edit" ? "bg-indigo-100 text-indigo-800" : "bg-slate-100 text-slate-700"}`}>{levelLabel(d.level)}</span>,
    },
];

function emptyText(me: Me) {
    if (me.is_admin) {
        return "Admin Platform tidak otomatis punya akses database. Tambahkan diri Anda ke Grup yang punya akses bila perlu.";
    }
    return "Minta admin menambahkan Anda ke Grup yang punya akses database.";
}

export function HomePage() {
    const me = useCurrentUser();
    return (
        <div className="space-y-6">
            <div>
                <h1 className="text-2xl font-bold text-slate-900">Halo, {me.user.username}</h1>
                <p className="mt-1 text-sm text-slate-600">{me.is_admin ? "Anda masuk sebagai Admin Platform." : "Berikut database yang bisa Anda akses."}</p>
            </div>
            <section className="space-y-3">
                <h2 className="text-lg font-semibold text-slate-900">Database</h2>
                <DataTable caption="Database yang bisa diakses" columns={columns} rows={me.databases} rowKey={(d) => d.id} minWidth="28rem" empty={<EmptyState title="Belum ada akses database." description={emptyText(me)} />} />
            </section>
        </div>
    );
}
