import type { Job, JobAction, JobStatus, Migration } from "./types";

export type JobRun = {
    startedAt: number;
    queueMs: number;
    batchMs: number;
    fromBatch: number;
};

export type JobRecord = {
    job: Job;
    run?: JobRun;
    pendingRequest?: boolean;
};

const rowsPerBatch = 5000;

function iso(ms: number): string {
    return new Date(ms).toISOString();
}

export function allowedActions(rec: JobRecord): JobAction[] {
    if (rec.pendingRequest) {
        return [];
    }
    const j = rec.job;
    switch (j.status) {
        case "queued":
        case "running":
            return ["cancel"];
        case "partial":
            return ["resume", "rollback"];
        case "failed":
            return ["retry"];
        case "cancelled":
            return ["retry"];
        case "uncertain":
        case "partially_reverted":
            return ["rollback"];
        case "success":
            return j.kind === "rollback" ? ["forward"] : ["rollback"];
    }
    return [];
}

export function materialize(rec: JobRecord, now: number): JobRecord {
    const run = rec.run;
    if (!run) {
        rec.job.allowed_actions = allowedActions(rec);
        return rec;
    }
    const j = rec.job;
    const elapsed = now - run.startedAt;
    const total = j.batches_total ?? 1;
    if (elapsed < run.queueMs) {
        j.status = "queued";
        j.started_at = null;
    } else {
        j.started_at = j.started_at ?? iso(run.startedAt + run.queueMs);
        const done = Math.min(total, run.fromBatch + Math.floor((elapsed - run.queueMs) / run.batchMs));
        j.batches_committed = done;
        j.rows_committed = j.is_batch_mode ? done * rowsPerBatch : 0;
        if (done >= total) {
            j.status = "success";
            j.finished_at = iso(run.startedAt + run.queueMs + (total - run.fromBatch) * run.batchMs);
            j.rows_committed = j.rows_total ?? j.rows_committed;
            delete rec.run;
        } else {
            j.status = "running";
        }
    }
    j.updated_at = iso(now);
    j.allowed_actions = allowedActions(rec);
    return rec;
}

export function startRun(rec: JobRecord, now: number, fromBatch: number) {
    rec.job.status = "queued";
    rec.job.queued_at = iso(now);
    rec.job.started_at = null;
    rec.job.finished_at = null;
    rec.job.error_message = "";
    rec.run = { startedAt: now, queueMs: 1500, batchMs: 700, fromBatch };
}

export function newJob(id: string, m: Migration, now: number): JobRecord {
    const batch = m.current_revision.is_batch_mode;
    const rec: JobRecord = {
        job: {
            job_id: id,
            execution_id: id,
            migration_id: m.id,
            database_id: m.database_id,
            title: m.title,
            kind: "up",
            status: "queued",
            attempt: 1,
            is_batch_mode: batch,
            batches_committed: 0,
            batches_total: batch ? 8 : 1,
            rows_committed: 0,
            rows_total: batch ? 8 * rowsPerBatch : null,
            error_message: "",
            queued_at: iso(now),
            started_at: null,
            finished_at: null,
            updated_at: iso(now),
            allowed_actions: [],
        },
    };
    startRun(rec, now, 0);
    return rec;
}

function fixed(id: string, m: Migration | undefined, dbId: string, now: number, status: JobStatus, patch: Partial<Job>): JobRecord {
    const minutes = (n: number) => iso(now - n * 60000);
    return {
        job: {
            job_id: id,
            execution_id: id,
            migration_id: m?.id ?? "0",
            database_id: dbId,
            title: m?.title ?? "Migration contoh",
            kind: "up",
            status,
            attempt: 1,
            is_batch_mode: true,
            batches_committed: 0,
            batches_total: 40,
            rows_committed: 0,
            rows_total: 200000,
            error_message: "",
            queued_at: minutes(30),
            started_at: minutes(29),
            finished_at: status === "success" || status === "failed" ? minutes(5) : null,
            updated_at: minutes(5),
            allowed_actions: [],
            ...patch,
        },
    };
}

export function demoJobs(m: Migration | undefined, dbId: string, now: number): Record<string, JobRecord> {
    const out: Record<string, JobRecord> = {
        "contoh-partial": fixed("contoh-partial", m, dbId, now, "partial", { batches_committed: 12, rows_committed: 60000, error_message: "Koneksi ke database terputus saat batch 13." }),
        "contoh-gagal": fixed("contoh-gagal", m, dbId, now, "failed", { is_batch_mode: false, batches_total: 1, rows_total: null, error_message: 'ERROR: column "status" of relation "pesanan" already exists (SQLSTATE 42701)' }),
        "contoh-tidak-pasti": fixed("contoh-tidak-pasti", m, dbId, now, "uncertain", { batches_committed: 27, rows_committed: 135000, error_message: "Koneksi terputus saat COMMIT batch 28." }),
        "contoh-berhasil": fixed("contoh-berhasil", m, dbId, now, "success", { batches_committed: 40, rows_committed: 200000 }),
        "contoh-berjalan": fixed("contoh-berjalan", m, dbId, now, "running", { batches_total: 400, rows_total: null, finished_at: null }),
    };
    const running = out["contoh-berjalan"];
    running.run = { startedAt: now - 60000, queueMs: 1000, batchMs: 1500, fromBatch: 0 };
    return out;
}

export function applyAction(rec: JobRecord, action: JobAction, now: number): { message: string; needsApproval: boolean } {
    const j = rec.job;
    switch (action) {
        case "cancel":
            delete rec.run;
            j.status = "cancelled";
            j.finished_at = iso(now);
            return { message: "Job dibatalkan. Batch yang sudah di-commit tetap tersimpan.", needsApproval: false };
        case "resume":
            startRun(rec, now, j.batches_committed);
            return { message: `Job dilanjutkan dari batch ke-${j.batches_committed + 1}.`, needsApproval: false };
        case "retry":
            j.attempt++;
            j.batches_committed = 0;
            j.rows_committed = 0;
            startRun(rec, now, 0);
            return { message: `Job dijalankan ulang sebagai percobaan ke-${j.attempt}.`, needsApproval: false };
        case "rollback":
            rec.pendingRequest = true;
            return { message: "Rollback diajukan dan menunggu persetujuan leader grup.", needsApproval: true };
        case "forward":
            rec.pendingRequest = true;
            return { message: "Forward diajukan dan menunggu persetujuan leader grup.", needsApproval: true };
    }
}
