import { useEffect, useState, type FormEvent } from "react";
import { useNavigate, useParams } from "react-router";
import { Button } from "../../components/Button";
import { Field, FormError, Input, Select, fieldErrors } from "../../components/Form";
import { Loading } from "../../components/Loading";
import { PageHeader, Section } from "../../components/PageHeader";
import { isApiError } from "../../lib/api";
import {
    checkConnection,
    emptyConnection,
    engines,
    fromConnection,
    isInlineError,
    sslModes,
    useConnection,
    useCreateConnection,
    useTestConnection,
    useTestSavedConnection,
    useUpdateConnection,
    type ConnectionInput,
    type ConnectionTest,
} from "../../lib/connections";
import { toast } from "../../lib/toast";
import { ErrorView } from "../ErrorPage";

export function ConnectionFormPage({ mode }: { mode: "new" | "edit" }) {
    const navigate = useNavigate();
    const rawId = useParams().id;
    const parsed = mode === "edit" && rawId ? Number(rawId) : undefined;
    const id = parsed !== undefined && Number.isInteger(parsed) && parsed > 0 ? parsed : undefined;
    const editing = mode === "edit";
    const connection = useConnection(editing ? id : undefined);
    const create = useCreateConnection();
    const update = useUpdateConnection(id ?? 0);
    const testNew = useTestConnection();
    const testSaved = useTestSavedConnection();
    const [form, setForm] = useState<ConnectionInput>(emptyConnection());
    const [errors, setErrors] = useState<Record<string, string>>({});
    const [formError, setFormError] = useState<string | null>(null);
    const [testResult, setTestResult] = useState<ConnectionTest | null>(null);

    useEffect(() => {
        if (connection.data) {
            setForm(fromConnection(connection.data));
        }
    }, [connection.data]);

    const set = <K extends keyof ConnectionInput>(key: K, value: ConnectionInput[K]) => {
        setForm((prev) => ({ ...prev, [key]: value }));
        setErrors((prev) => ({ ...prev, [key]: "" }));
        setTestResult(null);
    };

    const payload = (): ConnectionInput => ({
        ...form,
        name: form.name.trim(),
        host: form.host.trim(),
        db_name: form.db_name.trim(),
        username: form.username.trim(),
    });

    const onError = (err: unknown) => {
        setTestResult(null);
        if (isInlineError(err) && isApiError(err)) {
            const fields = fieldErrors(err.details);
            setErrors(fields);
            setFormError(Object.keys(fields).length > 0 ? null : err.message);
            return;
        }
        setFormError(isApiError(err) ? err.message : "Permintaan gagal.");
    };

    const validate = () => {
        const next = checkConnection(form, editing);
        setErrors(next);
        setFormError(null);
        setTestResult(null);
        return Object.keys(next).length === 0;
    };

    const onTest = () => {
        if (!validate()) {
            return;
        }
        const done = { onSuccess: (result: ConnectionTest) => setTestResult(result), onError };
        if (editing && id !== undefined) {
            testSaved.mutate({ id, input: payload() }, done);
        } else {
            testNew.mutate(payload(), done);
        }
    };

    const onSubmit = (e: FormEvent) => {
        e.preventDefault();
        if (!validate()) {
            return;
        }
        const done = {
            onSuccess: () => {
                toast.success(editing ? "Koneksi diperbarui." : "Koneksi dibuat.");
                navigate("/admin/koneksi", { replace: true });
            },
            onError,
        };
        if (editing && id !== undefined) {
            update.mutate(payload(), done);
        } else {
            create.mutate(payload(), done);
        }
    };

    if (editing && connection.isPending) {
        return <Loading />;
    }
    if (editing && (connection.isError || !connection.data || id === undefined)) {
        const code = isApiError(connection.error) && connection.error.status === 404 ? "404" : "500";
        return <ErrorView code={code} onRetry={code === "500" ? () => connection.refetch() : undefined} />;
    }

    const busy = create.isPending || update.isPending || testNew.isPending || testSaved.isPending;

    return (
        <div className="mx-auto max-w-3xl space-y-6">
            <PageHeader
                title={editing ? `Ubah koneksi ${connection.data?.name ?? ""}` : "Tambah koneksi"}
                description="Password disimpan terenkripsi dan tidak pernah ditampilkan kembali."
                back={{ to: "/admin/koneksi", label: "Daftar koneksi" }}
            />
            <Section title="Detail koneksi">
                <form onSubmit={onSubmit} noValidate className="space-y-4">
                    <FormError message={formError} />
                    <div className="grid gap-4 sm:grid-cols-2">
                        <Field label="Nama koneksi" error={errors.name}>
                            {(p) => <Input {...p} name="name" value={form.name} onChange={(e) => set("name", e.target.value)} autoComplete="off" />}
                        </Field>
                        <Field label="Engine" error={errors.engine} hint={editing ? "Engine tidak bisa diubah setelah koneksi dibuat." : undefined}>
                            {(p) => (
                                <Select
                                    {...p}
                                    name="engine"
                                    value={form.engine}
                                    disabled={editing}
                                    onChange={(e) => {
                                        const engine = e.target.value as ConnectionInput["engine"];
                                        const previous = engines.find((item) => item.value === form.engine);
                                        setForm((prev) => ({ ...prev, engine, port: prev.port === previous?.port ? engines.find((item) => item.value === engine)?.port ?? prev.port : prev.port }));
                                        setTestResult(null);
                                    }}
                                >
                                    {engines.map((item) => (
                                        <option key={item.value} value={item.value}>
                                            {item.label}
                                        </option>
                                    ))}
                                </Select>
                            )}
                        </Field>
                        <Field label="Host" error={errors.host}>
                            {(p) => <Input {...p} name="host" value={form.host} onChange={(e) => set("host", e.target.value)} autoComplete="off" />}
                        </Field>
                        <Field label="Port" error={errors.port}>
                            {(p) => <Input {...p} name="port" type="number" min={1} max={65535} value={form.port || ""} onChange={(e) => set("port", e.target.value === "" ? 0 : Number(e.target.value))} />}
                        </Field>
                        <Field label="Nama database" error={errors.db_name}>
                            {(p) => <Input {...p} name="db_name" value={form.db_name} onChange={(e) => set("db_name", e.target.value)} autoComplete="off" />}
                        </Field>
                        <Field label="Username" error={errors.username}>
                            {(p) => <Input {...p} name="username" value={form.username} onChange={(e) => set("username", e.target.value)} autoComplete="off" />}
                        </Field>
                        <Field label="Password" error={errors.password} hint={editing ? "Kosongkan untuk tetap memakai password tersimpan." : undefined}>
                            {(p) => <Input {...p} name="password" type="password" autoComplete="new-password" value={form.password} onChange={(e) => set("password", e.target.value)} />}
                        </Field>
                        <Field label="SSL mode" error={errors.sslmode}>
                            {(p) => (
                                <Select {...p} name="sslmode" value={form.sslmode} onChange={(e) => set("sslmode", e.target.value as ConnectionInput["sslmode"])}>
                                    {sslModes.map((mode) => (
                                        <option key={mode} value={mode}>
                                            {mode}
                                        </option>
                                    ))}
                                </Select>
                            )}
                        </Field>
                    </div>
                    {testResult && (
                        <div role="status" className={testResult.success ? "rounded-md bg-emerald-50 px-3 py-2 text-sm text-emerald-900 ring-1 ring-inset ring-emerald-200" : "alert-error"}>
                            {testResult.message}
                        </div>
                    )}
                    <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
                        <Button variant="secondary" onClick={() => navigate("/admin/koneksi")} disabled={busy}>
                            Batal
                        </Button>
                        <Button variant="secondary" onClick={onTest} loading={testNew.isPending || testSaved.isPending} disabled={busy && !testNew.isPending && !testSaved.isPending}>
                            Uji koneksi
                        </Button>
                        <Button type="submit" loading={create.isPending || update.isPending} disabled={busy && !create.isPending && !update.isPending}>
                            Simpan
                        </Button>
                    </div>
                </form>
            </Section>
        </div>
    );
}
