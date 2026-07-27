CREATE TABLE dart_disclosures (
    receipt_no TEXT PRIMARY KEY
        CHECK (length(receipt_no) = 14 AND receipt_no NOT GLOB '*[^0-9]*'),
    corp_class TEXT NOT NULL
        CHECK (corp_class IN ('Y', 'K', 'N', 'E')),
    corp_code TEXT NOT NULL
        CHECK (length(corp_code) = 8 AND corp_code NOT GLOB '*[^0-9]*'),
    corp_name TEXT NOT NULL,
    stock_code TEXT NOT NULL DEFAULT ''
        CHECK (
            stock_code = '' OR
            (
                length(stock_code) = 6 AND
                stock_code NOT GLOB '*[^0-9A-Z]*' AND
                stock_code = upper(stock_code)
            )
        ),
    report_name TEXT NOT NULL,
    receipt_date TEXT NOT NULL
        CHECK (length(receipt_date) = 10),
    submitter TEXT NOT NULL,
    remark TEXT NOT NULL DEFAULT '',
    viewer_url TEXT NOT NULL,
    source_url TEXT NOT NULL,
    observed_at TEXT NOT NULL,
    fetched_at TEXT NOT NULL,
    first_seen_at TEXT NOT NULL,
    last_seen_at TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX dart_disclosures_corp_date_idx
    ON dart_disclosures(corp_code, receipt_date DESC, receipt_no DESC);

CREATE INDEX dart_disclosures_stock_date_idx
    ON dart_disclosures(stock_code, receipt_date DESC)
    WHERE stock_code <> '';

CREATE INDEX dart_disclosures_report_name_idx
    ON dart_disclosures(report_name COLLATE NOCASE);
