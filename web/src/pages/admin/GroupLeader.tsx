import { useState, type FormEvent } from "react";
import { Button } from "../../components/Button";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import { Field, Select } from "../../components/Form";
import { Section } from "../../components/PageHeader";
import { isApiError } from "../../lib/api";
import { isInlineError, useSetLeader, type AdminGroupDetail } from "../../lib/admin";
import { useCurrentUser } from "../../lib/currentUser";
import { toast } from "../../lib/toast";

export function GroupLeader({ group }: { group: AdminGroupDetail }) {
    const me = useCurrentUser();
    const save = useSetLeader(group.id);
    const current = group.leader;
    const [selected, setSelected] = useState(current ? String(current.id) : "");
    const [error, setError] = useState<string | undefined>();
    const [confirm, setConfirm] = useState(false);
    const nextID = selected ? Number(selected) : null;
    const dirty = nextID !== (current?.id ?? null);
    const options = group.members.filter((m) => m.is_active || m.id === current?.id);
    const next = options.find((m) => m.id === nextID);

    const submit = () => {
        setError(undefined);
        save.mutate(nextID, {
            onSuccess: () => toast.success(next ? `${next.username} menjadi leader grup.` : "Leader grup dikosongkan."),
            onError: (err) => {
                if (isInlineError(err) && isApiError(err)) {
                    const detail = (err.details as Record<string, string> | undefined)?.user_id;
                    setError(detail ?? err.message);
                }
            },
            onSettled: () => setConfirm(false),
        });
    };

    const onSubmit = (e: FormEvent) => {
        e.preventDefault();
        if (!dirty) {
            return;
        }
        if (current) {
            setConfirm(true);
            return;
        }
        submit();
    };

    const hint = group.members.length === 0 ? "Tambahkan anggota terlebih dahulu." : undefined;

    return (
        <Section title="Leader">
            <form onSubmit={onSubmit} noValidate className="flex flex-col gap-2 sm:flex-row sm:items-end">
                <div className="min-w-0 flex-1">
                    <Field label="Pilih leader" error={error} hint={hint}>
                        {(p) => (
                            <Select {...p} name="leader_id" value={selected} disabled={options.length === 0 && !current} onChange={(e) => setSelected(e.target.value)}>
                                <option value="">Tanpa leader</option>
                                {options.map((u) => (
                                    <option key={u.id} value={u.id}>
                                        {u.username}
                                        {u.id === me.user.id ? " (Anda)" : ""}
                                        {u.is_active ? "" : " (Nonaktif)"}
                                    </option>
                                ))}
                            </Select>
                        )}
                    </Field>
                </div>
                <Button type="submit" loading={save.isPending && !confirm} disabled={!dirty} className="sm:mb-5">
                    Simpan leader
                </Button>
            </form>

            <ConfirmDialog open={confirm} title={next ? "Ganti leader?" : "Kosongkan leader?"} confirmLabel={next ? "Ya, ganti" : "Ya, kosongkan"} danger loading={save.isPending} onConfirm={submit} onClose={() => setConfirm(false)}>
                <p>{next ? <>Leader grup {group.name} berubah dari <strong>{current?.username}</strong> menjadi <strong>{next.username}</strong>.</> : <>Grup {group.name} tidak akan punya leader. Migration yang butuh persetujuan grup ini tertahan.</>}</p>
                <p>{current?.id === me.user.id ? "Anda leader saat ini. Anda akan keluar dan harus login lagi." : `Semua sesi aktif ${current?.username} akan langsung diakhiri.`}</p>
            </ConfirmDialog>
        </Section>
    );
}
