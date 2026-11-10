import { useEffect } from "react";
import { Link, useLocation } from "react-router";
import { useQueryClient } from "@tanstack/react-query";
import { setCsrfToken } from "../lib/api";
import { meKey } from "../lib/session";

export function SessionExpiredPage() {
    const qc = useQueryClient();
    const location = useLocation();
    const from = (location.state as { from?: string } | null)?.from;

    useEffect(() => {
        setCsrfToken(null);
        qc.clear();
        qc.setQueryData(meKey, null);
    }, [qc]);

    return (
        <div className="flex min-h-screen items-center justify-center bg-slate-50 px-4">
            <div className="w-full max-w-sm space-y-4 rounded-xl bg-white p-6 text-center shadow-sm ring-1 ring-slate-200">
                <h1 className="text-xl font-bold text-slate-900">Sesi berakhir</h1>
                <p className="text-sm text-slate-600">Sesi Anda sudah berakhir atau dicabut. Silakan login kembali.</p>
                <Link to="/login" replace state={{ from }} className="inline-flex min-h-10 w-full items-center justify-center rounded-md bg-indigo-600 px-4 py-2 text-sm font-semibold text-white hover:bg-indigo-700">
                    Login kembali
                </Link>
            </div>
        </div>
    );
}
