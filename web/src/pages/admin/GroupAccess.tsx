import { useState } from "react";
import { Button } from "../../components/Button";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import { FormError } from "../../components/Form";
import { Section } from "../../components/PageHeader";
import { isApiError } from "../../lib/api";
import { grantLabels, isInlineError, useSetAccess, type AdminGroupDetail, type GrantLevel } from "../../lib/admin";
import { useCurrentUser } from "../../lib/currentUser";
import { toast } from "../../lib/toast";

const engineLabel: Record<string, string> = { postgres: "PostgreSQL", mysql: "MySQL", mariadb: "MariaDB" };

const rank: Record<GrantLevel, number> = { none: 0, view: 1, edit: 2 };

export function GroupAccess({ group }: { group: AdminGroupDetail }) {
    const me = useCurrentUser();
    const save = useSetAccess(group.id);
    const initial = Object.fromEntries(group.access.map((a) => [a.db_id, a.level])) as Record<number, GrantLevel>;
    const [levels, setLevels] = useState<Record<number, GrantLevel>>(initial);
    const [confirm, setConfirm] = useState(false);
    const [formError, setFormError] = useState<string | null>(null);

    const changes = group.access.filter((a) => levels[a.db_id] !== a.level).map((a) => ({ db_id: a.db_id, level: levels[a.db_id] }));
    const lowers = group.access.some((a) => rank[levels[a.db_id]] < rank[a.level]);

    const doSave = () => {
        save.mutate(changes, {
            onSuccess: () => toast.success("Akses database Grup disimpan."),
            onError: (err) => {
                if (isInlineError(err) && isApiError(err)) {
                    setFormError(err.message);
                }
            },
            onSettled: () => setConfirm(false),
        });
    };

    const onSave = () => {
        setFormError(null);
        if (changes.length === 0) {
            return;
        }
        if (lowers && group.members.length > 0) {
            setConfirm(true);
            return;
        }
        doSave();
    };

    return (
        <Section title="Akses database" description="Bila anggota punya akses dari beberapa Grup, yang berlaku adalah level tertinggi.">
            <FormError message={formError} />
            {group.access.length === 0 ? (
                <p className="text-sm text-slate-600">Belum ada koneksi database. Tambahkan koneksi terlebih dahulu.</p>
            ) : (
                <ul aria-label="Akses per database" className="divide-y divide-slate-100 rounded-lg ring-1 ring-slate-200">
                    {group.access.map((a) => {
                        const id = `akses-${a.db_id}`;
                        const changed = levels[a.db_id] !== a.level;
                        return (
                            <li key={a.db_id} className={`flex flex-col gap-2 px-3 py-3 sm:flex-row sm:items-center sm:justify-between ${changed ? "bg-amber-50" : ""}`}>
                                <label htmlFor={id} className="min-w-0">
                                    <span className="block break-words font-medium text-slate-900">{a.name}</span>
                                    <span className="block text-xs text-slate-500">
                                        {engineLabel[a.engine] ?? a.engine}
                                        {changed && ` (sebelumnya ${grantLabels[a.level]})`}
                                    </span>
                                </label>
                                <select
                                    id={id}
                                    value={levels[a.db_id]}
                                    onChange={(e) => setLevels({ ...levels, [a.db_id]: e.target.value as GrantLevel })}
                                    className="block min-h-10 w-full rounded-md border-0 px-3 py-2 text-sm text-slate-900 ring-1 ring-inset ring-slate-300 focus:ring-2 focus:ring-inset focus:ring-indigo-600 sm:w-44"
                                >
                                    {(Object.keys(grantLabels) as GrantLevel[]).map((l) => (
                                        <option key={l} value={l}>
                                            {grantLabels[l]}
                                        </option>
                                    ))}
                                </select>
                            </li>
                        );
                    })}
                </ul>
            )}
            {group.access.length > 0 && (
                <div className="flex flex-col-reverse gap-2 sm:flex-row sm:items-center sm:justify-end">
                    {changes.length > 0 && <p className="text-sm text-slate-600 sm:mr-auto">{changes.length} perubahan belum disimpan.</p>}
                    <Button variant="secondary" onClick={() => setLevels(initial)} disabled={changes.length === 0 || save.isPending}>
                        Batalkan
                    </Button>
                    <Button onClick={onSave} loading={save.isPending && !confirm} disabled={changes.length === 0}>
                        Simpan akses
                    </Button>
                </div>
            )}
            <ConfirmDialog open={confirm} title="Kurangi akses database?" confirmLabel="Ya, simpan" danger loading={save.isPending} onConfirm={doSave} onClose={() => setConfirm(false)}>
                <p>Ada akses yang diturunkan atau dicabut.</p>
                <p>Sesi {group.members.length} anggota Grup ini akan langsung diakhiri.</p>
                {group.members.some((m) => m.id === me.user.id) && <p>Anda termasuk anggota, jadi Anda juga harus login lagi.</p>}
            </ConfirmDialog>
        </Section>
    );
}
