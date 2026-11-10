import { useState } from "react";
import { useNavigate } from "react-router";
import { Button } from "../../components/Button";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import { isApiError } from "../../lib/api";
import { useJobAction } from "../../lib/migrations";
import { jobActionOrder, type Job, type JobAction } from "../../lib/migrations/types";
import { toast } from "../../lib/toast";

type ActionText = {
    label: string;
    title: string;
    confirm: string;
    danger: boolean;
    body: (job: Job) => string;
};

const texts: Record<JobAction, ActionText> = {
    resume: { label: "Resume", title: "Lanjutkan job?", confirm: "Ya, lanjutkan", danger: false, body: (j) => `Eksekusi dilanjutkan dari batch ke-${j.batches_committed + 1}. Batch yang sudah di-commit tidak dijalankan ulang.` },
    retry: { label: "Retry", title: "Jalankan ulang job?", confirm: "Ya, jalankan ulang", danger: false, body: (j) => `Job dijalankan lagi sebagai percobaan ke-${j.attempt + 1}.` },
    forward: { label: "Forward", title: "Ajukan forward?", confirm: "Ya, ajukan", danger: false, body: () => "Migration akan diterapkan kembali. Permintaan ini perlu disetujui leader Grup lebih dulu." },
    rollback: { label: "Rollback", title: "Ajukan rollback?", confirm: "Ya, ajukan rollback", danger: true, body: () => "SQL down akan dijalankan untuk membatalkan perubahan. Permintaan ini perlu disetujui leader Grup lebih dulu." },
    cancel: { label: "Cancel", title: "Batalkan job?", confirm: "Ya, batalkan", danger: true, body: (j) => (j.is_batch_mode ? "Job dihentikan. Batch yang sudah di-commit tetap tersimpan." : "Job dihentikan sebelum selesai. Transaksi yang belum di-commit dibatalkan.") },
};

export function JobActions({ job }: { job: Job }) {
    const navigate = useNavigate();
    const run = useJobAction(job.job_id);
    const [open, setOpen] = useState<JobAction | null>(null);
    const actions = jobActionOrder.filter((a) => job.allowed_actions.includes(a));

    if (actions.length === 0) {
        return null;
    }

    const onConfirm = () => {
        if (!open) {
            return;
        }
        run.mutate(open, {
            onSuccess: (res) => {
                toast.success(res.message);
                if (res.needs_approval) {
                    navigate(`/migration/${res.migration_id}`);
                    return;
                }
                if (res.job_id && res.job_id !== job.job_id) {
                    navigate(`/migration/job/${res.job_id}`);
                }
            },
            onError: (err) => {
                if (isApiError(err) && err.status === 409) {
                    toast.error(err.message);
                }
            },
            onSettled: () => setOpen(null),
        });
    };

    const t = open ? texts[open] : null;

    return (
        <div className="flex flex-wrap gap-2">
            {actions.map((a) => (
                <Button key={a} variant={texts[a].danger ? "danger" : a === actions[0] ? "primary" : "secondary"} onClick={() => setOpen(a)}>
                    {texts[a].label}
                </Button>
            ))}
            <ConfirmDialog open={open !== null} title={t?.title ?? ""} confirmLabel={t?.confirm ?? ""} danger={t?.danger} loading={run.isPending} onConfirm={onConfirm} onClose={() => setOpen(null)}>
                <p>{t?.body(job)}</p>
            </ConfirmDialog>
        </div>
    );
}
