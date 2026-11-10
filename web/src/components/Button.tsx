import type { ButtonHTMLAttributes } from "react";
import { Spinner } from "./Loading";

type Variant = "primary" | "secondary" | "danger" | "ghost";

const variants: Record<Variant, string> = {
    primary: "bg-indigo-600 text-white hover:bg-indigo-700 focus-visible:outline-indigo-600 disabled:bg-indigo-300",
    secondary: "bg-white text-slate-800 ring-1 ring-inset ring-slate-300 hover:bg-slate-50 focus-visible:outline-indigo-600 disabled:text-slate-400",
    danger: "bg-red-600 text-white hover:bg-red-700 focus-visible:outline-red-600 disabled:bg-red-300",
    ghost: "text-slate-700 hover:bg-slate-100 focus-visible:outline-indigo-600 disabled:text-slate-400",
};

type Props = ButtonHTMLAttributes<HTMLButtonElement> & {
    variant?: Variant;
    loading?: boolean;
    block?: boolean;
};

export function Button({ variant = "primary", loading = false, block = false, className = "", disabled, children, type = "button", ...rest }: Props) {
    return (
        <button
            type={type}
            disabled={disabled || loading}
            aria-busy={loading || undefined}
            className={`inline-flex min-h-10 items-center justify-center gap-2 rounded-md px-4 py-2 text-sm font-semibold focus-visible:outline-2 focus-visible:outline-offset-2 disabled:cursor-not-allowed ${variants[variant]} ${block ? "w-full" : ""} ${className}`}
            {...rest}
        >
            {loading && <Spinner className="h-4 w-4" />}
            {children}
        </button>
    );
}
