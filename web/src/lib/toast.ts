import { useSyncExternalStore } from "react";

export type ToastKind = "success" | "error" | "info";

export type Toast = {
    id: number;
    kind: ToastKind;
    message: string;
};

let items: Toast[] = [];
let nextId = 1;
const listeners = new Set<() => void>();

function emit() {
    for (const l of listeners) {
        l();
    }
}

export function dismissToast(id: number) {
    items = items.filter((t) => t.id !== id);
    emit();
}

export function showToast(message: string, kind: ToastKind = "info", durationMs = 4000) {
    const id = nextId++;
    items = [...items.slice(-3), { id, kind, message }];
    emit();
    if (durationMs > 0) {
        window.setTimeout(() => dismissToast(id), durationMs);
    }
    return id;
}

export const toast = {
    success: (message: string) => showToast(message, "success"),
    error: (message: string) => showToast(message, "error", 6000),
    info: (message: string) => showToast(message, "info"),
};

function subscribe(l: () => void) {
    listeners.add(l);
    return () => {
        listeners.delete(l);
    };
}

export function useToasts(): Toast[] {
    return useSyncExternalStore(subscribe, () => items);
}
