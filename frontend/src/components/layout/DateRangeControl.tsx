import { useDateRange, type Preset } from "../../context/DateRangeContext";
import { toLocalDateStr, localDateInputToISO, localTimeZoneLabel } from "../../lib/format";

const PRESETS: { value: Preset; label: string }[] = [
  { value: "all", label: "Todo el período" },
  { value: "last_7_days", label: "Últimos 7 días" },
  { value: "last_3_days", label: "Últimos 3 días" },
  { value: "custom", label: "Personalizado" },
];

export default function DateRangeControl() {
  const { preset, setPreset, range, dataRange, setCustomRange } = useDateRange();

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
          <input type="date" value={toLocalDateStr(range.from)}
            min={toLocalDateStr(dataRange.from)} max={toLocalDateStr(dataRange.to)}
            onChange={(e) => {
              if (!e.target.value) return;
              setCustomRange(localDateInputToISO(e.target.value), range.to);
            }}
            className="border rounded px-2 py-1 text-sm" />
          <input type="date" value={toLocalDateStr(range.to)}
            min={toLocalDateStr(dataRange.from)} max={toLocalDateStr(dataRange.to)}
            onChange={(e) => {
              if (!e.target.value) return;
              setCustomRange(range.from, localDateInputToISO(e.target.value));
            }}
            className="border rounded px-2 py-1 text-sm" />
        </>
      )}
      <span className="text-xs text-slate-400">{localTimeZoneLabel()}</span>
    </div>
  );
}
