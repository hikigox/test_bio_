package store

const schemaSQL = `
CREATE TABLE IF NOT EXISTS meters (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  meter_id TEXT UNIQUE NOT NULL,
  name TEXT, location TEXT, status TEXT NOT NULL DEFAULT 'UNKNOWN',
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS readings (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  meter_id TEXT NOT NULL, timestamp TEXT NOT NULL,
  consumption_kwh REAL, voltage_v REAL, current_a REAL, power_factor REAL, status TEXT,
  UNIQUE(meter_id, timestamp)
);

CREATE TABLE IF NOT EXISTS events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  meter_id TEXT NOT NULL, timestamp TEXT NOT NULL, type TEXT, description TEXT
);

CREATE TABLE IF NOT EXISTS users (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  email TEXT UNIQUE NOT NULL, password_hash TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS analyses (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  status TEXT NOT NULL, stage TEXT, progress REAL,
  trigger TEXT, triggered_by INTEGER,
  started_at TEXT, finished_at TEXT, duration_ms INTEGER,
  engine_version TEXT, config_json TEXT,
  scope_meter_ids_json TEXT,
  data_from TEXT, data_to TEXT,
  baseline_from TEXT, baseline_to TEXT,
  readings_analyzed INTEGER, meters_analyzed INTEGER,
  anomalies_count INTEGER, high_priority_count INTEGER, avg_confidence REAL,
  summary_message TEXT, stage_log_json TEXT, error TEXT
);

CREATE TABLE IF NOT EXISTS meter_baselines (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  analysis_id INTEGER NOT NULL REFERENCES analyses(id),
  meter_id TEXT NOT NULL,
  method TEXT, window_from TEXT, window_to TEXT, change_point_at TEXT,
  baseline_kwh REAL, actual_kwh REAL, variation_pct REAL,
  hourly_profile_json TEXT,
  baseline_voltage_v REAL, baseline_current_a REAL, baseline_power_factor REAL,
  actual_voltage_v REAL, actual_current_a REAL, actual_power_factor REAL,
  readings_count INTEGER, data_quality_score REAL,
  UNIQUE(analysis_id, meter_id)
);

CREATE TABLE IF NOT EXISTS anomalies (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  analysis_id INTEGER NOT NULL REFERENCES analyses(id),
  meter_id TEXT NOT NULL,
  detected_at TEXT, period_from TEXT, period_to TEXT, change_point_at TEXT,
  type TEXT, severity TEXT,
  confidence REAL, confidence_label TEXT,
  priority_score REAL,
  baseline_kwh REAL, actual_kwh REAL, variation_pct REAL,
  reason TEXT, recommended_action TEXT, explanation_source TEXT,
  status TEXT NOT NULL DEFAULT 'OPEN', created_at TEXT, updated_at TEXT,
  evidence_json TEXT,
  UNIQUE(analysis_id, meter_id)
);

CREATE TABLE IF NOT EXISTS anomaly_signals (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  anomaly_id INTEGER NOT NULL REFERENCES anomalies(id),
  signal TEXT, observed REAL, threshold REAL, detail TEXT
);

CREATE TABLE IF NOT EXISTS anomaly_variables (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  anomaly_id INTEGER NOT NULL REFERENCES anomalies(id),
  variable TEXT, baseline REAL, actual REAL, delta_pct REAL, changed INTEGER
);

CREATE TABLE IF NOT EXISTS anomaly_events (
  anomaly_id INTEGER NOT NULL REFERENCES anomalies(id),
  event_id INTEGER NOT NULL REFERENCES events(id),
  relation TEXT, offset_hours REAL
);
`
