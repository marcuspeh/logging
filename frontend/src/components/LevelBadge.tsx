import clsx from "clsx";
import type { Level } from "../api/types";
import { levelClasses } from "../utils/format";

interface Props {
  level: Level;
  className?: string;
}

export function LevelBadge({ level, className }: Props) {
  return (
    <span
      className={clsx(
        "inline-flex items-center rounded border px-1.5 py-0.5 font-mono text-xs font-medium",
        levelClasses(level),
        className,
      )}
      title={`Level: ${level}`}
    >
      {level}
    </span>
  );
}
