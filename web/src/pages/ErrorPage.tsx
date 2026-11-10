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
        <div className="mx-auto flex max-w-md flex-col items-center gap-3 px-4 py-16 text-center">
            <p className="text-sm font-semibold text-indigo-600">{code}</p>
            <h1 className="text-2xl font-bold text-slate-900">{info.title}</h1>
            <p className="text-slate-600">{info.message}</p>
            <div className="mt-4 flex flex-wrap justify-center gap-3">
                {onRetry && (
                    <button type="button" onClick={onRetry} className="min-h-10 rounded-md bg-indigo-600 px-4 py-2 text-sm font-semibold text-white hover:bg-indigo-700">
                        Coba lagi
                    </button>
                )}
                <Link to="/" className="inline-flex min-h-10 items-center rounded-md px-4 py-2 text-sm font-semibold text-slate-700 ring-1 ring-inset ring-slate-300 hover:bg-slate-50">
                    Kembali ke beranda
                </Link>
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
        <div className="min-h-screen bg-slate-50">
            <ErrorView code={code} />
        </div>
    );
}
