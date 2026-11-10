import { Badge } from "../../components/Badge";
import { isMock } from "../../lib/migrations";
import { statusOf } from "../../lib/migrations/types";
import type { AccessLevel, Me } from "../../lib/session";

export function StatusBadge({ status }: { status: string }) {
    const info = statusOf(status);
    return <Badge tone={info.tone}>{info.label}</Badge>;
}

export function MockBanner() {
    if (!isMock) {
        return null;
    }
    return (
        <div role="note" className="rounded-md bg-amber-50 px-3 py-2 text-sm text-amber-900 ring-1 ring-inset ring-amber-200">
            Data contoh. API migration belum tersambung, jadi perubahan hanya tersimpan di tab ini.
        </div>
    );
}

export function levelFor(me: Me, dbId: string): AccessLevel | null {
    return me.databases.find((d) => String(d.id) === dbId)?.level ?? null;
}

export function databaseName(me: Me, dbId: string): string {
    return me.databases.find((d) => String(d.id) === dbId)?.name ?? `Database ${dbId}`;
}

export function formatDateTime(iso: string): string {
    return new Date(iso).toLocaleString("id-ID", { day: "numeric", month: "short", year: "numeric", hour: "2-digit", minute: "2-digit" });
}
