CREATE TABLE dart_document_archives (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    receipt_no TEXT NOT NULL
        REFERENCES dart_disclosures(receipt_no) ON DELETE CASCADE,
    sha256 TEXT NOT NULL
        CHECK (
            length(sha256) = 64 AND
            sha256 NOT GLOB '*[^0-9a-f]*'
        ),
    size_bytes INTEGER NOT NULL CHECK (size_bytes > 0),
    relative_path TEXT NOT NULL UNIQUE,
    source_url TEXT NOT NULL,
    observed_at TEXT NOT NULL,
    fetched_at TEXT NOT NULL,
    is_current INTEGER NOT NULL DEFAULT 1 CHECK (is_current IN (0, 1)),
    first_seen_at TEXT NOT NULL,
    last_seen_at TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(receipt_no, sha256)
);

CREATE TABLE dart_document_entries (
    archive_id INTEGER NOT NULL
        REFERENCES dart_document_archives(id) ON DELETE CASCADE,
    entry_index INTEGER NOT NULL CHECK (entry_index >= 0),
    name TEXT NOT NULL,
    compressed_bytes INTEGER NOT NULL CHECK (compressed_bytes >= 0),
    uncompressed_bytes INTEGER NOT NULL CHECK (uncompressed_bytes >= 0),
    crc32 INTEGER NOT NULL CHECK (crc32 >= 0),
    PRIMARY KEY(archive_id, entry_index)
);

CREATE UNIQUE INDEX dart_document_archives_current_idx
    ON dart_document_archives(receipt_no)
    WHERE is_current = 1;

CREATE INDEX dart_document_archives_receipt_idx
    ON dart_document_archives(receipt_no, is_current DESC, created_at DESC);
