import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { searchLogs } from "../api/logs";
import type { LogQueryResponse, QueryParams } from "../api/types";
import { fromSearchParams, toSearchParams } from "../utils/url";

// useSearch keeps QueryParams as the single source of truth, syncs them
// to the URL (so views are shareable + back-button works), and exposes a
// TanStack Query bound to those params.
export function useSearch() {
  // Hydrate initial params from the URL once.
  const initial = useMemo(() => fromSearchParams(window.location.search), []);
  const [params, setParams] = useState<QueryParams>(initial);

  // Reflect params into the URL whenever they change. We use replaceState
  // (not push) for in-flight typing so the back button isn't polluted.
  useEffect(() => {
    const sp = toSearchParams(params);
    const qs = sp.toString();
    const url = qs ? `?${qs}` : window.location.pathname;
    window.history.replaceState(null, "", url);
  }, [params]);

  // Fetch only when the backend will accept the query: either project
  // or logid must be set (matches parseQuery in server.go).
  const enabled = Boolean(params.project || params.logid);

  const query = useQuery<LogQueryResponse>({
    queryKey: ["search", params],
    queryFn: () => searchLogs(params),
    enabled,
    staleTime: 10_000,
    retry: 1,
  });

  // Debounced setter for free-text fields (logid). Cancels any pending
  // update on rapid re-entry so only the latest value is applied.
  const pendingRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const setDebounced = useCallback(
    (patch: Partial<QueryParams>, delayMs = 250) => {
      if (pendingRef.current) clearTimeout(pendingRef.current);
      pendingRef.current = setTimeout(() => {
        pendingRef.current = null;
        setParams((prev: QueryParams) => ({ ...prev, ...patch }));
      }, delayMs);
    },
    [],
  );

  const reset = useCallback(() => setParams({}), []);

  return { params, setParams, setDebounced, reset, query, enabled };
}
