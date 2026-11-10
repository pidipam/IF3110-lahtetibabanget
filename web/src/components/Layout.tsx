import { useEffect, useRef, useState } from "react";
import { Link, NavLink, Outlet, useLocation, useNavigate } from "react-router";
import { useLogout, type Me } from "../lib/session";
import { toast } from "../lib/toast";
import { Button } from "./Button";

type NavItem = {
    to: string;
    label: string;
    end?: boolean;
    adminOnly?: boolean;
};

export const navItems: NavItem[] = [{ to: "/", label: "Beranda", end: true }];

function visibleItems(me: Me) {
    return navItems.filter((i) => !i.adminOnly || me.is_admin);
}

const linkClass = ({ isActive }: { isActive: boolean }) => `rounded-md px-3 py-2 text-sm font-medium ${isActive ? "bg-indigo-50 text-indigo-700" : "text-slate-700 hover:bg-slate-100"}`;

export function Layout({ me }: { me: Me }) {
    const [open, setOpen] = useState(false);
    const location = useLocation();
    const navigate = useNavigate();
    const logout = useLogout();
    const drawer = useRef<HTMLDivElement>(null);
    const menuButton = useRef<HTMLButtonElement>(null);

    useEffect(() => {
        setOpen(false);
    }, [location.pathname]);

    useEffect(() => {
        if (!open) {
            return;
        }
        drawer.current?.querySelector<HTMLElement>("a, button")?.focus();
        const onKey = (e: KeyboardEvent) => {
            if (e.key === "Escape") {
                setOpen(false);
                menuButton.current?.focus();
            }
        };
        document.addEventListener("keydown", onKey);
        document.body.style.overflow = "hidden";
        return () => {
            document.removeEventListener("keydown", onKey);
            document.body.style.overflow = "";
        };
    }, [open]);

    const onLogout = () => {
        logout.mutate(undefined, {
            onSuccess: () => {
                toast.success("Anda sudah keluar.");
                navigate("/login", { replace: true });
            },
        });
    };

    const items = visibleItems(me);

    return (
        <div className="min-h-screen bg-slate-50">
            <header className="sticky top-0 z-30 border-b border-slate-200 bg-white">
                <div className="mx-auto flex h-14 max-w-6xl items-center gap-4 px-4">
                    <button ref={menuButton} type="button" className="-ml-2 rounded-md p-2 text-slate-700 hover:bg-slate-100 md:hidden" aria-label="Buka menu" aria-expanded={open} aria-controls="menu-laci" onClick={() => setOpen(true)}>
                        <svg className="h-6 w-6" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true">
                            <path d="M4 6h16M4 12h16M4 18h16" strokeLinecap="round" />
                        </svg>
                    </button>
                    <Link to="/" className="truncate font-semibold text-indigo-700">
                        DBMP
                    </Link>
                    <nav aria-label="Navigasi utama" className="hidden flex-1 items-center gap-1 md:flex">
                        {items.map((i) => (
                            <NavLink key={i.to} to={i.to} end={i.end} className={linkClass}>
                                {i.label}
                            </NavLink>
                        ))}
                    </nav>
                    <div className="ml-auto hidden items-center gap-3 md:flex">
                        <span className="text-sm text-slate-600">
                            {me.user.username}
                            {me.is_admin && <span className="ml-2 rounded bg-amber-100 px-1.5 py-0.5 text-xs font-medium text-amber-800">Admin</span>}
                        </span>
                        <Button variant="secondary" onClick={onLogout} loading={logout.isPending}>
                            Keluar
                        </Button>
                    </div>
                </div>
            </header>

            {open && (
                <div className="fixed inset-0 z-40 md:hidden">
                    <div className="absolute inset-0 bg-slate-900/40" onClick={() => setOpen(false)} aria-hidden="true" />
                    <div ref={drawer} id="menu-laci" role="dialog" aria-modal="true" aria-label="Menu" className="absolute inset-y-0 left-0 flex w-72 max-w-[85vw] flex-col bg-white shadow-xl">
                        <div className="flex h-14 items-center justify-between border-b border-slate-200 px-4">
                            <span className="font-semibold text-indigo-700">DBMP</span>
                            <button type="button" className="rounded-md p-2 text-slate-700 hover:bg-slate-100" aria-label="Tutup menu" onClick={() => setOpen(false)}>
                                <svg className="h-5 w-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true">
                                    <path d="M6 6l12 12M18 6L6 18" strokeLinecap="round" />
                                </svg>
                            </button>
                        </div>
                        <nav aria-label="Navigasi utama" className="flex flex-1 flex-col gap-1 overflow-y-auto p-3">
                            {items.map((i) => (
                                <NavLink key={i.to} to={i.to} end={i.end} className={linkClass}>
                                    {i.label}
                                </NavLink>
                            ))}
                        </nav>
                        <div className="space-y-3 border-t border-slate-200 p-4">
                            <p className="truncate text-sm text-slate-600">
                                Masuk sebagai <span className="font-medium text-slate-800">{me.user.username}</span>
                                {me.is_admin && " (Admin)"}
                            </p>
                            <Button variant="secondary" block onClick={onLogout} loading={logout.isPending}>
                                Keluar
                            </Button>
                        </div>
                    </div>
                </div>
            )}

            <main className="mx-auto max-w-6xl px-4 py-6">
                <Outlet context={me} />
            </main>
        </div>
    );
}
