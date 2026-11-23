import { Button } from "../../components/Button";
import { DataTable, type Column } from "../../components/DataTable";
import { FormError } from "../../components/Form";
import { Section } from "../../components/PageHeader";
import { isApiError } from "../../lib/api";
import { useExplain, type Explanation, type PlanNode, type StatementPlan } from "../../lib/explain";

const fixed = (n: number, digits: number) => n.toLocaleString("id-ID", { minimumFractionDigits: digits, maximumFractionDigits: digits });

function planColumns(analyze: boolean): Column<PlanNode>[] {
    const cols: Column<PlanNode>[] = [
        {
            key: "node",
            header: "Node",
            nowrap: true,
            render: (n) => (
                <span>
                    <span className="whitespace-pre font-mono text-slate-500">{n.prefix}</span>
                    <span className="font-medium text-slate-900">{n.node}</span>
                </span>
            ),
        },
        { key: "relation", header: "Tabel", render: (n) => n.relation },
        { key: "index", header: "Index", render: (n) => n.index },
        { key: "join", header: "Join", render: (n) => n.join },
        { key: "cost", header: "Estimasi cost", nowrap: true, render: (n) => <span className="font-mono">{`${fixed(n.startup_cost, 2)}..${fixed(n.total_cost, 2)}`}</span> },
        { key: "rows", header: "Estimasi baris", nowrap: true, render: (n) => fixed(n.plan_rows, 0) },
    ];
    if (analyze) {
        cols.push(
            { key: "actual_rows", header: "Baris aktual", nowrap: true, render: (n) => (n.has_actual ? fixed(n.actual_rows, 0) : "") },
            { key: "actual_time", header: "Waktu (ms)", nowrap: true, render: (n) => (n.has_actual ? fixed(n.actual_time, 3) : "") },
            { key: "loops", header: "Loops", nowrap: true, render: (n) => (n.has_actual ? fixed(n.loops, 0) : "") },
        );
    }
    return cols;
}

function Statement({ s, analyze }: { s: StatementPlan; analyze: boolean }) {
    return (
        <div className="min-w-0 space-y-3">
            <p className="text-sm font-semibold text-slate-900">
                Statement {s.number}
                {s.no_plan && <span className="ml-2 font-normal text-slate-500">tanpa execution plan</span>}
            </p>
            <pre className="max-h-48 overflow-auto whitespace-pre-wrap break-words rounded-xl border border-line bg-surface px-3 py-2 font-mono text-xs text-slate-800">{s.sql}</pre>
            {s.error ? (
                <FormError message={`Statement gagal: ${s.error}`} />
            ) : (
                !s.no_plan && (
                    <>
                        <DataTable bare columns={planColumns(analyze)} rows={s.nodes} rowKey={(n) => `${n.depth}-${n.prefix}-${n.node}-${n.relation}`} minWidth={analyze ? "52rem" : "40rem"} />
                        {s.has_timing && (
                            <p className="text-xs text-slate-500">
                                Waktu perencanaan {fixed(s.planning_time, 3)} ms{analyze && `, waktu eksekusi ${fixed(s.execution_time, 3)} ms`}.
                            </p>
                        )}
                    </>
                )
            )}
        </div>
    );
}

function Result({ data }: { data: Explanation }) {
    return (
        <div className="space-y-6 border-t border-line pt-5">
            <p className="text-sm font-semibold text-slate-900">
                Hasil {data.analyze ? "EXPLAIN ANALYZE" : "EXPLAIN"} untuk revisi {data.revision_number}
            </p>
            {data.statements.map((s) => (
                <Statement key={s.number} s={s} analyze={data.analyze} />
            ))}
            {data.stopped && <p className="text-sm text-slate-600">Analisis berhenti di statement yang gagal.</p>}
        </div>
    );
}

export function ExplainPanel({ migrationId, engine }: { migrationId: string; engine?: string }) {
    const explain = useExplain(migrationId);
    if (engine === "mysql" || engine === "mariadb") {
        return (
            <Section title="Analisis execution plan">
                <p className="text-sm text-slate-600">Analisis execution plan belum didukung untuk MySQL/MariaDB.</p>
            </Section>
        );
    }
    const error = isApiError(explain.error) && (explain.error.status === 409 || explain.error.status === 422) ? explain.error.message : null;
    const running = explain.isPending ? explain.variables : undefined;
    return (
        <Section title="Analisis execution plan" description="EXPLAIN ANALYZE juga mengukur waktu dan baris aktual. Semua perubahan di-rollback.">
            <div className="flex flex-wrap gap-2">
                <Button variant="secondary" loading={running === false} disabled={explain.isPending} onClick={() => explain.mutate(false)}>
                    EXPLAIN
                </Button>
                <Button variant="secondary" loading={running === true} disabled={explain.isPending} onClick={() => explain.mutate(true)}>
                    EXPLAIN ANALYZE
                </Button>
            </div>
            <FormError message={error} />
            {explain.data && !explain.isPending && <Result data={explain.data} />}
        </Section>
    );
}
