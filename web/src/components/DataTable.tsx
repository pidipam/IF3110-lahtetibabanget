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
    bare?: boolean;
};

export function DataTable<T>({ columns, rows, rowKey, caption, empty, minWidth = "40rem", bare = false }: Props<T>) {
    if (rows.length === 0) {
        return <>{empty ?? <EmptyState title="Belum ada data." />}</>;
    }
    return (
        <div className={`w-full max-w-full overflow-x-auto ${bare ? "" : "card px-3 py-1"}`} tabIndex={0} role="region" aria-label={caption ?? "Tabel"}>
            <table className="data-table" style={{ minWidth }}>
                {caption && <caption className="sr-only">{caption}</caption>}
                <thead>
                    <tr>
                        {columns.map((c) => (
                            <th key={c.key} scope="col" className={c.className ?? ""}>
                                {c.header}
                            </th>
                        ))}
                    </tr>
                </thead>
                <tbody>
                    {rows.map((row) => (
                        <tr key={rowKey(row)}>
                            {columns.map((c) => (
                                <td key={c.key} className={`text-slate-900 ${c.nowrap ? "whitespace-nowrap" : ""} ${c.className ?? ""}`}>
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
