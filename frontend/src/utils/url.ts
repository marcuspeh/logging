import type { Level, QueryParams } from "../api/types";

// Encode a QueryParams object to URLSearchParams, dropping empty values
// and converting limit/order into strings.
export function toSearchParams(p: QueryParams): URLSearchParams {
  const sp = new URLSearchParams();
  if (p.project) sp.set("project", p.project);
  if (p.logid) sp.set("logid", p.logid);
  if (p.level) sp.set("level", p.level);
  if (p.from) sp.set("from", p.from);
  if (p.to) sp.set("to", p.to);
  if (p.limit != null) sp.set("limit", String(p.limit));
  if (p.offset != null && p.offset > 0) sp.set("offset", String(p.offset));
  if (p.order) sp.set("order", p.order);
  return sp;
}

// Decode URLSearchParams (or a query string) back into QueryParams. Unknown
// keys are ignored; invalid values fall back to defaults.
export function fromSearchParams(input: URLSearchParams | string): QueryParams {
  const sp = typeof input === "string" ? new URLSearchParams(input) : input;
  const out: QueryParams = {};
  const project = sp.get("project");
  if (project) out.project = project;
  const logid = sp.get("logid");
  if (logid) out.logid = logid;
  const level = sp.get("level");
  if (level && isLevel(level)) out.level = level;
  const from = sp.get("from");
  if (from) out.from = from;
  const to = sp.get("to");
  if (to) out.to = to;
  const limit = sp.get("limit");
  if (limit) {
    const n = Number(limit);
    if (Number.isFinite(n) && n > 0) out.limit = n;
  }
  const offset = sp.get("offset");
  if (offset) {
    const n = Number(offset);
    if (Number.isFinite(n) && n >= 0) out.offset = n;
  }
  const order = sp.get("order");
  if (order === "asc" || order === "desc") out.order = order;
  return out;
}

function isLevel(s: string): s is Level {
  return s === "DEBUG" || s === "INFO" || s === "WARN" || s === "ERROR" || s === "FATAL";
}
