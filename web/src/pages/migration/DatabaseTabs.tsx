import { NavLink } from "react-router";

const tabClass = ({ isActive }: { isActive: boolean }) => `-mb-px inline-flex min-h-10 items-center border-b-2 px-3 text-sm font-medium ${isActive ? "border-indigo-600 text-indigo-700" : "border-transparent text-slate-600 hover:border-slate-300 hover:text-slate-800"}`;

export function DatabaseTabs({ dbId }: { dbId: string }) {
    return (
        <nav aria-label="Bagian database" className="flex gap-2 border-b border-slate-200">
            <NavLink to={`/migration/database/${dbId}`} end className={tabClass}>
                Migration
            </NavLink>
            <NavLink to={`/migration/database/${dbId}/riwayat`} className={tabClass}>
                Riwayat
            </NavLink>
        </nav>
    );
}
