import axios from "axios";

// Resolve the API base URL at build time via VITE_API_URL.
//
// Resolution order:
//   1. import.meta.env.VITE_API_URL — set by .env / .env.local / shell /
//      docker build-arg (see frontend/Dockerfile). The browser hits this
//      origin directly (e.g. http://localhost:4665) and the backend
//      exposes /query, /healthz, /files at the root — no /api prefix.
//   2. Fallback to the relative "/api" path, which works in dev via the
//      Vite proxy in vite.config.ts (the proxy strips "/api" before
//      forwarding to the backend).
function resolveBaseURL(): string {
  const raw = import.meta.env.VITE_API_URL?.trim();
  if (raw && raw.length > 0) {
    return raw.replace(/\/+$/, "");
  }
  return "/api";
}

export const apiClient = axios.create({
  baseURL: resolveBaseURL(),
  timeout: 15_000,
  headers: { Accept: "application/json" },
});

// Normalize backend error messages into a single string for the UI.
export function extractErrorMessage(err: unknown): string {
  if (axios.isAxiosError(err)) {
    const data = err.response?.data as { error?: string } | undefined;
    if (data?.error) return data.error;
    if (err.message) return err.message;
  }
  if (err instanceof Error) return err.message;
  return "Unknown error";
}
