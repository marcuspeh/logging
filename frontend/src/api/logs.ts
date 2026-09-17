import { apiClient } from "./client";
import type {
  LogFile,
  LogQueryResponse,
  QueryParams,
} from "./types";

// GET /query — backend requires either `project` or `logid`.
export async function searchLogs(params: QueryParams): Promise<LogQueryResponse> {
  const res = await apiClient.get<LogQueryResponse>("/query", { params });
  return res.data;
}

// GET /healthz — one-shot, not polled.
export async function getHealth(): Promise<{ status: string }> {
  const res = await apiClient.get<{ status: string }>("/healthz");
  return res.data;
}

// GET /files — kept for the future Projects page; not yet used in the UI.
export async function getFiles(): Promise<LogFile[]> {
  const res = await apiClient.get<LogFile[]>("/files");
  return res.data;
}
