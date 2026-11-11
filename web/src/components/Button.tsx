import type { ButtonHTMLAttributes } from "react";
import { Spinner } from "./Loading";

type Variant = "primary" | "secondary" | "danger" | "ghost";

const variants: Record<Variant, string> = {
    primary: "btn-primary",
    secondary: "btn-secondary",
    danger: "btn-danger",
    ghost: "btn-ghost",
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
            className={`btn ${variants[variant]} ${block ? "w-full" : ""} ${className}`}
            {...rest}
        >
            {loading && <Spinner className="h-4 w-4" />}
            {children}
        </button>
    );
}
