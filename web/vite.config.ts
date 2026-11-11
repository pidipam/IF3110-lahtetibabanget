import { writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { defineConfig, type Plugin } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

const backend = process.env.VITE_BACKEND ?? "http://localhost:8080";

function keepDistPlaceholder(): Plugin {
    return {
        name: "keep-dist-placeholder",
        apply: "build",
        closeBundle() {
            writeFileSync(fileURLToPath(new URL("./dist/.gitkeep", import.meta.url)), "");
        },
    };
}

export default defineConfig({
    base: "/app/",
    plugins: [react(), tailwindcss(), keepDistPlaceholder()],
    server: {
        port: 5173,
        proxy: {
            "/api": { target: backend, changeOrigin: false },
        },
    },
    build: {
        outDir: "dist",
        emptyOutDir: true,
    },
});
