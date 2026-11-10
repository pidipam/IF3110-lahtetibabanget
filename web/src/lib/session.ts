import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, isApiError, setCsrfToken } from "./api";

export type AccessLevel = "view" | "edit";

export type DatabaseAccess = {
    id: number;
    name: string;
    engine: string;
    level: AccessLevel;
};

export type Me = {
    user: { id: number; username: string };
    is_admin: boolean;
    csrf_token: string;
    databases: DatabaseAccess[];
};

export const meKey = ["me"] as const;

export async function fetchMe(): Promise<Me | null> {
    try {
        const me = await api.get<Me>("/api/me");
        setCsrfToken(me.csrf_token);
        return me;
    } catch (err) {
        if (isApiError(err) && err.code === "unauthenticated") {
            return null;
        }
        throw err;
    }
}

export function useMe() {
    return useQuery({ queryKey: meKey, queryFn: fetchMe, staleTime: 60_000 });
}

export function useLogin() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (input: { username: string; password: string }) => api.post<Me>("/api/login", input),
        meta: { skipGlobalError: true },
        onSuccess: (me) => {
            setCsrfToken(me.csrf_token);
            qc.clear();
            qc.setQueryData(meKey, me);
        },
    });
}

export function useLogout() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: () => api.post<void>("/api/logout"),
        onSettled: () => {
            setCsrfToken(null);
            qc.clear();
            qc.setQueryData(meKey, null);
        },
    });
}

export function levelLabel(level: AccessLevel): string {
    return level === "edit" ? "View + Edit" : "View Only";
}
