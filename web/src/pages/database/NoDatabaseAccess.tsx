import { Link } from "react-router";
import { EmptyState } from "../../components/EmptyState";

export function NoDatabaseAccess({ isAdmin }: { isAdmin: boolean }) {
    if (!isAdmin) {
        return <EmptyState title="Belum ada database yang bisa diakses." description="Minta admin menambahkan Anda ke grup yang punya akses database." />;
    }
    return (
        <EmptyState
            title="Belum ada database yang bisa Anda akses."
            description="Koneksi yang sudah dibuat tetap ada di halaman Koneksi. Database tampil di sini setelah koneksi diberikan ke grup (View Only atau View + Edit) dan Anda menjadi anggota grup tersebut."
            action={
                <div className="flex flex-wrap justify-center gap-2">
                    <Link to="/admin/grup" className="btn btn-primary">
                        Atur akses grup
                    </Link>
                    <Link to="/admin/koneksi" className="btn btn-secondary">
                        Lihat koneksi
                    </Link>
                </div>
            }
        />
    );
}
