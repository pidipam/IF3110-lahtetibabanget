import type { ApprovalStatus, ExecuteResponse, HistoryItem, HistoryPage, HistoryQuery, Migration, MigrationInput, RequiredApprovers } from "./types";

export type ClientMode = "mock" | "http";

export interface MigrationClient {
    mode: ClientMode;
    list(dbId: string): Promise<Migration[]>;
    get(id: string): Promise<Migration>;
    create(dbId: string, input: MigrationInput): Promise<Migration>;
    update(id: string, input: MigrationInput): Promise<Migration>;
    submit(id: string): Promise<Migration>;
    execute(id: string): Promise<ExecuteResponse>;
    requiredApprovers(dbId: string): Promise<RequiredApprovers>;
    approvalStatus(id: string): Promise<ApprovalStatus | null>;
    history(dbId: string, query: HistoryQuery): Promise<HistoryPage>;
    historyItem(dbId: string, executionId: string): Promise<HistoryItem>;
}

const enc = encodeURIComponent;

export const migrationPaths = {
    list: (dbId: string) => `/api/databases/${enc(dbId)}/migrations`,
    create: (dbId: string) => `/api/databases/${enc(dbId)}/migrations`,
    detail: (id: string) => `/api/migrations/${enc(id)}`,
    update: (id: string) => `/api/migrations/${enc(id)}`,
    submit: (id: string) => `/api/migrations/${enc(id)}/submit`,
    execute: (id: string) => `/api/migrations/${enc(id)}/execute`,
    requiredApprovers: (dbId: string) => `/api/databases/${enc(dbId)}/approvers`,
    approvalStatus: (id: string) => `/api/migrations/${enc(id)}/approvals`,
    history: (dbId: string, q: HistoryQuery) => `/api/databases/${enc(dbId)}/history?${new URLSearchParams({ hide_rollback: String(q.hideRollback), page: String(q.page), page_size: String(q.pageSize) })}`,
    historyItem: (dbId: string, executionId: string) => `/api/databases/${enc(dbId)}/history/${enc(executionId)}`,
};
