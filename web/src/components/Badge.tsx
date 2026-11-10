import type { ReactNode } from "react";

type Tone = "neutral" | "admin" | "off" | "info";

const tones: Record<Tone, string> = {
    neutral: "bg-slate-100 text-slate-700",
    admin: "bg-amber-100 text-amber-800",
    off: "bg-red-100 text-red-700",
    info: "bg-indigo-100 text-indigo-800",
};

export function Badge({ tone = "neutral", children }: { tone?: Tone; children: ReactNode }) {
    return <span className={`inline-flex items-center whitespace-nowrap rounded px-2 py-0.5 text-xs font-medium ${tones[tone]}`}>{children}</span>;
}
