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
        <div className="min-h-screen px-4">
            <section className="hero pt-10">
                <h1>401</h1>
                <p>Sesi Berakhir</p>
            </section>
            <div className="card mx-auto max-w-[440px] space-y-4 p-6 sm:p-7">
                <p className="text-slate-900">Sesi Anda sudah berakhir atau dicabut. Silakan login kembali.</p>
                <Link to="/login" replace state={{ from }} className="btn btn-primary w-full">
                    Login kembali
                </Link>
            </div>
        </div>
    );
}
