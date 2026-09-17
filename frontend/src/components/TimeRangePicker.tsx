import { useMemo } from "react";
import clsx from "clsx";

interface Props {
  from: string; // RFC3339 or ""
  to: string;
  onChange: (next: { from: string; to: string }) => void;
}

type Preset = "15m" | "1h" | "6h" | "24h" | "7d" | "custom";

const PRESETS: { id: Preset; label: string; minutes: number }[] = [
  { id: "15m", label: "15m", minutes: 15 },
  { id: "1h", label: "1h", minutes: 60 },
  { id: "6h", label: "6h", minutes: 60 * 6 },
  { id: "24h", label: "24h", minutes: 60 * 24 },
  { id: "7d", label: "7d", minutes: 60 * 24 * 7 },
];

// Resolve which preset (if any) currently matches the from/to window.
// "custom" means the dates don't line up with any preset; we render the
// datetime inputs editable in that case.
function activePreset(from: string, to: string): Preset {
  if (!from || !to) return "custom";
  const f = new Date(from).getTime();
  const t = new Date(to).getTime();
  if (isNaN(f) || isNaN(t)) return "custom";
  const diffMin = (t - f) / 60_000;
  for (const p of PRESETS) {
    if (Math.abs(diffMin - p.minutes) < 0.5) return p.id;
  }
  return "custom";
}

// Convert <input type="datetime-local"> value (no TZ) to an RFC3339
// string interpreted as UTC. The UI uses local timezone for display
// elsewhere; here we anchor to local-to-UTC so the user sees what
// they typed.
function localToRfc3339(local: string): string {
  if (!local) return "";
  const d = new Date(local);
  if (isNaN(d.getTime())) return "";
  return d.toISOString();
}

function rfc3339ToLocal(rfc: string): string {
  if (!rfc) return "";
  const d = new Date(rfc);
  if (isNaN(d.getTime())) return "";
  // datetime-local needs YYYY-MM-DDTHH:mm (no seconds, no TZ).
  const pad = (n: number) => String(n).padStart(2, "0");
  return (
    `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}` +
    `T${pad(d.getHours())}:${pad(d.getMinutes())}`
  );
}

export function TimeRangePicker({ from, to, onChange }: Props) {
  const preset = useMemo(() => activePreset(from, to), [from, to]);

  const applyPreset = (p: Preset) => {
    if (p === "custom") {
      onChange({ from: "", to: "" });
      return;
    }
    const minutes = PRESETS.find((x) => x.id === p)?.minutes ?? 60;
    const t = new Date();
    const f = new Date(t.getTime() - minutes * 60_000);
    onChange({ from: f.toISOString(), to: t.toISOString() });
  };

  const isCustom = preset === "custom";

  return (
    <div className="space-y-2">
      <div className="flex flex-wrap gap-1">
        {PRESETS.map((p) => (
          <button
            key={p.id}
            type="button"
            onClick={() => applyPreset(p.id)}
            className={clsx(
              "rounded border px-2 py-1 text-xs",
              preset === p.id
                ? "border-slate-900 bg-slate-900 text-white"
                : "border-slate-300 bg-white text-slate-700 hover:bg-slate-100",
            )}
          >
            {p.label}
          </button>
        ))}
        <button
          type="button"
          onClick={() => applyPreset("custom")}
          className={clsx(
            "rounded border px-2 py-1 text-xs",
            isCustom
              ? "border-slate-900 bg-slate-900 text-white"
              : "border-slate-300 bg-white text-slate-700 hover:bg-slate-100",
          )}
        >
          Custom
        </button>
      </div>
      <div className="grid grid-cols-2 gap-2">
        <label className="space-y-1 text-xs text-slate-600">
          <span>From</span>
          <input
            type="datetime-local"
            value={rfc3339ToLocal(from)}
            onChange={(e) =>
              onChange({ from: localToRfc3339(e.target.value), to })
            }
            disabled={!isCustom}
            className="w-full rounded border border-slate-300 bg-white px-2 py-1 font-mono text-xs disabled:bg-slate-100 disabled:text-slate-400"
          />
        </label>
        <label className="space-y-1 text-xs text-slate-600">
          <span>To</span>
          <input
            type="datetime-local"
            value={rfc3339ToLocal(to)}
            onChange={(e) =>
              onChange({ from, to: localToRfc3339(e.target.value) })
            }
            disabled={!isCustom}
            className="w-full rounded border border-slate-300 bg-white px-2 py-1 font-mono text-xs disabled:bg-slate-100 disabled:text-slate-400"
          />
        </label>
      </div>
    </div>
  );
}
