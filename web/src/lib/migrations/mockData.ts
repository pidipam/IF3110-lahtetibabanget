import type { MigrationInput } from "./types";

export type MockSeed = {
    title: string;
    status: "Draft" | "Pending Review" | "Approved" | "Success";
    revisions: number;
    input: MigrationInput;
    daysAgo: number;
};

export const mockSeeds: MockSeed[] = [
    {
        title: "Tambah index email pelanggan",
        status: "Success",
        revisions: 1,
        daysAgo: 12,
        input: {
            title: "Tambah index email pelanggan",
            sql_up: "CREATE INDEX idx_pelanggan_email ON pelanggan (email);",
            sql_down: "DROP INDEX idx_pelanggan_email;",
            is_irreversible: false,
            is_batch_mode: false,
        },
    },
    {
        title: "Tambah kolom status pada pesanan",
        status: "Approved",
        revisions: 2,
        daysAgo: 3,
        input: {
            title: "Tambah kolom status pada pesanan",
            sql_up: "ALTER TABLE pesanan\n    ADD COLUMN status TEXT NOT NULL DEFAULT 'baru';",
            sql_down: "ALTER TABLE pesanan\n    DROP COLUMN status;",
            is_irreversible: false,
            is_batch_mode: false,
        },
    },
    {
        title: "Isi ulang kolom total_harga",
        status: "Pending Review",
        revisions: 1,
        daysAgo: 1,
        input: {
            title: "Isi ulang kolom total_harga",
            sql_up: "UPDATE pesanan\nSET total_harga = jumlah * harga_satuan\nWHERE total_harga IS NULL;",
            sql_down: "UPDATE pesanan\nSET total_harga = NULL;",
            is_irreversible: false,
            is_batch_mode: true,
            batch_column: "id",
            batch_size: 5000,
        },
    },
    {
        title: "Hapus tabel log_lama",
        status: "Draft",
        revisions: 1,
        daysAgo: 0,
        input: {
            title: "Hapus tabel log_lama",
            sql_up: "DROP TABLE log_lama;",
            is_irreversible: true,
            irreversible_reason: "Data log sudah diarsipkan ke penyimpanan terpisah.",
            is_batch_mode: false,
        },
    },
];
