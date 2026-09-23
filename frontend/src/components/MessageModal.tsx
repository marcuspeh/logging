import { useEffect, useRef, useState } from "react";
import { Copy, Check, X } from "lucide-react";
import type { LogRow } from "../api/types";
import { LevelBadge } from "./LevelBadge";
import { formatTimestamp } from "../utils/format";

interface Props {
  row: LogRow | null;
  onClose: () => void;
  onPickLogId: (logid: string) => void;
}

// MessageModal renders the full log row content in a centered dialog.
//
// Why a modal instead of an in-row expand?
//   - The row list is virtualized; growing a single row mid-list forces
//     every row below to shift. Even with useLayoutEffect-based
//     re-measurement, browsers can briefly paint at a stale offset and
//     the next row overlaps the expanded one.
//   - A modal keeps every row at a fixed height, so the virtualizer
//     never has to reflow. The full message is just shown on demand.
export function MessageModal({ row, onClose, onPickLogId }: Props) {
  const [copied, setCopied] = useState<string | null>(null);
  const closeBtnRef = useRef<HTMLButtonElement>(null);

  // Close on Escape; focus the close button on open so keyboard users
  // can dismiss without hunting for the X.
  useEffect(() => {
    if (!row) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    closeBtnRef.current?.focus();
    // Lock background scroll while the modal is open.
    const prevOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      window.removeEventListener("keydown", onKey);
      document.body.style.overflow = prevOverflow;
    };
  }, [row, onClose]);

  if (!row) return null;

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
      role="dialog"
      aria-modal="true"
      aria-label="Log message details"
      className="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/50 p-4"
      onClick={onClose}
    >
      <div
        className="flex max-h-[85vh] w-full max-w-3xl flex-col overflow-hidden rounded-lg bg-white shadow-xl"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="flex items-center justify-between border-b border-slate-200 px-4 py-3">
          <div className="flex min-w-0 items-center gap-2 font-mono text-xs">
            <time
              dateTime={row.timestamp}
              className="shrink-0 text-slate-500 tabular-nums"
            >
              {formatTimestamp(row.timestamp)}
            </time>
            <LevelBadge level={row.level} />
            <span className="min-w-0 flex-1 truncate text-slate-700">
              {row.project}
            </span>
          </div>
          <button
            ref={closeBtnRef}
            type="button"
            onClick={onClose}
            aria-label="Close"
            className="ml-2 inline-flex h-7 w-7 shrink-0 items-center justify-center rounded text-slate-500 hover:bg-slate-100 hover:text-slate-800"
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        {/* Body — scrollable so very long messages don't blow out the page */}
        <div className="min-h-0 flex-1 overflow-auto px-4 py-3 font-mono text-xs">
          <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-slate-500">
            <button
              type="button"
              onClick={() => onPickLogId(row.logid)}
              className="max-w-full truncate text-slate-500 hover:text-slate-900 hover:underline"
              title={`Filter by logid: ${row.logid}`}
            >
              {row.logid}
            </button>
            {row.caller ? (
              <button
                type="button"
                onClick={() => copy(row.caller!, "caller")}
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
            className="mt-3 whitespace-pre-wrap break-words text-slate-800"
            style={{ overflowWrap: "anywhere" }}
          >
            {row.message}
          </p>
        </div>
      </div>
    </div>
  );
}