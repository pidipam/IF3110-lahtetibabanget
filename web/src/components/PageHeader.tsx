import type { ReactNode } from "react";
import { Link } from "react-router";

type Props = {
    title: ReactNode;
    description?: ReactNode;
    back?: { to: string; label: string };
    actions?: ReactNode;
};

export function PageHeader({ title, description, back, actions }: Props) {
    return (
        <div className="space-y-2">
            {back && (
                <Link to={back.to} className="inline-flex min-h-10 items-center gap-1 text-sm font-medium text-indigo-700 hover:underline">
                    <svg className="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true">
                        <path d="M15 18l-6-6 6-6" strokeLinecap="round" strokeLinejoin="round" />
                    </svg>
                    {back.label}
                </Link>
            )}
            <div className="flex flex-wrap items-start justify-between gap-3">
                <div className="min-w-0">
                    <h1 className="break-words text-2xl font-bold text-slate-900">{title}</h1>
                    {description && <div className="mt-1 text-sm text-slate-600">{description}</div>}
                </div>
                {actions && <div className="flex flex-wrap gap-2">{actions}</div>}
            </div>
        </div>
    );
}

type SectionProps = {
    title: string;
    description?: ReactNode;
    children: ReactNode;
    danger?: boolean;
};

export function Section({ title, description, children, danger = false }: SectionProps) {
    return (
        <section className={`min-w-0 space-y-4 rounded-xl bg-white p-4 shadow-sm ring-1 sm:p-6 ${danger ? "ring-red-200" : "ring-slate-200"}`}>
            <div>
                <h2 className={`text-lg font-semibold ${danger ? "text-red-700" : "text-slate-900"}`}>{title}</h2>
                {description && <p className="mt-1 text-sm text-slate-600">{description}</p>}
            </div>
            {children}
        </section>
    );
}
