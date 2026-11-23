import { useMutation, useQuery } from "@tanstack/react-query";
import { api } from "./api";

export type SchemaDiffSource = {
    database_id: number;
    database_name: string;
    schemas: string[];
    schema: string;
    current_ddl: string;
};

export type SchemaChange = {
    op: "add" | "drop" | "alter";
    op_label: string;
    kind: string;
    object: string;
    detail: string;
    destructive: boolean;
};

export type SchemaDiffResult = {
    schema: string;
    title: string;
    changes: SchemaChange[];
    warnings: string[];
    sql: string;
    statements: number;
    skipped: number;
    too_large: boolean;
    can_create: boolean;
};

export type SchemaDiffInput = {
    schema: string;
    desired_ddl: string;
    include_destructive: boolean;
};

const base = (dbId: string) => `/api/databases/${encodeURIComponent(dbId)}/schema-diff`;

export function useSchemaDiffSource(dbId: string, schema: string) {
    const query = schema ? `?${new URLSearchParams({ schema })}` : "";
    return useQuery({ queryKey: ["schema-diff", dbId, schema], queryFn: () => api.get<SchemaDiffSource>(base(dbId) + query) });
}

export function useSchemaDiffPreview(dbId: string) {
    return useMutation({
        mutationFn: (input: SchemaDiffInput) => api.post<SchemaDiffResult>(base(dbId), input),
        meta: { inlineStatuses: [422] },
    });
}
