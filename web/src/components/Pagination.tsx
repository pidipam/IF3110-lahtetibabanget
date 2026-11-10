import { Button } from "./Button";

type Props = {
    page: number;
    pageSize: number;
    total: number;
    onChange: (page: number) => void;
};

export function Pagination({ page, pageSize, total, onChange }: Props) {
    if (total === 0) {
        return null;
    }
    const pages = Math.max(1, Math.ceil(total / pageSize));
    const from = (page - 1) * pageSize + 1;
    const to = Math.min(total, page * pageSize);
    return (
        <nav aria-label="Paginasi" className="flex flex-wrap items-center justify-between gap-3">
            <p className="text-sm text-slate-600">{from > total ? "Halaman ini kosong." : `Menampilkan ${from} sampai ${to} dari ${total}`}</p>
            <div className="flex items-center gap-2">
                <Button variant="secondary" onClick={() => onChange(Math.min(page - 1, pages))} disabled={page <= 1}>
                    Sebelumnya
                </Button>
                <span className="whitespace-nowrap text-sm text-slate-600">
                    {Math.min(page, pages)} / {pages}
                </span>
                <Button variant="secondary" onClick={() => onChange(page + 1)} disabled={page >= pages}>
                    Berikutnya
                </Button>
            </div>
        </nav>
    );
}
