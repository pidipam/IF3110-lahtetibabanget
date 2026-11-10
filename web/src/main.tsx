import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "react-router/dom";
import { ToastViewport } from "./components/ToastViewport";
import { createQueryClient } from "./lib/queryClient";
import { router } from "./router";
import "./index.css";

const queryClient = createQueryClient((to, opts) => router.navigate(to, opts), () => router.state.location.pathname);

createRoot(document.getElementById("root")!).render(
    <StrictMode>
        <QueryClientProvider client={queryClient}>
            <RouterProvider router={router} />
            <ToastViewport />
        </QueryClientProvider>
    </StrictMode>,
);
