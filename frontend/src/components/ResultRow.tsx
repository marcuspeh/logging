import { forwardRef, useState } from "react";
import { Copy, Check, ChevronDown } from "lucide-react";
import clsx from "clsx";
import type { LogRow } from "../api/types";
import { LevelBadge } from "./LevelBadge";
import { formatTimestamp } from "../utils/format";

interface Props {
  row: LogRow;
  style?: React.CSSProperties;
  onPickLogId: (logid: string) => void;
}

// Preview length before "Show more" appears. Long enough to fit ~3
// log lines on a narrow phone, short enough that single-line messages
// never trigger the expand toggle.
const PREVIEW_CHARS = 240;

// ResultRow renders a single log row in a virtualized list.
//
// Mobile-safety notes:
//   - The outer div has `overflow-hidden` so a row can never visually
//     bleed into the next slot while the virtualizer is catching up
//     with `measureElement`.
//   - The meta line is split into two predictable rows (timestamp + level
//     + project on top; logid + caller on bottom). Layout height is
//     bounded even when logids are 60+ chars.
//   - Message preview is a JS-truncated string, not CSS line-clamp, so
//     it survives `whitespace-pre-wrap` and any browser quirk.
export const ResultRow = forwardRef<HTMLDivElement, Props>(
  function ResultRow({ row, style, onPickLogId }, ref) {
    const [copied, setCopied] = useState<string | null>(null);
    const [expanded, setExpanded] = useState(false);

    const copy = async (text: string, key: string) => {
      try {
        await navigator.clipboard.writeText(text);
        setCopied(key);
        setTimeout(() => setCopied(null), 1200);
      } catch {
        // Clipboard might be blocked; silently no-op.
      }
    };

    const isLong = row.message.length > PREVIEW_CHARS;
    const display =
      !isLong || expanded
        ? row.message
        : row.message.slice(0, PREVIEW_CHARS).trimEnd() + "…";

    return (
      <div
        ref={ref}
        style={style}
        className="overflow-hidden border-b border-slate-100 px-3 py-2 font-mono text-xs"
      >
        {/* Line 1: timestamp + level + project (always fits on one row). */}
        <div className="flex items-center gap-2">
          <time
            dateTime={row.timestamp}
            title={row.timestamp}
            className="shrink-0 text-slate-500 tabular-nums"
          >
            {formatTimestamp(row.timestamp)}
          </time>
          <LevelBadge level={row.level} />
          <span
            className="min-w-0 flex-1 truncate text-slate-700"
            title={row.project}
          >
            {row.project}
          </span>
        </div>

        {/* Line 2: logid + caller, with its own wrap budget. */}
        <div className="mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-0.5">
          <button
            type="button"
            onClick={() => onPickLogId(row.logid)}
            className="min-w-0 max-w-full truncate text-slate-500 hover:text-slate-900 hover:underline"
            title={`Filter by logid: ${row.logid}`}
          >
            {row.logid}
          </button>
          {row.caller ? (
            <button
              type="button"
              onClick={() => copy(row.caller, "caller")}
              className="inline-flex max-w-full items-center gap-1 truncate text-slate-400 hover:text-slate-700"
              title={`Copy caller (${row.caller})`}
            >
              <span className="truncate">{row.caller}</span>
              {copied === "caller" ? (
                <Check className="h-3 w-3 shrink-0 text-green-600" />
              ) : (
                <Copy className="h-3 w-3 shrink-0" />
              )}
            </button>
          ) : null}
        </div>

        {/* Message. `overflow-wrap-anywhere` breaks unbreakable strings
            (URLs, hex blobs) inside the row's width instead of pushing
            the row wider. */}
        <p
          className="mt-1 whitespace-pre-wrap break-words text-slate-800"
          style={{ overflowWrap: "anywhere" }}
        >
          {display}
        </p>

        {isLong ? (
          <button
            type="button"
            onClick={() => setExpanded((v) => !v)}
            className="mt-1 inline-flex items-center gap-1 text-xs font-medium text-slate-500 hover:text-slate-800"
          >
            <ChevronDown
              className={clsx(
                "h-3 w-3 transition-transform",
                expanded && "rotate-180",
              )}
            />
            {expanded ? "Show less" : `Show more (${row.message.length} chars)`}
          </button>
        ) : null}
      </div>
    );
  },
);
