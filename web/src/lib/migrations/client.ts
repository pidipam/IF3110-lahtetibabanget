import type { ApprovalStatus, ExecuteResponse, Migration, MigrationInput, RequiredApprovers } from "./types";

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
};
