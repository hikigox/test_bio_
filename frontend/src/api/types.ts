export interface MeterListItem {
  meter_id: string;
  name: string;
  consumption_kwh: number | null;
  baseline_kwh: number | null;
  variation_pct: number | null;
  status: "UNKNOWN" | "OK" | "ALERT" | "CRITICAL";
  analysis_id: number | null;
  anomaly: AnomalySummary | null;
}

// MeterDetail is the response shape of GET /meters/:meterId
// (backend/internal/api/meters.go meterDetail): everything a MeterListItem
// has, plus the baseline block (hourly profile + baseline V/I/PF + the
// change point), the meter's current-period average V/I/PF, and its
// latest_anomaly (same shape as `anomaly`, exposed separately because the
// backend's currently-active `anomaly` field can be filtered out of range
// while latest_anomaly always reflects the vigente analysis).
export interface MeterDetail extends MeterListItem {
  baseline: MeterBaselineDetail | null;
  current_voltage: number | null;
  current_current: number | null;
  current_power_factor: number | null;
  latest_anomaly: AnomalySummary | null;
}

export interface MeterBaselineDetail {
  hourly_profile: number[];
  voltage: number;
  current: number;
  power_factor: number;
  change_point_at: string | null;
}

// AnomalySummaryLite is the item shape of GET /meters/:meterId/anomalies
// (backend/internal/api/meters.go:441-447): a plain {id, type, severity,
// confidence, status} row, distinct from AnomalySummary (which carries
// priority_rank/in_range/active_from/active_to — fields this endpoint
// doesn't return).
export interface AnomalySummaryLite {
  id: number;
  type: AnomalySummary["type"];
  severity: AnomalySummary["severity"];
  confidence: number;
  status: "OPEN" | "INVESTIGATING" | "RESOLVED" | "DISMISSED";
}

export interface AnomalySummary {
  id: number;
  type: "REAL_ANOMALY" | "EXPLAINABLE_ANOMALY" | "DATA_QUALITY" | "FALSE_POSITIVE";
  severity: "HIGH" | "MEDIUM" | "LOW";
  confidence: number;
  priority_rank: number;
  in_range: boolean;
  active_from: string;
  active_to: string;
}

export interface AnomalySignal {
  signal: string;
  observed: number;
  threshold: number;
  detail: string;
}

export interface AnomalyVariable {
  variable: string;
  baseline: number;
  actual: number;
  delta_pct: number;
  changed: boolean;
}

export interface AnomalyEvent {
  id: number;
  type: string;
  description: string;
  relation: string;
  offset_hours: number;
}

export interface AnomalyDetail {
  id: number;
  meter_id: string;
  type: AnomalySummary["type"];
  severity: AnomalySummary["severity"];
  confidence: number;
  confidence_label: string;
  status: "OPEN" | "INVESTIGATING" | "RESOLVED" | "DISMISSED";
  reason: string;
  recommended_action: string;
  active_from: string;
  active_to: string;
  duration_hours: number;
  change_point_at: string | null;
  baseline_kwh: number;
  actual_kwh: number;
  variation_pct: number;
  explanation_source: string;
  signals: AnomalySignal[];
  variables: AnomalyVariable[];
  events: AnomalyEvent[];
  evidence: {
    // Both come back as JSON `null` from the backend when empty (nil Go
    // slices), not `[]` — must be null-guarded wherever read.
    decision_path: string[] | null;
    confidence_breakdown: Record<string, number>;
    data_quality_issues: { kind: string; count: number }[] | null;
    affected_readings: { count: number; first: string; last: string };
  };
}

export interface AnalysisStatus {
  id: number;
  status: "PENDING" | "RUNNING" | "COMPLETED" | "FAILED";
  stage: "READINGS" | "BASELINE" | "DETECTION" | "CORRELATION" | "EVENTS" | "EXPLANATION" | "RECOMMENDATION";
  progress: number;
  summary: { anomalies: number; priority: number; avg_confidence: number; message: string };
}

export interface DashboardSummary {
  data_range: { from: string; to: string };
  period: { from: string; to: string };
  meters: number;
  total_consumption_kwh: number;
  anomalies: number;
  high_priority: number;
  avg_confidence: number;
  by_meter: {
    meter_id: string;
    consumption_kwh: number;
    share_pct: number;
    status: string;
    // vacío ("") cuando el medidor no tiene ninguna anomalía vigente
    // (backend/internal/api/dashboard.go: zero-value de Severity).
    severity: "" | "HIGH" | "MEDIUM" | "LOW";
  }[];
}
