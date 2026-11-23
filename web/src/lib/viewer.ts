import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { api } from "./api";
import type { Page } from "./admin";

export type AccessLevel = "view" | "edit";

export type Database = {
    id: number;
    name: string;
    engine: string;
    level: AccessLevel;
};

export type ColumnMeta = {
    position: number;
    name: string;
    type: string;
    nullable: boolean;
    has_default: boolean;
    default?: string;
};

export type ConstraintMeta = {
    name: string;
    type: string;
    definition: string;
    columns: string[];
};

export type IndexMeta = {
    name: string;
    unique: boolean;
    primary: boolean;
    definition: string;
};

export type TableMeta = {
    schema: string;
    name: string;
    kind: string;
    kind_label: string;
    columns: ColumnMeta[];
    constraints: ConstraintMeta[];
    indexes: IndexMeta[];
};

export type SchemaMeta = {
    name: string;
    tables: TableMeta[];
};

export type DatabaseDetail = {
    id: number;
    name: string;
    engine: string;
    level: AccessLevel;
    can_edit: boolean;
    schemas: SchemaMeta[];
};

export type Cell = {
    text: string;
    null: boolean;
    truncated: boolean;
};

export type TablePagination = {
    page: number;
    page_size: number;
    total: number;
    total_pages: number;
    from: number;
    to: number;
    has_prev: boolean;
    has_next: boolean;
    prev_page?: number;
    next_page?: number;
};

export type TablePage = {
    database_id: number;
    schema: string;
    name: string;
    kind: string;
    columns: string[];
    rows: Cell[][];
    pagination: TablePagination;
    order: string;
};

export const viewerPageSize = 20;

export const viewerKeys = {
    all: ["databases"] as const,
    list: (page: number) => ["databases", "list", page] as const,
    detail: (id: number) => ["databases", "detail", id] as const,
    table: (id: number, schema: string, table: string, page: number) => ["databases", "table", id, schema, table, page] as const,
};

export function useDatabases(page: number) {
    return useQuery({
        queryKey: viewerKeys.list(page),
        queryFn: () => api.get<Page<Database>>(`/api/databases?page=${page}&page_size=${viewerPageSize}`),
        placeholderData: keepPreviousData,
    });
}

export function useDatabase(id: number | undefined) {
    return useQuery({
        queryKey: viewerKeys.detail(id ?? 0),
        queryFn: () => api.get<DatabaseDetail>(`/api/databases/${id}`),
        enabled: id !== undefined,
    });
}

export function useTablePage(id: number | undefined, schema: string | undefined, table: string | undefined, page: number) {
    return useQuery({
        queryKey: viewerKeys.table(id ?? 0, schema ?? "", table ?? "", page),
        queryFn: () => api.get<TablePage>(`/api/databases/${id}/tables/${encodeURIComponent(schema ?? "")}/${encodeURIComponent(table ?? "")}?page=${page}&page_size=${viewerPageSize}`),
        enabled: id !== undefined && !!schema && !!table,
        placeholderData: keepPreviousData,
    });
}

export const engineLabel: Record<string, string> = { postgres: "PostgreSQL", mysql: "MySQL", mariadb: "MariaDB" };

export function labelForEngine(engine: string): string {
    return engineLabel[engine] ?? engine;
}
