import { useState, type FormEvent } from "react";
import { Link, useNavigate, useParams } from "react-router";
import { useQueryClient } from "@tanstack/react-query";
import { Button } from "../../components/Button";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import { Checkbox, Field, FormError, Input, fieldErrors } from "../../components/Form";
import { Loading } from "../../components/Loading";
import { PageHeader, Section } from "../../components/PageHeader";
import { isApiError, setCsrfToken } from "../../lib/api";
import { isInlineError, useResetPassword, useUpdateUser, useUser, type AdminUserDetail } from "../../lib/admin";
import { useCurrentUser } from "../../lib/currentUser";
import { meKey } from "../../lib/session";
import { toast } from "../../lib/toast";
import { ErrorView } from "../ErrorPage";
import { checkPassword, checkUsername, passwordHint, usernameHint } from "./UserNewPage";
import { UserBadges, formatDate } from "./UsersPage";

export function parseId(raw: string | undefined): number | null {
    const id = Number(raw);
    return Number.isInteger(id) && id > 0 ? id : null;
}

export function UserEditPage() {
    const id = parseId(useParams().id);
    if (id === null) {
        return <ErrorView code="404" />;
    }
    return <UserEdit id={id} />;
}

function UserEdit({ id }: { id: number }) {
    const user = useUser(id);
    if (user.isPending) {
        return <Loading />;
    }
    if (user.isError) {
        const code = isApiError(user.error) && user.error.status === 404 ? "404" : "500";
        return <ErrorView code={code} onRetry={code === "500" ? () => user.refetch() : undefined} />;
    }
    return (
        <div className="mx-auto max-w-2xl space-y-6">
            <PageHeader title={user.data.username} description={<UserBadges user={user.data} />} back={{ to: "/admin/user", label: "Daftar user" }} />
            <UserForm key={user.data.id} user={user.data} />
            <UserGroups user={user.data} />
            <ResetPassword user={user.data} />
        </div>
    );
}

function UserForm({ user }: { user: AdminUserDetail }) {
    const me = useCurrentUser();
    const isSelf = me.user.id === user.id;
    const update = useUpdateUser(user.id, isSelf);
    const [username, setUsername] = useState(user.username);
    const [isAdmin, setIsAdmin] = useState(user.is_admin);
    const [isActive, setIsActive] = useState(user.is_active);
    const [errors, setErrors] = useState<Record<string, string>>({});
    const [formError, setFormError] = useState<string | null>(null);
    const [confirm, setConfirm] = useState(false);

    const changes: { username?: string; is_admin?: boolean; is_active?: boolean } = {};
    if (username.trim().toLowerCase() !== user.username) {
        changes.username = username.trim();
    }
    if (isAdmin !== user.is_admin) {
        changes.is_admin = isAdmin;
    }
    if (isActive !== user.is_active) {
        changes.is_active = isActive;
    }
    const dirty = Object.keys(changes).length > 0;
    const lowers = (user.is_admin && !isAdmin) || (user.is_active && !isActive);

    const save = () => {
        update.mutate(changes, {
            onSuccess: (u) => {
                setConfirm(false);
                setUsername(u.username);
                toast.success("Perubahan user disimpan.");
            },
            onError: (err) => {
                setConfirm(false);
                if (isInlineError(err) && isApiError(err)) {
                    const fields = fieldErrors(err.details);
                    setErrors(fields);
                    setFormError(Object.keys(fields).length > 0 ? null : err.message);
                }
            },
        });
    };

    const onSubmit = (e: FormEvent) => {
        e.preventDefault();
        const u = checkUsername(username);
        setErrors(u ? { username: u } : {});
        setFormError(null);
        if (u || !dirty) {
            return;
        }
        if (lowers) {
            setConfirm(true);
            return;
        }
        save();
    };

    return (
        <Section title="Data user" description={`Dibuat ${formatDate(user.created_at)}.`}>
            <form onSubmit={onSubmit} noValidate className="space-y-4">
                <FormError message={formError} />
                <Field label="Username" error={errors.username} hint={usernameHint}>
                    {(p) => <Input {...p} name="username" autoComplete="off" autoCapitalize="off" value={username} onChange={(e) => setUsername(e.target.value)} />}
                </Field>
                <Checkbox label="Admin" hint={isSelf ? "Anda tidak bisa mencabut hak admin akun sendiri." : "Admin bisa mengelola user, Grup, dan koneksi."} checked={isAdmin} disabled={isSelf} onChange={(e) => setIsAdmin(e.target.checked)} />
                <Checkbox label="Aktif" hint={isSelf ? "Anda tidak bisa menonaktifkan akun sendiri." : "User nonaktif tidak bisa login."} checked={isActive} disabled={isSelf} onChange={(e) => setIsActive(e.target.checked)} />
                <div className="flex justify-end">
                    <Button type="submit" loading={update.isPending && !confirm} disabled={!dirty}>
                        Simpan perubahan
                    </Button>
                </div>
            </form>
            <ConfirmDialog open={confirm} title="Kurangi hak user?" confirmLabel="Ya, simpan" danger loading={update.isPending} onConfirm={save} onClose={() => setConfirm(false)}>
                <p>
                    {user.is_active && !isActive ? `${user.username} akan dinonaktifkan dan tidak bisa login.` : `Hak admin ${user.username} akan dicabut.`}
                </p>
                <p>Semua sesi aktif user ini akan langsung diakhiri.</p>
            </ConfirmDialog>
        </Section>
    );
}

function UserGroups({ user }: { user: AdminUserDetail }) {
    return (
        <Section title="Grup" description="Akses database user berasal dari Grup yang diikutinya.">
            {user.groups.length === 0 ? (
                <p className="text-sm text-slate-600">User ini belum masuk Grup mana pun.</p>
            ) : (
                <ul className="flex flex-wrap gap-2">
                    {user.groups.map((g) => (
                        <li key={g.id}>
                            <Link to={`/admin/grup/${g.id}`} className="inline-flex min-h-10 items-center rounded-md bg-slate-100 px-3 text-sm font-medium text-slate-800 hover:bg-slate-200">
                                {g.name}
                            </Link>
                        </li>
                    ))}
                </ul>
            )}
        </Section>
    );
}

function ResetPassword({ user }: { user: AdminUserDetail }) {
    const me = useCurrentUser();
    const isSelf = me.user.id === user.id;
    const reset = useResetPassword(user.id);
    const qc = useQueryClient();
    const navigate = useNavigate();
    const [password, setPassword] = useState("");
    const [error, setError] = useState<string | undefined>();
    const [confirm, setConfirm] = useState(false);

    const onSubmit = (e: FormEvent) => {
        e.preventDefault();
        const p = checkPassword(password);
        setError(p);
        if (!p) {
            setConfirm(true);
        }
    };

    const doReset = () => {
        reset.mutate(password, {
            onSuccess: () => {
                setConfirm(false);
                setPassword("");
                if (isSelf) {
                    setCsrfToken(null);
                    qc.clear();
                    qc.setQueryData(meKey, null);
                    toast.success("Password Anda diganti. Silakan login dengan password baru.");
                    navigate("/login", { replace: true });
                    return;
                }
                toast.success(`Password ${user.username} direset.`);
            },
            onError: (err) => {
                setConfirm(false);
                if (isInlineError(err) && isApiError(err)) {
                    setError(fieldErrors(err.details).password ?? err.message);
                }
            },
        });
    };

    return (
        <Section title="Reset password" description="Semua sesi user akan diakhiri setelah password diganti.">
            <form onSubmit={onSubmit} noValidate className="space-y-4">
                <Field label="Password baru" error={error} hint={passwordHint}>
                    {(p) => <Input {...p} name="new-password" type="password" autoComplete="new-password" value={password} onChange={(e) => setPassword(e.target.value)} />}
                </Field>
                <div className="flex justify-end">
                    <Button type="submit" variant="secondary">
                        Reset password
                    </Button>
                </div>
            </form>
            <ConfirmDialog open={confirm} title="Reset password?" confirmLabel="Ya, reset" danger loading={reset.isPending} onConfirm={doReset} onClose={() => setConfirm(false)}>
                {isSelf ? <p>Ini akun Anda sendiri. Anda akan keluar dan harus login lagi dengan password baru.</p> : <p>Password {user.username} akan diganti dan semua sesi aktifnya diakhiri.</p>}
            </ConfirmDialog>
        </Section>
    );
}
