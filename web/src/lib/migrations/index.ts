import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { isApiError } from "../api";
import type { MigrationClient } from "./client";
import { httpClient } from "./httpClient";
import { mockClient } from "./mockClient";
import type { Migration, MigrationInput } from "./types";

export const migrationClient: MigrationClient = import.meta.env.VITE_MIGRATION_API === "http" ? httpClient : mockClient;

export const isMock = migrationClient.mode === "mock";

export const migrationKeys = {
    all: ["migrations"] as const,
    list: (dbId: string) => ["migrations", "db", dbId] as const,
    detail: (id: string) => ["migrations", "item", id] as const,
    approval: (id: string) => ["migrations", "item", id, "approval"] as const,
    approvers: (dbId: string) => ["migrations", "db", dbId, "approvers"] as const,
};

const inlineMeta = { inlineStatuses: [409, 422] };

export function isInlineError(err: unknown): boolean {
    return isApiError(err) && (err.status === 409 || err.status === 422);
}

export function useMigrations(dbId: string) {
    return useQuery({ queryKey: migrationKeys.list(dbId), queryFn: () => migrationClient.list(dbId) });
}

export function useMigration(id: string) {
    return useQuery({ queryKey: migrationKeys.detail(id), queryFn: () => migrationClient.get(id) });
}

export function useRequiredApprovers(dbId: string | undefined) {
    return useQuery({ queryKey: migrationKeys.approvers(dbId ?? ""), queryFn: () => migrationClient.requiredApprovers(dbId!), enabled: !!dbId });
}

export function useApprovalStatus(id: string, enabled: boolean) {
    return useQuery({ queryKey: migrationKeys.approval(id), queryFn: () => migrationClient.approvalStatus(id), enabled });
}

function useStore() {
    const qc = useQueryClient();
    return (m: Migration) => {
        qc.setQueryData(migrationKeys.detail(m.id), m);
        qc.invalidateQueries({ queryKey: migrationKeys.list(m.database_id) });
        qc.invalidateQueries({ queryKey: migrationKeys.approval(m.id) });
    };
}

export function useCreateMigration(dbId: string) {
    const store = useStore();
    return useMutation({ mutationFn: (input: MigrationInput) => migrationClient.create(dbId, input), meta: inlineMeta, onSuccess: store });
}

export function useUpdateMigration(id: string) {
    const store = useStore();
    return useMutation({ mutationFn: (input: MigrationInput) => migrationClient.update(id, input), meta: inlineMeta, onSuccess: store });
}

export function useSubmitMigration(id: string) {
    const store = useStore();
    return useMutation({ mutationFn: () => migrationClient.submit(id), onSuccess: store });
}

export function useExecuteMigration(id: string, dbId: string) {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: () => migrationClient.execute(id),
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: migrationKeys.detail(id) });
            qc.invalidateQueries({ queryKey: migrationKeys.list(dbId) });
        },
    });
}
