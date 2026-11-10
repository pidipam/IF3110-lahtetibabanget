import { useState, type FormEvent } from "react";
import { Navigate, useLocation, useNavigate } from "react-router";
import { Button } from "../components/Button";
import { Field, FormError, Input, fieldErrors } from "../components/Form";
import { Loading } from "../components/Loading";
import { isApiError } from "../lib/api";
import { routeForError } from "../lib/queryClient";
import { useLogin, useMe } from "../lib/session";

function safeFrom(state: unknown): string {
    const from = (state as { from?: string } | null)?.from;
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
        <div className="flex min-h-screen items-center justify-center bg-slate-50 px-4 py-8">
            <div className="w-full max-w-sm">
                <div className="mb-6 text-center">
                    <h1 className="text-2xl font-bold text-slate-900">Database Management Platform</h1>
                    <p className="mt-1 text-sm text-slate-600">Masuk untuk melanjutkan.</p>
                </div>
                <form onSubmit={onSubmit} noValidate className="space-y-4 rounded-xl bg-white p-6 shadow-sm ring-1 ring-slate-200">
                    <FormError message={formError} />
                    <Field label="Username" error={errors.username}>
                        {(p) => <Input {...p} name="username" autoComplete="username" autoCapitalize="off" value={username} onChange={(e) => setUsername(e.target.value)} autoFocus />}
                    </Field>
                    <Field label="Password" error={errors.password}>
                        {(p) => <Input {...p} name="password" type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} />}
                    </Field>
                    <Button type="submit" block loading={login.isPending}>
                        Masuk
                    </Button>
                </form>
            </div>
        </div>
    );
}
