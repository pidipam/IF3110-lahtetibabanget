import { ApiError, api } from "../api";
import type { MigrationClient } from "./client";
import { httpClient } from "./httpClient";
import { mockSeeds } from "./mockData";
import type { ApprovalGroup, ApprovalStatus, Migration, MigrationInput, Revision } from "./types";
import { validateInput } from "./validate";

type Store = {
    seq: number;
    seeded: string[];
    items: Migration[];
};

const storageKey = "dbmp-mock-migrations";

let memory: Store = { seq: 9000, seeded: [], items: [] };

function load(): Store {
    try {
        const raw = window.sessionStorage.getItem(storageKey);
        if (raw) {
            memory = JSON.parse(raw) as Store;
        }
    } catch {
        return memory;
    }
    return memory;
}

function save(s: Store) {
    memory = s;
    try {
        window.sessionStorage.setItem(storageKey, JSON.stringify(s));
    } catch {
        return;
    }
}

function delay<T>(value: T): Promise<T> {
    return new Promise((resolve) => window.setTimeout(() => resolve(structuredClone(value)), 250));
}

function fail(status: number, code: string, message: string, details?: unknown): Promise<never> {
    return new Promise((_, reject) => window.setTimeout(() => reject(new ApiError(status, { code, message, details })), 250));
}

function checksum(text: string): string {
    let h = 2166136261;
    for (let i = 0; i < text.length; i++) {
        h = Math.imul(h ^ text.charCodeAt(i), 16777619) >>> 0;
    }
    return h.toString(16).padStart(8, "0").repeat(8);
}

function revisionFrom(s: Store, input: MigrationInput, number: number, at: string): Revision {
    s.seq++;
    return {
        id: String(s.seq),
        revision_number: number,
        sql_up: input.sql_up,
        sql_down: input.is_irreversible ? undefined : input.sql_down,
        is_irreversible: input.is_irreversible,
        irreversible_reason: input.is_irreversible ? input.irreversible_reason : undefined,
        is_batch_mode: input.is_batch_mode,
        batch_column: input.is_batch_mode ? input.batch_column : undefined,
        batch_size: input.is_batch_mode ? input.batch_size : undefined,
        checksum: checksum(input.sql_up + "\n--\n" + (input.sql_down ?? "")),
        created_at: at,
    };
}

function seed(s: Store, dbId: string) {
    if (s.seeded.includes(dbId)) {
        return;
    }
    s.seeded.push(dbId);
    mockSeeds.forEach((m, i) => {
        const at = new Date(Date.now() - m.daysAgo * 86400000).toISOString();
        s.seq++;
        const id = String(s.seq);
        s.items.push({
            id,
            database_id: dbId,
            title: m.title,
            linear_order: i + 1,
            status: m.status,
            current_revision: revisionFrom(s, m.input, m.revisions, at),
            created_by: "contoh",
            created_at: at,
            updated_at: at,
        });
    });
}

function find(s: Store, id: string): Migration | undefined {
    return s.items.find((m) => m.id === id);
}

function invalid(input: MigrationInput): Promise<never> | null {
    const details = validateInput(input);
    if (Object.keys(details).length > 0) {
        return fail(422, "validation_error", "Data migration tidak valid.", details);
    }
    return null;
}

async function approvalFor(m: Migration): Promise<ApprovalStatus | null> {
    if (m.status === "Draft") {
        return null;
    }
    const required = await httpClient.requiredApprovers(m.database_id);
    const allApproved = m.status !== "Pending Review";
    const at = m.updated_at;
    const groups: ApprovalGroup[] = required.groups.map((g, i) => {
        const approved = g.leader !== null && (allApproved || (m.created_by === "contoh" && i === 0 && required.groups.length > 1));
        return { group_id: g.group_id, group_name: g.group_name, level: g.level, leader: g.leader, decision: approved ? "approved" : "pending", decided_by: approved ? g.leader : null, decided_at: approved ? at : null };
    });
    const noLeader = groups.filter((g) => g.decision === "pending" && !g.leader).map((g) => g.group_name);
    const waiting = groups.filter((g) => g.decision === "pending" && g.leader).map((g) => g.group_name);
    let state: ApprovalStatus["state"] = "approved";
    let message = "Semua leader Grup sudah menyetujui.";
    if (groups.length === 0) {
        state = "blocked";
        message = required.message;
    } else if (noLeader.length > 0) {
        state = "blocked";
        message = `Grup ${noLeader.join(", ")} belum punya leader aktif. Minta admin menetapkan leader.`;
    } else if (waiting.length > 0) {
        state = "waiting";
        message = `Menunggu persetujuan leader Grup ${waiting.join(", ")}.`;
    }
    return {
        execution_id: Number(m.id) * 10,
        migration_id: Number(m.id),
        title: m.title,
        database: { id: Number(m.database_id), name: "" },
        kind: "up",
        revision_number: m.current_revision.revision_number,
        current_revision: m.current_revision.revision_number,
        execution_status: m.status === "Pending Review" ? "pending_review" : "approved",
        requested_by: { id: 0, username: m.created_by },
        created_at: m.updated_at,
        state,
        message,
        groups,
        can_decide: false,
    };
}

export const mockClient: MigrationClient = {
    mode: "mock",
    list: (dbId) => {
        const s = load();
        seed(s, dbId);
        save(s);
        return delay(s.items.filter((m) => m.database_id === dbId).sort((a, b) => b.updated_at.localeCompare(a.updated_at)));
    },
    get: (id) => {
        const m = find(load(), id);
        return m ? delay(m) : fail(404, "not_found", "Migration tidak ditemukan.");
    },
    create: async (dbId, input) => {
        const bad = invalid(input);
        if (bad) {
            return bad;
        }
        const me = await api.get<{ user: { username: string } }>("/api/me");
        const s = load();
        seed(s, dbId);
        const at = new Date().toISOString();
        s.seq++;
        const m: Migration = { id: String(s.seq), database_id: dbId, title: input.title, linear_order: s.items.filter((x) => x.database_id === dbId).length + 1, status: "Draft", current_revision: revisionFrom(s, input, 1, at), created_by: me.user.username, created_at: at, updated_at: at };
        s.items.push(m);
        save(s);
        return delay(m);
    },
    update: (id, input) => {
        const s = load();
        const m = find(s, id);
        if (!m) {
            return fail(404, "not_found", "Migration tidak ditemukan.");
        }
        if (m.status !== "Draft" && m.status !== "Pending Review" && m.status !== "Approved") {
            return fail(409, "invalid_status", "Migration dengan status ini tidak bisa diubah.");
        }
        const bad = invalid(input);
        if (bad) {
            return bad;
        }
        const at = new Date().toISOString();
        m.title = input.title;
        m.current_revision = revisionFrom(s, input, m.current_revision.revision_number + 1, at);
        m.status = "Draft";
        m.updated_at = at;
        save(s);
        return delay(m);
    },
    submit: (id) => {
        const s = load();
        const m = find(s, id);
        if (!m) {
            return fail(404, "not_found", "Migration tidak ditemukan.");
        }
        if (m.status !== "Draft") {
            return fail(409, "invalid_status", "Hanya migration berstatus Draft yang bisa diajukan.");
        }
        m.status = "Pending Review";
        m.updated_at = new Date().toISOString();
        save(s);
        return delay(m);
    },
    execute: (id) => {
        const s = load();
        const m = find(s, id);
        if (!m) {
            return fail(404, "not_found", "Migration tidak ditemukan.");
        }
        if (m.status !== "Approved") {
            return fail(409, "invalid_status", "Migration harus disetujui sebelum dijalankan.");
        }
        m.status = "Pending Queue";
        m.updated_at = new Date().toISOString();
        save(s);
        return delay({ job_id: `contoh-${m.id}-${Date.now().toString(36)}`, message: "Migration masuk antrean." });
    },
    requiredApprovers: (dbId) => httpClient.requiredApprovers(dbId),
    approvalStatus: async (id) => {
        const m = find(load(), id);
        if (!m) {
            return fail(404, "not_found", "Migration tidak ditemukan.");
        }
        return approvalFor(m);
    },
};
