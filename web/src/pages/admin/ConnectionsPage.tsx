import { useState } from "react";
import { Link } from "react-router";
import { Badge } from "../../components/Badge";
import { Button } from "../../components/Button";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import type { Column } from "../../components/DataTable";
import { EmptyState } from "../../components/EmptyState";
import { Loading } from "../../components/Loading";
import { PageHeader } from "../../components/PageHeader";
import { ResponsiveList } from "../../components/ResponsiveList";
import { isApiError } from "../../lib/api";
import { fromConnection, useConnections, useDeleteConnection, useTestSavedConnection, type Connection } from "../../lib/connections";
import { labelForEngine } from "../../lib/viewer";
import { toast } from "../../lib/toast";
import { ErrorView } from "../ErrorPage";
import { formatDate } from "./UsersPage";

export function ConnectionsPage() {
    const connections = useConnections();
    const test = useTestSavedConnection();
    const remove = useDeleteConnection();
    const [pendingDelete, setPendingDelete] = useState<Connection | null>(null);
    const [formError, setFormError] = useState<string | null>(null);

    const onTest = (c: Connection) => {
        test.mutate(
            { id: c.id, input: fromConnection(c) },
            {
                onSuccess: (result) => {
                    if (result.success) {
                        toast.success(result.message);
                    } else {
                        toast.error(result.message);
                    }
                },
                onError: (err) => setFormError(isApiError(err) ? err.message : "Uji koneksi gagal."),
            },
        );
    };

    const onDelete = () => {
        if (!pendingDelete) {
            return;
        }
        const id = pendingDelete.id;
        remove.mutate(id, {
            onSuccess: () => {
                toast.success("Koneksi dihapus.");
                setPendingDelete(null);
                setFormError(null);
            },
            onError: (err) => {
                setFormError(isApiError(err) ? err.message : "Koneksi gagal dihapus.");
                setPendingDelete(null);
            },
        });
    };

    const columns: Column<Connection>[] = [
        {
            key: "name",
            header: "Nama",
            render: (c) => (
                <Link to={`/admin/koneksi/${c.id}`} className="font-medium text-indigo-700 hover:underline">
                    {c.name}
                </Link>
            ),
        },
        { key: "engine", header: "Engine", nowrap: true, render: (c) => labelForEngine(c.engine) },
        { key: "target", header: "Target", render: (c) => <span className="break-all font-mono text-xs">{c.host}:{c.port}/{c.db_name}</span> },
        { key: "username", header: "Username", render: (c) => <span className="break-all">{c.username}</span> },
        { key: "created", header: "Dibuat", nowrap: true, render: (c) => formatDate(c.created_at) },
        {
            key: "actions",
            header: "Aksi",
            nowrap: true,
            render: (c) => (
                <div className="flex flex-wrap justify-end gap-2">
                    <Button variant="secondary" onClick={() => onTest(c)} loading={test.isPending && test.variables?.id === c.id}>
                        Uji
                    </Button>
                    <Button variant="ghost" onClick={() => setPendingDelete(c)}>
                        Hapus
                    </Button>
                </div>
            ),
        },
    ];

    return (
        <div className="space-y-6">
            <PageHeader
                title="Koneksi database"
                description="Kelola koneksi yang dipakai migration dan viewer."
                actions={
                    <Link to="/admin/koneksi/baru" className="btn btn-primary">
                        Tambah koneksi
                    </Link>
                }
            />
            {connections.isPending ? (
                <Loading />
            ) : connections.isError ? (
                <ErrorView code="500" onRetry={() => connections.refetch()} />
            ) : (
                <>
                    {formError && <div role="alert" className="alert-error">{formError}</div>}
                    <ResponsiveList
                        caption="Daftar koneksi database"
                        columns={columns}
                        rows={connections.data}
                        rowKey={(c) => c.id}
                        minWidth="52rem"
                        empty={
                            <EmptyState
                                title="Belum ada koneksi."
                                description="Tambahkan koneksi database terlebih dahulu sebelum memberi akses ke grup."
                            />
                        }
                    />
                    {connections.data.length > 0 && (
                        <p className="flex items-center gap-2 text-sm text-slate-600">
                            <Badge tone="info">{connections.data.length}</Badge> koneksi terdaftar
                        </p>
                    )}
                </>
            )}
            <ConfirmDialog
                open={pendingDelete !== null}
                title="Hapus koneksi?"
                confirmLabel="Ya, hapus"
                danger
                loading={remove.isPending}
                onConfirm={onDelete}
                onClose={() => setPendingDelete(null)}
            >
                <p>Koneksi <strong>{pendingDelete?.name}</strong> akan dihapus.</p>
                <p>Akses grup yang terkait dan sesi anggotanya akan dicabut.</p>
            </ConfirmDialog>
        </div>
    );
}
