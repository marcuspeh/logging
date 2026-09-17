import { RefreshCw } from "lucide-react";
import { FilterPanel } from "../components/FilterPanel";
import { ResultsTable } from "../components/ResultsTable";
import { EmptyState } from "../components/EmptyState";
import { ErrorBanner } from "../components/ErrorBanner";
import { useSearch } from "../hooks/useSearch";
import { extractErrorMessage } from "../api/client";

// QueryPage is the only screen for now. It composes the filter panel
// and results table around the URL-synced useSearch hook.
export function QueryPage() {
  const { params, setParams, setDebounced, reset, query, enabled } = useSearch();
  const rows = query.data?.results ?? [];
  const count = query.data?.count ?? 0;

  // Search button is a no-op for now (filters are reactive), but having
  // it means the UI matches the original plan and is keyboard-friendly.
  const onSubmit = () => {
    /* queries are auto-driven by useSearch; explicit submit kept for a11y */
  };

  return (
    <div className="grid grid-cols-1 gap-6 lg:grid-cols-[280px_1fr]">
      <aside>
        <FilterPanel
          params={params}
          onChange={setParams}
          onDebounced={setDebounced}
          onReset={reset}
          onSubmit={onSubmit}
          isFetching={query.isFetching}
          enabled={enabled}
        />
      </aside>

      <section className="flex flex-col gap-3">
        <header className="flex items-center justify-between">
          <div className="text-sm text-slate-600">
            {enabled ? (
              query.isFetching ? (
                <span>Loading…</span>
              ) : query.isError ? (
                <span className="text-red-700">Query failed</span>
              ) : (
                <span>
                  <strong className="text-slate-900">{count}</strong> result
                  {count === 1 ? "" : "s"}
                </span>
              )
            ) : (
              <span className="text-slate-500">Enter a filter to search.</span>
            )}
          </div>
          {enabled ? (
            <button
              type="button"
              onClick={() => query.refetch()}
              className="inline-flex items-center gap-1.5 rounded border border-slate-300 bg-white px-2 py-1 text-xs font-medium text-slate-700 hover:bg-slate-100 disabled:opacity-50"
              disabled={query.isFetching}
              title="Re-run this query"
            >
              <RefreshCw
                className={`h-3.5 w-3.5 ${query.isFetching ? "animate-spin" : ""}`}
              />
              Refresh
            </button>
          ) : null}
        </header>

        {query.isError ? (
          <ErrorBanner
            message={extractErrorMessage(query.error)}
            onRetry={() => query.refetch()}
          />
        ) : null}

        {enabled && !query.isError && rows.length === 0 && !query.isFetching ? (
          <EmptyState
            title="No matching log rows"
            hint="Try widening the time range, removing the level filter, or checking the project / logid spelling."
          />
        ) : null}

        {enabled && rows.length > 0 ? (
          <ResultsTable
            rows={rows}
            onPickLogId={(logid) => setParams({ ...params, logid })}
          />
        ) : null}

        {!enabled ? (
          <EmptyState
            title="Start by filtering"
            hint="Provide a project name or a log id to query the backend."
          />
        ) : null}
      </section>
    </div>
  );
}
