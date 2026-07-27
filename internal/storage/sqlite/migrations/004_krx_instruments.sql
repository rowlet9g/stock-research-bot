ALTER TABLE instruments
    ADD COLUMN krx_standard_code TEXT NOT NULL DEFAULT '';

ALTER TABLE instruments
    ADD COLUMN instrument_type TEXT NOT NULL DEFAULT 'unknown'
    CHECK (
        instrument_type IN (
            'unknown',
            'common_stock',
            'preferred_stock',
            'etf',
            'etn',
            'other_equity'
        )
    );

ALTER TABLE instruments
    ADD COLUMN krx_verified_at TEXT NOT NULL DEFAULT '';

CREATE TABLE krx_instruments (
    short_code TEXT PRIMARY KEY COLLATE NOCASE
        CHECK (
            length(short_code) = 6 AND
            short_code NOT GLOB '*[^0-9A-Z]*' AND
            short_code = upper(short_code)
        ),
    standard_code TEXT NOT NULL DEFAULT ''
        CHECK (
            standard_code = '' OR
            (
                length(standard_code) = 12 AND
                standard_code NOT GLOB '*[^0-9A-Z]*' AND
                standard_code = upper(standard_code)
            )
        ),
    name TEXT NOT NULL,
    abbreviated_name TEXT NOT NULL DEFAULT '',
    english_name TEXT NOT NULL DEFAULT '',
    market TEXT NOT NULL,
    security_group TEXT NOT NULL DEFAULT '',
    section TEXT NOT NULL DEFAULT '',
    share_type TEXT NOT NULL DEFAULT '',
    instrument_type TEXT NOT NULL
        CHECK (
            instrument_type IN (
                'common_stock',
                'preferred_stock',
                'etf',
                'etn',
                'other_equity'
            )
        ),
    listing_date TEXT NOT NULL DEFAULT '',
    dataset TEXT NOT NULL
        CHECK (dataset IN ('kospi', 'kosdaq', 'konex', 'etf', 'etn')),
    source_url TEXT NOT NULL,
    observed_at TEXT NOT NULL,
    fetched_at TEXT NOT NULL,
    is_current INTEGER NOT NULL DEFAULT 1 CHECK (is_current IN (0, 1)),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE UNIQUE INDEX krx_instruments_standard_code_idx
    ON krx_instruments(standard_code)
    WHERE standard_code <> '';

CREATE INDEX krx_instruments_dataset_current_idx
    ON krx_instruments(dataset, is_current, short_code);

CREATE INDEX krx_instruments_name_idx
    ON krx_instruments(name COLLATE NOCASE)
    WHERE is_current = 1;
