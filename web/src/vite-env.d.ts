/// <reference types="vite/client" />

interface ImportMetaEnv {
    readonly VITE_MIGRATION_API?: "mock" | "http";
}

interface ImportMeta {
    readonly env: ImportMetaEnv;
}
