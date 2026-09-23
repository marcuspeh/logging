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
// All rows use a fixed height imported from ResultRow so the
// virtualizer's offset math is exact — no measurement race, no
// overlapping rows even when individual messages are huge.
export function ResultsTable({ rows, onPickLogId }: Props) {
  const parentRef = useRef<HTMLDivElement>(null);

  const virtualizer = useVirtualizer({
    count: rows.length,
    getScrollElement: () => parentRef.current,
    estimateSize: () => ROW_HEIGHT,
    overscan: 6,
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
              style={{
                position: "absolute",
                top: 0,
                left: 0,
                right: 0,
                // translate3d forces GPU compositing so abs-positioned
                // rows stay aligned with the virtualizer's start offsets.
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
