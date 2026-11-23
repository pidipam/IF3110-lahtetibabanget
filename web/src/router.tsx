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
import { ApprovalsPage } from "./pages/approval/ApprovalsPage";
import { DatabaseDetailPage } from "./pages/database/DatabaseDetailPage";
import { DatabasesPage } from "./pages/database/DatabasesPage";
import { ConnectionFormPage } from "./pages/admin/ConnectionFormPage";
import { ConnectionsPage } from "./pages/admin/ConnectionsPage";
import { GroupNewPage } from "./pages/admin/GroupNewPage";
import { GroupPage } from "./pages/admin/GroupPage";
import { GroupsPage } from "./pages/admin/GroupsPage";
import { UserEditPage } from "./pages/admin/UserEditPage";
import { UserNewPage } from "./pages/admin/UserNewPage";
import { UsersPage } from "./pages/admin/UsersPage";
import { DatabasePickerPage } from "./pages/migration/DatabasePickerPage";
import { ExecutionDetailPage } from "./pages/migration/ExecutionDetailPage";
import { HistoryPage } from "./pages/migration/HistoryPage";
import { JobMonitorPage } from "./pages/migration/JobMonitorPage";
import { MigrationEditPage, MigrationNewPage } from "./pages/migration/MigrationEditorPage";
import { MigrationListPage } from "./pages/migration/MigrationListPage";
import { MigrationReviewPage } from "./pages/migration/MigrationReviewPage";
import { SchemaDiffPage } from "./pages/migration/SchemaDiffPage";

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
                        { path: "/database", element: <DatabasesPage /> },
                        { path: "/database/:dbId", element: <DatabaseDetailPage /> },
                        { path: "/persetujuan", element: <ApprovalsPage /> },
                        { path: "/migration", element: <DatabasePickerPage /> },
                        { path: "/migration/database/:dbId", element: <MigrationListPage /> },
                        { path: "/migration/database/:dbId/baru", element: <MigrationNewPage /> },
                        { path: "/migration/database/:dbId/riwayat", element: <HistoryPage /> },
                        { path: "/migration/database/:dbId/riwayat/:executionId", element: <ExecutionDetailPage /> },
                        { path: "/migration/database/:dbId/schema-diff", element: <SchemaDiffPage /> },
                        { path: "/migration/job/:jobId", element: <JobMonitorPage /> },
                        { path: "/migration/:id", element: <MigrationReviewPage /> },
                        { path: "/migration/:id/ubah", element: <MigrationEditPage /> },
                        {
                            path: "/admin",
                            element: <RequireAdmin />,
                            children: [
                                { index: true, element: <Navigate to="/admin/user" replace /> },
                                { path: "koneksi", element: <ConnectionsPage /> },
                                { path: "koneksi/baru", element: <ConnectionFormPage mode="new" /> },
                                { path: "koneksi/:id", element: <ConnectionFormPage mode="edit" /> },
                                { path: "user", element: <UsersPage /> },
                                { path: "user/baru", element: <UserNewPage /> },
                                { path: "user/:id", element: <UserEditPage /> },
                                { path: "grup", element: <GroupsPage /> },
                                { path: "grup/baru", element: <GroupNewPage /> },
                                { path: "grup/:id", element: <GroupPage /> },
                            ],
                        },
                        { path: "/error/:code", element: <ErrorPage /> },
                        { path: "*", element: <NotFoundPage /> },
                    ],
                },
            ],
        },
    ],
    { basename },
);
