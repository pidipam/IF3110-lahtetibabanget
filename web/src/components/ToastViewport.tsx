import { dismissToast, useToasts, type ToastKind } from "../lib/toast";

const styles: Record<ToastKind, string> = {
    success: "bg-emerald-50 text-emerald-800 ring-emerald-200",
    error: "bg-red-50 text-red-800 ring-red-200",
    info: "bg-white text-slate-800 ring-slate-200",
};

export function ToastViewport() {
    const items = useToasts();
    return (
        <div aria-live="polite" className="pointer-events-none fixed inset-x-0 bottom-0 z-50 flex flex-col items-center gap-2 p-4 sm:items-end">
            {items.map((t) => (
                <div key={t.id} role={t.kind === "error" ? "alert" : "status"} className={`pointer-events-auto flex w-full max-w-sm items-start gap-3 rounded-lg px-4 py-3 text-sm shadow-lg ring-1 ${styles[t.kind]}`}>
                    <p className="flex-1">{t.message}</p>
                    <button type="button" onClick={() => dismissToast(t.id)} className="rounded px-1 text-lg leading-none opacity-60 hover:opacity-100" aria-label="Tutup notifikasi">
                        &times;
                    </button>
                </div>
            ))}
        </div>
    );
}
