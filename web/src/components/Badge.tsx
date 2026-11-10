import type { ReactNode } from "react";

type Tone = "neutral" | "admin" | "off" | "info" | "success";

const tones: Record<Tone, string> = {
    neutral: "",
    admin: "badge-admin",
    off: "badge-off",
    info: "badge-info",
    success: "badge-success",
};

export function Badge({ tone = "neutral", children }: { tone?: Tone; children: ReactNode }) {
    return <span className={`badge ${tones[tone]}`}>{children}</span>;
}
