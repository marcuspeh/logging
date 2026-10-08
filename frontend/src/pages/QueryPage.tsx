import { useEffect, useMemo, useRef, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { motion, AnimatePresence } from "framer-motion";
import {
  Search,
  Terminal,
  ChevronDown,
  ChevronRight,
  FileCode2,
  Fingerprint,
  AlertTriangle,
} from "lucide-react";
import { Input } from "../components/ui/Input";
import { Select } from "../components/ui/Select";
import { JsonHighlighter } from "../components/ui/JsonHighlighter";
import { useSearch } from "../hooks/useSearch";
import { useProjects } from "../hooks/useProjects";
import type { Level, LogRow } from "../api/types";
import { extractErrorMessage } from "../api/client";
import { cn } from "../lib/utils";

// Time-range presets the user can pick from. "custom" represents any
// range that doesn't snap to a preset; the URL then drives the actual
// `from` / `to` values the user has typed into the pickers.
const TIME_PRESETS: Record<string, number | "custom"> = {
  "15m": 15,
  "1h": 60,
  "6h": 60 * 6,
  "24h": 60 * 24,
  "72h": 60 * 24 * 3,
  custom: "custom",
};
const DEFAULT_TIME_PRESET = "1h";
const DEFAULT_VIEW = 200;

// Try to match a (from, to) pair to one of the presets. If `to` is "now"
// (within the last minute) and the gap matches a preset, return that
// preset; otherwise return "custom".
function presetFromWindow(
  fromIso: string | undefined,
  toIso: string | undefined,
): string {
  if (!fromIso || !toIso) return DEFAULT_TIME_PRESET;
  const from = new Date(fromIso).getTime();
  const to = new Date(toIso).getTime();
  if (!Number.isFinite(from) || !Number.isFinite(to)) return "custom";
  const minutes = Math.round((to - from) / 60_000);
  // Allow a small slack window so "now" doesn't drift out of the preset.
  for (const [preset, value] of Object.entries(TIME_PRESETS)) {
    if (value === "custom") continue;
    if (Math.abs(minutes - value) <= 1) return preset;
  }
  return "custom";
}

function windowFromPreset(preset: string): { from?: string; to?: string } {
  const minutes = TIME_PRESETS[preset];
  if (!minutes || minutes === "custom") return {};
  const to = new Date();
  const from = new Date(to.getTime() - minutes * 60_000);
  return { from: from.toISOString(), to: to.toISOString() };
}

// Composite row key: logid is not unique across rows (one logid can
// span many timestamps/messages). Includes index as a final tiebreaker
// so React keys stay stable within a single rendered slice.
function rowKey(r: LogRow, index: number): string {
  return `${r.timestamp}|${r.project}|${r.logid}|${index}`;
}

// Level badge matching the prototype palette.
function LevelBadge({ level }: { level: string }) {
  const colors: Record<string, string> = {
    ERROR: "bg-red-50 text-red-700 border-red-200",
    WARN: "bg-amber-50 text-amber-700 border-amber-200",
    INFO: "bg-blue-50 text-blue-700 border-blue-200",
    DEBUG: "bg-slate-50 text-slate-600 border-slate-200",
    FATAL: "bg-purple-50 text-purple-700 border-purple-200",
  };
  return (
    <span
      className={cn(
        "px-2 py-0.5 rounded text-[10px] border font-mono font-bold uppercase tracking-wider",
        colors[level] ?? colors.DEBUG,
      )}
    >
      {level}
    </span>
  );
}

// Format an ISO timestamp as separate date (YYYY-MM-DD) and time
// (HH:MM:SS.mmm) strings in the local zone. Returns the raw ISO on
// parse failure.
function shortTime(iso: string): { date: string; time: string } {
  const d = new Date(iso);
  if (isNaN(d.getTime())) return { date: iso, time: "" };
  const date = d.toLocaleDateString(undefined, {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  });
  const time = d.toLocaleTimeString(undefined, {
    hour12: false,
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    fractionalSecondDigits: 3,
  });
  return { date, time };
}

// Build a synthetic JSON payload from a LogRow so the expanded panel
// has something meaningful to show even though the backend doesn't
// return a `details` field.
function syntheticPayload(row: LogRow): string {
  return JSON.stringify(
    {
      timestamp: row.timestamp,
      project: row.project,
      logid: row.logid,
      level: row.level,
      caller: row.caller || null,
      message: row.message,
    },
    null,
    2,
  );
}

// QueryPage is the single page of the app. It mirrors the prototype's
// "Argos Logs" module: top filter bar with service / Log ID / Level /
// Time-range, then a virtualized-free list of expandable log rows
// showing metadata + a JSON payload panel when expanded.
//
// This component is the *sole* writer to the URL. The useSearch hook
// reads filters from props/state and never touches the URL.
//
// The data flow:
//   * The useSearch hook owns the committed server query (project + logid
//     + time range). We pass those to the backend.
//   * The level filter is applied client-side on top of the result so
//     it doesn't force a refetch when toggled.
//   * The service dropdown's "free text" matches the backend's `project`
//     field; suggestions come from the /projects endpoint.
export function QueryPage() {
  const [searchParams, setSearchParams] = useSearchParams();

  const psmFilter = searchParams.get("psm") ?? "";
  const levelFilter = searchParams.get("level") ?? "";
  const logIdFilter = searchParams.get("logId") ?? "";
  const fromFilter = searchParams.get("from") ?? "";
  const toFilter = searchParams.get("to") ?? "";
  const expandedId = searchParams.get("expandedId") ?? "";
  // `view` is the *client* display cap (rows.slice(0, view)). Distinct
  // from the server page size, which the hook owns internally. Using a
  // separate key prevents the hook's URL writes from clobbering this
  // value (and vice versa).
  const rawView = Number(searchParams.get("view") ?? DEFAULT_VIEW);
  const view =
    Number.isFinite(rawView) && rawView > 0 ? rawView : DEFAULT_VIEW;

  // Derive the displayed time preset from the (from, to) window.
  const timeRange = presetFromWindow(fromFilter, toFilter);

  const updateParams = (key: string, value: string) => {
    const next = new URLSearchParams(searchParams);
    if (value) next.set(key, value);
    else next.delete(key);
    setSearchParams(next, { replace: true });
  };

  const {
    apply,
    loadMore,
    reset,
    query,
    enabled,
    rows,
    totalCount,
    hasMore,
  } = useSearch();

  // Sync URL-driven filters into useSearch's params. URL is the source
  // of truth — both `psm` and `project` keys map to the backend's
  // `project` field, and the preset URL key (`time`) is translated to
  // ISO `from`/`to` before being passed to the hook.
  useEffect(() => {
    apply({
      project: psmFilter || undefined,
      logid: logIdFilter || undefined,
      level: (levelFilter as Level) || undefined,
      from: fromFilter || undefined,
      to: toFilter || undefined,
    });
    // apply is a stable callback from the hook.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [psmFilter, levelFilter, logIdFilter, fromFilter, toFilter]);

  // Service suggestions come from the backend's /projects endpoint.
  const projectsQuery = useProjects();
  const projectOptions = projectsQuery.data ?? [];

  // Client-side level filter applied on top of backend results, so
  // toggling a level chip doesn't trigger a refetch.
  const filteredRows = useMemo(() => {
    if (!levelFilter) return rows;
    return rows.filter((r) => r.level === levelFilter);
  }, [rows, levelFilter]);

  // Client display cap. Tracked in local state so rapid clicks don't
  // thrash the URL; the URL is updated on settle via the effect below.
  const [displayLimit, setDisplayLimit] = useState(view);
  useEffect(() => {
    setDisplayLimit(view);
  }, [view]);
  const viewCommitTimer = useRef<number | null>(null);
  useEffect(() => {
    if (displayLimit === view) return;
    if (viewCommitTimer.current !== null) {
      window.clearTimeout(viewCommitTimer.current);
    }
    viewCommitTimer.current = window.setTimeout(() => {
      updateParams("view", String(displayLimit));
      viewCommitTimer.current = null;
    }, 250);
    return () => {
      if (viewCommitTimer.current !== null) {
        window.clearTimeout(viewCommitTimer.current);
        viewCommitTimer.current = null;
      }
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [displayLimit]);

  const displayedRows = filteredRows.slice(0, displayLimit);
  const hasMoreDisplay = displayLimit < filteredRows.length;

  // Scroll the expanded row into view when its id is set via URL.
  const lastExpandedRef = useRef<string | null>(null);
  useEffect(() => {
    if (!expandedId || expandedId === lastExpandedRef.current) return;
    lastExpandedRef.current = expandedId;
    setTimeout(() => {
      const el = document.getElementById(`log-${expandedId}`);
      if (el) el.scrollIntoView({ behavior: "smooth", block: "center" });
    }, 100);
  }, [expandedId]);

  const [psmOpen, setPsmOpen] = useState(false);
  // Track the service blur timer so we can cancel it on unmount / re-focus
  // and avoid setState on an unmounted component.
  const psmBlurTimer = useRef<number | null>(null);
  useEffect(() => {
    return () => {
      if (psmBlurTimer.current !== null) {
        window.clearTimeout(psmBlurTimer.current);
      }
    };
  }, []);
  const schedulePsmClose = () => {
    if (psmBlurTimer.current !== null) {
      window.clearTimeout(psmBlurTimer.current);
    }
    psmBlurTimer.current = window.setTimeout(() => {
      setPsmOpen(false);
      psmBlurTimer.current = null;
    }, 200);
  };
  const cancelPsmClose = () => {
    if (psmBlurTimer.current !== null) {
      window.clearTimeout(psmBlurTimer.current);
      psmBlurTimer.current = null;
    }
  };

  // Pending state for the client-side "View More" button so we can
  // disable rapid double-clicks. (React's setState batching already
  // prevents double-increment within a single click, but the user can
  // click again before the slice render lands.)
  const [viewLoading, setViewLoading] = useState(false);
  useEffect(() => {
    if (!viewLoading) return;
    if (displayedRows.length >= displayLimit) setViewLoading(false);
  }, [displayedRows.length, displayLimit, viewLoading]);

  const onChangeTimePreset = (next: string) => {
    const sp = new URLSearchParams(searchParams);
    if (next === "custom") {
      // Custom range — drop any preset-derived from/to so the user can
      // type their own window. We don't currently expose from/to inputs
      // in the UI; treat "custom" as "no window" (i.e. server defaults).
      sp.delete("from");
      sp.delete("to");
    } else {
      const { from, to } = windowFromPreset(next);
      if (from) sp.set("from", from);
      else sp.delete("from");
      if (to) sp.set("to", to);
      else sp.delete("to");
    }
    setSearchParams(sp, { replace: true });
  };

  return (
    <div className="flex flex-col h-full bg-white border border-slate-200 rounded-lg shadow-sm overflow-hidden relative">
      <div className="absolute top-0 left-0 w-full h-1 bg-gradient-to-r from-blue-500 to-cyan-400" />

      {/* Header & Filters */}
      <div className="p-4 border-b border-slate-200 bg-slate-50/50 flex flex-col gap-4 mt-1">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2 text-slate-800">
            <Terminal className="w-5 h-5 text-blue-600" />
            <h2 className="font-mono font-semibold tracking-wider">ARGOS LOGS</h2>
          </div>
          <div className="flex items-center gap-3">
            <div className="text-xs font-mono font-medium text-slate-500 bg-white px-2 py-1 rounded-md border border-slate-200 shadow-sm">
              {enabled
                ? query.isFetching
                  ? "LOADING…"
                  : levelFilter
                    ? `${filteredRows.length} OF ${rows.length} MATCH${
                        filteredRows.length === 1 ? "" : "ES"
                      }`
                    : `${filteredRows.length} MATCH${
                        filteredRows.length === 1 ? "" : "ES"
                      }`
                : "IDLE"}
            </div>
          </div>
        </div>

        <div className="grid grid-cols-1 md:grid-cols-4 gap-3">
          {/* Service (formerly PSM) with suggestions */}
          <div className="relative">
            <Input
              icon={<Terminal className="w-4 h-4" />}
              placeholder="Search or type service..."
              value={psmFilter === "all" ? "" : psmFilter}
              onChange={(e) => updateParams("psm", e.target.value)}
              onFocus={() => {
                cancelPsmClose();
                setPsmOpen(true);
              }}
              onBlur={schedulePsmClose}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  cancelPsmClose();
                  setPsmOpen(false);
                }
              }}
              showClear
              onClear={() => {
                cancelPsmClose();
                setPsmOpen(false);
                updateParams("psm", "");
              }}
              className="font-mono text-sm"
            />
            {psmOpen ? (
              <div className="absolute top-full left-0 w-full mt-1 bg-white border border-slate-200 rounded-md shadow-lg max-h-60 overflow-y-auto z-20 font-mono text-sm ring-1 ring-slate-900/5">
                {!psmFilter && projectOptions.length > 0 ? (
                  <div className="px-3 py-1.5 text-[10px] uppercase tracking-wider text-slate-400 bg-slate-50 border-b border-slate-100 font-bold sticky top-0">
                    Suggested services
                  </div>
                ) : null}
                {projectOptions
                  .filter((p) => !psmFilter || p.toLowerCase().includes(psmFilter.toLowerCase()))
                  .map((p) => (
                    <div
                      key={p}
                      className="px-3 py-2 cursor-pointer hover:bg-blue-50 hover:text-blue-700 text-slate-700 transition-colors"
                      onMouseDown={(e) => {
                        e.preventDefault();
                        updateParams("psm", p);
                        cancelPsmClose();
                        setPsmOpen(false);
                      }}
                    >
                      {p}
                    </div>
                  ))}
                {psmFilter &&
                !projectOptions.some((p) => p.toLowerCase() === psmFilter.toLowerCase()) ? (
                  <div className="px-3 py-2 text-slate-500 text-xs italic bg-slate-50 border-t border-slate-100">
                    Press Enter to use "{psmFilter}"
                  </div>
                ) : null}
              </div>
            ) : null}
          </div>

          {/* Log ID */}
          <Input
            icon={<Search className="w-4 h-4" />}
            placeholder="Search Log ID..."
            value={logIdFilter}
            onChange={(e) => updateParams("logId", e.target.value)}
            showClear
            onClear={() => updateParams("logId", "")}
            className="font-mono text-sm"
          />

          {/* Level */}
          <Select
            value={levelFilter}
            onChange={(e) => updateParams("level", e.target.value)}
            options={[
              { label: "All Levels", value: "" },
              { label: "DEBUG", value: "DEBUG" },
              { label: "INFO", value: "INFO" },
              { label: "WARN", value: "WARN" },
              { label: "ERROR", value: "ERROR" },
              { label: "FATAL", value: "FATAL" },
            ]}
          />

          {/* Time range */}
          <Select
            value={timeRange}
            onChange={(e) => onChangeTimePreset(e.target.value)}
            options={[
              { label: "Last 15 minutes", value: "15m" },
              { label: "Last 1 hour", value: "1h" },
              { label: "Last 6 hours", value: "6h" },
              { label: "Last 24 hours", value: "24h" },
              { label: "Last 72 hours", value: "72h" },
              { label: "Custom Range...", value: "custom" },
            ]}
          />
        </div>
      </div>

      {/* Body */}
      {query.isError ? (
        <div className="flex items-start gap-3 border-b border-slate-200 bg-red-50 px-4 py-3 text-sm text-red-800">
          <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
          <div className="flex-1">
            <p className="font-medium">Something went wrong</p>
            <p className="mt-0.5 break-words text-red-700">
              {extractErrorMessage(query.error)}
            </p>
          </div>
          <button
            type="button"
            onClick={() => query.refetch()}
            className="rounded border border-red-300 px-2 py-1 text-xs font-medium text-red-800 hover:bg-red-100"
          >
            Retry
          </button>
        </div>
      ) : null}

      {/* Log Stream */}
      <div className="flex-1 overflow-auto bg-white relative">
        {!enabled ? (
          <div className="py-24 text-center text-slate-500 font-sans flex flex-col items-center justify-center">
            <Terminal className="w-12 h-12 mb-4 text-slate-300" />
            <p className="text-lg font-medium text-slate-700">No filters set</p>
            <p className="text-sm mt-1">Type a service or Log ID to start.</p>
          </div>
        ) : filteredRows.length > 0 ? (
          <div className="min-w-[800px] flex flex-col font-mono text-sm divide-y divide-slate-100 pb-8">
            {displayedRows.map((row, index) => {
              const isExpanded = expandedId === row.logid;
              const timeStr = shortTime(row.timestamp);
              const detailsJson = syntheticPayload(row);
              return (
                <div
                  key={rowKey(row, index)}
                  id={`log-${row.logid}`}
                  className={cn(
                    "group flex flex-col hover:bg-slate-50 transition-colors cursor-pointer py-3 px-4",
                    isExpanded ? "bg-slate-50" : "",
                  )}
                  onClick={() =>
                    updateParams("expandedId", isExpanded ? "" : row.logid)
                  }
                >
                  {/* Header row */}
                  <div className="flex items-start gap-4">
                    <div className="flex items-center gap-3 w-44 flex-shrink-0 pt-0.5">
                      <span className="text-slate-400 text-xs leading-tight">
                        <span className="block">{timeStr.date}</span>
                        <span className="block">{timeStr.time}</span>
                      </span>
                      <LevelBadge level={row.level} />
                    </div>
                    <div className="flex-1 min-w-0 flex flex-col gap-1.5">
                      <div className="flex items-center gap-3 text-xs text-slate-500 overflow-hidden">
                        <span
                          onClick={(e) => {
                            e.stopPropagation();
                            updateParams("psm", row.project);
                          }}
                          className="font-semibold text-blue-600 flex items-center gap-1.5 bg-blue-50 hover:bg-blue-100 hover:border-blue-300 cursor-pointer transition-colors px-2 py-0.5 rounded border border-blue-100"
                          title="Filter by this service"
                        >
                          <Terminal className="w-3 h-3" /> {row.project}
                        </span>
                        {row.caller ? (
                          <span className="flex items-center gap-1.5 text-slate-600 truncate bg-slate-100 px-2 py-0.5 rounded border border-slate-200">
                            <FileCode2 className="w-3 h-3" /> {row.caller}
                          </span>
                        ) : null}
                        <span
                          onClick={(e) => {
                            e.stopPropagation();
                            updateParams("logId", row.logid);
                          }}
                          className="flex items-center gap-1.5 text-slate-500 bg-slate-50 hover:bg-blue-50 hover:text-blue-600 hover:border-blue-200 cursor-pointer transition-colors px-2 py-0.5 rounded border border-slate-200"
                          title="Search for this Log ID"
                        >
                          <Fingerprint className="w-3 h-3" /> {row.logid}
                        </span>
                      </div>
                      <div
                        className={cn(
                          "text-slate-800 pr-8 mt-1",
                          !isExpanded
                            ? "truncate"
                            : "whitespace-normal break-words leading-relaxed",
                        )}
                      >
                        {row.message}
                      </div>
                    </div>
                    <div className="flex-shrink-0 pt-1 text-slate-300 group-hover:text-blue-500 transition-colors">
                      {isExpanded ? (
                        <ChevronDown className="w-5 h-5" />
                      ) : (
                        <ChevronRight className="w-5 h-5" />
                      )}
                    </div>
                  </div>

                  <AnimatePresence>
                    {isExpanded ? (
                      <motion.div
                        initial={{ height: 0, opacity: 0 }}
                        animate={{ height: "auto", opacity: 1 }}
                        exit={{ height: 0, opacity: 0 }}
                        className="overflow-hidden"
                      >
                        <div
                          className="pl-48 pr-12 pt-4 pb-4"
                          onClick={(e) => e.stopPropagation()}
                        >
                          <div className="flex flex-col xl:flex-row gap-4">
                            <div className="xl:w-1/3 min-w-[200px] flex flex-col gap-4 bg-slate-50 border border-slate-200 rounded-lg p-4 shadow-sm">
                              <div>
                                <div className="text-[10px] text-slate-400 font-bold uppercase tracking-wider mb-1">
                                  Log ID
                                </div>
                                <div
                                  onClick={() => updateParams("logId", row.logid)}
                                  className="text-xs font-mono text-slate-700 bg-white hover:bg-blue-50 hover:text-blue-700 hover:border-blue-200 cursor-pointer transition-colors border border-slate-200 px-2 py-1.5 rounded truncate"
                                  title="Search for this Log ID"
                                >
                                  {row.logid}
                                </div>
                              </div>
                              <div>
                                <div className="text-[10px] text-slate-400 font-bold uppercase tracking-wider mb-1">
                                  Source File
                                </div>
                                <div className="text-xs font-mono text-slate-700 bg-white border border-slate-200 px-2 py-1.5 rounded truncate">
                                  {row.caller || "—"}
                                </div>
                              </div>
                              <div>
                                <div className="text-[10px] text-slate-400 font-bold uppercase tracking-wider mb-1">
                                  Timestamp
                                </div>
                                <div className="text-xs font-mono text-slate-700 bg-white border border-slate-200 px-2 py-1.5 rounded truncate">
                                  {row.timestamp}
                                </div>
                              </div>
                            </div>
                            <div className="flex-1 bg-slate-900 rounded-lg border border-slate-800 relative shadow-inner flex flex-col max-h-64">
                              <div className="absolute top-0 left-0 w-1 h-full bg-blue-500 rounded-l-lg" />
                              <div className="text-slate-400 text-[10px] uppercase tracking-wider px-4 py-3 border-b border-slate-800 flex items-center gap-2 flex-shrink-0 bg-slate-900/50">
                                <FileCode2 className="w-3 h-3" /> Full Payload
                              </div>
                              <div className="p-4 overflow-y-auto flex-1 [&::-webkit-scrollbar-track]:bg-slate-900 [&::-webkit-scrollbar-thumb]:bg-slate-700 [&::-webkit-scrollbar-thumb:hover]:bg-slate-600">
                                <JsonHighlighter
                                  code={detailsJson}
                                  className="text-slate-300 text-xs"
                                />
                              </div>
                            </div>
                          </div>
                        </div>
                      </motion.div>
                    ) : null}
                  </AnimatePresence>
                </div>
              );
            })}
            {hasMoreDisplay ? (
              <div className="py-6 flex justify-center">
                <button
                  onClick={() => {
                    if (viewLoading) return;
                    setViewLoading(true);
                    setDisplayLimit((prev) => prev + DEFAULT_VIEW);
                  }}
                  disabled={viewLoading}
                  className="px-6 py-2 bg-slate-100 hover:bg-slate-200 text-slate-700 text-sm font-medium rounded-full shadow-sm transition-colors border border-slate-200 flex items-center gap-2 font-sans disabled:opacity-50"
                >
                  <ChevronDown className="w-4 h-4" />
                  View More Logs
                </button>
              </div>
            ) : null}
            {hasMore && !hasMoreDisplay ? (
              <div className="py-4 flex justify-center">
                <button
                  onClick={loadMore}
                  disabled={query.isFetching}
                  className="px-6 py-2 bg-white hover:bg-slate-50 text-slate-700 text-sm font-medium rounded-full shadow-sm transition-colors border border-slate-200 flex items-center gap-2 font-sans disabled:opacity-50"
                >
                  <ChevronDown className="w-4 h-4" />
                  {query.isFetching ? "Loading…" : `Load more from server (${rows.length} of ${totalCount})`}
                </button>
              </div>
            ) : null}
          </div>
        ) : (
          <div className="py-24 text-center text-slate-500 font-sans flex flex-col items-center justify-center">
            <Terminal className="w-12 h-12 mb-4 text-slate-300" />
            <p className="text-lg font-medium text-slate-700">No logs found</p>
            <p className="text-sm mt-1">Try adjusting your filters or search query.</p>
          </div>
        )}
      </div>

      {/* Footer status / reset */}
      {enabled ? (
        <div className="border-t border-slate-200 bg-slate-50/50 px-4 py-2 flex items-center justify-between text-xs text-slate-500">
          <span>
            {query.isFetching
              ? "Loading…"
              : query.isError
                ? "Query failed"
                : levelFilter
                  ? (
                    <>
                      Showing <strong className="text-slate-900">{filteredRows.length}</strong>{" "}
                      of <strong className="text-slate-900">{rows.length}</strong> loaded
                      (filtered; <strong className="text-slate-900">{totalCount}</strong> server total)
                    </>
                  )
                  : (
                    <>
                      Showing <strong className="text-slate-900">{filteredRows.length}</strong> of{" "}
                      <strong className="text-slate-900">{totalCount}</strong> result
                      {totalCount === 1 ? "" : "s"}
                    </>
                  )}
          </span>
          <button
            onClick={() => {
              reset();
              setSearchParams(new URLSearchParams(), { replace: true });
            }}
            className="text-xs text-slate-500 hover:text-slate-800"
          >
            Reset filters
          </button>
        </div>
      ) : null}
    </div>
  );
}