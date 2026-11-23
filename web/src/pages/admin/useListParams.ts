import { useCallback } from "react";
import { useSearchParams } from "react-router";
import type { ListParams } from "../../lib/admin";

export function useListParams() {
    const [params, setParams] = useSearchParams();
    const raw = Number(params.get("page"));
    const list: ListParams = { page: Number.isInteger(raw) && raw > 0 ? raw : 1, q: params.get("q") ?? "" };

    const setPage = useCallback(
        (page: number) =>
            setParams((prev) => {
                const next = new URLSearchParams(prev);
                if (page > 1) {
                    next.set("page", String(page));
                } else {
                    next.delete("page");
                }
                return next;
            }),
        [setParams],
    );

    const setQuery = useCallback(
        (q: string) =>
            setParams(
                (prev) => {
                    const next = new URLSearchParams(prev);
                    next.delete("page");
                    if (q) {
                        next.set("q", q);
                    } else {
                        next.delete("q");
                    }
                    return next;
                },
                { replace: true },
            ),
        [setParams],
    );

    return { list, setPage, setQuery };
}
