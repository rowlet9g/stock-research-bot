CREATE TABLE instruments (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    ticker TEXT NOT NULL UNIQUE COLLATE NOCASE,
    yahoo_ticker TEXT NOT NULL UNIQUE COLLATE NOCASE,
    dart_corp_code TEXT NOT NULL DEFAULT '',
    market TEXT NOT NULL,
    currency TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE positions (
    instrument_id INTEGER PRIMARY KEY,
    quantity_units INTEGER NOT NULL,
    average_cost_units INTEGER NOT NULL CHECK (average_cost_units >= 0),
    currency TEXT NOT NULL,
    as_of TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (instrument_id) REFERENCES instruments(id) ON DELETE CASCADE
);

CREATE TABLE trades (
    id INTEGER PRIMARY KEY,
    instrument_id INTEGER NOT NULL,
    external_id TEXT NOT NULL DEFAULT '',
    trade_date TEXT NOT NULL,
    action TEXT NOT NULL CHECK (action IN ('BUY', 'SELL')),
    quantity_units INTEGER NOT NULL CHECK (quantity_units > 0),
    price_units INTEGER NOT NULL CHECK (price_units >= 0),
    fees_units INTEGER NOT NULL CHECK (fees_units >= 0),
    taxes_units INTEGER NOT NULL CHECK (taxes_units >= 0),
    currency TEXT NOT NULL,
    source TEXT NOT NULL,
    idempotency_key TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL,
    FOREIGN KEY (instrument_id) REFERENCES instruments(id) ON DELETE RESTRICT
);

CREATE INDEX trades_instrument_date_idx
    ON trades(instrument_id, trade_date DESC, id DESC);

CREATE TABLE theses (
    instrument_id INTEGER PRIMARY KEY,
    summary TEXT NOT NULL,
    invalidation_condition TEXT NOT NULL,
    expected_holding_period TEXT NOT NULL,
    check_metrics_json TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (instrument_id) REFERENCES instruments(id) ON DELETE CASCADE
);

CREATE TABLE import_runs (
    id INTEGER PRIMARY KEY,
    source TEXT NOT NULL,
    file_sha256 TEXT NOT NULL,
    rows_seen INTEGER NOT NULL,
    rows_inserted INTEGER NOT NULL,
    imported_at TEXT NOT NULL,
    UNIQUE (source, file_sha256)
);
