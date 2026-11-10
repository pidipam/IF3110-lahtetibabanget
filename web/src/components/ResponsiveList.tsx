import type { ReactNode } from "react";
import { DataTable, type Column } from "./DataTable";
import { EmptyState } from "./EmptyState";

type Props<T> = {
    columns: Column<T>[];
    rows: T[];
    rowKey: (row: T) => string | number;
    caption: string;
    empty?: ReactNode;
    minWidth?: string;
};

export function ResponsiveList<T>({ columns, rows, rowKey, caption, empty, minWidth }: Props<T>) {
    if (rows.length === 0) {
        return <>{empty ?? <EmptyState title="Belum ada data." />}</>;
    }
    const [first, ...rest] = columns;
    return (
        <>
            <div className="hidden md:block">
                <DataTable columns={columns} rows={rows} rowKey={rowKey} caption={caption} minWidth={minWidth} />
            </div>
            <ul aria-label={caption} className="space-y-3 md:hidden">
                {rows.map((row) => (
                    <li key={rowKey(row)} className="card p-4">
                        <div className="mb-2 min-w-0 break-words font-medium text-slate-900">{first.render(row)}</div>
                        <dl className="space-y-2 text-sm">
                            {rest.map((c) => (
                                <div key={c.key} className="flex min-w-0 items-center justify-between gap-3">
                                    <dt className="shrink-0 text-slate-500">{c.header}</dt>
                                    <dd className="min-w-0 break-words text-right text-slate-800">{c.render(row)}</dd>
                                </div>
                            ))}
                        </dl>
                    </li>
                ))}
            </ul>
        </>
    );
}
