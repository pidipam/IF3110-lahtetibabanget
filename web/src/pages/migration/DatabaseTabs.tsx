import { NavLink } from "react-router";
import { useCurrentUser } from "../../lib/currentUser";

const tabClass = ({ isActive }: { isActive: boolean }) => `-mb-px inline-flex min-h-10 items-center whitespace-nowrap border-b-2 px-3 text-sm font-medium ${isActive ? "border-indigo-600 text-indigo-700" : "border-transparent text-slate-600 hover:border-slate-300 hover:text-slate-800"}`;

export function DatabaseTabs({ dbId }: { dbId: string }) {
    const me = useCurrentUser();
    const db = me.databases.find((d) => String(d.id) === dbId);
    const schemaDiff = db?.level === "edit" && db.engine !== "mysql" && db.engine !== "mariadb";
    return (
        <nav aria-label="Bagian database" className="flex gap-2 overflow-x-auto border-b border-slate-200">
            <NavLink to={`/migration/database/${dbId}`} end className={tabClass}>
                Migration
            </NavLink>
            <NavLink to={`/migration/database/${dbId}/riwayat`} className={tabClass}>
                Riwayat
            </NavLink>
            {schemaDiff && (
                <NavLink to={`/migration/database/${dbId}/schema-diff`} className={tabClass}>
                    Schema diff
                </NavLink>
            )}
        </nav>
    );
}
