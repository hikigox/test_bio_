export function formatNumberEsES(value: number): string {
  // useGrouping explícito: el "auto" por defecto de es-ES usa la estrategia
  // CLDR "min2" (solo agrupa si el primer grupo tiene >= 2 dígitos), lo que
  // deja números como 1860 sin separador de miles ("1860" en vez de
  // "1.860"). Forzar `true` da el separador de miles siempre, como espera
  // el resto de la UI.
  return new Intl.NumberFormat("es-ES", { maximumFractionDigits: 0, useGrouping: true }).format(value);
}

function pad(n: number): string {
  return String(n).padStart(2, "0");
}

// El backend persiste y calcula todo en UTC (spec 01); estas funciones son
// solo de PRESENTACIÓN — convierten esos timestamps a la hora local del
// navegador del usuario, sin tocar nada de lo que viaja por la API. `new
// Date(iso)` ya interpreta el "Z"/offset del ISO string y expone getters
// locales (getFullYear/getHours/...), así que basta con leer esos getters en
// vez de recortar el string UTC crudo como se hacía antes.
export function toLocalDateStr(iso: string): string {
  const d = new Date(iso);
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

export function toLocalDateTimeStr(iso: string): string {
  const d = new Date(iso);
  return `${toLocalDateStr(iso)} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

export function toLocalMonthDayStr(iso: string): string {
  const d = new Date(iso);
  return `${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

// Etiqueta corta del offset local respecto a UTC (p.ej. "UTC-5", "UTC+2:30"),
// para indicar en la UI que las fechas mostradas ya no son UTC sino la hora
// del sistema del usuario.
export function localTimeZoneLabel(): string {
  const offsetMin = -new Date().getTimezoneOffset();
  const sign = offsetMin >= 0 ? "+" : "-";
  const abs = Math.abs(offsetMin);
  const hh = Math.floor(abs / 60);
  const mm = abs % 60;
  return mm === 0 ? `UTC${sign}${hh}` : `UTC${sign}${hh}:${pad(mm)}`;
}

// Convierte el valor de un <input type="date"> (una fecha calendario LOCAL,
// sin hora) al instante UTC correspondiente a la medianoche LOCAL de ese día.
// `new Date("YYYY-MM-DD")` interpretaría ese string como medianoche UTC, no
// local — para un usuario que no está en UTC eso corre el rango un día.
export function localDateInputToISO(dateStr: string): string {
  const [y, m, d] = dateStr.split("-").map(Number);
  return new Date(y, m - 1, d).toISOString();
}

export function formatPercent(value: number): string {
  const sign = value >= 0 ? "+" : "-";
  const abs = Math.abs(value);
  const formatted = new Intl.NumberFormat("es-ES", { minimumFractionDigits: 1, maximumFractionDigits: 1 }).format(abs);
  return `${sign}${formatted} %`;
}

export function severityColor(severity: "HIGH" | "MEDIUM" | "LOW"): string {
  switch (severity) {
    case "HIGH":
      return "text-red-600 bg-red-50";
    case "MEDIUM":
      return "text-amber-600 bg-amber-50";
    case "LOW":
      return "text-slate-600 bg-slate-100";
  }
}

export function statusColor(status: "OK" | "ALERT" | "CRITICAL" | "UNKNOWN"): string {
  switch (status) {
    case "OK":
      return "text-green-600 bg-green-50";
    case "ALERT":
      return "text-amber-600 bg-amber-50";
    case "CRITICAL":
      return "text-red-600 bg-red-50";
    default:
      return "text-slate-500 bg-slate-100";
  }
}

// Paleta fija (brand-neutral) para asignar un color estable por meter_id,
// determinista por hash simple (no depende del orden de renderizado).
const METER_PALETTE = [
  "#2563eb", "#059669", "#d97706", "#dc2626", "#7c3aed",
  "#0891b2", "#db2777", "#65a30d", "#ea580c", "#4f46e5",
  "#0d9488", "#c026d3",
];

export function meterColor(meterId: string): string {
  let hash = 0;
  for (let i = 0; i < meterId.length; i++) {
    hash = (hash * 31 + meterId.charCodeAt(i)) >>> 0;
  }
  return METER_PALETTE[hash % METER_PALETTE.length];
}
