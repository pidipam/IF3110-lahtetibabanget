import { useState, type FormEvent } from "react";
import { Link } from "react-router";
import { Badge } from "../../components/Badge";
import { Button } from "../../components/Button";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import { Field, Select } from "../../components/Form";
import { Section } from "../../components/PageHeader";
import { isApiError } from "../../lib/api";
import { isInlineError, useAddMember, useCandidates, useRemoveMember, type AdminGroupDetail, type AdminUser } from "../../lib/admin";
import { useCurrentUser } from "../../lib/currentUser";
import { toast } from "../../lib/toast";
import { UserBadges } from "./UsersPage";

export function GroupMembers({ group }: { group: AdminGroupDetail }) {
    const me = useCurrentUser();
    const candidates = useCandidates(group.id);
    const add = useAddMember(group.id);
    const remove = useRemoveMember(group.id);
    const [selected, setSelected] = useState("");
    const [error, setError] = useState<string | undefined>();
    const [target, setTarget] = useState<AdminUser | null>(null);

    const onAdd = (e: FormEvent) => {
        e.preventDefault();
        const userID = Number(selected);
        if (!userID) {
            setError("Pilih user terlebih dahulu.");
            return;
        }
        setError(undefined);
        add.mutate(userID, {
            onSuccess: () => {
                const u = candidates.data?.find((c) => c.id === userID);
                setSelected("");
                toast.success(`${u?.username ?? "User"} ditambahkan ke grup.`);
            },
            onError: (err) => {
                if (isInlineError(err) && isApiError(err)) {
                    setError(err.message);
                }
            },
        });
    };

    const onRemove = () => {
        if (!target) {
            return;
        }
        remove.mutate(target.id, {
            onSuccess: () => toast.success(`${target.username} dikeluarkan dari grup.`),
            onSettled: () => setTarget(null),
        });
    };

    const options = candidates.data ?? [];

    return (
        <Section title="Anggota">
            <form onSubmit={onAdd} noValidate className="flex flex-col gap-2 sm:flex-row sm:items-end">
                <div className="min-w-0 flex-1">
                    <Field label="Tambah anggota" error={error} hint={candidates.isSuccess && options.length === 0 ? "Semua user aktif sudah menjadi anggota." : undefined}>
                        {(p) => (
                            <Select {...p} name="user_id" value={selected} disabled={!candidates.isSuccess || options.length === 0} onChange={(e) => setSelected(e.target.value)}>
                                <option value="">{candidates.isPending ? "Memuat user..." : "Pilih user"}</option>
                                {options.map((u) => (
                                    <option key={u.id} value={u.id}>
                                        {u.username}
                                        {u.is_admin ? " (Admin)" : ""}
                                    </option>
                                ))}
                            </Select>
                        )}
                    </Field>
                </div>
                <Button type="submit" loading={add.isPending} disabled={options.length === 0} className={error ? "sm:mb-5" : ""}>
                    Tambah
                </Button>
            </form>

            {group.members.length === 0 ? (
                <p className="text-sm text-slate-600">Grup ini belum punya anggota.</p>
            ) : (
                <ul aria-label="Daftar anggota" className="divide-y divide-slate-100 rounded-lg ring-1 ring-slate-200">
                    {group.members.map((u) => (
                        <li key={u.id} className="flex flex-wrap items-center justify-between gap-2 px-3 py-2">
                            <div className="flex min-w-0 flex-wrap items-center gap-2">
                                <Link to={`/admin/user/${u.id}`} className="break-all font-medium text-indigo-700 hover:underline">
                                    {u.username}
                                </Link>
                                {u.id === me.user.id && <span className="text-sm text-slate-500">(Anda)</span>}
                                <UserBadges user={u} />
                                {group.leader?.id === u.id && <Badge tone="info">Leader</Badge>}
                            </div>
                            <Button variant="danger" onClick={() => setTarget(u)} aria-label={`Keluarkan ${u.username}`}>
                                Keluarkan
                            </Button>
                        </li>
                    ))}
                </ul>
            )}

            <ConfirmDialog open={target !== null} title="Keluarkan anggota?" confirmLabel="Ya, keluarkan" danger loading={remove.isPending} onConfirm={onRemove} onClose={() => setTarget(null)}>
                <p>
                    <strong>{target?.username}</strong> akan dikeluarkan dari grup {group.name}.
                </p>
                <p>{target?.id === me.user.id ? "Ini akun Anda sendiri. Anda akan keluar dan harus login lagi." : "Semua sesi aktif user ini akan langsung diakhiri."}</p>
            </ConfirmDialog>
        </Section>
    );
}
