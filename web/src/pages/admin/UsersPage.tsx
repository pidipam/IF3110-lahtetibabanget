import { Link } from "react-router";
import { Badge } from "../../components/Badge";
import type { Column } from "../../components/DataTable";
import { EmptyState } from "../../components/EmptyState";
import { Loading } from "../../components/Loading";
import { PageHeader } from "../../components/PageHeader";
import { Pagination } from "../../components/Pagination";
import { ResponsiveList } from "../../components/ResponsiveList";
import { SearchInput } from "../../components/SearchInput";
import { pageSize, useUsers, type AdminUser } from "../../lib/admin";
import { useCurrentUser } from "../../lib/currentUser";
import { ErrorView } from "../ErrorPage";
import { useListParams } from "./useListParams";

export function formatDate(iso: string): string {
    return new Date(iso).toLocaleDateString("id-ID", { day: "numeric", month: "short", year: "numeric" });
}

export function UserBadges({ user }: { user: AdminUser }) {
    return (
        <span className="inline-flex flex-wrap justify-end gap-1">
            {user.is_admin ? <Badge tone="admin">Admin</Badge> : <Badge>User</Badge>}
            {!user.is_active && <Badge tone="off">Nonaktif</Badge>}
        </span>
    );
}

export function UsersPage() {
    const me = useCurrentUser();
    const { list, setPage, setQuery } = useListParams();
    const users = useUsers(list);

    const columns: Column<AdminUser>[] = [
        {
            key: "username",
            header: "Username",
            render: (u) => (
                <Link to={`/admin/user/${u.id}`} className="font-medium text-indigo-700 hover:underline">
                    {u.username}
                    {u.id === me.user.id && <span className="ml-1 font-normal text-slate-500">(Anda)</span>}
                </Link>
            ),
        },
        { key: "role", header: "Peran", render: (u) => <UserBadges user={u} /> },
        { key: "created", header: "Dibuat", nowrap: true, render: (u) => formatDate(u.created_at) },
    ];

    return (
        <div className="space-y-6">
            <PageHeader
                title="User"
                actions={
                    <Link to="/admin/user/baru" className="btn btn-primary">
                        Tambah user
                    </Link>
                }
            />
            <SearchInput label="Cari username" placeholder="Cari username" value={list.q} onSearch={setQuery} />
            {users.isPending ? (
                <Loading />
            ) : users.isError ? (
                <ErrorView code="500" onRetry={() => users.refetch()} />
            ) : (
                <div className="space-y-4">
                    <ResponsiveList
                        caption="Daftar user"
                        columns={columns}
                        rows={users.data.items}
                        rowKey={(u) => u.id}
                        minWidth="32rem"
                        empty={<EmptyState title={list.q ? "Tidak ada user yang cocok." : "Belum ada user."} />}
                    />
                    <Pagination page={list.page} pageSize={pageSize} total={users.data.total} onChange={setPage} />
                </div>
            )}
        </div>
    );
}
