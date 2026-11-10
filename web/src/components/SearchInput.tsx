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
                className="field"
            />
        </div>
    );
}
