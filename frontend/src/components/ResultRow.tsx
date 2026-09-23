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

// Initial estimate for the virtualizer. Each row reports its real
// height via ResizeObserver (see ResultsTable) so the actual height
// can grow without clipping when a message is expanded.
//
// Collapsed row: meta header (~36px) + 2-line preview (~36px) +
// button + padding ≈ 110px.
// Expanded row: meta header + full message (no upper bound).
export const ROW_HEIGHT = 110;

// ResultRow renders a single log row in a virtualized list.
//
// Layout:
//   - Meta lines (timestamp/level/project + logid/caller) take 2 rows.
//   - Message is CSS-clamped to 2 lines when collapsed; the clamp is
//     lifted and the row grows when the user clicks "more".
//   - The parent virtualizer measures each row's real height, so
//     expanded rows reflow neighbouring offsets instead of overlapping.
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

    // "Long" = visually long enough to clamp on the narrowest phone.
    // At ~16px wide chars × 2 lines × ~40 cols on a 360px screen, 120
    // chars is roughly the line-clamp threshold; anything past that
    // gets the expand toggle.
    const isLong = row.message.length > 120;

    return (
      <div
        ref={ref}
        style={style}
        className="border-b border-slate-100 px-3 py-2 font-mono text-xs"
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

        {/* Message. Collapsed: clamped to MAX_MESSAGE_LINES with
            ellipsis. Expanded: clamp lifted, row grows past
            ROW_HEIGHT, virtualizer scroll container handles overflow. */}
        <p
          className={clsx(
            "mt-1 overflow-hidden whitespace-pre-wrap break-words text-slate-800",
            !expanded && isLong && "line-clamp-2",
          )}
          style={{ overflowWrap: "anywhere" }}
        >
          {row.message}
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
            {expanded ? "Show less" : "Show full message"}
          </button>
        ) : null}
      </div>
    );
  },
);
