import { useEffect, useId, useState } from "react";

type Props = {
    label: string;
    value: string;
    placeholder?: string;
    onSearch: (value: string) => void;
};

export function SearchInput({ label, value, placeholder, onSearch }: Props) {
    const [text, setText] = useState(value);
    const id = useId();

    useEffect(() => {
        setText(value);
    }, [value]);

    useEffect(() => {
        if (text.trim() === value) {
            return;
        }
        const t = window.setTimeout(() => onSearch(text.trim()), 300);
        return () => window.clearTimeout(t);
    }, [text, value, onSearch]);

    return (
        <div className="w-full sm:max-w-xs">
            <label className="sr-only" htmlFor={id}>
                {label}
            </label>
            <input
                id={id}
                type="search"
                value={text}
                placeholder={placeholder}
                onChange={(e) => setText(e.target.value)}
                className="block min-h-10 w-full rounded-md border-0 px-3 py-2 text-sm text-slate-900 ring-1 ring-inset ring-slate-300 placeholder:text-slate-400 focus:ring-2 focus:ring-inset focus:ring-indigo-600"
            />
        </div>
    );
}
