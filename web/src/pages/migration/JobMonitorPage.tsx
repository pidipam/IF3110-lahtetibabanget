import { useEffect, useState, type ReactNode } from "react";
import { Link, useParams } from "react-router";
import { Badge } from "../../components/Badge";
import { Loading } from "../../components/Loading";
import { PageHeader, Section } from "../../components/PageHeader";
import { ProgressBar } from "../../components/ProgressBar";
import { isApiError } from "../../lib/api";
import { useCurrentUser } from "../../lib/currentUser";
import { useJob } from "../../lib/migrations";
import { jobKindLabel, jobStatusOf, pollInterval, type Job } from "../../lib/migrations/types";
import { ErrorView } from "../ErrorPage";
import { JobActions } from "./JobActions";
import { MockBanner, databaseName, formatDateTime, levelFor } from "./shared";

function useNow(ms: number) {
    const [now, setNow] = useState(() => Date.now());
    useEffect(() => {
        const t = window.setInterval(() => setNow(Date.now()), ms);
        return () => window.clearInterval(t);
    }, [ms]);
    return now;
}

function ago(from: number, now: number): string {
    const s = Math.max(0, Math.round((now - from) / 1000));
    if (s < 5) {
        return "baru saja";
    }
    if (s < 60) {
        return `${s} detik lalu`;
    }
    return `${Math.floor(s / 60)} menit lalu`;
}

type Notice = { tone: "info" | "warning" | "error" | "success"; title: string; body: ReactNode };

const noticeStyle = {
    info: "bg-slate-50 text-slate-800 ring-slate-200",
    warning: "bg-amber-50 text-amber-900 ring-amber-200",
    error: "bg-red-50 text-red-800 ring-red-200",
    success: "bg-emerald-50 text-emerald-900 ring-emerald-200",
};

function noticeFor(j: Job): Notice | null {
    const total = j.batches_total;
    const progress = total !== null ? `${j.batches_committed} dari ${total}` : `${j.batches_committed}`;
    switch (j.status) {
        case "queued":
            return { tone: "info", title: "Menunggu giliran", body: "Job ada di antrean. Satu database hanya menjalankan satu migration pada satu waktu." };
        case "partial":
            return { tone: "warning", title: "Sebagian batch sudah di-commit", body: `${progress} batch sudah tersimpan di database. Lanjutkan dengan Resume, atau batalkan perubahan dengan Rollback.` };
        case "uncertain":
            return { tone: "warning", title: "Status tidak pasti", body: "Koneksi terputus saat commit, jadi belum diketahui apakah batch terakhir tersimpan. Periksa data di database sebelum memilih aksi." };
        case "partially_reverted":
            return { tone: "warning", title: "Rollback berhenti di tengah", body: "Sebagian perubahan sudah dibatalkan, sebagian belum. Lanjutkan rollback agar database kembali konsisten." };
        case "failed":
            return { tone: "error", title: "Job gagal", body: j.is_batch_mode && j.batches_committed > 0 ? `${progress} batch sempat di-commit sebelum gagal.` : "Perubahan dibatalkan dan database tidak berubah." };
        case "cancelled":
            return { tone: "info", title: "Job dibatalkan", body: j.is_batch_mode && j.batches_committed > 0 ? `${progress} batch sudah di-commit sebelum dibatalkan dan tetap tersimpan.` : "Tidak ada perubahan yang tersimpan." };
        case "success":
            return { tone: "success", title: "Job selesai", body: "Semua perubahan berhasil diterapkan." };
    }
    return null;
}

function Time({ label, value }: { label: string; value: string | null }) {
    return (
        <div className="min-w-0">
            <dt className="text-xs font-medium uppercase tracking-wide text-slate-500">{label}</dt>
            <dd className="mt-0.5 text-sm text-slate-900">{value ? formatDateTime(value) : <span className="text-slate-400">Belum</span>}</dd>
        </div>
    );
}

export function JobMonitorPage() {
    const { jobId = "" } = useParams();
    const me = useCurrentUser();
    const job = useJob(jobId);
    const now = useNow(1000);

    if (job.isPending) {
        return <Loading label="Memuat job..." />;
    }
    if (job.isError && !job.data) {
        const code = isApiError(job.error) && job.error.status === 404 ? "404" : "500";
        return <ErrorView code={code} onRetry={code === "500" ? () => job.refetch() : undefined} />;
    }
    const j = job.data;
    if (!levelFor(me, j.database_id)) {
        return <ErrorView code="403" />;
    }

    const status = jobStatusOf(j.status);
    const interval = pollInterval(j.status);
    const notice = noticeFor(j);
    const barTone = j.status === "success" ? "success" : j.status === "running" || j.status === "queued" ? "info" : "warning";

    return (
        <div className="space-y-6">
            <PageHeader
                title={j.title}
                description={
                    <span className="flex flex-wrap items-center gap-2">
                        <span aria-live="polite">
                            <Badge tone={status.tone}>{status.label}</Badge>
                        </span>
                        <Badge>{jobKindLabel[j.kind] ?? j.kind}</Badge>
                        <span>Percobaan ke-{j.attempt}</span>
                        <span>{databaseName(me, j.database_id)}</span>
                    </span>
                }
                back={{ to: `/migration/${j.migration_id}`, label: "Kembali ke migration" }}
                actions={<JobActions job={j} />}
            />
            <MockBanner />

            <p className="flex flex-wrap items-center gap-2 text-sm text-slate-600">
                {interval ? (
                    <>
                        <span className="relative flex h-2.5 w-2.5" aria-hidden="true">
                            <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-indigo-400 opacity-75" />
                            <span className="relative inline-flex h-2.5 w-2.5 rounded-full bg-indigo-600" />
                        </span>
                        Memantau setiap {interval / 1000} detik.
                    </>
                ) : (
                    "Pemantauan berhenti karena job selesai."
                )}
                <span>Diperbarui {ago(job.dataUpdatedAt, now)}.</span>
                {job.isError && <span className="text-red-700">Gagal memuat data terbaru, mencoba lagi.</span>}
            </p>

            {notice && (
                <div role="status" className={`rounded-md px-3 py-3 text-sm ring-1 ring-inset ${noticeStyle[notice.tone]}`}>
                    <p className="font-semibold">{notice.title}</p>
                    <p className="mt-1">{notice.body}</p>
                </div>
            )}

            {j.error_message && (
                <Section title="Pesan error">
                    <pre className="overflow-x-auto whitespace-pre-wrap break-words rounded-md bg-slate-900 p-3 font-mono text-xs text-slate-100">{j.error_message}</pre>
                </Section>
            )}

            <div className="grid gap-6 lg:grid-cols-3">
                <div className="lg:col-span-2">
                    <Section title="Progres">
                        {j.is_batch_mode ? (
                            <div className="space-y-4">
                                <ProgressBar label="Batch" value={j.batches_committed} total={j.batches_total} unit="batch" tone={barTone} />
                                <ProgressBar label="Row" value={j.rows_committed} total={j.rows_total} unit="baris" tone={barTone} />
                            </div>
                        ) : (
                            <div className="space-y-2">
                                <p className="text-sm text-slate-600">Mode normal: seluruh SQL dijalankan dalam satu transaksi.</p>
                                <ProgressBar label="Transaksi" value={j.status === "success" ? 1 : 0} total={1} unit="transaksi" tone={barTone} />
                            </div>
                        )}
                    </Section>
                </div>
                <Section title="Waktu">
                    <dl className="grid gap-3">
                        <Time label="Masuk antrean" value={j.queued_at} />
                        <Time label="Mulai" value={j.started_at} />
                        <Time label="Selesai" value={j.finished_at} />
                    </dl>
                </Section>
            </div>

            <p className="text-sm text-slate-500">
                Job <span className="break-all font-mono">{j.job_id}</span>. Lihat{" "}
                <Link to={`/migration/database/${j.database_id}/riwayat`} className="text-indigo-700 hover:underline">
                    riwayat eksekusi
                </Link>{" "}
                database ini.
            </p>
        </div>
    );
}
