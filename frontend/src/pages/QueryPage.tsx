import { ChevronDown, RefreshCw } from "lucide-react";
import { FilterPanel } from "../components/FilterPanel";
import { ResultsTable } from "../components/ResultsTable";
import { EmptyState } from "../components/EmptyState";
import { ErrorBanner } from "../components/ErrorBanner";
import { useSearch } from "../hooks/useSearch";
import { extractErrorMessage } from "../api/client";

// QueryPage is the only screen for now. It composes the filter panel
// and results table around the URL-synced useSearch hook. Free-text
// fields (project, logid) only commit on Enter / Search; structural
// filters apply immediately.
//
// Pagination:
//   - First fetch returns 200 rows.
//   - "Load 200 more" button at the bottom of the results appends the
//     next page. Disabled when we've loaded every matching row.
export function QueryPage() {
  const {
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
  } = useSearch();

  // Click-to-filter on a result row's logid: stamp both draft and params
  // immediately so the filtered view renders without needing Enter.
  const onPickLogId = (logid: string) => {
    apply({ ...draft, logid });
  };

  return (
    <div className="grid grid-cols-1 gap-6 lg:grid-cols-[280px_1fr]">
      <aside>
        <FilterPanel
          draft={draft}
          params={params}
          onUpdateDraft={updateDraft}
          onApply={apply}
          onReset={reset}
          onCommit={commit}
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
              ) : totalCount > 0 ? (
                <span>
                  Showing <strong className="text-slate-900">{rows.length}</strong> of{" "}
                  <strong className="text-slate-900">{totalCount}</strong> result
                  {totalCount === 1 ? "" : "s"}
                </span>
              ) : (
                <span>
                  <strong className="text-slate-900">0</strong> results
                </span>
              )
            ) : (
              <span className="text-slate-500">Enter a filter and press Enter to search.</span>
            )}
          </div>
          {enabled ? (
            <button
              type="button"
              onClick={() => query.refetch()}
              className="inline-flex items-center gap-1.5 rounded border border-slate-300 bg-white px-2 py-1 text-xs font-medium text-slate-700 hover:bg-slate-100 disabled:opacity-50"
              disabled={query.isFetching}
              title="Re-run this query from the first page"
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
          <ResultsTable rows={rows} onPickLogId={onPickLogId} />
        ) : null}

        {enabled && rows.length > 0 && hasMore ? (
          <div className="flex justify-center">
            <button
              type="button"
              onClick={loadMore}
              disabled={query.isFetching}
              className="inline-flex items-center gap-2 rounded border border-slate-300 bg-white px-4 py-2 text-sm font-medium text-slate-700 hover:bg-slate-100 disabled:opacity-50"
            >
              <ChevronDown className="h-4 w-4" />
              {query.isFetching
                ? "Loading…"
                : `Load 200 more (${rows.length} of ${totalCount})`}
            </button>
          </div>
        ) : null}

        {!enabled ? (
          <EmptyState
            title="Start by filtering"
            hint="Type a project name or a log id, then press Enter."
          />
        ) : null}
      </section>
    </div>
  );
}
