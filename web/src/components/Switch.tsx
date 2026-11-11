import { useId } from "react";

type Props = {
    label: string;
    hint?: string;
    checked: boolean;
    disabled?: boolean;
    onChange: (checked: boolean) => void;
};

export function Switch({ label, hint, checked, disabled = false, onChange }: Props) {
    const id = useId();
    return (
        <div className="flex items-start justify-between gap-4">
            <div className="min-w-0">
                <label htmlFor={id} className="text-sm font-medium text-slate-800">
                    {label}
                </label>
                {hint && (
                    <p id={`${id}-hint`} className="text-xs text-slate-500">
                        {hint}
                    </p>
                )}
            </div>
            <button
                id={id}
                type="button"
                role="switch"
                aria-checked={checked}
                aria-describedby={hint ? `${id}-hint` : undefined}
                disabled={disabled}
                onClick={() => onChange(!checked)}
                className={`relative inline-flex h-7 w-12 shrink-0 items-center rounded-full transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-indigo-600 disabled:cursor-not-allowed disabled:opacity-50 ${checked ? "bg-indigo-600" : "bg-slate-300"}`}
            >
                <span aria-hidden="true" className={`inline-block h-5 w-5 rounded-full bg-white shadow transition-transform ${checked ? "translate-x-6" : "translate-x-1"}`} />
            </button>
        </div>
    );
}
