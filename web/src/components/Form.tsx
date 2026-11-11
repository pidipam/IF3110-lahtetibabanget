import { useId, type InputHTMLAttributes, type ReactNode, type SelectHTMLAttributes, type TextareaHTMLAttributes } from "react";

const control = "field";

type FieldProps = {
    label: string;
    error?: string;
    hint?: string;
    children: (props: { id: string; "aria-invalid"?: boolean; "aria-describedby"?: string }) => ReactNode;
};

export function Field({ label, error, hint, children }: FieldProps) {
    const id = useId();
    const describedBy = [hint ? `${id}-hint` : "", error ? `${id}-error` : ""].filter(Boolean).join(" ") || undefined;
    return (
        <div className="space-y-1.5">
            <label htmlFor={id} className="field-label">
                {label}
            </label>
            {children({ id, "aria-invalid": error ? true : undefined, "aria-describedby": describedBy })}
            {hint && !error && (
                <p id={`${id}-hint`} className="text-xs text-slate-500">
                    {hint}
                </p>
            )}
            {error && (
                <p id={`${id}-error`} className="text-xs text-red-600">
                    {error}
                </p>
            )}
        </div>
    );
}

export function Input({ className = "", ...rest }: InputHTMLAttributes<HTMLInputElement>) {
    return <input className={`${control} min-h-10 ${className}`} {...rest} />;
}

export function Textarea({ className = "", ...rest }: TextareaHTMLAttributes<HTMLTextAreaElement>) {
    return <textarea className={`${control} ${className}`} {...rest} />;
}

export function Select({ className = "", children, ...rest }: SelectHTMLAttributes<HTMLSelectElement>) {
    return (
        <select className={`${control} min-h-10 ${className}`} {...rest}>
            {children}
        </select>
    );
}

export function FormError({ message }: { message?: string | null }) {
    if (!message) {
        return null;
    }
    return (
        <div role="alert" className="alert-error">
            {message}
        </div>
    );
}

export function fieldErrors(details: unknown): Record<string, string> {
    if (details && typeof details === "object" && !Array.isArray(details)) {
        const out: Record<string, string> = {};
        for (const [k, v] of Object.entries(details as Record<string, unknown>)) {
            if (typeof v === "string") {
                out[k] = v;
            }
        }
        return out;
    }
    return {};
}

type CheckboxProps = Omit<InputHTMLAttributes<HTMLInputElement>, "type"> & {
    label: string;
    hint?: string;
};

export function Checkbox({ label, hint, className = "", ...rest }: CheckboxProps) {
    const id = useId();
    return (
        <div className={`flex gap-3 ${className}`}>
            <input id={id} type="checkbox" className="mt-0.5 h-5 w-5 shrink-0 rounded border-slate-300 accent-indigo-600 disabled:cursor-not-allowed" aria-describedby={hint ? `${id}-hint` : undefined} {...rest} />
            <div className="min-w-0">
                <label htmlFor={id} className={`text-sm font-medium ${rest.disabled ? "text-slate-500" : "text-slate-800"}`}>
                    {label}
                </label>
                {hint && (
                    <p id={`${id}-hint`} className="text-xs text-slate-500">
                        {hint}
                    </p>
                )}
            </div>
        </div>
    );
}
