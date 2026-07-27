package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/models"
)

type DARTDocumentArchiveSyncResult struct {
	ReceiptNo      string    `json:"receipt_no"`
	ArchiveID      int64     `json:"archive_id"`
	Inserted       bool      `json:"inserted"`
	VersionChanged bool      `json:"version_changed"`
	SyncedAt       time.Time `json:"synced_at"`
}

func (s *Store) SyncDARTDocumentArchive(
	ctx context.Context,
	archive models.DARTDocumentArchive,
) (DARTDocumentArchiveSyncResult, error) {
	result := DARTDocumentArchiveSyncResult{
		ReceiptNo: archive.ReceiptNo,
		SyncedAt:  s.now().UTC(),
	}
	if err := validateDARTDocumentArchive(archive); err != nil {
		return result, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, fmt.Errorf(
			"begin OpenDART document sync for %s: %w",
			archive.ReceiptNo,
			err,
		)
	}
	defer tx.Rollback()

	var existingID int64
	matchedByContent := false
	err = tx.QueryRowContext(ctx, `
		SELECT id
		FROM dart_document_archives
		WHERE receipt_no = ? AND content_sha256 = ?
	`, archive.ReceiptNo, archive.ContentSHA256).Scan(&existingID)
	switch {
	case err == nil:
		matchedByContent = true
	case err == sql.ErrNoRows:
		err = tx.QueryRowContext(ctx, `
			SELECT id
			FROM dart_document_archives
			WHERE receipt_no = ? AND sha256 = ?
		`, archive.ReceiptNo, archive.SHA256).Scan(&existingID)
		switch {
		case err == nil:
		case err == sql.ErrNoRows:
			result.Inserted = true
		default:
			return result, fmt.Errorf(
				"inspect OpenDART document archive by raw hash: %w",
				err,
			)
		}
	default:
		return result, fmt.Errorf(
			"inspect OpenDART document archive by content hash: %w",
			err,
		)
	}

	var currentHash string
	var currentContentHash string
	err = tx.QueryRowContext(ctx, `
		SELECT sha256, content_sha256
		FROM dart_document_archives
		WHERE receipt_no = ? AND is_current = 1
		LIMIT 1
	`, archive.ReceiptNo).Scan(&currentHash, &currentContentHash)
	switch {
	case err == nil:
		if currentContentHash != "" {
			result.VersionChanged = currentContentHash != archive.ContentSHA256
		} else {
			result.VersionChanged = currentHash != archive.SHA256
		}
	case err == sql.ErrNoRows:
	default:
		return result, fmt.Errorf("inspect current OpenDART document archive: %w", err)
	}

	now := result.SyncedAt.Format(timeFormat)
	if _, err := tx.ExecContext(ctx, `
		UPDATE dart_document_archives
		SET is_current = 0, updated_at = ?
		WHERE receipt_no = ?
		  AND id <> ?
		  AND is_current = 1
	`, now, archive.ReceiptNo, existingID); err != nil {
		return result, fmt.Errorf("retire previous OpenDART document archive: %w", err)
	}

	observedAt := archive.Source.ObservedAt.UTC().Format(timeFormat)
	if result.Inserted {
		insertResult, err := tx.ExecContext(ctx, `
			INSERT INTO dart_document_archives(
				receipt_no,
				sha256,
				content_sha256,
				size_bytes,
				relative_path,
				source_url,
				observed_at,
				fetched_at,
				is_current,
				first_seen_at,
				last_seen_at,
				created_at,
				updated_at
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?, ?)
		`,
			archive.ReceiptNo,
			archive.SHA256,
			archive.ContentSHA256,
			archive.SizeBytes,
			archive.RelativePath,
			archive.Source.SourceURL,
			observedAt,
			archive.Source.FetchedAt.UTC().Format(timeFormat),
			now,
			now,
			now,
			now,
		)
		if err != nil {
			return result, fmt.Errorf("insert OpenDART document archive: %w", err)
		}
		result.ArchiveID, err = insertResult.LastInsertId()
		if err != nil {
			return result, fmt.Errorf("read OpenDART document archive ID: %w", err)
		}
	} else if matchedByContent {
		result.ArchiveID = existingID
		if _, err := tx.ExecContext(ctx, `
			UPDATE dart_document_archives
			SET
				source_url = ?,
				observed_at = ?,
				fetched_at = ?,
				is_current = 1,
				last_seen_at = ?,
				updated_at = ?
			WHERE id = ?
		`,
			archive.Source.SourceURL,
			observedAt,
			archive.Source.FetchedAt.UTC().Format(timeFormat),
			now,
			now,
			existingID,
		); err != nil {
			return result, fmt.Errorf("update OpenDART document archive: %w", err)
		}
	} else {
		result.ArchiveID = existingID
		if _, err := tx.ExecContext(ctx, `
			UPDATE dart_document_archives
			SET
				content_sha256 = ?,
				size_bytes = ?,
				relative_path = ?,
				source_url = ?,
				observed_at = ?,
				fetched_at = ?,
				is_current = 1,
				last_seen_at = ?,
				updated_at = ?
			WHERE id = ?
		`,
			archive.ContentSHA256,
			archive.SizeBytes,
			archive.RelativePath,
			archive.Source.SourceURL,
			observedAt,
			archive.Source.FetchedAt.UTC().Format(timeFormat),
			now,
			now,
			existingID,
		); err != nil {
			return result, fmt.Errorf(
				"backfill OpenDART document content hash: %w",
				err,
			)
		}
	}

	if result.Inserted || !matchedByContent {
		if _, err := tx.ExecContext(
			ctx,
			`DELETE FROM dart_document_entries WHERE archive_id = ?`,
			result.ArchiveID,
		); err != nil {
			return result, fmt.Errorf("replace OpenDART document entries: %w", err)
		}
		for _, entry := range archive.Entries {
			if _, err := tx.ExecContext(ctx, `
			INSERT INTO dart_document_entries(
				archive_id,
				entry_index,
				name,
				sha256,
				compressed_bytes,
				uncompressed_bytes,
				crc32
			)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`,
				result.ArchiveID,
				entry.Index,
				entry.Name,
				entry.SHA256,
				entry.CompressedBytes,
				entry.UncompressedBytes,
				entry.CRC32,
			); err != nil {
				return result, fmt.Errorf(
					"insert OpenDART document entry %d: %w",
					entry.Index,
					err,
				)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return result, fmt.Errorf("commit OpenDART document archive: %w", err)
	}
	return result, nil
}

func (s *Store) DARTDisclosureByReceipt(
	ctx context.Context,
	receiptNo string,
) (models.DARTDisclosure, error) {
	receiptNo = strings.TrimSpace(receiptNo)
	row := s.db.QueryRowContext(ctx, `
		SELECT
			corp_class,
			corp_code,
			corp_name,
			stock_code,
			report_name,
			receipt_no,
			receipt_date,
			submitter,
			remark,
			viewer_url,
			source_url,
			observed_at,
			fetched_at
		FROM dart_disclosures
		WHERE receipt_no = ?
	`, receiptNo)
	disclosure, err := scanDARTDisclosure(row)
	if err == sql.ErrNoRows {
		return models.DARTDisclosure{}, fmt.Errorf(
			"%w: OpenDART disclosure %q",
			ErrNotFound,
			receiptNo,
		)
	}
	if err != nil {
		return models.DARTDisclosure{}, fmt.Errorf(
			"query OpenDART disclosure %q: %w",
			receiptNo,
			err,
		)
	}
	return disclosure, nil
}

func (s *Store) ListPendingDARTDocumentDisclosures(
	ctx context.Context,
	corpCode string,
	limit int,
) ([]models.DARTDisclosure, error) {
	corpCode = strings.TrimSpace(corpCode)
	if !fixedDigits(corpCode, 8) {
		return nil, fmt.Errorf("invalid OpenDART corporation code %q", corpCode)
	}
	if limit <= 0 || limit > 1000 {
		return nil, fmt.Errorf("document disclosure limit must be between 1 and 1000")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			disclosure.corp_class,
			disclosure.corp_code,
			disclosure.corp_name,
			disclosure.stock_code,
			disclosure.report_name,
			disclosure.receipt_no,
			disclosure.receipt_date,
			disclosure.submitter,
			disclosure.remark,
			disclosure.viewer_url,
			disclosure.source_url,
			disclosure.observed_at,
			disclosure.fetched_at
		FROM dart_disclosures AS disclosure
		WHERE disclosure.corp_code = ?
		  AND NOT EXISTS (
				SELECT 1
				FROM dart_document_archives AS archive
				WHERE archive.receipt_no = disclosure.receipt_no
				  AND archive.is_current = 1
		  )
		ORDER BY disclosure.receipt_date DESC, disclosure.receipt_no DESC
		LIMIT ?
	`, corpCode, limit)
	if err != nil {
		return nil, fmt.Errorf(
			"list pending OpenDART documents for %s: %w",
			corpCode,
			err,
		)
	}
	defer rows.Close()

	disclosures := []models.DARTDisclosure{}
	for rows.Next() {
		disclosure, err := scanDARTDisclosure(rows)
		if err != nil {
			return nil, fmt.Errorf("scan pending OpenDART document: %w", err)
		}
		disclosures = append(disclosures, disclosure)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending OpenDART documents: %w", err)
	}
	return disclosures, nil
}

func (s *Store) ListDARTDocumentArchives(
	ctx context.Context,
	receiptNo string,
	limit int,
) ([]models.DARTDocumentArchive, error) {
	receiptNo = strings.TrimSpace(receiptNo)
	if !fixedDigits(receiptNo, 14) {
		return nil, fmt.Errorf("invalid OpenDART receipt number %q", receiptNo)
	}
	if limit <= 0 || limit > 100 {
		return nil, fmt.Errorf("document archive limit must be between 1 and 100")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			id,
			receipt_no,
			sha256,
			content_sha256,
			size_bytes,
			relative_path,
			is_current,
			source_url,
			observed_at,
			fetched_at,
			created_at,
			updated_at
		FROM dart_document_archives
		WHERE receipt_no = ?
		ORDER BY is_current DESC, created_at DESC
		LIMIT ?
	`, receiptNo, limit)
	if err != nil {
		return nil, fmt.Errorf("list OpenDART document archives: %w", err)
	}

	archives := []models.DARTDocumentArchive{}
	for rows.Next() {
		var archive models.DARTDocumentArchive
		var isCurrent int
		var observedAt string
		var fetchedAt string
		var createdAt string
		var updatedAt string
		if err := rows.Scan(
			&archive.ID,
			&archive.ReceiptNo,
			&archive.SHA256,
			&archive.ContentSHA256,
			&archive.SizeBytes,
			&archive.RelativePath,
			&isCurrent,
			&archive.Source.SourceURL,
			&observedAt,
			&fetchedAt,
			&createdAt,
			&updatedAt,
		); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan OpenDART document archive: %w", err)
		}
		archive.IsCurrent = isCurrent == 1
		archive.Source.Provider = "opendart"
		parsedObservedAt, err := parseTime(observedAt)
		if err != nil {
			rows.Close()
			return nil, fmt.Errorf("parse document observed time: %w", err)
		}
		archive.Source.ObservedAt = &parsedObservedAt
		archive.Source.FetchedAt, err = parseTime(fetchedAt)
		if err != nil {
			rows.Close()
			return nil, fmt.Errorf("parse document fetched time: %w", err)
		}
		archive.CreatedAt, err = parseTime(createdAt)
		if err != nil {
			rows.Close()
			return nil, fmt.Errorf("parse document created time: %w", err)
		}
		archive.UpdatedAt, err = parseTime(updatedAt)
		if err != nil {
			rows.Close()
			return nil, fmt.Errorf("parse document updated time: %w", err)
		}
		archive.Entries = []models.DARTDocumentEntry{}
		archives = append(archives, archive)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close OpenDART document archive rows: %w", err)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate OpenDART document archives: %w", err)
	}

	for index := range archives {
		entryRows, err := s.db.QueryContext(ctx, `
			SELECT
				entry_index,
				name,
				sha256,
				compressed_bytes,
				uncompressed_bytes,
				crc32
			FROM dart_document_entries
			WHERE archive_id = ?
			ORDER BY entry_index
		`, archives[index].ID)
		if err != nil {
			return nil, fmt.Errorf("list OpenDART document entries: %w", err)
		}
		for entryRows.Next() {
			var entry models.DARTDocumentEntry
			if err := entryRows.Scan(
				&entry.Index,
				&entry.Name,
				&entry.SHA256,
				&entry.CompressedBytes,
				&entry.UncompressedBytes,
				&entry.CRC32,
			); err != nil {
				entryRows.Close()
				return nil, fmt.Errorf("scan OpenDART document entry: %w", err)
			}
			archives[index].Entries = append(archives[index].Entries, entry)
		}
		if err := entryRows.Close(); err != nil {
			return nil, fmt.Errorf("close OpenDART document entry rows: %w", err)
		}
		if err := entryRows.Err(); err != nil {
			return nil, fmt.Errorf("iterate OpenDART document entries: %w", err)
		}
	}
	return archives, nil
}

func (s *Store) DARTDocumentArchiveByContentHash(
	ctx context.Context,
	receiptNo string,
	contentSHA256 string,
) (models.DARTDocumentArchive, error) {
	receiptNo = strings.TrimSpace(receiptNo)
	contentSHA256 = strings.ToLower(strings.TrimSpace(contentSHA256))
	switch {
	case !fixedDigits(receiptNo, 14):
		return models.DARTDocumentArchive{}, fmt.Errorf(
			"invalid OpenDART receipt number %q",
			receiptNo,
		)
	case !fixedLowerHex(contentSHA256, 64):
		return models.DARTDocumentArchive{}, fmt.Errorf(
			"invalid OpenDART document content SHA-256 %q",
			contentSHA256,
		)
	}

	var archive models.DARTDocumentArchive
	var isCurrent int
	var observedAt string
	var fetchedAt string
	var createdAt string
	var updatedAt string
	err := s.db.QueryRowContext(ctx, `
		SELECT
			id,
			receipt_no,
			sha256,
			content_sha256,
			size_bytes,
			relative_path,
			is_current,
			source_url,
			observed_at,
			fetched_at,
			created_at,
			updated_at
		FROM dart_document_archives
		WHERE receipt_no = ? AND content_sha256 = ?
	`, receiptNo, contentSHA256).Scan(
		&archive.ID,
		&archive.ReceiptNo,
		&archive.SHA256,
		&archive.ContentSHA256,
		&archive.SizeBytes,
		&archive.RelativePath,
		&isCurrent,
		&archive.Source.SourceURL,
		&observedAt,
		&fetchedAt,
		&createdAt,
		&updatedAt,
	)
	if err == sql.ErrNoRows {
		return models.DARTDocumentArchive{}, fmt.Errorf(
			"%w: OpenDART document %s content %s",
			ErrNotFound,
			receiptNo,
			contentSHA256,
		)
	}
	if err != nil {
		return models.DARTDocumentArchive{}, fmt.Errorf(
			"query OpenDART document archive by content hash: %w",
			err,
		)
	}
	archive.IsCurrent = isCurrent == 1
	archive.Source.Provider = "opendart"
	parsedObservedAt, err := parseTime(observedAt)
	if err != nil {
		return models.DARTDocumentArchive{}, fmt.Errorf(
			"parse document observed time: %w",
			err,
		)
	}
	archive.Source.ObservedAt = &parsedObservedAt
	archive.Source.FetchedAt, err = parseTime(fetchedAt)
	if err != nil {
		return models.DARTDocumentArchive{}, fmt.Errorf(
			"parse document fetched time: %w",
			err,
		)
	}
	archive.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return models.DARTDocumentArchive{}, fmt.Errorf(
			"parse document created time: %w",
			err,
		)
	}
	archive.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return models.DARTDocumentArchive{}, fmt.Errorf(
			"parse document updated time: %w",
			err,
		)
	}
	archive.Entries, err = s.listDARTDocumentEntries(ctx, archive.ID)
	if err != nil {
		return models.DARTDocumentArchive{}, err
	}
	return archive, nil
}

func (s *Store) listDARTDocumentEntries(
	ctx context.Context,
	archiveID int64,
) ([]models.DARTDocumentEntry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			entry_index,
			name,
			sha256,
			compressed_bytes,
			uncompressed_bytes,
			crc32
		FROM dart_document_entries
		WHERE archive_id = ?
		ORDER BY entry_index
	`, archiveID)
	if err != nil {
		return nil, fmt.Errorf("list OpenDART document entries: %w", err)
	}
	defer rows.Close()

	entries := []models.DARTDocumentEntry{}
	for rows.Next() {
		var entry models.DARTDocumentEntry
		if err := rows.Scan(
			&entry.Index,
			&entry.Name,
			&entry.SHA256,
			&entry.CompressedBytes,
			&entry.UncompressedBytes,
			&entry.CRC32,
		); err != nil {
			return nil, fmt.Errorf("scan OpenDART document entry: %w", err)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate OpenDART document entries: %w", err)
	}
	return entries, nil
}

func validateDARTDocumentArchive(archive models.DARTDocumentArchive) error {
	switch {
	case !fixedDigits(archive.ReceiptNo, 14):
		return fmt.Errorf(
			"invalid OpenDART document receipt number %q",
			archive.ReceiptNo,
		)
	case !fixedLowerHex(archive.SHA256, 64):
		return fmt.Errorf(
			"invalid OpenDART document SHA-256 %q",
			archive.SHA256,
		)
	case !fixedLowerHex(archive.ContentSHA256, 64):
		return fmt.Errorf(
			"invalid OpenDART document content SHA-256 %q",
			archive.ContentSHA256,
		)
	case archive.SizeBytes <= 0:
		return fmt.Errorf("OpenDART document archive size must be positive")
	case strings.TrimSpace(archive.RelativePath) == "":
		return fmt.Errorf("OpenDART document relative path is required")
	case len(archive.Entries) == 0:
		return fmt.Errorf("OpenDART document archive has no entries")
	case archive.Source.Provider != "opendart":
		return fmt.Errorf(
			"OpenDART document has unexpected provider %q",
			archive.Source.Provider,
		)
	case strings.TrimSpace(archive.Source.SourceURL) == "":
		return fmt.Errorf("OpenDART document source URL is required")
	case archive.Source.ObservedAt == nil:
		return fmt.Errorf("OpenDART document observed time is required")
	case archive.Source.FetchedAt.IsZero():
		return fmt.Errorf("OpenDART document fetched time is required")
	}
	seenIndexes := make(map[int]struct{}, len(archive.Entries))
	for index, entry := range archive.Entries {
		switch {
		case entry.Index < 0:
			return fmt.Errorf("OpenDART document entry %d has a negative index", index+1)
		case strings.TrimSpace(entry.Name) == "":
			return fmt.Errorf("OpenDART document entry %d has no name", index+1)
		case !fixedLowerHex(entry.SHA256, 64):
			return fmt.Errorf(
				"OpenDART document entry %d has invalid SHA-256 %q",
				index+1,
				entry.SHA256,
			)
		case entry.CompressedBytes < 0 || entry.UncompressedBytes < 0:
			return fmt.Errorf("OpenDART document entry %d has a negative size", index+1)
		}
		if _, exists := seenIndexes[entry.Index]; exists {
			return fmt.Errorf(
				"OpenDART document entry %d duplicates index %d",
				index+1,
				entry.Index,
			)
		}
		seenIndexes[entry.Index] = struct{}{}
	}
	return nil
}

func fixedLowerHex(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') &&
			(character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}
