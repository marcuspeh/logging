import { Search, X } from "lucide-react";
import clsx from "clsx";
import { LEVELS, type Level, type QueryParams } from "../api/types";
import { levelClasses } from "../utils/format";
import { TimeRangePicker } from "./TimeRangePicker";

interface Props {
  params: QueryParams;
  onChange: (next: QueryParams) => void;
  onDebounced: (patch: Partial<QueryParams>) => void;
  onReset: () => void;
  onSubmit: () => void;
  isFetching: boolean;
  enabled: boolean;
}

// FilterPanel owns the form chrome for the query. It applies text-input
// changes via onDebounced (debounced in useSearch) and structural
// changes immediately via onChange.
export function FilterPanel({
  params,
  onChange,
  onDebounced,
  onReset,
  onSubmit,
  isFetching,
  enabled,
}: Props) {
  const toggleLevel = (lvl: Level) => {
    onChange({ ...params, level: params.level === lvl ? undefined : lvl });
  };

  return (
    <div className="space-y-4 rounded border border-slate-200 bg-white p-4">
      <div className="space-y-1">
        <label className="text-xs font-medium text-slate-600" htmlFor="f-project">
          Project
        </label>
        <input
          id="f-project"
          type="text"
          autoComplete="off"
          placeholder="e.g. billing-service"
          value={params.project ?? ""}
          onChange={(e) => onDebounced({ project: e.target.value || undefined })}
          className="w-full rounded border border-slate-300 bg-white px-2 py-1.5 text-sm focus:border-slate-500 focus:outline-none"
        />
      </div>

      <div className="space-y-1">
        <label className="text-xs font-medium text-slate-600" htmlFor="f-logid">
          Log ID
        </label>
        <input
          id="f-logid"
          type="text"
          autoComplete="off"
          placeholder="correlation id"
          value={params.logid ?? ""}
          onChange={(e) => onDebounced({ logid: e.target.value || undefined })}
          className="w-full rounded border border-slate-300 bg-white px-2 py-1.5 text-sm focus:border-slate-500 focus:outline-none"
        />
      </div>

      <div className="space-y-1">
        <span className="text-xs font-medium text-slate-600">Level</span>
        <div className="flex flex-wrap gap-1">
          {LEVELS.map((lvl) => {
            const active = params.level === lvl;
            return (
              <button
                key={lvl}
                type="button"
                onClick={() => toggleLevel(lvl)}
                className={clsx(
                  "rounded border px-2 py-1 font-mono text-xs",
                  active
                    ? levelClasses(lvl)
                    : "border-slate-300 bg-white text-slate-600 hover:bg-slate-100",
                )}
              >
                {lvl}
              </button>
            );
          })}
        </div>
      </div>

      <div className="space-y-2">
        <span className="text-xs font-medium text-slate-600">Time range</span>
        <TimeRangePicker
          from={params.from ?? ""}
          to={params.to ?? ""}
          onChange={({ from, to }) =>
            onChange({
              ...params,
              from: from || undefined,
              to: to || undefined,
            })
          }
        />
      </div>

      <div className="grid grid-cols-2 gap-2">
        <label className="space-y-1 text-xs text-slate-600">
          <span>Limit</span>
          <input
            type="number"
            min={1}
            max={1000}
            value={params.limit ?? 100}
            onChange={(e) =>
              onChange({
                ...params,
                limit: e.target.value ? Number(e.target.value) : undefined,
              })
            }
            className="w-full rounded border border-slate-300 bg-white px-2 py-1.5 text-sm"
          />
        </label>
        <label className="space-y-1 text-xs text-slate-600">
          <span>Order</span>
          <select
            value={params.order ?? "desc"}
            onChange={(e) =>
              onChange({
                ...params,
                order: e.target.value as "asc" | "desc",
              })
            }
            className="w-full rounded border border-slate-300 bg-white px-2 py-1.5 text-sm"
          >
            <option value="desc">Newest first</option>
            <option value="asc">Oldest first</option>
          </select>
        </label>
      </div>

      {!enabled ? (
        <p className="rounded border border-amber-200 bg-amber-50 px-2 py-1.5 text-xs text-amber-800">
          Set <span className="font-mono">project</span> or{" "}
          <span className="font-mono">logid</span> to search.
        </p>
      ) : null}

      <div className="flex gap-2">
        <button
          type="button"
          onClick={onSubmit}
          disabled={!enabled || isFetching}
          className="inline-flex flex-1 items-center justify-center gap-1.5 rounded bg-slate-900 px-3 py-1.5 text-sm font-medium text-white hover:bg-slate-800 disabled:cursor-not-allowed disabled:bg-slate-400"
        >
          <Search className="h-4 w-4" />
          Search
        </button>
        <button
          type="button"
          onClick={onReset}
          className="inline-flex items-center gap-1.5 rounded border border-slate-300 bg-white px-3 py-1.5 text-sm font-medium text-slate-700 hover:bg-slate-100"
        >
          <X className="h-4 w-4" />
          Reset
        </button>
      </div>
    </div>
  );
}
