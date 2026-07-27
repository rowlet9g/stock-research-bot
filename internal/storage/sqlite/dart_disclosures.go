package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/models"
)

type DARTDisclosureSyncResult struct {
	CorpCode        string    `json:"corp_code"`
	DisclosuresSeen int       `json:"disclosures_seen"`
	Inserted        int       `json:"inserted"`
	Updated         int       `json:"updated"`
	SyncedAt        time.Time `json:"synced_at"`
}

func (s *Store) SyncDARTDisclosures(
	ctx context.Context,
	corpCode string,
	disclosures []models.DARTDisclosure,
) (DARTDisclosureSyncResult, error) {
	corpCode = strings.TrimSpace(corpCode)
	result := DARTDisclosureSyncResult{
		CorpCode:        corpCode,
		DisclosuresSeen: len(disclosures),
		SyncedAt:        s.now().UTC(),
	}
	if !fixedDigits(corpCode, 8) {
		return result, fmt.Errorf("invalid OpenDART corporation code %q", corpCode)
	}
	if err := validateDARTDisclosures(corpCode, disclosures); err != nil {
		return result, err
	}
	if len(disclosures) == 0 {
		return result, nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, fmt.Errorf("begin OpenDART disclosure sync for %s: %w", corpCode, err)
	}
	defer tx.Rollback()

	statement, err := tx.PrepareContext(ctx, `
		INSERT INTO dart_disclosures(
			receipt_no,
			corp_class,
			corp_code,
			corp_name,
			stock_code,
			report_name,
			receipt_date,
			submitter,
			remark,
			viewer_url,
			source_url,
			observed_at,
			fetched_at,
			first_seen_at,
			last_seen_at,
			created_at,
			updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(receipt_no) DO UPDATE SET
			corp_class = excluded.corp_class,
			corp_code = excluded.corp_code,
			corp_name = excluded.corp_name,
			stock_code = excluded.stock_code,
			report_name = excluded.report_name,
			receipt_date = excluded.receipt_date,
			submitter = excluded.submitter,
			remark = excluded.remark,
			viewer_url = excluded.viewer_url,
			source_url = excluded.source_url,
			observed_at = excluded.observed_at,
			fetched_at = excluded.fetched_at,
			last_seen_at = excluded.last_seen_at,
			updated_at = excluded.updated_at
	`)
	if err != nil {
		return result, fmt.Errorf("prepare OpenDART disclosure upsert: %w", err)
	}
	defer statement.Close()

	now := result.SyncedAt.Format(timeFormat)
	for _, disclosure := range disclosures {
		var exists int
		err := tx.QueryRowContext(
			ctx,
			`SELECT 1 FROM dart_disclosures WHERE receipt_no = ?`,
			disclosure.ReceiptNo,
		).Scan(&exists)
		switch {
		case err == nil:
			result.Updated++
		case err == sql.ErrNoRows:
			result.Inserted++
		default:
			return result, fmt.Errorf(
				"inspect OpenDART disclosure %q: %w",
				disclosure.ReceiptNo,
				err,
			)
		}

		observedAt := disclosure.Source.ObservedAt.UTC().Format(timeFormat)
		if _, err := statement.ExecContext(
			ctx,
			disclosure.ReceiptNo,
			disclosure.CorpClass,
			disclosure.CorpCode,
			disclosure.CorpName,
			disclosure.StockCode,
			disclosure.ReportName,
			disclosure.ReceiptDate.UTC().Format("2006-01-02"),
			disclosure.Submitter,
			disclosure.Remark,
			disclosure.ViewerURL,
			disclosure.Source.SourceURL,
			observedAt,
			disclosure.Source.FetchedAt.UTC().Format(timeFormat),
			now,
			now,
			now,
			now,
		); err != nil {
			return result, fmt.Errorf(
				"upsert OpenDART disclosure %q: %w",
				disclosure.ReceiptNo,
				err,
			)
		}
	}
	if err := tx.Commit(); err != nil {
		return result, fmt.Errorf(
			"commit OpenDART disclosure sync for %s: %w",
			corpCode,
			err,
		)
	}
	return result, nil
}

func (s *Store) ListDARTDisclosures(
	ctx context.Context,
	corpCode string,
	limit int,
) ([]models.DARTDisclosure, error) {
	corpCode = strings.TrimSpace(corpCode)
	if !fixedDigits(corpCode, 8) {
		return nil, fmt.Errorf("invalid OpenDART corporation code %q", corpCode)
	}
	if limit <= 0 || limit > 1000 {
		return nil, fmt.Errorf("disclosure limit must be between 1 and 1000")
	}

	rows, err := s.db.QueryContext(ctx, `
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
		WHERE corp_code = ?
		ORDER BY receipt_date DESC, receipt_no DESC
		LIMIT ?
	`, corpCode, limit)
	if err != nil {
		return nil, fmt.Errorf("list OpenDART disclosures for %s: %w", corpCode, err)
	}
	defer rows.Close()

	disclosures := []models.DARTDisclosure{}
	for rows.Next() {
		disclosure, err := scanDARTDisclosure(rows)
		if err != nil {
			return nil, fmt.Errorf("scan OpenDART disclosure: %w", err)
		}
		disclosures = append(disclosures, disclosure)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate OpenDART disclosures: %w", err)
	}
	return disclosures, nil
}

func scanDARTDisclosure(row scanner) (models.DARTDisclosure, error) {
	var disclosure models.DARTDisclosure
	var receiptDate string
	var observedAt string
	var fetchedAt string
	if err := row.Scan(
		&disclosure.CorpClass,
		&disclosure.CorpCode,
		&disclosure.CorpName,
		&disclosure.StockCode,
		&disclosure.ReportName,
		&disclosure.ReceiptNo,
		&receiptDate,
		&disclosure.Submitter,
		&disclosure.Remark,
		&disclosure.ViewerURL,
		&disclosure.Source.SourceURL,
		&observedAt,
		&fetchedAt,
	); err != nil {
		return models.DARTDisclosure{}, err
	}

	var err error
	disclosure.ReceiptDate, err = time.Parse("2006-01-02", receiptDate)
	if err != nil {
		return models.DARTDisclosure{}, fmt.Errorf("parse receipt date: %w", err)
	}
	parsedObservedAt, err := parseTime(observedAt)
	if err != nil {
		return models.DARTDisclosure{}, fmt.Errorf("parse observed time: %w", err)
	}
	disclosure.Source.ObservedAt = &parsedObservedAt
	disclosure.Source.FetchedAt, err = parseTime(fetchedAt)
	if err != nil {
		return models.DARTDisclosure{}, fmt.Errorf("parse fetched time: %w", err)
	}
	disclosure.Source.Provider = "opendart"
	return disclosure, nil
}

func validateDARTDisclosures(
	corpCode string,
	disclosures []models.DARTDisclosure,
) error {
	seenReceiptNumbers := make(map[string]struct{}, len(disclosures))
	for index, disclosure := range disclosures {
		switch {
		case disclosure.CorpCode != corpCode:
			return fmt.Errorf(
				"OpenDART disclosure %d belongs to corporation %q",
				index+1,
				disclosure.CorpCode,
			)
		case disclosure.CorpClass != "Y" &&
			disclosure.CorpClass != "K" &&
			disclosure.CorpClass != "N" &&
			disclosure.CorpClass != "E":
			return fmt.Errorf(
				"OpenDART disclosure %d has invalid corporation class %q",
				index+1,
				disclosure.CorpClass,
			)
		case strings.TrimSpace(disclosure.CorpName) == "":
			return fmt.Errorf("OpenDART disclosure %d has no corporation name", index+1)
		case disclosure.StockCode != "" &&
			!fixedUpperAlphanumeric(disclosure.StockCode, 6):
			return fmt.Errorf(
				"OpenDART disclosure %d has invalid stock code %q",
				index+1,
				disclosure.StockCode,
			)
		case strings.TrimSpace(disclosure.ReportName) == "":
			return fmt.Errorf("OpenDART disclosure %d has no report name", index+1)
		case !fixedDigits(disclosure.ReceiptNo, 14):
			return fmt.Errorf(
				"OpenDART disclosure %d has invalid receipt number %q",
				index+1,
				disclosure.ReceiptNo,
			)
		case disclosure.ReceiptDate.IsZero():
			return fmt.Errorf("OpenDART disclosure %d has no receipt date", index+1)
		case strings.TrimSpace(disclosure.Submitter) == "":
			return fmt.Errorf("OpenDART disclosure %d has no submitter", index+1)
		case strings.TrimSpace(disclosure.ViewerURL) == "":
			return fmt.Errorf("OpenDART disclosure %d has no viewer URL", index+1)
		case disclosure.Source.Provider != "opendart":
			return fmt.Errorf(
				"OpenDART disclosure %d has unexpected provider %q",
				index+1,
				disclosure.Source.Provider,
			)
		case strings.TrimSpace(disclosure.Source.SourceURL) == "":
			return fmt.Errorf("OpenDART disclosure %d has no source URL", index+1)
		case disclosure.Source.ObservedAt == nil:
			return fmt.Errorf("OpenDART disclosure %d has no observed time", index+1)
		case disclosure.Source.FetchedAt.IsZero():
			return fmt.Errorf("OpenDART disclosure %d has no fetched time", index+1)
		}
		if _, exists := seenReceiptNumbers[disclosure.ReceiptNo]; exists {
			return fmt.Errorf(
				"OpenDART disclosure %d duplicates receipt number %q",
				index+1,
				disclosure.ReceiptNo,
			)
		}
		seenReceiptNumbers[disclosure.ReceiptNo] = struct{}{}
	}
	return nil
}
