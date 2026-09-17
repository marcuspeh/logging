// Wire types mirroring the backend's HTTP responses (see backend/internal/api).
// Keep these in sync with query.go / server.go.

export type Level = "DEBUG" | "INFO" | "WARN" | "ERROR" | "FATAL";

export const LEVELS: Level[] = ["DEBUG", "INFO", "WARN", "ERROR", "FATAL"];

// One row in /query results.
export interface LogRow {
  timestamp: string; // RFC3339 UTC
  project: string;
  logid: string;
  level: Level;
  message: string;
  caller: string;
}

// /query response envelope.
export interface LogQueryResponse {
  count: number;
  results: LogRow[];
}

// /files response (kept here for future Projects page; not consumed yet).
export interface LogFile {
  file: string;
  started: string;
  ended: string;
  row_count: number;
  projects: string[];
  size_bytes: number;
}

// Parameters accepted by /query (see parseQuery in server.go).
export interface QueryParams {
  project?: string;
  logid?: string;
  level?: Level;
  from?: string; // RFC3339
  to?: string;   // RFC3339
  limit?: number;
  order?: "asc" | "desc";
}
