import type { ReactNode } from "react";

type Props = {
    title: string;
    description?: ReactNode;
    action?: ReactNode;
};

export function EmptyState({ title, description, action }: Props) {
    return (
        <div className="flex flex-col items-center gap-2 rounded-[18px] border border-line bg-surface px-4 py-10 text-center">
            <svg className="h-10 w-10 text-slate-400" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" aria-hidden="true">
                <ellipse cx="12" cy="5" rx="8" ry="3" />
                <path d="M4 5v14c0 1.7 3.6 3 8 3s8-1.3 8-3V5" />
                <path d="M4 12c0 1.7 3.6 3 8 3s8-1.3 8-3" />
            </svg>
            <p className="font-semibold text-slate-800">{title}</p>
            {description && <p className="max-w-md text-sm text-slate-600">{description}</p>}
            {action && <div className="mt-2">{action}</div>}
        </div>
    );
}
