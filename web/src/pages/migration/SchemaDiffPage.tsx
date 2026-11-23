import { useState } from "react";
import { useNavigate, useParams, useSearchParams } from "react-router";
import { Badge } from "../../components/Badge";
import { Button } from "../../components/Button";
import { DataTable, type Column } from "../../components/DataTable";
import { Field, FormError, Select, fieldErrors } from "../../components/Form";
import { Loading } from "../../components/Loading";
import { PageHeader, Section } from "../../components/PageHeader";
import { SqlEditor } from "../../components/SqlEditor";
import { Switch } from "../../components/Switch";
import { isApiError } from "../../lib/api";
import { useCurrentUser } from "../../lib/currentUser";
import { useSchemaDiffPreview, useSchemaDiffSource, type SchemaChange, type SchemaDiffResult, type SchemaDiffSource } from "../../lib/schemaDiff";
import { levelLabel } from "../../lib/session";
import { ErrorView } from "../ErrorPage";
import { DatabaseTabs } from "./DatabaseTabs";
import { databaseName, levelFor } from "./shared";

export type MigrationDraft = { title: string; sqlUp: string };

const changeColumns: Column<SchemaChange>[] = [
    {
        key: "op",
        header: "Operasi",
        nowrap: true,
        render: (c) => (
            <span className="inline-flex items-center gap-2">
                {c.op_label}
                {c.destructive && <Badge tone="off">destruktif</Badge>}
            </span>
        ),
    },
    { key: "kind", header: "Jenis", nowrap: true, render: (c) => <span className="text-slate-600">{c.kind}</span> },
    { key: "object", header: "Objek", render: (c) => <span className="break-all font-mono text-xs">{c.object}</span> },
    { key: "detail", header: "Detail", render: (c) => c.detail },
];

export function SchemaDiffPage() {
    const { dbId = "" } = useParams();
    const me = useCurrentUser();
    const [params, setParams] = useSearchParams();
    const schema = params.get("schema") ?? "";
    const level = levelFor(me, dbId);
    const engine = me.databases.find((d) => String(d.id) === dbId)?.engine;
    const unsupported = engine === "mysql" || engine === "mariadb";
    const source = useSchemaDiffSource(level === "edit" && !unsupported ? dbId : "", schema);

    if (level !== "edit") {
        return <ErrorView code="403" />;
    }
    const header = (
        <>
            <PageHeader
                title="Schema diff"
                description={
                    <span className="flex flex-wrap items-center gap-2">
                        {databaseName(me, dbId)}
                        <Badge tone="info">{levelLabel(level)}</Badge>
                    </span>
                }
                back={me.databases.length > 1 ? { to: "/migration", label: "Pilih database" } : undefined}
            />
            <DatabaseTabs dbId={dbId} />
        </>
    );
    if (unsupported) {
        return (
            <div className="space-y-6">
                {header}
                <Section title="Tidak tersedia">
                    <p className="text-sm text-slate-600">Schema diff hanya tersedia untuk PostgreSQL.</p>
                </Section>
            </div>
        );
    }
    const selectSchema = (next: string) => setParams(next ? { schema: next } : {});
    return (
        <div className="space-y-6">
            {header}
            {source.isPending ? (
                <Loading />
            ) : source.isError ? (
                isApiError(source.error) && source.error.status === 422 ? (
                    <Section title="Schema tidak ditemukan">
                        <FormError message={source.error.message} />
                        <Button variant="secondary" onClick={() => selectSchema("")}>
                            Pakai schema bawaan
                        </Button>
                    </Section>
                ) : (
                    <ErrorView code={isApiError(source.error) && source.error.status === 404 ? "404" : "500"} onRetry={() => source.refetch()} />
                )
            ) : source.data.schemas.length === 0 ? (
                <Section title="Tidak ada schema">
                    <p className="text-sm text-slate-600">Database ini tidak punya schema yang dapat dibandingkan.</p>
                </Section>
            ) : (
                <Editor key={source.data.schema} dbId={dbId} source={source.data} onSchema={selectSchema} />
            )}
        </div>
    );
}

function Editor({ dbId, source, onSchema }: { dbId: string; source: SchemaDiffSource; onSchema: (schema: string) => void }) {
    const [desired, setDesired] = useState(source.current_ddl);
    const [destructive, setDestructive] = useState(false);
    const preview = useSchemaDiffPreview(dbId);
    const errors = isApiError(preview.error) && preview.error.status === 422 ? fieldErrors(preview.error.details) : {};
    const general = isApiError(preview.error) && preview.error.status === 422 && !errors.desired_ddl && !errors.schema ? preview.error.message : null;

    const compare = () => preview.mutate({ schema: source.schema, desired_ddl: desired, include_destructive: destructive });

    return (
        <>
            <Section title="Kondisi DDL yang diinginkan" description="Ubah DDL di bawah, lalu tekan Bandingkan.">
                <Field label="Schema" error={errors.schema}>
                    {(p) => (
                        <Select {...p} className="sm:w-64" value={source.schema} onChange={(e) => onSchema(e.target.value)}>
                            {source.schemas.map((s) => (
                                <option key={s} value={s}>
                                    {s}
                                </option>
                            ))}
                        </Select>
                    )}
                </Field>
                <Field label="DDL" error={errors.desired_ddl}>
                    {(p) => (
                        <SqlEditor
                            id={p.id}
                            aria-invalid={p["aria-invalid"]}
                            aria-describedby={p["aria-describedby"]}
                            value={desired}
                            onChange={(v) => {
                                setDesired(v);
                                preview.reset();
                            }}
                            rows={18}
                        />
                    )}
                </Field>
                <Switch label="Sertakan perubahan destruktif (DROP)" checked={destructive} onChange={setDestructive} />
                <FormError message={general} />
                <div className="flex flex-wrap gap-2">
                    <Button loading={preview.isPending} onClick={compare}>
                        Bandingkan
                    </Button>
                    {desired !== source.current_ddl && (
                        <Button
                            variant="ghost"
                            onClick={() => {
                                setDesired(source.current_ddl);
                                preview.reset();
                            }}
                        >
                            Kembalikan ke DDL saat ini
                        </Button>
                    )}
                </div>
            </Section>
            {preview.data && <Result dbId={dbId} result={preview.data} />}
        </>
    );
}

function Result({ dbId, result }: { dbId: string; result: SchemaDiffResult }) {
    const navigate = useNavigate();
    const summary =
        result.changes.length === 0
            ? "Tidak ada perbedaan antara schema saat ini dan DDL yang diinginkan."
            : result.statements === 0
              ? "Tidak ada statement aktif. Aktifkan perubahan destruktif bila ingin menghapus objek."
              : `${result.statements} statement aktif${result.skipped > 0 ? `, ${result.skipped} statement destruktif dilewati` : ""}.`;
    const create = () => navigate(`/migration/database/${dbId}/baru`, { state: { draft: { title: result.title, sqlUp: result.sql } satisfies MigrationDraft } });
    return (
        <Section title="Hasil" description={summary}>
            {result.warnings.length > 0 && (
                <ul className="list-disc space-y-1 rounded-xl border border-amber-200 bg-amber-50 py-3 pl-8 pr-4 text-sm text-amber-900">
                    {result.warnings.map((w) => (
                        <li key={w}>{w}</li>
                    ))}
                </ul>
            )}
            {result.changes.length > 0 && <DataTable bare columns={changeColumns} rows={result.changes} rowKey={(c) => `${c.op}-${c.kind}-${c.object}-${c.detail}`} minWidth="36rem" />}
            {result.sql.trim() !== "" && (
                <div className="min-w-0 space-y-1">
                    <h3 className="text-sm font-medium text-slate-800">Kandidat SQL up</h3>
                    <SqlEditor value={result.sql} readOnly rows={12} aria-label="Kandidat SQL up" />
                </div>
            )}
            <FormError message={result.too_large ? "Hasil SQL melebihi 100 KB." : null} />
            {result.can_create && (
                <div className="flex flex-wrap gap-2">
                    <Button onClick={create}>Buat migration</Button>
                </div>
            )}
        </Section>
    );
}
