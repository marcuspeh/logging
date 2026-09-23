import { useRef } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import type { LogRow } from "../api/types";
import { ResultRow, ROW_HEIGHT } from "./ResultRow";

interface Props {
  rows: LogRow[];
  onPickLogId: (logid: string) => void;
}

// ResultsTable virtualizes the row list so we can render up to the
// backend's 1000-row cap without paying for 1000 DOM nodes.
//
// Each row reports its own measured height via ResizeObserver (passed
// through `measureElement`), so expanding a long message grows the
// row and reflows the offsets of every row beneath it. The estimate
// is only used before the first measurement lands.
export function ResultsTable({ rows, onPickLogId }: Props) {
  const parentRef = useRef<HTMLDivElement>(null);

  const virtualizer = useVirtualizer({
    count: rows.length,
    getScrollElement: () => parentRef.current,
    estimateSize: () => ROW_HEIGHT,
    overscan: 4,
    measureElement: (el) =>
      el?.getBoundingClientRect().height ?? ROW_HEIGHT,
  });

  return (
    <div
      ref={parentRef}
      className="h-[calc(100vh-220px)] min-h-[400px] overflow-auto rounded border border-slate-200 bg-white"
    >
      <div
        style={{
          height: `${virtualizer.getTotalSize()}px`,
          width: "100%",
          position: "relative",
        }}
      >
        {virtualizer.getVirtualItems().map((vRow) => {
          const row = rows[vRow.index];
          return (
            <ResultRow
              key={vRow.key}
              row={row}
              data-index={vRow.index}
              ref={virtualizer.measureElement}
              style={{
                position: "absolute",
                top: 0,
                left: 0,
                right: 0,
                transform: `translate3d(0, ${vRow.start}px, 0)`,
              }}
              onPickLogId={onPickLogId}
            />
          );
        })}
      </div>
    </div>
  );
}
