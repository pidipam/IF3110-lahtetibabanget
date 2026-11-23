import { useId } from "react";

type Props = {
    label: string;
    value: number;
    total: number | null;
    unit: string;
    tone?: "info" | "warning" | "success";
};

const fill = { info: "bg-indigo-600", warning: "bg-amber-500", success: "bg-emerald-600" };

const fmt = (n: number) => n.toLocaleString("id-ID");

export function ProgressBar({ label, value, total, unit, tone = "info" }: Props) {
    const id = useId();
    const known = total !== null && total > 0;
    const pct = known ? Math.min(100, Math.round((value / total) * 100)) : 0;
    const text = known ? `${fmt(value)} dari ${fmt(total)} ${unit} (${pct}%)` : `${fmt(value)} ${unit}, total belum diketahui`;
    return (
        <div className="space-y-1">
            <div className="flex flex-wrap items-baseline justify-between gap-x-3 text-sm">
                <span id={id} className="font-medium text-slate-800">
                    {label}
                </span>
                <span className="text-slate-600">{text}</span>
            </div>
            <div
                role="progressbar"
                aria-labelledby={id}
                aria-valuemin={known ? 0 : undefined}
                aria-valuemax={known ? total : undefined}
                aria-valuenow={known ? value : undefined}
                aria-valuetext={text}
                className="h-2.5 w-full overflow-hidden rounded-full bg-slate-200"
            >
                {known ? <div className={`h-full rounded-full transition-[width] duration-500 ${fill[tone]}`} style={{ width: `${pct}%` }} /> : <div className={`h-full w-full animate-pulse rounded-full opacity-40 ${fill[tone]}`} />}
            </div>
        </div>
    );
}
