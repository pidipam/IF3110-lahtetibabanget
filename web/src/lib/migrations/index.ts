import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { isApiError } from "../api";
import type { MigrationClient } from "./client";
import { httpClient } from "./httpClient";
import { mockClient } from "./mockClient";
import { pollInterval, type HistoryQuery, type JobAction, type Migration, type MigrationInput } from "./types";

export const migrationClient: MigrationClient = import.meta.env.VITE_MIGRATION_API === "http" ? httpClient : mockClient;

export const isMock = migrationClient.mode === "mock";

export const migrationKeys = {
    all: ["migrations"] as const,
    list: (dbId: string) => ["migrations", "db", dbId] as const,
    detail: (id: string) => ["migrations", "item", id] as const,
    approval: (id: string) => ["migrations", "item", id, "approval"] as const,
    approvers: (dbId: string) => ["migrations", "db", dbId, "approvers"] as const,
    history: (dbId: string) => ["migrations", "db", dbId, "history"] as const,
    historyPage: (dbId: string, q: HistoryQuery) => ["migrations", "db", dbId, "history", q] as const,
    historyItem: (dbId: string, executionId: string) => ["migrations", "db", dbId, "history", "item", executionId] as const,
};

export const historyPageSize = 20;

export const jobKey = (id: string) => ["jobs", id] as const;

export function useJob(id: string) {
    return useQuery({
        queryKey: jobKey(id),
        queryFn: () => migrationClient.job(id),
        refetchOnWindowFocus: true,
        refetchIntervalInBackground: false,
        refetchInterval: (q) => {
            const err = q.state.error;
            if (err) {
                if (isApiError(err) && err.status >= 400 && err.status < 500) {
                    return false;
                }
                return Math.min(30000, 2000 * 2 ** Math.min(q.state.fetchFailureCount, 4));
            }
            return q.state.data ? pollInterval(q.state.data.status) : false;
        },
    });
}

export function useJobAction(id: string) {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (action: JobAction) => migrationClient.jobAction(id, action),
        meta: { inlineStatuses: [409] },
        onSettled: () => qc.invalidateQueries({ queryKey: jobKey(id) }),
        onSuccess: (res) => {
            qc.invalidateQueries({ queryKey: migrationKeys.detail(res.migration_id) });
            qc.invalidateQueries({ queryKey: migrationKeys.approval(res.migration_id) });
        },
    });
}

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

export function useHistory(dbId: string, q: HistoryQuery) {
    return useQuery({ queryKey: migrationKeys.historyPage(dbId, q), queryFn: () => migrationClient.history(dbId, q), placeholderData: keepPreviousData });
}

export function useHistoryItem(dbId: string, executionId: string) {
    return useQuery({ queryKey: migrationKeys.historyItem(dbId, executionId), queryFn: () => migrationClient.historyItem(dbId, executionId) });
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
            qc.invalidateQueries({ queryKey: migrationKeys.history(dbId) });
        },
    });
}
