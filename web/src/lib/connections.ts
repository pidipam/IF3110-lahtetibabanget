import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, isApiError } from "./api";
import { adminKeys } from "./admin";
import { meKey } from "./session";

export type Engine = "postgres" | "mysql" | "mariadb";

export type SSLMode = "disable" | "allow" | "prefer" | "require" | "verify-ca" | "verify-full";

export type Connection = {
    id: number;
    name: string;
    engine: Engine;
    host: string;
    port: number;
    db_name: string;
    username: string;
    sslmode: SSLMode;
    created_at: string;
    updated_at: string;
};

export type ConnectionInput = {
    name: string;
    engine: Engine;
    host: string;
    port: number;
    db_name: string;
    username: string;
    password: string;
    sslmode: SSLMode;
};

export type ConnectionTest = {
    success: boolean;
    code?: string;
    message: string;
};

export const engines: { value: Engine; label: string; port: number }[] = [
    { value: "postgres", label: "PostgreSQL", port: 5432 },
    { value: "mysql", label: "MySQL", port: 3306 },
    { value: "mariadb", label: "MariaDB", port: 3306 },
];

export const sslModes: SSLMode[] = ["disable", "allow", "prefer", "require", "verify-ca", "verify-full"];

export const connectionKeys = {
    all: ["connections"] as const,
    list: ["connections", "list"] as const,
    detail: (id: number) => ["connections", "detail", id] as const,
};

const inlineMeta = { inlineStatuses: [409, 422] };

export function isInlineError(err: unknown): boolean {
    return isApiError(err) && (err.status === 409 || err.status === 422);
}

export function emptyConnection(): ConnectionInput {
    return { name: "", engine: "postgres", host: "", port: 5432, db_name: "", username: "", password: "", sslmode: "prefer" };
}

export function fromConnection(c: Connection): ConnectionInput {
    return { name: c.name, engine: c.engine, host: c.host, port: c.port, db_name: c.db_name, username: c.username, password: "", sslmode: c.sslmode };
}

export function checkConnection(input: ConnectionInput, editing: boolean): Record<string, string> {
    const errors: Record<string, string> = {};
    if (!input.name.trim()) {
        errors.name = "Nama koneksi wajib diisi.";
    } else if (input.name.trim().length > 64) {
        errors.name = "Nama koneksi maksimal 64 karakter.";
    }
    if (!input.host.trim()) {
        errors.host = "Host wajib diisi.";
    } else if (input.host.trim().length > 253 || /[ /@?#\\]/.test(input.host.trim())) {
        errors.host = "Host tidak valid.";
    }
    if (!Number.isInteger(input.port) || input.port < 1 || input.port > 65535) {
        errors.port = "Port harus antara 1 dan 65535.";
    }
    if (!input.db_name.trim()) {
        errors.db_name = "Nama database wajib diisi.";
    } else if (input.db_name.trim().length > 63) {
        errors.db_name = "Nama database maksimal 63 karakter.";
    }
    if (!input.username.trim()) {
        errors.username = "Username wajib diisi.";
    } else if (input.username.trim().length > 63) {
        errors.username = "Username maksimal 63 karakter.";
    }
    if (!editing && !input.password) {
        errors.password = "Password wajib diisi.";
    } else if (new TextEncoder().encode(input.password).length > 256) {
        errors.password = "Password terlalu panjang.";
    }
    return errors;
}

export function useConnections() {
    return useQuery({
        queryKey: connectionKeys.list,
        queryFn: () => api.get<{ items: Connection[] }>("/api/admin/connections").then((r) => r.items),
    });
}

export function useConnection(id: number | undefined) {
    return useQuery({
        queryKey: connectionKeys.detail(id ?? 0),
        queryFn: () => api.get<Connection>(`/api/admin/connections/${id}`),
        enabled: id !== undefined,
    });
}

export function useTestConnection() {
    return useMutation({
        mutationFn: (input: ConnectionInput) => api.post<ConnectionTest>("/api/admin/connections/test", input),
        meta: inlineMeta,
    });
}

export function useTestSavedConnection() {
    return useMutation({
        mutationFn: ({ id, input }: { id: number; input: ConnectionInput }) => api.post<ConnectionTest>(`/api/admin/connections/${id}/test`, input),
        meta: inlineMeta,
    });
}

function useConnectionMutation() {
    const qc = useQueryClient();
    return () => {
        qc.invalidateQueries({ queryKey: connectionKeys.all });
        qc.invalidateQueries({ queryKey: adminKeys.groups });
        qc.invalidateQueries({ queryKey: meKey });
        qc.invalidateQueries({ queryKey: ["databases"] });
    };
}

export function useCreateConnection() {
    const invalidate = useConnectionMutation();
    return useMutation({
        mutationFn: (input: ConnectionInput) => api.post<Connection>("/api/admin/connections", input),
        meta: inlineMeta,
        onSuccess: invalidate,
    });
}

export function useUpdateConnection(id: number) {
    const invalidate = useConnectionMutation();
    return useMutation({
        mutationFn: (input: ConnectionInput) => api.patch<Connection>(`/api/admin/connections/${id}`, input),
        meta: inlineMeta,
        onSuccess: invalidate,
    });
}

export function useDeleteConnection() {
    const invalidate = useConnectionMutation();
    return useMutation({
        mutationFn: (id: number) => api.delete<void>(`/api/admin/connections/${id}`),
        meta: inlineMeta,
        onSuccess: invalidate,
    });
}
