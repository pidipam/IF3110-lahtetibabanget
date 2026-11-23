import { useMutation } from "@tanstack/react-query";
import { api } from "./api";

export type PlanNode = {
    depth: number;
    prefix: string;
    node: string;
    relation: string;
    index: string;
    join: string;
    startup_cost: number;
    total_cost: number;
    plan_rows: number;
    has_actual: boolean;
    actual_rows: number;
    actual_time: number;
    loops: number;
};

export type StatementPlan = {
    number: number;
    sql: string;
    no_plan: boolean;
    error: string;
    nodes: PlanNode[];
    has_timing: boolean;
    planning_time: number;
    execution_time: number;
};

export type Explanation = {
    analyze: boolean;
    revision_number: number;
    stopped: boolean;
    statements: StatementPlan[];
};

export function useExplain(migrationId: string) {
    return useMutation({
        mutationFn: (analyze: boolean) => api.post<Explanation>(`/api/migrations/${encodeURIComponent(migrationId)}/explain`, { analyze }),
        meta: { inlineStatuses: [409, 422] },
    });
}
