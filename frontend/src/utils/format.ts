import type { Level } from "../api/types";

// Format an RFC3339 timestamp for display: local TZ short form, with
// full UTC in the title attribute via `<time>` rendering.
export function formatTimestamp(iso: string): string {
  const d = new Date(iso);
  if (isNaN(d.getTime())) return iso;
  // Locale-aware, omits seconds for compactness; tooltips carry full value.
  return d.toLocaleString(undefined, {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  });
}

export function isoTimestamp(iso: string): string {
  return iso; // already RFC3339; kept as a hook for future formatting
}

// Tailwind classes per log level. Stable across the app so colour stays
// consistent whether the level is shown as a badge or a row stripe.
export function levelClasses(level: Level): string {
  switch (level) {
    case "DEBUG":
      return "bg-indigo-100 text-indigo-800 border-indigo-200";
    case "INFO":
      return "bg-sky-100 text-sky-800 border-sky-200";
    case "WARN":
      return "bg-amber-100 text-amber-800 border-amber-200";
    case "ERROR":
      return "bg-red-100 text-red-800 border-red-200";
    case "FATAL":
      return "bg-purple-100 text-purple-800 border-purple-200";
    default:
      return "bg-slate-100 text-slate-800 border-slate-200";
  }
}

// Human-readable byte size for the (future) Projects page.
export function formatBytes(n: number): string {
  if (!Number.isFinite(n) || n < 0) return "—";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  let v = n;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(v < 10 && i > 0 ? 1 : 0)} ${units[i]}`;
}

// Relative "2m ago" / "3h ago" / "5d ago".
export function formatRelative(iso: string, now: Date = new Date()): string {
  const d = new Date(iso);
  if (isNaN(d.getTime())) return iso;
  const diffSec = Math.max(0, (now.getTime() - d.getTime()) / 1000);
  if (diffSec < 60) return `${Math.floor(diffSec)}s ago`;
  const diffMin = diffSec / 60;
  if (diffMin < 60) return `${Math.floor(diffMin)}m ago`;
  const diffHr = diffMin / 60;
  if (diffHr < 24) return `${Math.floor(diffHr)}h ago`;
  const diffDay = diffHr / 24;
  return `${Math.floor(diffDay)}d ago`;
}
