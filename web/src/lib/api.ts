export type ApiErrorBody = {
    code: string;
    message: string;
    details?: unknown;
};

export class ApiError extends Error {
    status: number;
    code: string;
    details?: unknown;

    constructor(status: number, body: ApiErrorBody) {
        super(body.message);
        this.name = "ApiError";
        this.status = status;
        this.code = body.code;
        this.details = body.details;
    }
}

export function isApiError(err: unknown): err is ApiError {
    return err instanceof ApiError;
}

let csrfToken: string | null = null;
let csrfRequest: Promise<string> | null = null;

export function setCsrfToken(token: string | null) {
    csrfToken = token;
}

async function ensureCsrf(): Promise<string> {
    if (csrfToken) {
        return csrfToken;
    }
    if (!csrfRequest) {
        csrfRequest = send<{ csrf_token: string }>("GET", "/api/csrf")
            .then((res) => {
                csrfToken = res.csrf_token;
                return res.csrf_token;
            })
            .finally(() => {
                csrfRequest = null;
            });
    }
    return csrfRequest;
}

async function parseError(res: Response): Promise<ApiError> {
    try {
        const body = await res.json();
        if (body && body.error && typeof body.error.code === "string") {
            return new ApiError(res.status, body.error);
        }
    } catch {
        return new ApiError(res.status, { code: res.status >= 500 ? "internal" : "unknown", message: defaultMessage(res.status) });
    }
    return new ApiError(res.status, { code: res.status >= 500 ? "internal" : "unknown", message: defaultMessage(res.status) });
}

function defaultMessage(status: number): string {
    if (status === 503) {
        return "Layanan sedang tidak tersedia.";
    }
    if (status >= 500) {
        return "Terjadi kesalahan pada server.";
    }
    return "Permintaan gagal.";
}

async function send<T>(method: string, path: string, body?: unknown, csrf?: string): Promise<T> {
    const headers: Record<string, string> = { Accept: "application/json" };
    if (body !== undefined) {
        headers["Content-Type"] = "application/json";
    }
    if (csrf) {
        headers["X-CSRF-Token"] = csrf;
    }
    let res: Response;
    try {
        res = await fetch(path, { method, headers, credentials: "same-origin", body: body === undefined ? undefined : JSON.stringify(body) });
    } catch {
        throw new ApiError(0, { code: "network", message: "Tidak dapat terhubung ke server." });
    }
    if (!res.ok) {
        throw await parseError(res);
    }
    if (res.status === 204) {
        return undefined as T;
    }
    return (await res.json()) as T;
}

export async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
    if (method === "GET" || method === "HEAD") {
        return send<T>(method, path);
    }
    const token = await ensureCsrf();
    try {
        return await send<T>(method, path, body, token);
    } catch (err) {
        if (isApiError(err) && err.code === "csrf_invalid") {
            setCsrfToken(null);
            return send<T>(method, path, body, await ensureCsrf());
        }
        throw err;
    }
}

export const api = {
    get: <T>(path: string) => request<T>("GET", path),
    post: <T>(path: string, body?: unknown) => request<T>("POST", path, body),
    put: <T>(path: string, body?: unknown) => request<T>("PUT", path, body),
    patch: <T>(path: string, body?: unknown) => request<T>("PATCH", path, body),
    delete: <T>(path: string) => request<T>("DELETE", path),
};
