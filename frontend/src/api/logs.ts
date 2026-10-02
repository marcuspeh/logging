import { apiClient } from "./client";
import type {
  LogFile,
  LogQueryResponse,
  ProjectsResponse,
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

// GET /projects — sorted, deduplicated project names across all loaded
// index entries. Used to populate the project autocomplete.
export async function getProjects(): Promise<ProjectsResponse> {
  const res = await apiClient.get<ProjectsResponse>("/projects");
  return res.data;
}
