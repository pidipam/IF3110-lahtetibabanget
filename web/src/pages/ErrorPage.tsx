import { Link, isRouteErrorResponse, useParams, useRouteError } from "react-router";

type Info = { title: string; message: string };

const messages: Record<string, Info> = {
    "403": { title: "Akses ditolak", message: "Anda tidak punya izin untuk membuka halaman atau melakukan aksi ini." },
    "404": { title: "Halaman tidak ditemukan", message: "Halaman yang Anda cari tidak ada." },
    "503": { title: "Database target tidak terjangkau", message: "Database tujuan sedang tidak bisa dihubungi. Coba lagi beberapa saat lagi." },
    "500": { title: "Terjadi kesalahan", message: "Ada masalah pada server. Coba lagi beberapa saat lagi." },
};

export function ErrorView({ code, onRetry }: { code: string; onRetry?: () => void }) {
    const info = messages[code] ?? messages["500"];
    return (
        <div className="px-4">
            <section className="hero">
                <h1>{code}</h1>
                <p>{info.title}</p>
            </section>
            <div className="card mx-auto max-w-[440px] space-y-4 p-6 sm:p-7">
                <p className="text-slate-900">{info.message}</p>
                <div className="flex flex-wrap gap-3">
                    {onRetry && (
                        <button type="button" onClick={onRetry} className="btn btn-primary">
                            Coba lagi
                        </button>
                    )}
                    <Link to="/" className="btn btn-secondary">
                        Kembali ke beranda
                    </Link>
                </div>
            </div>
        </div>
    );
}

export function ErrorPage() {
    const { code = "500" } = useParams();
    return <ErrorView code={messages[code] ? code : "404"} />;
}

export function NotFoundPage() {
    return <ErrorView code="404" />;
}

export function RouteErrorPage() {
    const err = useRouteError();
    const code = isRouteErrorResponse(err) ? String(err.status) : "500";
    return (
        <div className="min-h-screen">
            <ErrorView code={code} />
        </div>
    );
}
