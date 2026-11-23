import { useState } from "react";
import { Link, useParams, useSearchParams } from "react-router";
import { Badge } from "../../components/Badge";
import { Button } from "../../components/Button";
import { DataTable, type Column } from "../../components/DataTable";
import { EmptyState } from "../../components/EmptyState";
import { Loading } from "../../components/Loading";
import { PageHeader, Section } from "../../components/PageHeader";
import { Pagination } from "../../components/Pagination";
import { isApiError } from "../../lib/api";
import { useDatabase, useTablePage, viewerPageSize, type Cell, type TableMeta } from "../../lib/viewer";
import { ErrorView } from "../ErrorPage";

type ViewerRow = { index: number; cells: Cell[] };

function CellView({ cell }: { cell: Cell }) {
    if (cell.null) {
        return <span className="italic text-slate-500">NULL</span>;
    }
    return (
        <span className="block max-w-[24rem] truncate" title={cell.text}>
            {cell.text}
            {cell.truncated && <span className="ml-1 text-indigo-700">…</span>}
        </span>
    );
}

function errorCode(error: unknown): string {
    if (isApiError(error) && ["403", "404", "503"].includes(String(error.status))) {
        return String(error.status);
    }
    return "500";
}

export function DatabaseDetailPage() {
    const rawId = useParams().dbId;
    const parsed = rawId ? Number(rawId) : undefined;
    const id = parsed !== undefined && Number.isInteger(parsed) && parsed > 0 ? parsed : undefined;
    const detail = useDatabase(id);
    const [params, setParams] = useSearchParams();
    const [showMeta, setShowMeta] = useState(false);
    const schemaParam = params.get("schema") ?? "";
    const tableParam = params.get("table") ?? "";
    const rawPage = Number(params.get("page"));
    const page = Number.isInteger(rawPage) && rawPage > 0 ? rawPage : 1;

    const tables: TableMeta[] = detail.data?.schemas.flatMap((s) => s.tables) ?? [];
    const current = tables.find((t) => t.schema === schemaParam && t.name === tableParam) ?? tables[0];
    const tablePage = useTablePage(id, current?.schema, current?.name, page);

    const selectTable = (schema: string, name: string) => {
        setParams((prev) => {
            const next = new URLSearchParams(prev);
            next.set("schema", schema);
            next.set("table", name);
            next.delete("page");
            return next;
        });
    };

    const changePage = (next: number) => {
        setParams((prev) => {
            const query = new URLSearchParams(prev);
            if (next > 1) {
                query.set("page", String(next));
            } else {
                query.delete("page");
            }
            return query;
        });
    };

    if (detail.isPending) {
        return <Loading />;
    }
    if (detail.isError || !detail.data || id === undefined) {
        const code = errorCode(detail.error);
        return <ErrorView code={code} onRetry={code === "500" ? () => detail.refetch() : undefined} />;
    }

    const columns: Column<ViewerRow>[] = [
        { key: "no", header: "#", nowrap: true, render: (r) => <span className="text-slate-500">{r.index}</span> },
        ...(tablePage.data?.columns ?? []).map((name, index) => ({
            key: `col-${index}`,
            header: name,
            nowrap: true,
            render: (r: ViewerRow) => <CellView cell={r.cells[index]} />,
        })),
    ];
    const rows: ViewerRow[] = (tablePage.data?.rows ?? []).map((cells, index) => ({ index: (page - 1) * viewerPageSize + index + 1, cells }));
    const minWidth = `${Math.max(32, (tablePage.data?.columns.length ?? 0) * 10 + 4)}rem`;

    return (
        <div className="space-y-6">
            <PageHeader
                title={detail.data.name}
                description={
                    <span className="flex flex-wrap items-center gap-2">
                        <Badge tone={detail.data.level === "edit" ? "admin" : "neutral"}>{detail.data.level === "edit" ? "View + Edit" : "View Only"}</Badge>
                        <span>{detail.data.schemas.length} schema</span>
                        <span>{tables.length} tabel</span>
                    </span>
                }
                back={{ to: "/database", label: "Daftar database" }}
            />
            <div className="grid gap-6 lg:grid-cols-[16rem_minmax(0,1fr)]">
                <aside className="card min-w-0 space-y-4 p-4">
                    <div>
                        <label htmlFor="pilih-tabel" className="field-label">
                            Pilih tabel
                        </label>
                        <select
                            id="pilih-tabel"
                            className="field mt-1.5"
                            value={current ? `${current.schema}\u0000${current.name}` : ""}
                            disabled={tables.length === 0}
                            onChange={(e) => {
                                const [schema, name] = e.target.value.split("\u0000");
                                if (schema && name) {
                                    selectTable(schema, name);
                                }
                            }}
                        >
                            {tables.length === 0 && <option value="">Tidak ada tabel</option>}
                            {detail.data.schemas.map((s) => (
                                <optgroup key={s.name} label={s.name}>
                                    {s.tables.map((t) => (
                                        <option key={`${s.name}.${t.name}`} value={`${t.schema}\u0000${t.name}`}>
                                            {t.name}
                                        </option>
                                    ))}
                                </optgroup>
                            ))}
                        </select>
                    </div>
                    <div className="hidden space-y-4 lg:block">
                        {detail.data.schemas.map((s) => (
                            <div key={s.name} className="min-w-0">
                                <h2 className="mb-1 text-xs font-semibold uppercase tracking-wide text-slate-500">{s.name}</h2>
                                <ul className="space-y-1">
                                    {s.tables.map((t) => {
                                        const active = current?.schema === t.schema && current.name === t.name;
                                        return (
                                            <li key={`${t.schema}.${t.name}`}>
                                                <button
                                                    type="button"
                                                    onClick={() => selectTable(t.schema, t.name)}
                                                    aria-current={active ? "true" : undefined}
                                                    className={`w-full truncate rounded-lg px-3 py-2 text-left text-sm ${active ? "bg-indigo-600 font-semibold text-on-pink" : "text-slate-700 hover:bg-surface-2"}`}
                                                >
                                                    {t.name}
                                                </button>
                                            </li>
                                        );
                                    })}
                                </ul>
                            </div>
                        ))}
                    </div>
                </aside>
                <div className="min-w-0 space-y-6">
                    {!current ? (
                        <EmptyState title="Database ini belum punya tabel." description="Buat migration pertama untuk menambahkan tabel." />
                    ) : (
                        <>
                            <div className="flex flex-wrap items-center justify-between gap-3 rounded-2xl border border-line bg-surface px-4 py-3">
                                <div className="min-w-0">
                                    <p className="break-all font-medium text-slate-900">{current.schema}.{current.name}</p>
                                    <p className="text-xs text-slate-500">{current.kind_label} · {current.columns.length} kolom</p>
                                </div>
                                <Button variant="ghost" onClick={() => setShowMeta((v) => !v)} aria-expanded={showMeta}>
                                    {showMeta ? "Sembunyikan struktur" : "Lihat struktur"}
                                </Button>
                            </div>
                            {showMeta && (
                                <div className="space-y-4">
                                    <Section title="Kolom">
                                        <div className="overflow-x-auto">
                                            <table className="data-table" style={{ minWidth: "36rem" }}>
                                                <thead>
                                                    <tr>
                                                        <th scope="col">#</th>
                                                        <th scope="col">Nama</th>
                                                        <th scope="col">Tipe</th>
                                                        <th scope="col">Nullable</th>
                                                        <th scope="col">Default</th>
                                                    </tr>
                                                </thead>
                                                <tbody>
                                                    {current.columns.map((c) => (
                                                        <tr key={c.name}>
                                                            <td className="text-slate-500">{c.position}</td>
                                                            <td className="font-mono text-slate-900">{c.name}</td>
                                                            <td className="font-mono text-xs text-slate-700">{c.type}</td>
                                                            <td>{c.nullable ? "Ya" : "Tidak"}</td>
                                                            <td className="font-mono text-xs text-slate-700">{c.has_default ? c.default : "—"}</td>
                                                        </tr>
                                                    ))}
                                                </tbody>
                                            </table>
                                        </div>
                                    </Section>
                                    {(current.constraints.length > 0 || current.indexes.length > 0) && (
                                        <Section title="Constraint dan index">
                                            <ul className="space-y-3 text-sm">
                                                {current.constraints.map((c) => (
                                                    <li key={`${c.type}-${c.name}`} className="min-w-0">
                                                        <span className="font-medium text-slate-900">{c.name}</span>
                                                        <span className="ml-2 text-slate-500">{c.type}: {c.columns.join(", ") || c.definition}</span>
                                                    </li>
                                                ))}
                                                {current.indexes.map((i) => (
                                                    <li key={i.name} className="min-w-0 break-words text-slate-700">
                                                        {i.name} · {i.primary ? "primary" : i.unique ? "unique" : "index"} · <span className="font-mono text-xs">{i.definition}</span>
                                                    </li>
                                                ))}
                                            </ul>
                                        </Section>
                                    )}
                                </div>
                            )}
                            <Section title="Data tabel" description="Gulir horizontal untuk melihat semua kolom. Sel panjang dipotong dan bisa dilihat lewat tooltip.">
                                {tablePage.isPending ? (
                                    <Loading label="Memuat data..." />
                                ) : tablePage.isError ? (
                                    <ErrorView code={errorCode(tablePage.error)} onRetry={() => tablePage.refetch()} />
                                ) : (
                                    <div className="space-y-4">
                                        <DataTable
                                            columns={columns}
                                            rows={rows}
                                            rowKey={(r) => r.index}
                                            caption={`Data tabel ${current.schema}.${current.name}`}
                                            minWidth={minWidth}
                                            bare
                                            empty={<EmptyState title="Tabel ini kosong." />}
                                        />
                                        <Pagination page={page} pageSize={viewerPageSize} total={tablePage.data?.pagination.total ?? 0} onChange={changePage} />
                                        {tablePage.data && (
                                            <p className="text-xs text-slate-500">
                                                Urutan: {tablePage.data.order}
                                            </p>
                                        )}
                                    </div>
                                )}
                            </Section>
                            <p className="text-sm text-slate-600">
                                <Link to={`/migration/database/${id}`} className="font-medium text-indigo-700 hover:underline">
                                    Lihat migration database ini
                                </Link>
                            </p>
                        </>
                    )}
                </div>
            </div>
        </div>
    );
}
