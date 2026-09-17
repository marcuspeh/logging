import { useCallback, useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { searchLogs } from "../api/logs";
import type { LogQueryResponse, QueryParams } from "../api/types";
import { fromSearchParams, toSearchParams } from "../utils/url";

// useSearch keeps QueryParams as the single source of truth for the
// *executed* query, syncs them to the URL (so views are shareable +
// back-button works), and exposes a TanStack Query bound to those
// params. Free-text fields (project, logid) live in a separate `draft`
// so the query only fires when the user commits via Enter / Search.
export function useSearch() {
  // Hydrate initial params from the URL once.
  const initial = useMemo(() => fromSearchParams(window.location.search), []);

  // draft: what the inputs currently show. params: what the next query
  // will run against. They diverge while the user is typing.
  const [params, setParams] = useState<QueryParams>(initial);
  const [draft, setDraft] = useState<QueryParams>(initial);

  // Reflect params into the URL whenever they change. We use replaceState
  // (not push) so the back button isn't polluted by every keystroke.
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

  // Commit the current draft into params (triggers a fetch if enabled).
  const commit = useCallback(() => setParams(draft), [draft]);

  // Apply a patch to BOTH draft and params — used by non-text controls
  // (level, time range, limit, order) where we want an immediate query.
  const apply = useCallback((patch: Partial<QueryParams>) => {
    setDraft((prev) => ({ ...prev, ...patch }));
    setParams((prev) => ({ ...prev, ...patch }));
  }, []);

  // Update only the draft — used by free-text inputs (project, logid)
  // that fire a query on Enter / Search, not on every keystroke.
  const updateDraft = useCallback((patch: Partial<QueryParams>) => {
    setDraft((prev) => ({ ...prev, ...patch }));
  }, []);

  const reset = useCallback(() => {
    setParams({});
    setDraft({});
  }, []);

  return {
    draft,
    params,
    commit,
    apply,
    updateDraft,
    reset,
    query,
    enabled,
  };
}
