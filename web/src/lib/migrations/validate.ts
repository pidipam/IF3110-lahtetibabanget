import type { MigrationInput, Revision } from "./types";

export type EditorValues = {
    title: string;
    sqlUp: string;
    sqlDown: string;
    irreversible: boolean;
    reason: string;
    batch: boolean;
    batchColumn: string;
    batchSize: string;
};

export const batchColumnPattern = /^[a-z_][a-z0-9_]{0,62}$/;

export const maxBatchSize = 100000;

export const emptyValues: EditorValues = { title: "", sqlUp: "", sqlDown: "", irreversible: false, reason: "", batch: false, batchColumn: "", batchSize: "1000" };

export function valuesFrom(title: string, rev: Revision): EditorValues {
    return {
        title,
        sqlUp: rev.sql_up,
        sqlDown: rev.sql_down ?? "",
        irreversible: rev.is_irreversible,
        reason: rev.irreversible_reason ?? "",
        batch: rev.is_batch_mode,
        batchColumn: rev.batch_column ?? "",
        batchSize: rev.batch_size ? String(rev.batch_size) : "1000",
    };
}

export function validate(v: EditorValues): Record<string, string> {
    const errors: Record<string, string> = {};
    const title = v.title.trim();
    if (!title) {
        errors.title = "Judul wajib diisi.";
    } else if ([...title].length > 100) {
        errors.title = "Judul maksimal 100 karakter.";
    }
    if (!v.sqlUp.trim()) {
        errors.sql_up = "SQL up wajib diisi.";
    }
    if (v.irreversible) {
        if (!v.reason.trim()) {
            errors.irreversible_reason = "Alasan wajib diisi untuk migration yang tidak bisa di-rollback.";
        }
    } else if (!v.sqlDown.trim()) {
        errors.sql_down = "SQL down wajib diisi, atau tandai migration sebagai irreversible.";
    }
    if (v.batch) {
        if (!batchColumnPattern.test(v.batchColumn.trim())) {
            errors.batch_column = "Nama kolom diawali huruf kecil atau garis bawah, lalu huruf kecil, angka, atau garis bawah. Maksimal 63 karakter.";
        }
        const size = Number(v.batchSize);
        if (!/^\d+$/.test(v.batchSize.trim()) || size < 1 || size > maxBatchSize) {
            errors.batch_size = `Ukuran batch harus bilangan bulat 1 sampai ${maxBatchSize.toLocaleString("id-ID")}.`;
        }
    }
    return errors;
}

export function toInput(v: EditorValues): MigrationInput {
    const input: MigrationInput = { title: v.title.trim(), sql_up: v.sqlUp, is_irreversible: v.irreversible, is_batch_mode: v.batch };
    if (v.irreversible) {
        input.irreversible_reason = v.reason.trim();
    } else {
        input.sql_down = v.sqlDown;
    }
    if (v.batch) {
        input.batch_column = v.batchColumn.trim();
        input.batch_size = Number(v.batchSize);
    }
    return input;
}

export function validateInput(input: MigrationInput): Record<string, string> {
    return validate({
        title: input.title,
        sqlUp: input.sql_up,
        sqlDown: input.sql_down ?? "",
        irreversible: input.is_irreversible,
        reason: input.irreversible_reason ?? "",
        batch: input.is_batch_mode,
        batchColumn: input.batch_column ?? "",
        batchSize: input.batch_size === undefined ? "" : String(input.batch_size),
    });
}
