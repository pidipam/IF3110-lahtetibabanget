import type { ReactNode } from "react";
import { EmptyState } from "./EmptyState";

export type Column<T> = {
    key: string;
    header: ReactNode;
    render: (row: T) => ReactNode;
    className?: string;
    nowrap?: boolean;
};

type Props<T> = {
    columns: Column<T>[];
    rows: T[];
    rowKey: (row: T) => string | number;
    caption?: string;
    empty?: ReactNode;
    minWidth?: string;
};

export function DataTable<T>({ columns, rows, rowKey, caption, empty, minWidth = "40rem" }: Props<T>) {
    if (rows.length === 0) {
        return <>{empty ?? <EmptyState title="Belum ada data." />}</>;
    }
    return (
        <div className="w-full max-w-full overflow-x-auto rounded-lg ring-1 ring-slate-200" tabIndex={0} role="region" aria-label={caption ?? "Tabel"}>
            <table className="w-full divide-y divide-slate-200 text-left text-sm" style={{ minWidth }}>
                {caption && <caption className="sr-only">{caption}</caption>}
                <thead className="bg-slate-50">
                    <tr>
                        {columns.map((c) => (
                            <th key={c.key} scope="col" className={`whitespace-nowrap px-4 py-3 font-semibold text-slate-700 ${c.className ?? ""}`}>
                                {c.header}
                            </th>
                        ))}
                    </tr>
                </thead>
                <tbody className="divide-y divide-slate-100 bg-white">
                    {rows.map((row) => (
                        <tr key={rowKey(row)} className="hover:bg-slate-50">
                            {columns.map((c) => (
                                <td key={c.key} className={`px-4 py-3 align-top text-slate-800 ${c.nowrap ? "whitespace-nowrap" : ""} ${c.className ?? ""}`}>
                                    {c.render(row)}
                                </td>
                            ))}
                        </tr>
                    ))}
                </tbody>
            </table>
        </div>
    );
}
