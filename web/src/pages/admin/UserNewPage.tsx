import { useState, type FormEvent } from "react";
import { useNavigate } from "react-router";
import { Button } from "../../components/Button";
import { Checkbox, Field, FormError, Input, fieldErrors } from "../../components/Form";
import { PageHeader, Section } from "../../components/PageHeader";
import { isApiError } from "../../lib/api";
import { isInlineError, useCreateUser } from "../../lib/admin";
import { toast } from "../../lib/toast";

export const usernameHint = "3 sampai 32 karakter: huruf kecil, angka, titik, garis bawah, atau strip.";

export const passwordHint = "Minimal 8 karakter.";

export function checkUsername(value: string): string | undefined {
    if (!/^[a-z0-9_.-]{3,32}$/.test(value.trim().toLowerCase())) {
        return "Username tidak sesuai aturan.";
    }
    return undefined;
}

export function checkPassword(value: string): string | undefined {
    if (value.length < 8) {
        return "Password minimal 8 karakter.";
    }
    if (new TextEncoder().encode(value).length > 72) {
        return "Password maksimal 72 byte.";
    }
    return undefined;
}

export function UserNewPage() {
    const navigate = useNavigate();
    const create = useCreateUser();
    const [username, setUsername] = useState("");
    const [password, setPassword] = useState("");
    const [isAdmin, setIsAdmin] = useState(false);
    const [errors, setErrors] = useState<Record<string, string>>({});
    const [formError, setFormError] = useState<string | null>(null);

    const onSubmit = (e: FormEvent) => {
        e.preventDefault();
        const next: Record<string, string> = {};
        const u = checkUsername(username);
        const p = checkPassword(password);
        if (u) {
            next.username = u;
        }
        if (p) {
            next.password = p;
        }
        setErrors(next);
        setFormError(null);
        if (Object.keys(next).length > 0) {
            return;
        }
        create.mutate(
            { username: username.trim(), password, is_admin: isAdmin },
            {
                onSuccess: (user) => {
                    toast.success(`User ${user.username} berhasil dibuat.`);
                    navigate(`/admin/user/${user.id}`, { replace: true });
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
            <PageHeader title="Tambah user" back={{ to: "/admin/user", label: "Daftar user" }} />
            <Section title="Data user">
                <form onSubmit={onSubmit} noValidate className="space-y-4">
                    <FormError message={formError} />
                    <Field label="Username" error={errors.username} hint={usernameHint}>
                        {(p) => <Input {...p} name="username" autoComplete="off" autoCapitalize="off" value={username} onChange={(e) => setUsername(e.target.value)} autoFocus />}
                    </Field>
                    <Field label="Password" error={errors.password} hint={passwordHint}>
                        {(p) => <Input {...p} name="password" type="password" autoComplete="new-password" value={password} onChange={(e) => setPassword(e.target.value)} />}
                    </Field>
                    <Checkbox label="Jadikan admin" hint="Admin bisa mengelola user, Grup, dan koneksi." checked={isAdmin} onChange={(e) => setIsAdmin(e.target.checked)} />
                    <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
                        <Button variant="secondary" onClick={() => navigate("/admin/user")}>
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
