import { useState, type FormEvent } from "react";
import { Navigate, useLocation, useNavigate } from "react-router";
import { Button } from "../components/Button";
import { Field, FormError, Input, fieldErrors } from "../components/Form";
import { Loading } from "../components/Loading";
import { Logo } from "../components/Layout";
import { isApiError } from "../lib/api";
import { routeForError } from "../lib/queryClient";
import { useLogin, useMe } from "../lib/session";

function safeFrom(state: unknown): string {
    const raw = (state as { from?: string } | null)?.from;
    const from = typeof raw === "string" && (raw === "/app" || raw.startsWith("/app/")) ? raw.slice(4) || "/" : raw;
    if (typeof from === "string" && from.startsWith("/") && !from.startsWith("//") && from !== "/login" && from !== "/sesi-berakhir") {
        return from;
    }
    return "/";
}

export function LoginPage() {
    const me = useMe();
    const location = useLocation();
    const navigate = useNavigate();
    const login = useLogin();
    const [username, setUsername] = useState("");
    const [password, setPassword] = useState("");
    const [errors, setErrors] = useState<Record<string, string>>({});
    const [formError, setFormError] = useState<string | null>(null);
    const from = safeFrom(location.state);

    if (me.isPending) {
        return <Loading fullPage />;
    }
    if (me.data) {
        return <Navigate to={from} replace />;
    }

    const onSubmit = (e: FormEvent) => {
        e.preventDefault();
        const next: Record<string, string> = {};
        if (!username.trim()) {
            next.username = "Username wajib diisi.";
        }
        if (!password) {
            next.password = "Password wajib diisi.";
        }
        setErrors(next);
        setFormError(null);
        if (Object.keys(next).length > 0) {
            return;
        }
        login.mutate(
            { username: username.trim(), password },
            {
                onSuccess: () => navigate(from, { replace: true }),
                onError: (err) => {
                    setPassword("");
                    if (isApiError(err) && err.code === "validation_error") {
                        setErrors(fieldErrors(err.details));
                        setFormError(err.message);
                        return;
                    }
                    const target = routeForError(err);
                    if (target && !(isApiError(err) && err.code === "invalid_credentials")) {
                        navigate(target);
                        return;
                    }
                    setFormError(isApiError(err) ? err.message : "Login gagal. Coba lagi.");
                },
            },
        );
    };

    return (
        <div className="min-h-screen">
            <header className="mx-auto flex max-w-[1180px] items-center px-4 py-4 md:px-10 md:py-[22px]">
                <span className="flex items-center gap-2.5 font-brand text-xl font-bold tracking-tight text-slate-900">
                    <Logo />
                    lahtetibabanget
                </span>
            </header>
            <main className="px-4 pb-16">
                <section className="hero">
                    <h1>Manajemen Database</h1>
                </section>
                <div className="card mx-auto max-w-[440px] p-6 sm:p-7">
                    <form onSubmit={onSubmit} noValidate className="space-y-4">
                        <FormError message={formError} />
                        <Field label="Username" error={errors.username}>
                            {(p) => <Input {...p} name="username" autoComplete="username" autoCapitalize="off" value={username} onChange={(e) => setUsername(e.target.value)} autoFocus />}
                        </Field>
                        <Field label="Password" error={errors.password}>
                            {(p) => <Input {...p} name="password" type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} />}
                        </Field>
                        <Button type="submit" block loading={login.isPending} className="mt-2">
                            Masuk
                        </Button>
                    </form>
                </div>
            </main>
        </div>
    );
}
