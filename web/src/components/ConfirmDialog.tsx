import { useEffect, useId, useRef, type ReactNode } from "react";
import { Button } from "./Button";

type Props = {
    open: boolean;
    title: string;
    children: ReactNode;
    confirmLabel: string;
    danger?: boolean;
    loading?: boolean;
    onConfirm: () => void;
    onClose: () => void;
};

export function ConfirmDialog({ open, title, children, confirmLabel, danger = false, loading = false, onConfirm, onClose }: Props) {
    const ref = useRef<HTMLDialogElement>(null);
    const id = useId();

    useEffect(() => {
        const d = ref.current;
        if (!d) {
            return;
        }
        if (open && !d.open) {
            d.showModal();
        }
        if (!open && d.open) {
            d.close();
        }
    }, [open]);

    return (
        <dialog
            ref={ref}
            aria-labelledby={`${id}-title`}
            aria-describedby={`${id}-body`}
            onCancel={(e) => {
                e.preventDefault();
                if (!loading) {
                    onClose();
                }
            }}
            className="m-auto w-[calc(100%-2rem)] max-w-md rounded-[22px] border border-line bg-[#1b1a1e] p-0 text-slate-900 shadow-xl backdrop:bg-black/60"
        >
            {open && (
                <div className="space-y-4 p-6">
                    <h2 id={`${id}-title`} className="text-lg font-semibold text-slate-900">
                        {title}
                    </h2>
                    <div id={`${id}-body`} className="space-y-2 text-sm text-slate-700">
                        {children}
                    </div>
                    <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
                        <Button variant="secondary" onClick={onClose} disabled={loading} autoFocus>
                            Batal
                        </Button>
                        <Button variant={danger ? "danger" : "primary"} onClick={onConfirm} loading={loading}>
                            {confirmLabel}
                        </Button>
                    </div>
                </div>
            )}
        </dialog>
    );
}
