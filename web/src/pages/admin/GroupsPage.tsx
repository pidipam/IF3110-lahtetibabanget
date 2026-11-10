import { Link } from "react-router";
import type { Column } from "../../components/DataTable";
import { EmptyState } from "../../components/EmptyState";
import { Loading } from "../../components/Loading";
import { PageHeader } from "../../components/PageHeader";
import { Pagination } from "../../components/Pagination";
import { ResponsiveList } from "../../components/ResponsiveList";
import { SearchInput } from "../../components/SearchInput";
import { pageSize, useGroups, type AdminGroup } from "../../lib/admin";
import { ErrorView } from "../ErrorPage";
import { useListParams } from "./useListParams";

const columns: Column<AdminGroup>[] = [
    {
        key: "name",
        header: "Nama",
        render: (g) => (
            <Link to={`/admin/grup/${g.id}`} className="font-medium text-indigo-700 hover:underline">
                {g.name}
            </Link>
        ),
    },
    { key: "description", header: "Deskripsi", render: (g) => (g.description ? <span className="line-clamp-2">{g.description}</span> : <span className="text-slate-400">Tanpa deskripsi</span>) },
    { key: "members", header: "Anggota", nowrap: true, render: (g) => `${g.member_count} orang` },
];

export function GroupsPage() {
    const { list, setPage, setQuery } = useListParams();
    const groups = useGroups(list);

    return (
        <div className="space-y-6">
            <PageHeader
                title="Grup"
                description="Grup menentukan database yang bisa diakses anggotanya."
                actions={
                    <Link to="/admin/grup/baru" className="inline-flex min-h-10 items-center rounded-md bg-indigo-600 px-4 py-2 text-sm font-semibold text-white hover:bg-indigo-700">
                        Tambah Grup
                    </Link>
                }
            />
            <SearchInput label="Cari nama Grup" placeholder="Cari nama Grup" value={list.q} onSearch={setQuery} />
            {groups.isPending ? (
                <Loading />
            ) : groups.isError ? (
                <ErrorView code="500" onRetry={() => groups.refetch()} />
            ) : (
                <div className="space-y-4">
                    <ResponsiveList
                        caption="Daftar Grup"
                        columns={columns}
                        rows={groups.data.items}
                        rowKey={(g) => g.id}
                        minWidth="36rem"
                        empty={<EmptyState title={list.q ? "Tidak ada Grup yang cocok." : "Belum ada Grup."} />}
                    />
                    <Pagination page={list.page} pageSize={pageSize} total={groups.data.total} onChange={setPage} />
                </div>
            )}
        </div>
    );
}
