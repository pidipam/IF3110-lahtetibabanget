import { useState, type FormEvent } from "react";
import { useNavigate, useParams } from "react-router";
import { Button } from "../../components/Button";
import { Field, FormError, Input, Textarea, fieldErrors } from "../../components/Form";
import { Loading } from "../../components/Loading";
import { PageHeader, Section } from "../../components/PageHeader";
import { SqlEditor } from "../../components/SqlEditor";
import { Switch } from "../../components/Switch";
import { isApiError } from "../../lib/api";
import { useCurrentUser } from "../../lib/currentUser";
import { isInlineError, useCreateMigration, useMigration, useUpdateMigration } from "../../lib/migrations";
import { canEdit, type Migration, type MigrationInput } from "../../lib/migrations/types";
import { emptyValues, maxBatchSize, toInput, validate, valuesFrom, type EditorValues } from "../../lib/migrations/validate";
import { toast } from "../../lib/toast";
import { ErrorView } from "../ErrorPage";
import { MockBanner, StatusBadge, databaseName, levelFor } from "./shared";

const fieldOrder = ["title", "sql_up", "sql_down", "irreversible_reason", "batch_column", "batch_size"];

export function MigrationNewPage() {
    const { dbId = "" } = useParams();
    const me = useCurrentUser();
    const create = useCreateMigration(dbId);
    if (levelFor(me, dbId) !== "edit") {
        return <ErrorView code="403" />;
    }
    return <MigrationEditor title="Buat migration" dbId={dbId} initial={emptyValues} back={`/migration/database/${dbId}`} pending={create.isPending} save={(input, cb) => create.mutate(input, cb)} />;
}

export function MigrationEditPage() {
    const { id = "" } = useParams();
    const me = useCurrentUser();
    const migration = useMigration(id);
    const update = useUpdateMigration(id);
    if (migration.isPending) {
        return <Loading />;
    }
    if (migration.isError) {
        const code = isApiError(migration.error) && migration.error.status === 404 ? "404" : "500";
        return <ErrorView code={code} onRetry={code === "500" ? () => migration.refetch() : undefined} />;
    }
    const m = migration.data;
    if (levelFor(me, m.database_id) !== "edit") {
        return <ErrorView code="403" />;
    }
    if (!canEdit(m.status)) {
        return (
            <div className="mx-auto max-w-xl space-y-6">
                <PageHeader title={m.title} back={{ to: `/migration/${m.id}`, label: "Kembali ke review" }} />
                <Section title="Tidak bisa diubah" description="Migration dengan status ini tidak bisa diubah lagi.">
                    <StatusBadge status={m.status} />
                </Section>
            </div>
        );
    }
    return <MigrationEditor key={m.current_revision.id} title="Ubah migration" dbId={m.database_id} migration={m} initial={valuesFrom(m.title, m.current_revision)} back={`/migration/${m.id}`} pending={update.isPending} save={(input, cb) => update.mutate(input, cb)} />;
}

type SaveCallbacks = {
    onSuccess: (m: Migration) => void;
    onError: (err: unknown) => void;
};

type EditorProps = {
    title: string;
    dbId: string;
    migration?: Migration;
    initial: EditorValues;
    back: string;
    pending: boolean;
    save: (input: MigrationInput, cb: SaveCallbacks) => void;
};

function MigrationEditor({ title, dbId, migration, initial, back, pending, save }: EditorProps) {
    const me = useCurrentUser();
    const navigate = useNavigate();
    const [v, setV] = useState<EditorValues>(initial);
    const [errors, setErrors] = useState<Record<string, string>>({});
    const [formError, setFormError] = useState<string | null>(null);
    const set = <K extends keyof EditorValues>(key: K, value: EditorValues[K]) => setV((prev) => ({ ...prev, [key]: value }));

    const focusFirst = (errs: Record<string, string>) => {
        const first = fieldOrder.find((f) => errs[f]);
        if (first) {
            window.requestAnimationFrame(() => document.querySelector<HTMLElement>(`[data-field="${first}"] textarea, [data-field="${first}"] input`)?.focus());
        }
    };

    const onSubmit = (e: FormEvent) => {
        e.preventDefault();
        const errs = validate(v);
        setErrors(errs);
        setFormError(Object.keys(errs).length > 0 ? "Periksa kembali isian yang ditandai." : null);
        if (Object.keys(errs).length > 0) {
            focusFirst(errs);
            return;
        }
        save(toInput(v), {
            onSuccess: (m) => {
                toast.success(migration ? `Revisi ${m.current_revision.revision_number} disimpan.` : "Migration dibuat sebagai Draft.");
                navigate(`/migration/${m.id}`, { replace: true });
            },
            onError: (err) => {
                if (isInlineError(err) && isApiError(err)) {
                    const fields = fieldErrors(err.details);
                    setErrors(fields);
                    setFormError(err.message);
                    focusFirst(fields);
                }
            },
        });
    };

    return (
        <div className="mx-auto max-w-4xl space-y-6">
            <PageHeader title={title} description={migration ? `${databaseName(me, dbId)}, revisi ${migration.current_revision.revision_number}` : databaseName(me, dbId)} back={{ to: back, label: migration ? "Kembali ke review" : "Daftar migration" }} />
            <MockBanner />
            {migration && migration.status !== "Draft" && (
                <div role="note" className="rounded-md bg-amber-50 px-3 py-2 text-sm text-amber-900 ring-1 ring-inset ring-amber-200">
                    Menyimpan perubahan membuat revisi baru. Persetujuan untuk revisi sekarang tidak berlaku lagi dan migration perlu diajukan ulang.
                </div>
            )}
            <form onSubmit={onSubmit} noValidate className="space-y-6">
                <FormError message={formError} />
                <Section title="Informasi">
                    <div data-field="title">
                        <Field label="Judul" error={errors.title} hint="Maksimal 100 karakter.">
                            {(p) => <Input {...p} name="title" autoComplete="off" value={v.title} onChange={(e) => set("title", e.target.value)} />}
                        </Field>
                    </div>
                </Section>

                <Section title="SQL">
                    <div data-field="sql_up">
                        <Field label="SQL up" error={errors.sql_up} hint="Perubahan yang dijalankan saat migration diterapkan.">
                            {(p) => <SqlEditor {...p} value={v.sqlUp} onChange={(x) => set("sqlUp", x)} rows={10} placeholder="ALTER TABLE ..." />}
                        </Field>
                    </div>
                    <Switch label="Irreversible" hint="Aktifkan bila migration tidak bisa dibatalkan dengan SQL down." checked={v.irreversible} onChange={(x) => set("irreversible", x)} />
                    {v.irreversible ? (
                        <div data-field="irreversible_reason">
                            <Field label="Alasan irreversible" error={errors.irreversible_reason} hint="Wajib diisi. Jelaskan kenapa migration ini tidak bisa di-rollback.">
                                {(p) => <Textarea {...p} name="irreversible_reason" rows={3} value={v.reason} onChange={(e) => set("reason", e.target.value)} />}
                            </Field>
                        </div>
                    ) : (
                        <div data-field="sql_down">
                            <Field label="SQL down" error={errors.sql_down} hint="Dipakai untuk rollback.">
                                {(p) => <SqlEditor {...p} value={v.sqlDown} onChange={(x) => set("sqlDown", x)} rows={8} placeholder="ALTER TABLE ..." />}
                            </Field>
                        </div>
                    )}
                </Section>

                <Section title="Mode eksekusi">
                    <fieldset className="space-y-2">
                        <legend className="sr-only">Mode eksekusi</legend>
                        <div className="grid gap-2 sm:grid-cols-2">
                            <ModeOption name="mode" checked={!v.batch} onChange={() => set("batch", false)} title="Normal" description="Seluruh SQL dijalankan dalam satu transaksi." />
                            <ModeOption name="mode" checked={v.batch} onChange={() => set("batch", true)} title="Batch" description="Perubahan data besar dijalankan bertahap per kelompok baris." />
                        </div>
                    </fieldset>
                    {v.batch && (
                        <div className="grid gap-4 sm:grid-cols-2">
                            <div data-field="batch_column">
                                <Field label="Kolom batch" error={errors.batch_column} hint="Kolom berurutan untuk membagi baris, misalnya id.">
                                    {(p) => <Input {...p} name="batch_column" autoComplete="off" autoCapitalize="off" spellCheck={false} value={v.batchColumn} onChange={(e) => set("batchColumn", e.target.value)} />}
                                </Field>
                            </div>
                            <div data-field="batch_size">
                                <Field label="Ukuran batch" error={errors.batch_size} hint={`Jumlah baris per batch, 1 sampai ${maxBatchSize.toLocaleString("id-ID")}.`}>
                                    {(p) => <Input {...p} name="batch_size" inputMode="numeric" value={v.batchSize} onChange={(e) => set("batchSize", e.target.value)} />}
                                </Field>
                            </div>
                        </div>
                    )}
                </Section>

                <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
                    <Button variant="secondary" onClick={() => navigate(back)}>
                        Batal
                    </Button>
                    <Button type="submit" loading={pending}>
                        {migration ? "Simpan revisi" : "Simpan sebagai Draft"}
                    </Button>
                </div>
            </form>
        </div>
    );
}

function ModeOption({ name, checked, onChange, title, description }: { name: string; checked: boolean; onChange: () => void; title: string; description: string }) {
    return (
        <label className={`flex cursor-pointer gap-3 rounded-lg p-3 ring-1 ${checked ? "bg-indigo-50 ring-indigo-400" : "bg-white ring-slate-200 hover:ring-slate-300"}`}>
            <input type="radio" name={name} checked={checked} onChange={onChange} className="mt-0.5 h-4 w-4 shrink-0 accent-indigo-600" />
            <span className="min-w-0">
                <span className="block text-sm font-medium text-slate-900">{title}</span>
                <span className="block text-xs text-slate-600">{description}</span>
            </span>
        </label>
    );
}
