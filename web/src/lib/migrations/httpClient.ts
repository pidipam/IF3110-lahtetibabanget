import { api, isApiError } from "../api";
import { migrationPaths, type MigrationClient } from "./client";
import type { ApprovalStatus, ExecuteResponse, Migration, RequiredApprovers } from "./types";

export const httpClient: MigrationClient = {
    mode: "http",
    list: async (dbId) => {
        const res = await api.get<Migration[] | { items: Migration[] }>(migrationPaths.list(dbId));
        return Array.isArray(res) ? res : res.items;
    },
    get: (id) => api.get<Migration>(migrationPaths.detail(id)),
    create: (dbId, input) => api.post<Migration>(migrationPaths.create(dbId), input),
    update: (id, input) => api.put<Migration>(migrationPaths.update(id), input),
    submit: (id) => api.post<Migration>(migrationPaths.submit(id)),
    execute: (id) => api.post<ExecuteResponse>(migrationPaths.execute(id)),
    requiredApprovers: (dbId) => api.get<RequiredApprovers>(migrationPaths.requiredApprovers(dbId)),
    approvalStatus: async (id) => {
        try {
            return await api.get<ApprovalStatus>(migrationPaths.approvalStatus(id));
        } catch (err) {
            if (isApiError(err) && err.code === "no_pending_review") {
                return null;
            }
            throw err;
        }
    },
};
