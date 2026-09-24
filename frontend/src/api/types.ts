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

export interface AnomalyDetail {
  id: number;
  meter_id: string;
  type: AnomalySummary["type"];
  severity: AnomalySummary["severity"];
  confidence: number;
  status: "OPEN" | "INVESTIGATING" | "RESOLVED" | "DISMISSED";
  reason: string;
  recommended_action: string;
  evidence: {
    decision_path: string[];
    confidence_breakdown: Record<string, number>;
    data_quality_issues: { kind: string; count: number }[];
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
