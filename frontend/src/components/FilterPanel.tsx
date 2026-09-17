import { Search, X } from "lucide-react";
import clsx from "clsx";
import type { FormEvent, KeyboardEvent } from "react";
import { LEVELS, type Level, type QueryParams } from "../api/types";
import { levelClasses } from "../utils/format";
import { TimeRangePicker } from "./TimeRangePicker";

interface Props {
  draft: QueryParams;
  params: QueryParams;
  onUpdateDraft: (patch: Partial<QueryParams>) => void;
  onApply: (patch: Partial<QueryParams>) => void;
  onReset: () => void;
  onCommit: () => void;
  isFetching: boolean;
  enabled: boolean;
}

// FilterPanel owns the form chrome for the query. Free-text fields
// (project, logid) only update the draft until the user presses Enter
// or clicks Search; structural filters (level, time, limit, order) apply
// immediately so picking a level chip already narrows the results.
export function FilterPanel({
  draft,
  onUpdateDraft,
  onApply,
  onReset,
  onCommit,
  isFetching,
  enabled,
}: Props) {
  const toggleLevel = (lvl: Level) => {
    onApply({ level: draft.level === lvl ? undefined : lvl });
  };

  const handleEnter = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter") {
      e.preventDefault();
      onCommit();
    }
  };

  const handleSubmit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    onCommit();
  };

  return (
    <form className="space-y-4 rounded border border-slate-200 bg-white p-4" onSubmit={handleSubmit}>
      <div className="space-y-1">
        <label className="text-xs font-medium text-slate-600" htmlFor="f-project">
          Project
        </label>
        <input
          id="f-project"
          type="text"
          autoComplete="off"
          placeholder="e.g. billing-service"
          value={draft.project ?? ""}
          onChange={(e) => onUpdateDraft({ project: e.target.value || undefined })}
          onKeyDown={handleEnter}
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
          value={draft.logid ?? ""}
          onChange={(e) => onUpdateDraft({ logid: e.target.value || undefined })}
          onKeyDown={handleEnter}
          className="w-full rounded border border-slate-300 bg-white px-2 py-1.5 text-sm focus:border-slate-500 focus:outline-none"
        />
      </div>

      <div className="space-y-1">
        <span className="text-xs font-medium text-slate-600">Level</span>
        <div className="flex flex-wrap gap-1">
          {LEVELS.map((lvl) => {
            const active = draft.level === lvl;
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
          from={draft.from ?? ""}
          to={draft.to ?? ""}
          onChange={({ from, to }) =>
            onApply({
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
            value={draft.limit ?? 100}
            onChange={(e) =>
              onApply({
                limit: e.target.value ? Number(e.target.value) : undefined,
              })
            }
            className="w-full rounded border border-slate-300 bg-white px-2 py-1.5 text-sm"
          />
        </label>
        <label className="space-y-1 text-xs text-slate-600">
          <span>Order</span>
          <select
            value={draft.order ?? "desc"}
            onChange={(e) =>
              onApply({
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
          <span className="font-mono">logid</span> and press{" "}
          <kbd className="rounded border border-amber-300 bg-white px-1 font-mono text-[10px]">
            Enter
          </kbd>{" "}
          or click Search.
        </p>
      ) : null}

      <div className="flex gap-2">
        <button
          type="submit"
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
    </form>
  );
}
