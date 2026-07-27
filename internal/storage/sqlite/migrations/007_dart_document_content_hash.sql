ALTER TABLE dart_document_archives
    ADD COLUMN content_sha256 TEXT NOT NULL DEFAULT ''
    CHECK (
        content_sha256 = '' OR
        (
            length(content_sha256) = 64 AND
            content_sha256 NOT GLOB '*[^0-9a-f]*'
        )
    );

ALTER TABLE dart_document_entries
    ADD COLUMN sha256 TEXT NOT NULL DEFAULT ''
    CHECK (
        sha256 = '' OR
        (
            length(sha256) = 64 AND
            sha256 NOT GLOB '*[^0-9a-f]*'
        )
    );

CREATE UNIQUE INDEX dart_document_archives_content_idx
    ON dart_document_archives(receipt_no, content_sha256)
    WHERE content_sha256 <> '';
