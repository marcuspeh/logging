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

// Max rendered message length before we collapse + show "Show more".
// Long log lines (stack traces, request bodies) don't need to scroll
// 30 lines high on mobile — they just need to be reachable.
const MESSAGE_COLLAPSE_HEIGHT = 2.5; // tailwind line-clamp-N value

// ResultRow renders a single log row in a virtualized list. ref is
// forwarded so the parent's useVirtualizer can measure each row's
// height (collapsed vs expanded messages reflow the scroll height).
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

  return (
    <div
      ref={ref}
      style={style}
      className="border-b border-slate-100 px-3 py-2 font-mono text-xs"
    >
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
        <time
          dateTime={row.timestamp}
          title={row.timestamp}
          className="shrink-0 text-slate-500 tabular-nums"
        >
          {formatTimestamp(row.timestamp)}
        </time>
        <LevelBadge level={row.level} />
        <span
          className="min-w-0 max-w-full truncate text-slate-700"
          title={row.project}
        >
          {row.project}
        </span>
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

      <p
        className={clsx(
          "mt-1 break-words whitespace-pre-wrap text-slate-800",
          !expanded && "overflow-hidden",
        )}
        style={
          !expanded
            ? {
                display: "-webkit-box",
                WebkitLineClamp: MESSAGE_COLLAPSE_HEIGHT,
                WebkitBoxOrient: "vertical",
              }
            : undefined
        }
      >
        {row.message}
      </p>

      {/* "Show more" only renders when the message actually overflows.
          We measure with a sentinel: text length is a cheap heuristic. */}
      {row.message.length > 280 ? (
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
          {expanded ? "Show less" : "Show more"}
        </button>
      ) : null}
    </div>
  );
  },
);
