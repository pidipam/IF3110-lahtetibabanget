import { useState, type FormEvent } from "react";
import { useNavigate, useParams } from "react-router";
import { Button } from "../../components/Button";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import { Field, FormError, Input, Textarea, fieldErrors } from "../../components/Form";
import { Loading } from "../../components/Loading";
import { PageHeader, Section } from "../../components/PageHeader";
import { isApiError } from "../../lib/api";
import { isInlineError, useDeleteGroup, useGroup, useUpdateGroup, type AdminGroupDetail } from "../../lib/admin";
import { useCurrentUser } from "../../lib/currentUser";
import { toast } from "../../lib/toast";
import { ErrorView } from "../ErrorPage";
import { GroupAccess } from "./GroupAccess";
import { GroupLeader } from "./GroupLeader";
import { GroupMembers } from "./GroupMembers";
import { checkGroup } from "./GroupNewPage";
import { parseId } from "./UserEditPage";

export function GroupPage() {
    const id = parseId(useParams().id);
    if (id === null) {
        return <ErrorView code="404" />;
    }
    return <GroupDetail id={id} />;
}

function GroupDetail({ id }: { id: number }) {
    const group = useGroup(id);
    if (group.isPending) {
        return <Loading />;
    }
    if (group.isError) {
        const code = isApiError(group.error) && group.error.status === 404 ? "404" : "500";
        return <ErrorView code={code} onRetry={code === "500" ? () => group.refetch() : undefined} />;
    }
    const g = group.data;
    return (
        <div className="mx-auto max-w-3xl space-y-6">
            <PageHeader title={g.name} description={g.description || "Tanpa deskripsi"} back={{ to: "/admin/grup", label: "Daftar grup" }} />
            <GroupForm key={`${g.id}-${g.name}-${g.description}`} group={g} />
            <GroupMembers group={g} />
            <GroupLeader key={`${g.leader?.id ?? 0}-${g.members.map((m) => m.id).join(",")}`} group={g} />
            <GroupAccess key={g.access.map((a) => `${a.db_id}:${a.level}`).join(",")} group={g} />
            <DeleteGroup group={g} />
        </div>
    );
}

function GroupForm({ group }: { group: AdminGroupDetail }) {
    const update = useUpdateGroup(group.id);
    const [name, setName] = useState(group.name);
    const [description, setDescription] = useState(group.description);
    const [errors, setErrors] = useState<Record<string, string>>({});
    const [formError, setFormError] = useState<string | null>(null);
    const dirty = name.trim() !== group.name || description.trim() !== group.description;

    const onSubmit = (e: FormEvent) => {
        e.preventDefault();
        const next = checkGroup(name, description);
        setErrors(next);
        setFormError(null);
        if (Object.keys(next).length > 0 || !dirty) {
            return;
        }
        update.mutate(
            { name: name.trim(), description: description.trim() },
            {
                onSuccess: () => toast.success("Perubahan grup disimpan."),
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
        <Section title="Ubah grup">
            <form onSubmit={onSubmit} noValidate className="space-y-4">
                <FormError message={formError} />
                <Field label="Nama" error={errors.name || (name ? checkGroup(name, "").name : undefined)}>
                    {(p) => <Input {...p} name="name" autoComplete="off" value={name} onChange={(e) => { setName(e.target.value); setErrors((x) => ({ ...x, name: "" })); }} />}
                </Field>
                <Field label="Deskripsi" error={errors.description || checkGroup("x", description).description}>
                    {(p) => <Textarea {...p} name="description" rows={3} value={description} onChange={(e) => { setDescription(e.target.value); setErrors((x) => ({ ...x, description: "" })); }} />}
                </Field>
                <div className="flex justify-end">
                    <Button type="submit" loading={update.isPending} disabled={!dirty}>
                        Simpan perubahan
                    </Button>
                </div>
            </form>
        </Section>
    );
}

function DeleteGroup({ group }: { group: AdminGroupDetail }) {
    const me = useCurrentUser();
    const remove = useDeleteGroup(group.id);
    const navigate = useNavigate();
    const [confirm, setConfirm] = useState(false);

    const onConfirm = () => {
        remove.mutate(undefined, {
            onSuccess: () => {
                toast.success(`Grup ${group.name} dihapus.`);
                navigate("/admin/grup", { replace: true });
            },
            onSettled: () => setConfirm(false),
        });
    };

    return (
        <Section title="Hapus grup" description="Anggota kehilangan akses database dari grup ini dan sesinya diakhiri." danger>
            <div className="flex justify-end">
                <Button variant="danger" onClick={() => setConfirm(true)}>
                    Hapus grup
                </Button>
            </div>
            <ConfirmDialog open={confirm} title="Hapus grup?" confirmLabel="Ya, hapus" danger loading={remove.isPending} onConfirm={onConfirm} onClose={() => setConfirm(false)}>
                <p>
                    Grup <strong>{group.name}</strong> akan dihapus permanen.
                </p>
                <p>{group.member_count > 0 ? `Sesi ${group.member_count} anggota akan langsung diakhiri.` : "Grup ini belum punya anggota."}</p>
                {group.members.some((m) => m.id === me.user.id) && <p>Anda termasuk anggota sehingga Anda juga harus login lagi.</p>}
            </ConfirmDialog>
        </Section>
    );
}
