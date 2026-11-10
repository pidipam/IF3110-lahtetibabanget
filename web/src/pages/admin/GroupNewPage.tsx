import { useState, type FormEvent } from "react";
import { useNavigate } from "react-router";
import { Button } from "../../components/Button";
import { Field, FormError, Input, Textarea, fieldErrors } from "../../components/Form";
import { PageHeader, Section } from "../../components/PageHeader";
import { isApiError } from "../../lib/api";
import { isInlineError, useCreateGroup } from "../../lib/admin";
import { toast } from "../../lib/toast";

export function checkGroup(name: string, description: string): Record<string, string> {
    const out: Record<string, string> = {};
    const n = [...name.trim()].length;
    if (n === 0 || n > 64) {
        out.name = "Nama grup wajib diisi, maksimal 64 karakter.";
    }
    if ([...description.trim()].length > 500) {
        out.description = "Deskripsi maksimal 500 karakter.";
    }
    return out;
}

export function GroupNewPage() {
    const navigate = useNavigate();
    const create = useCreateGroup();
    const [name, setName] = useState("");
    const [description, setDescription] = useState("");
    const [errors, setErrors] = useState<Record<string, string>>({});
    const [formError, setFormError] = useState<string | null>(null);

    const onSubmit = (e: FormEvent) => {
        e.preventDefault();
        const next = checkGroup(name, description);
        setErrors(next);
        setFormError(null);
        if (Object.keys(next).length > 0) {
            return;
        }
        create.mutate(
            { name: name.trim(), description: description.trim() },
            {
                onSuccess: (g) => {
                    toast.success(`Grup ${g.name} berhasil dibuat.`);
                    navigate(`/admin/grup/${g.id}`, { replace: true });
                },
                onError: (err) => {
                    if (isInlineError(err) && isApiError(err)) {
                        const fields = fieldErrors(err.details);
                        setErrors(fields);
                        setFormError(Object.keys(fields).length > 0 ? null : err.message);
                    }
                },
            },
        );
    };

    return (
        <div className="mx-auto max-w-xl space-y-6">
            <PageHeader title="Tambah grup" back={{ to: "/admin/grup", label: "Daftar grup" }} />
            <Section title="Data grup">
                <form onSubmit={onSubmit} noValidate className="space-y-4">
                    <FormError message={formError} />
                    <Field label="Nama" error={errors.name || (name ? checkGroup(name, "").name : undefined)}>
                        {(p) => <Input {...p} name="name" autoComplete="off" value={name} onChange={(e) => { setName(e.target.value); setErrors((x) => ({ ...x, name: "" })); }} autoFocus />}
                    </Field>
                    <Field label="Deskripsi" error={errors.description || checkGroup("x", description).description}>
                        {(p) => <Textarea {...p} name="description" rows={3} value={description} onChange={(e) => { setDescription(e.target.value); setErrors((x) => ({ ...x, description: "" })); }} />}
                    </Field>
                    <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
                        <Button variant="secondary" onClick={() => navigate("/admin/grup")}>
                            Batal
                        </Button>
                        <Button type="submit" loading={create.isPending}>
                            Simpan
                        </Button>
                    </div>
                </form>
            </Section>
        </div>
    );
}
