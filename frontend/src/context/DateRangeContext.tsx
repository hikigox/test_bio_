import { createContext, useContext, useMemo, type ReactNode } from "react";
import { useSearchParams } from "react-router-dom";

export type Preset = "all" | "last_7_days" | "last_3_days" | "custom";

interface DateRange {
  from: string;
  to: string;
}

interface DateRangeContextValue {
  range: DateRange;
  dataRange: DateRange;
  preset: Preset;
  setPreset: (preset: Preset) => void;
  setCustomRange: (from: string, to: string) => void;
}

const DateRangeContext = createContext<DateRangeContextValue | undefined>(undefined);

export function DateRangeProvider({ children, dataRange }: { children: ReactNode; dataRange: DateRange }) {
  const [params, setParams] = useSearchParams();

  const preset = (params.get("preset") as Preset) || "all";
  const from = params.get("from") || dataRange.from;
  const to = params.get("to") || dataRange.to;

  function setPreset(newPreset: Preset) {
    const to = new Date(dataRange.to);
    let from = new Date(dataRange.from);
    if (newPreset === "last_7_days") {
      from = new Date(to.getTime() - 7 * 24 * 3600 * 1000);
    } else if (newPreset === "last_3_days") {
      from = new Date(to.getTime() - 3 * 24 * 3600 * 1000);
    }
    const next = new URLSearchParams(params);
    next.set("preset", newPreset);
    if (newPreset === "all") {
      next.delete("from");
      next.delete("to");
    } else {
      next.set("from", from.toISOString());
      next.set("to", dataRange.to);
    }
    setParams(next);
  }

  function setCustomRange(newFrom: string, newTo: string) {
    const next = new URLSearchParams(params);
    next.set("preset", "custom");
    next.set("from", newFrom);
    next.set("to", newTo);
    setParams(next);
  }

  const value = useMemo(
    () => ({ range: { from, to }, dataRange, preset, setPreset, setCustomRange }),
    [from, to, preset, dataRange, params]
  );

  return <DateRangeContext.Provider value={value}>{children}</DateRangeContext.Provider>;
}

export function useDateRange(): DateRangeContextValue {
  const ctx = useContext(DateRangeContext);
  if (!ctx) throw new Error("useDateRange debe usarse dentro de DateRangeProvider");
  return ctx;
}
