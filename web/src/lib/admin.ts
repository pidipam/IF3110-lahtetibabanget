import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, isApiError } from "./api";
import { meKey } from "./session";

export type AdminUser = {
    id: number;
    username: string;
    is_admin: boolean;
    is_active: boolean;
    created_at: string;
};

export type GroupRef = {
    id: number;
    name: string;
};

export type AdminUserDetail = AdminUser & {
    groups: GroupRef[];
};

export type AdminGroup = {
    id: number;
    name: string;
    description: string;
    member_count: number;
    created_at: string;
};

export type GrantLevel = "none" | "view" | "edit";

export type GroupAccess = {
    db_id: number;
    name: string;
    engine: string;
    level: GrantLevel;
};

export type AdminGroupDetail = AdminGroup & {
    members: AdminUser[];
    access: GroupAccess[];
};

export type Page<T> = {
    items: T[];
    page: number;
    page_size: number;
    total: number;
};

export type ListParams = {
    page: number;
    q: string;
};

export const pageSize = 20;

export const inlineMeta = { inlineStatuses: [409, 422] };

export const grantLabels: Record<GrantLevel, string> = { none: "Tidak ada", view: "View Only", edit: "View + Edit" };

export const adminKeys = {
    users: ["admin", "users"] as const,
    userList: (p: ListParams) => ["admin", "users", "list", p] as const,
    user: (id: number) => ["admin", "users", id] as const,
    groups: ["admin", "groups"] as const,
    groupList: (p: ListParams) => ["admin", "groups", "list", p] as const,
    group: (id: number) => ["admin", "groups", id] as const,
    candidates: (id: number) => ["admin", "groups", id, "candidates"] as const,
};

function listQuery(p: ListParams): string {
    const params = new URLSearchParams({ page: String(p.page), page_size: String(pageSize) });
    if (p.q) {
        params.set("q", p.q);
    }
    return params.toString();
}

export function isInlineError(err: unknown): boolean {
    return isApiError(err) && (err.status === 409 || err.status === 422);
}

export function useUsers(p: ListParams) {
    return useQuery({ queryKey: adminKeys.userList(p), queryFn: () => api.get<Page<AdminUser>>(`/api/admin/users?${listQuery(p)}`), placeholderData: keepPreviousData });
}

export function useUser(id: number) {
    return useQuery({ queryKey: adminKeys.user(id), queryFn: () => api.get<AdminUserDetail>(`/api/admin/users/${id}`) });
}

export function useCreateUser() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (input: { username: string; password: string; is_admin: boolean }) => api.post<AdminUser>("/api/admin/users", input),
        meta: inlineMeta,
        onSuccess: () => qc.invalidateQueries({ queryKey: adminKeys.users }),
    });
}

export function useUpdateUser(id: number, isSelf: boolean) {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (input: { username?: string; is_admin?: boolean; is_active?: boolean }) => api.patch<AdminUserDetail>(`/api/admin/users/${id}`, input),
        meta: inlineMeta,
        onSuccess: (user) => {
            qc.setQueryData(adminKeys.user(id), user);
            qc.invalidateQueries({ queryKey: adminKeys.users });
            qc.invalidateQueries({ queryKey: adminKeys.groups });
            if (isSelf) {
                qc.invalidateQueries({ queryKey: meKey });
            }
        },
    });
}

export function useResetPassword(id: number) {
    return useMutation({
        mutationFn: (password: string) => api.post<void>(`/api/admin/users/${id}/password`, { password }),
        meta: inlineMeta,
    });
}

export function useGroups(p: ListParams) {
    return useQuery({ queryKey: adminKeys.groupList(p), queryFn: () => api.get<Page<AdminGroup>>(`/api/admin/groups?${listQuery(p)}`), placeholderData: keepPreviousData });
}

export function useGroup(id: number) {
    return useQuery({ queryKey: adminKeys.group(id), queryFn: () => api.get<AdminGroupDetail>(`/api/admin/groups/${id}`) });
}

export function useCandidates(id: number) {
    return useQuery({ queryKey: adminKeys.candidates(id), queryFn: () => api.get<{ items: AdminUser[] }>(`/api/admin/groups/${id}/candidates`).then((r) => r.items) });
}

export function useCreateGroup() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (input: { name: string; description: string }) => api.post<AdminGroup>("/api/admin/groups", input),
        meta: inlineMeta,
        onSuccess: () => qc.invalidateQueries({ queryKey: adminKeys.groups }),
    });
}

function useGroupMutation<V, R>(fn: (v: V) => Promise<R>, inline = false, skipGroup?: number) {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: fn,
        meta: inline ? inlineMeta : undefined,
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: adminKeys.groups, predicate: (q) => skipGroup === undefined || q.queryKey[2] !== skipGroup });
            qc.invalidateQueries({ queryKey: adminKeys.users });
            qc.invalidateQueries({ queryKey: meKey });
        },
    });
}

export function useUpdateGroup(id: number) {
    return useGroupMutation((input: { name: string; description: string }) => api.patch<AdminGroupDetail>(`/api/admin/groups/${id}`, input), true);
}

export function useDeleteGroup(id: number) {
    return useGroupMutation(() => api.delete<void>(`/api/admin/groups/${id}`), false, id);
}

export function useAddMember(id: number) {
    return useGroupMutation((userID: number) => api.post<void>(`/api/admin/groups/${id}/members`, { user_id: userID }), true);
}

export function useRemoveMember(id: number) {
    return useGroupMutation((userID: number) => api.delete<void>(`/api/admin/groups/${id}/members/${userID}`));
}

export function useSetAccess(id: number) {
    return useGroupMutation((access: { db_id: number; level: GrantLevel }[]) => api.put<{ items: GroupAccess[] }>(`/api/admin/groups/${id}/access`, { access }), true);
}
