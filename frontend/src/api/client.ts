import axios from "axios";

// Single shared axios instance. All requests use the relative "/api" path,
// which Vite proxies to the backend in dev and the browser hits directly
// in prod (backend CORS to be added later).
export const apiClient = axios.create({
  baseURL: "/api",
  timeout: 15_000,
  headers: { Accept: "application/json" },
});

// Normalize backend error messages into a single string for the UI.
export function extractErrorMessage(err: unknown): string {
  if (axios.isAxiosError(err)) {
    const data = err.response?.data as { error?: string } | undefined;
    if (data?.error) {
      return data.error;
    }
    if (err.message) {
      return err.message;
    }
  }
  
  if (err instanceof Error) {
    return err.message;
  } 
  return "Unknown error";
}
