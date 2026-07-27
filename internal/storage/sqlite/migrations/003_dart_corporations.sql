CREATE TABLE dart_corporations (
    corp_code TEXT PRIMARY KEY
        CHECK (length(corp_code) = 8 AND corp_code NOT GLOB '*[^0-9]*'),
    corp_name TEXT NOT NULL,
    corp_english_name TEXT NOT NULL DEFAULT '',
    stock_code TEXT NOT NULL DEFAULT ''
        CHECK (
            stock_code = '' OR
            (
                length(stock_code) = 6 AND
                stock_code NOT GLOB '*[^0-9A-Z]*' AND
                stock_code = upper(stock_code)
            )
        ),
    modify_date TEXT NOT NULL,
    source_url TEXT NOT NULL,
    observed_at TEXT NOT NULL,
    fetched_at TEXT NOT NULL,
    is_current INTEGER NOT NULL DEFAULT 1 CHECK (is_current IN (0, 1)),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX dart_corporations_stock_code_idx
    ON dart_corporations(stock_code)
    WHERE is_current = 1 AND stock_code <> '';

CREATE INDEX dart_corporations_name_idx
    ON dart_corporations(corp_name COLLATE NOCASE)
    WHERE is_current = 1;
