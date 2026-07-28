CREATE TABLE alerts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    fingerprint TEXT NOT NULL UNIQUE,
    kind TEXT NOT NULL,
    severity TEXT NOT NULL,
    status TEXT NOT NULL,
    title TEXT NOT NULL,
    fact TEXT NOT NULL,
    source_run_id INTEGER NOT NULL REFERENCES analysis_runs(id) ON DELETE RESTRICT,
    payload_json TEXT NOT NULL,
    occurrence_count INTEGER NOT NULL DEFAULT 0,
    first_detected_at TEXT NOT NULL,
    last_detected_at TEXT NOT NULL,
    sent_at TEXT NOT NULL DEFAULT '',
    acknowledged_at TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE alert_observations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    alert_id INTEGER NOT NULL REFERENCES alerts(id) ON DELETE CASCADE,
    source_run_id INTEGER NOT NULL REFERENCES analysis_runs(id) ON DELETE RESTRICT,
    payload_sha256 TEXT NOT NULL,
    observed_at TEXT NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE(alert_id, source_run_id)
);

CREATE INDEX idx_alerts_status_severity_detected
    ON alerts(status, severity, last_detected_at DESC, id DESC);

CREATE INDEX idx_alert_observations_run
    ON alert_observations(source_run_id, alert_id);
