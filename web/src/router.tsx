import { Navigate, Outlet, createBrowserRouter, useLocation } from "react-router";
import { Layout } from "./components/Layout";
import { Loading } from "./components/Loading";
import { routeForError } from "./lib/queryClient";
import { useCurrentUser } from "./lib/currentUser";
import { useMe } from "./lib/session";
import { ErrorPage, ErrorView, NotFoundPage, RouteErrorPage } from "./pages/ErrorPage";
import { HomePage } from "./pages/HomePage";
import { LoginPage } from "./pages/LoginPage";
import { SessionExpiredPage } from "./pages/SessionExpiredPage";

export const basename = "/app";

function RequireAuth() {
    const me = useMe();
    const location = useLocation();
    if (me.isPending) {
        return <Loading fullPage />;
    }
    if (me.isError) {
        if (routeForError(me.error)) {
            return <Loading fullPage />;
        }
        return <ErrorView code="500" onRetry={() => me.refetch()} />;
    }
    if (!me.data) {
        return <Navigate to="/login" replace state={{ from: location.pathname + location.search }} />;
    }
    return <Layout me={me.data} />;
}

export function RequireAdmin() {
    const me = useCurrentUser();
    if (!me.is_admin) {
        return <ErrorView code="403" />;
    }
    return <Outlet context={me} />;
}

export const router = createBrowserRouter(
    [
        {
            errorElement: <RouteErrorPage />,
            children: [
                { path: "/login", element: <LoginPage /> },
                { path: "/sesi-berakhir", element: <SessionExpiredPage /> },
                {
                    element: <RequireAuth />,
                    children: [
                        { index: true, element: <HomePage /> },
                        { path: "/error/:code", element: <ErrorPage /> },
                        { path: "*", element: <NotFoundPage /> },
                    ],
                },
            ],
        },
    ],
    { basename },
);
