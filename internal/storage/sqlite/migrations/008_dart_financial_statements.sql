CREATE TABLE dart_financial_statements (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    corp_code TEXT NOT NULL
        CHECK (length(corp_code) = 8 AND corp_code NOT GLOB '*[^0-9]*'),
    business_year INTEGER NOT NULL
        CHECK (business_year BETWEEN 2015 AND 9999),
    report_code TEXT NOT NULL
        CHECK (report_code IN ('11011', '11012', '11013', '11014')),
    fs_kind TEXT NOT NULL
        CHECK (fs_kind IN ('CFS', 'OFS')),
    receipt_no TEXT NOT NULL
        CHECK (length(receipt_no) = 14 AND receipt_no NOT GLOB '*[^0-9]*'),
    content_sha256 TEXT NOT NULL
        CHECK (
            length(content_sha256) = 64 AND
            content_sha256 NOT GLOB '*[^0-9a-f]*'
        ),
    source_url TEXT NOT NULL,
    observed_at TEXT NOT NULL,
    fetched_at TEXT NOT NULL,
    is_current INTEGER NOT NULL DEFAULT 1 CHECK (is_current IN (0, 1)),
    first_seen_at TEXT NOT NULL,
    last_seen_at TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(
        corp_code,
        business_year,
        report_code,
        fs_kind,
        content_sha256
    )
);

CREATE TABLE dart_financial_accounts (
    statement_id INTEGER NOT NULL
        REFERENCES dart_financial_statements(id) ON DELETE CASCADE,
    row_index INTEGER NOT NULL CHECK (row_index >= 0),
    statement_kind TEXT NOT NULL
        CHECK (statement_kind IN ('BS', 'IS', 'CIS', 'CF', 'SCE')),
    statement_name TEXT NOT NULL,
    account_id TEXT NOT NULL DEFAULT '',
    account_name TEXT NOT NULL,
    account_detail TEXT NOT NULL DEFAULT '',
    current_term_name TEXT NOT NULL,
    current_amount TEXT NOT NULL DEFAULT '',
    current_add_amount TEXT NOT NULL DEFAULT '',
    previous_term_name TEXT NOT NULL DEFAULT '',
    previous_amount TEXT NOT NULL DEFAULT '',
    previous_interim_term_name TEXT NOT NULL DEFAULT '',
    previous_interim_amount TEXT NOT NULL DEFAULT '',
    previous_add_amount TEXT NOT NULL DEFAULT '',
    before_previous_term_name TEXT NOT NULL DEFAULT '',
    before_previous_amount TEXT NOT NULL DEFAULT '',
    order_no INTEGER NOT NULL CHECK (order_no >= 0),
    currency TEXT NOT NULL
        CHECK (
            length(currency) = 3 AND
            currency NOT GLOB '*[^A-Z]*' AND
            currency = upper(currency)
        ),
    PRIMARY KEY(statement_id, row_index)
);

CREATE UNIQUE INDEX dart_financial_statements_current_idx
    ON dart_financial_statements(
        corp_code,
        business_year,
        report_code,
        fs_kind
    )
    WHERE is_current = 1;

CREATE INDEX dart_financial_statements_query_idx
    ON dart_financial_statements(
        corp_code,
        business_year DESC,
        report_code,
        fs_kind,
        is_current DESC,
        created_at DESC
    );

CREATE INDEX dart_financial_accounts_statement_order_idx
    ON dart_financial_accounts(
        statement_id,
        statement_kind,
        order_no,
        row_index
    );
