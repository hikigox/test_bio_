import { useDateRange, type Preset } from "../../context/DateRangeContext";

const PRESETS: { value: Preset; label: string }[] = [
  { value: "all", label: "Todo el período" },
  { value: "last_7_days", label: "Últimos 7 días" },
  { value: "last_3_days", label: "Últimos 3 días" },
  { value: "custom", label: "Personalizado" },
];

export default function DateRangeControl() {
  const { preset, setPreset, range, setCustomRange } = useDateRange();

  return (
    <div className="flex items-center gap-2">
      <select
        value={preset}
        onChange={(e) => setPreset(e.target.value as Preset)}
        className="border rounded px-2 py-1 text-sm"
      >
        {PRESETS.map((p) => (
          <option key={p.value} value={p.value}>{p.label}</option>
        ))}
      </select>
      {preset === "custom" && (
        <>
          <input type="date" value={range.from.slice(0, 10)}
            onChange={(e) => setCustomRange(new Date(e.target.value).toISOString(), range.to)}
            className="border rounded px-2 py-1 text-sm" />
          <input type="date" value={range.to.slice(0, 10)}
            onChange={(e) => setCustomRange(range.from, new Date(e.target.value).toISOString())}
            className="border rounded px-2 py-1 text-sm" />
        </>
      )}
      <span className="text-xs text-slate-400">UTC</span>
    </div>
  );
}
