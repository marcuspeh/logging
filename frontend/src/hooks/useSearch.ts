import { useCallback, useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { searchLogs } from "../api/logs";
import type { LogQueryResponse, LogRow, QueryParams } from "../api/types";
import { fromSearchParams, toSearchParams } from "../utils/url";

// Initial page size for the first request and every "Load more" click.
const PAGE_SIZE = 200;

// useSearch keeps QueryParams as the single source of truth for the
// *executed* query, syncs them to the URL (so views are shareable +
// back-button works), and exposes a TanStack Query bound to those
// params. Free-text fields (project, logid) live in a separate `draft`
// so the query only fires when the user commits via Enter / Search.
//
// Pagination model:
//   - The first fetch is `limit=PAGE_SIZE, offset=0`.
//   - "Load more" calls refetch with `offset += PAGE_SIZE`; the new page
//     is APPENDED to the existing rows, not replaced.
//   - Any filter change resets offset to 0 and clears the accumulator.
//   - `totalCount` is the backend's reported total (Response.Count).
//     `loadedCount` is what we've rendered so far. The UI uses these to
//     show "Showing N of M" + a "Load more" button when loaded < total.
export function useSearch() {
  // Hydrate initial params from the URL once.
  const initial = useMemo(() => {
    const parsed = fromSearchParams(window.location.search);
    return { ...parsed, limit: PAGE_SIZE, offset: 0 };
  }, []);

  // params: what the next query will run against (offset bumps on load more).
  // draft: what the inputs currently show.
  const [params, setParams] = useState<QueryParams>(initial);
  const [draft, setDraft] = useState<QueryParams>(initial);

  // Accumulated rows across pages of the same filter set. Reset on
  // filter change (see replaceParams below).
  const [rows, setRows] = useState<LogRow[]>([]);
  const [totalCount, setTotalCount] = useState(0);
  // Filter signature: changes when any committed filter changes.
  // Used to decide whether a new fetch should replace or append.
  const [filterSig, setFilterSig] = useState<string>(signature(initial));

  // Reflect params into the URL whenever they change. Use replaceState
  // so the back button isn't polluted by every keystroke / load-more.
  useEffect(() => {
    const sp = toSearchParams(params);
    const qs = sp.toString();
    const url = qs ? `?${qs}` : window.location.pathname;
    window.history.replaceState(null, "", url);
  }, [params]);

  const enabled = Boolean(params.project || params.logid);

  const query = useQuery<LogQueryResponse>({
    queryKey: ["search", params],
    queryFn: () => searchLogs(params),
    enabled,
    staleTime: 10_000,
    retry: 1,
  });

  // On each successful fetch, decide whether to append or replace based
  // on whether the filter signature has changed since the last fetch.
  useEffect(() => {
    if (!query.data) return;
    const incomingSig = signature(params);
    if (incomingSig !== filterSig) {
      // Filter changed: reset accumulation.
      setRows(query.data.results);
      setFilterSig(incomingSig);
    } else {
      // Same filter, possibly a new page: append.
      setRows((prev) => mergeUnique(prev, query.data!.results));
    }
    setTotalCount(query.data.count);
  }, [query.data, params, filterSig]);

  const commit = useCallback(() => setParams(draft), [draft]);

  const apply = useCallback((patch: Partial<QueryParams>) => {
    setDraft((prev) => ({ ...prev, ...patch }));
    setParams((prev) => ({ ...prev, ...patch }));
  }, []);

  const updateDraft = useCallback((patch: Partial<QueryParams>) => {
    setDraft((prev) => ({ ...prev, ...patch }));
  }, []);

  // Load the next page: bump offset, keep everything else. The useEffect
  // above detects filterSig unchanged and appends the new rows.
  const loadMore = useCallback(() => {
    setParams((prev) => ({
      ...prev,
      offset: (prev.offset ?? 0) + PAGE_SIZE,
    }));
  }, []);

  const reset = useCallback(() => {
    setParams({ limit: PAGE_SIZE, offset: 0 });
    setDraft({ limit: PAGE_SIZE, offset: 0 });
    setRows([]);
    setTotalCount(0);
  }, []);

  const hasMore = rows.length < totalCount;

  return {
    draft,
    params,
    commit,
    apply,
    updateDraft,
    reset,
    loadMore,
    query,
    enabled,
    rows,
    totalCount,
    hasMore,
    pageSize: PAGE_SIZE,
  };
}

// Signature of the user-visible filter (everything except offset / limit).
// Used to decide whether a new fetch is a "new query" (replace) or a
// "load more" (append).
function signature(p: QueryParams): string {
  const { project, logid, level, from, to, order } = p;
  return JSON.stringify({ project, logid, level, from, to, order });
}

// Append incoming rows, skipping any we've already shown (same timestamp
// + project + logid + message = same row). Dedup is defensive — backend
// shouldn't return duplicates across pages, but this guards against race
// conditions and keeps the rendered list stable.
function mergeUnique(existing: LogRow[], incoming: LogRow[]): LogRow[] {
  const seen = new Set(existing.map(rowKey));
  const out = existing.slice();
  for (const r of incoming) {
    const k = rowKey(r);
    if (!seen.has(k)) {
      seen.add(k);
      out.push(r);
    }
  }
  return out;
}

function rowKey(r: LogRow): string {
  return `${r.timestamp}|${r.project}|${r.logid}|${r.message}`;
}
