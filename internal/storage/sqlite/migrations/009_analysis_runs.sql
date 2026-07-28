CREATE TABLE analysis_runs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    input_sha256 TEXT NOT NULL,
    output_sha256 TEXT NOT NULL,
    rule_version TEXT NOT NULL,
    idempotency_key TEXT NOT NULL UNIQUE,
    payload_json TEXT NOT NULL,
    generated_at TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE INDEX idx_analysis_runs_kind_generated
    ON analysis_runs(kind, generated_at DESC, id DESC);

CREATE INDEX idx_analysis_runs_input
    ON analysis_runs(kind, input_sha256, rule_version);
