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

export const navItems: NavItem[] = [
    { to: "/", label: "Beranda", end: true },
    { to: "/migration", label: "Migration" },
    { to: "/admin/user", label: "User", adminOnly: true },
    { to: "/admin/grup", label: "Grup", adminOnly: true },
];

export function Logo() {
    return (
        <svg className="h-7 w-7 flex-none" viewBox="0 0 28 28" fill="none" aria-hidden="true">
            <ellipse cx="14" cy="7" rx="10" ry="4" fill="#d9a7cf" />
            <path d="M4 7v7c0 2.2 4.5 4 10 4s10-1.8 10-4V7c0 2.2-4.5 4-10 4S4 9.2 4 7Z" fill="#f3f1f4" opacity=".85" />
            <path d="M4 14v7c0 2.2 4.5 4 10 4s10-1.8 10-4v-7c0 2.2-4.5 4-10 4s-10-1.8-10-4Z" fill="#f3f1f4" opacity=".55" />
        </svg>
    );
}

function visibleItems(me: Me) {
    return navItems.filter((i) => !i.adminOnly || me.is_admin);
}

const linkClass = ({ isActive }: { isActive: boolean }) => `flex items-center gap-2 rounded-full px-[18px] py-2 text-sm font-medium ${isActive ? "bg-indigo-600 font-semibold text-on-pink" : "text-slate-700 hover:bg-surface-2"}`;

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
        <div className="min-h-screen">
            <header className="relative z-30">
                <div className="mx-auto flex max-w-[1180px] items-center gap-4 px-4 py-4 md:grid md:grid-cols-[1fr_auto_1fr] md:px-10 md:py-[22px]">
                    <div className="flex min-w-0 items-center gap-2">
                        <button ref={menuButton} type="button" className="-ml-2 rounded-full p-2 text-slate-700 hover:bg-surface-2 md:hidden" aria-label="Buka menu" aria-expanded={open} aria-controls="menu-laci" onClick={() => setOpen(true)}>
                            <svg className="h-6 w-6" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true">
                                <path d="M4 6h16M4 12h16M4 18h16" strokeLinecap="round" />
                            </svg>
                        </button>
                        <Link to="/" className="flex min-w-0 items-center gap-2.5 font-brand text-xl font-bold tracking-tight text-slate-900">
                            <Logo />
                            <span className="truncate">lahtetibabanget</span>
                        </Link>
                    </div>
                    <nav aria-label="Navigasi utama" className="hidden items-center gap-1 rounded-full border border-line bg-surface-2 p-[5px] md:flex">
                        {items.map((i) => (
                            <NavLink key={i.to} to={i.to} end={i.end} className={linkClass}>
                                {i.label}
                            </NavLink>
                        ))}
                    </nav>
                    <div className="hidden items-center justify-end gap-3.5 md:flex">
                        <span className="truncate text-[15px] font-semibold text-slate-900">{me.user.username}</span>
                        <Button variant="ghost" onClick={onLogout} loading={logout.isPending}>
                            Keluar
                        </Button>
                    </div>
                </div>
            </header>

            {open && (
                <div className="fixed inset-0 z-40 md:hidden">
                    <div className="absolute inset-0 bg-black/60" onClick={() => setOpen(false)} aria-hidden="true" />
                    <div ref={drawer} id="menu-laci" role="dialog" aria-modal="true" aria-label="Menu" className="absolute inset-y-0 left-0 flex w-72 max-w-[85vw] flex-col border-r border-line bg-[#141317] shadow-xl">
                        <div className="flex h-16 items-center justify-between border-b border-line px-4">
                            <span className="flex items-center gap-2 font-brand text-lg font-bold text-slate-900"><Logo />lahtetibabanget</span>
                            <button type="button" className="rounded-full p-2 text-slate-700 hover:bg-surface-2" aria-label="Tutup menu" onClick={() => setOpen(false)}>
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
                        <div className="space-y-3 border-t border-line p-4">
                            <p className="truncate text-[15px] font-semibold text-slate-900">{me.user.username}</p>
                            <Button variant="ghost" block onClick={onLogout} loading={logout.isPending}>
                                Keluar
                            </Button>
                        </div>
                    </div>
                </div>
            )}

            <main className="mx-auto max-w-[1080px] px-4 pb-16 pt-4 sm:px-6 sm:pt-7">
                <Outlet context={me} />
            </main>
        </div>
    );
}
