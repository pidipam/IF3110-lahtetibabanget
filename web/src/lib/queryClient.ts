import { MutationCache, QueryCache, QueryClient } from "@tanstack/react-query";
import { isApiError } from "./api";
import { meKey } from "./session";
import { toast } from "./toast";

declare module "@tanstack/react-query" {
    interface Register {
        queryMeta: { skipGlobalError?: boolean };
        mutationMeta: { skipGlobalError?: boolean };
    }
}

export const routes = {
    login: "/login",
    sessionExpired: "/sesi-berakhir",
    forbidden: "/error/403",
    unavailable: "/error/503",
};

export function routeForError(err: unknown): string | null {
    if (!isApiError(err)) {
        return null;
    }
    if (err.status === 401) {
        return err.code === "session_expired" ? routes.sessionExpired : routes.login;
    }
    if (err.status === 403 && err.code !== "csrf_invalid") {
        return routes.forbidden;
    }
    if (err.status === 503) {
        return routes.unavailable;
    }
    return null;
}

type Navigate = (to: string, opts: { replace: boolean; state?: unknown }) => void;

export function createQueryClient(navigate: Navigate, currentPath: () => string) {
    const handle = (err: unknown, kind: "query" | "mutation", skip?: boolean) => {
        if (skip) {
            return;
        }
        const target = routeForError(err);
        if (target) {
            if (target === routes.login) {
                client.setQueryData(meKey, null);
            }
            if (currentPath() !== target) {
                navigate(target, { replace: kind === "query", state: { from: currentPath() } });
            }
            return;
        }
        if (kind === "mutation") {
            toast.error(isApiError(err) ? err.message : "Terjadi kesalahan.");
        }
    };
    const client: QueryClient = new QueryClient({
        queryCache: new QueryCache({ onError: (err, query) => handle(err, "query", query.meta?.skipGlobalError) }),
        mutationCache: new MutationCache({ onError: (err, _vars, _ctx, mutation) => handle(err, "mutation", mutation.meta?.skipGlobalError) }),
        defaultOptions: {
            queries: {
                retry: (count, err) => count < 2 && isApiError(err) && (err.status === 0 || (err.status >= 500 && err.status !== 503)),
                refetchOnWindowFocus: false,
            },
        },
    });
    return client;
}
