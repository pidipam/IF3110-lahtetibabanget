import { useMemo, useRef, type KeyboardEvent } from "react";

type Props = {
    id?: string;
    value: string;
    onChange?: (value: string) => void;
    readOnly?: boolean;
    rows?: number;
    placeholder?: string;
    "aria-invalid"?: boolean;
    "aria-describedby"?: string;
    "aria-label"?: string;
};

const indent = "    ";

export function SqlEditor({ id, value, onChange, readOnly = false, rows = 14, placeholder, ...aria }: Props) {
    const area = useRef<HTMLTextAreaElement>(null);
    const gutter = useRef<HTMLDivElement>(null);
    const escaped = useRef(false);
    const lineCount = useMemo(() => Math.max(value.split("\n").length, rows), [value, rows]);

    const onKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
        if (e.key === "Escape") {
            escaped.current = true;
            return;
        }
        if (e.key !== "Tab" || readOnly || escaped.current || !onChange) {
            escaped.current = false;
            return;
        }
        e.preventDefault();
        const el = e.currentTarget;
        const { selectionStart: start, selectionEnd: end } = el;
        const next = value.slice(0, start) + indent + value.slice(end);
        onChange(next);
        requestAnimationFrame(() => {
            el.selectionStart = el.selectionEnd = start + indent.length;
        });
    };

    const syncScroll = () => {
        if (gutter.current && area.current) {
            gutter.current.scrollTop = area.current.scrollTop;
        }
    };

    const hintId = id ? `${id}-sql-hint` : undefined;
    const describedBy = [aria["aria-describedby"], readOnly ? undefined : hintId].filter(Boolean).join(" ") || undefined;

    return (
        <div>
            <div className={`flex overflow-hidden rounded-md ring-1 ring-inset focus-within:ring-2 focus-within:ring-indigo-600 ${aria["aria-invalid"] ? "ring-red-500" : "ring-slate-300"} ${readOnly ? "bg-slate-50" : "bg-white"}`}>
                <div ref={gutter} aria-hidden="true" className="select-none overflow-hidden border-r border-slate-200 bg-slate-50 px-2 py-2 text-right font-mono text-xs leading-6 text-slate-400" style={{ height: `${rows * 1.5 + 1}rem` }}>
                    {Array.from({ length: lineCount }, (_, i) => (
                        <div key={i}>{i + 1}</div>
                    ))}
                </div>
                <textarea
                    ref={area}
                    id={id}
                    value={value}
                    readOnly={readOnly}
                    onChange={(e) => onChange?.(e.target.value)}
                    onKeyDown={onKeyDown}
                    onScroll={syncScroll}
                    rows={rows}
                    placeholder={placeholder}
                    spellCheck={false}
                    autoCapitalize="off"
                    autoComplete="off"
                    autoCorrect="off"
                    wrap="off"
                    aria-label={aria["aria-label"]}
                    aria-invalid={aria["aria-invalid"]}
                    aria-describedby={describedBy}
                    className="block w-full resize-none overflow-auto border-0 bg-transparent px-3 py-2 font-mono text-xs leading-6 text-slate-900 outline-none placeholder:text-slate-400"
                    style={{ height: `${rows * 1.5 + 1}rem`, tabSize: 4 }}
                />
            </div>
            {!readOnly && (
                <p id={hintId} className="mt-1 text-xs text-slate-500">
                    Tab menyisipkan 4 spasi. Tekan Esc lalu Tab untuk keluar dari editor.
                </p>
            )}
        </div>
    );
}
