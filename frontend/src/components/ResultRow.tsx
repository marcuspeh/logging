import { useState } from "react";
import { Copy, Check } from "lucide-react";
import clsx from "clsx";
import type { LogRow } from "../api/types";
import { LevelBadge } from "./LevelBadge";
import { formatTimestamp } from "../utils/format";

interface Props {
  row: LogRow;
  style: React.CSSProperties;
  onPickLogId: (logid: string) => void;
}

// ResultRow renders a single log row in a fixed-height virtualized list.
// Clicking logid sets it as the active filter; caller/copy button copies
// the value to the clipboard with a transient confirmation state.
export function ResultRow({ row, style, onPickLogId }: Props) {
  const [copied, setCopied] = useState<string | null>(null);

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
      style={style}
      className="border-b border-slate-100 px-3 py-2 font-mono text-xs"
    >
      <div className="flex items-center gap-2">
        <time
          dateTime={row.timestamp}
          title={row.timestamp}
          className="shrink-0 text-slate-500"
        >
          {formatTimestamp(row.timestamp)}
        </time>
        <LevelBadge level={row.level} />
        <span className="truncate text-slate-700" title={row.project}>
          {row.project}
        </span>
        <button
          type="button"
          onClick={() => onPickLogId(row.logid)}
          className="truncate text-slate-500 hover:text-slate-900 hover:underline"
          title={`Filter by logid: ${row.logid}`}
        >
          {row.logid}
        </button>
        {row.caller ? (
          <button
            type="button"
            onClick={() => copy(row.caller, "caller")}
            className="ml-auto inline-flex items-center gap-1 truncate text-slate-400 hover:text-slate-700"
            title={`Copy caller (${row.caller})`}
          >
            <span className="truncate">{row.caller}</span>
            {copied === "caller" ? (
              <Check className="h-3 w-3 text-green-600" />
            ) : (
              <Copy className="h-3 w-3" />
            )}
          </button>
        ) : null}
      </div>
      <p
        className={clsx(
          "mt-1 break-words whitespace-pre-wrap text-slate-800",
        )}
      >
        {row.message}
      </p>
    </div>
  );
}
