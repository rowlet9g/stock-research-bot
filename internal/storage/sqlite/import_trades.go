package sqlite

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/decimal"
)

var tradeCSVHeaders = []string{
	"external_id",
	"trade_date",
	"ticker",
	"action",
	"quantity",
	"price",
	"fees",
	"taxes",
	"currency",
}

type TradeImportResult struct {
	Source          string    `json:"source"`
	FileSHA256      string    `json:"file_sha256"`
	RowsSeen        int       `json:"rows_seen"`
	RowsInserted    int       `json:"rows_inserted"`
	RowsDuplicated  int       `json:"rows_duplicated"`
	AlreadyImported bool      `json:"already_imported"`
	ImportedAt      time.Time `json:"imported_at"`
}

func (s *Store) ImportTradeCSV(ctx context.Context, path string, source string) (TradeImportResult, error) {
	source = strings.ToLower(strings.TrimSpace(source))
	if source == "" {
		return TradeImportResult{}, fmt.Errorf("trade import source is required")
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return TradeImportResult{}, fmt.Errorf("read trade CSV %q: %w", path, err)
	}
	sum := sha256.Sum256(content)
	fileHash := hex.EncodeToString(sum[:])

	rows, err := parseTradeCSV(content, path)
	if err != nil {
		return TradeImportResult{}, err
	}
	return s.ImportTradeRows(ctx, source, fileHash, rows)
}

func (s *Store) ImportTradeRows(
	ctx context.Context,
	source string,
	fileHash string,
	rows []TradeImportRow,
) (TradeImportResult, error) {
	source = strings.ToLower(strings.TrimSpace(source))
	if source == "" {
		return TradeImportResult{}, fmt.Errorf("trade import source is required")
	}
	fileHash = strings.ToLower(strings.TrimSpace(fileHash))
	if fileHash == "" {
		return TradeImportResult{}, fmt.Errorf("trade import file hash is required")
	}
	if len(rows) == 0 {
		return TradeImportResult{}, fmt.Errorf("trade import has no rows")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TradeImportResult{}, fmt.Errorf("begin trade import: %w", err)
	}
	defer tx.Rollback()

	existing, found, err := findImportRun(ctx, tx, source, fileHash)
	if err != nil {
		return TradeImportResult{}, err
	}
	if found {
		existing.AlreadyImported = true
		return existing, nil
	}

	importedAt := s.now().UTC()
	result := TradeImportResult{
		Source:     source,
		FileSHA256: fileHash,
		RowsSeen:   len(rows),
		ImportedAt: importedAt,
	}
	for _, row := range rows {
		row.Input.Source = source
		row.Input.IdempotencyKey = tradeIdempotencyKey(row.Ticker, row.Input)
		if err := validateTradeInput(row.Input); err != nil {
			return TradeImportResult{}, fmt.Errorf("trade row %d: %w", row.RowNumber, err)
		}
		instrumentID, err := instrumentIDByTicker(ctx, tx, row.Ticker)
		if err != nil {
			return TradeImportResult{}, fmt.Errorf("trade row %d: %w", row.RowNumber, err)
		}
		inserted, err := insertTrade(ctx, tx, instrumentID, row.Input, importedAt)
		if err != nil {
			return TradeImportResult{}, fmt.Errorf("trade row %d: %w", row.RowNumber, err)
		}
		if inserted {
			result.RowsInserted++
		} else {
			result.RowsDuplicated++
		}
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO import_runs(source, file_sha256, rows_seen, rows_inserted, imported_at)
		VALUES (?, ?, ?, ?, ?)
	`,
		result.Source,
		result.FileSHA256,
		result.RowsSeen,
		result.RowsInserted,
		result.ImportedAt.Format(timeFormat),
	)
	if err != nil {
		return TradeImportResult{}, fmt.Errorf("record trade import: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return TradeImportResult{}, fmt.Errorf("commit trade import: %w", err)
	}
	return result, nil
}

type TradeImportRow struct {
	RowNumber int
	Ticker    string
	Input     TradeInput
}

func parseTradeCSV(content []byte, path string) ([]TradeImportRow, error) {
	reader := csv.NewReader(bytes.NewReader(content))
	reader.TrimLeadingSpace = true
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parse trade CSV %q: %w", path, err)
	}
	if len(records) < 2 {
		return nil, fmt.Errorf("trade CSV %q has no data rows", path)
	}

	header := map[string]int{}
	for i, value := range records[0] {
		name := strings.TrimSpace(strings.TrimPrefix(value, "\uFEFF"))
		if _, exists := header[name]; exists {
			return nil, fmt.Errorf("trade CSV %q has duplicate header %q", path, name)
		}
		header[name] = i
	}
	for _, name := range tradeCSVHeaders {
		if _, exists := header[name]; !exists {
			return nil, fmt.Errorf("trade CSV %q is missing required header %q", path, name)
		}
	}

	rows := make([]TradeImportRow, 0, len(records)-1)
	for index, record := range records[1:] {
		rowNumber := index + 2
		if blankCSVRow(record) {
			continue
		}

		ticker := csvField(record, header, "ticker")
		if ticker == "" {
			return nil, fmt.Errorf("trade CSV %q row %d: ticker is required", path, rowNumber)
		}
		tradeDate, timePrecision, err := parseTradeDate(csvField(record, header, "trade_date"))
		if err != nil {
			return nil, fmt.Errorf("trade CSV %q row %d: %w", path, rowNumber, err)
		}
		action := strings.ToUpper(csvField(record, header, "action"))
		quantityUnits, err := parseRequiredDecimal(record, header, "quantity")
		if err != nil {
			return nil, fmt.Errorf("trade CSV %q row %d: %w", path, rowNumber, err)
		}
		priceUnits, err := parseRequiredDecimal(record, header, "price")
		if err != nil {
			return nil, fmt.Errorf("trade CSV %q row %d: %w", path, rowNumber, err)
		}
		feesUnits, _, err := parseOptionalDecimal(record, header, "fees")
		if err != nil {
			return nil, fmt.Errorf("trade CSV %q row %d: %w", path, rowNumber, err)
		}
		taxesUnits, taxesKnown, err := parseOptionalDecimal(record, header, "taxes")
		if err != nil {
			return nil, fmt.Errorf("trade CSV %q row %d: %w", path, rowNumber, err)
		}
		currency := strings.ToUpper(csvField(record, header, "currency"))
		externalID := csvField(record, header, "external_id")
		if externalID == "" {
			return nil, fmt.Errorf("trade CSV %q row %d: external_id is required", path, rowNumber)
		}

		input := TradeInput{
			ExternalID:    externalID,
			TradeDate:     tradeDate,
			Action:        action,
			QuantityUnits: quantityUnits,
			PriceUnits:    priceUnits,
			FeesUnits:     feesUnits,
			TaxesUnits:    taxesUnits,
			PriceSource:   "reported",
			TaxesKnown:    taxesKnown,
			TimePrecision: timePrecision,
			Currency:      currency,
		}
		rows = append(rows, TradeImportRow{
			RowNumber: rowNumber,
			Ticker:    ticker,
			Input:     input,
		})
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("trade CSV %q has no non-empty data rows", path)
	}
	return rows, nil
}

func tradeIdempotencyKey(ticker string, input TradeInput) string {
	parts := []string{
		strings.ToLower(strings.TrimSpace(input.Source)),
		"external_id",
		strings.ToUpper(strings.TrimSpace(ticker)),
		strings.TrimSpace(input.ExternalID),
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return hex.EncodeToString(sum[:])
}

func findImportRun(
	ctx context.Context,
	tx *sql.Tx,
	source string,
	fileHash string,
) (TradeImportResult, bool, error) {
	var result TradeImportResult
	var importedAt string
	err := tx.QueryRowContext(ctx, `
		SELECT source, file_sha256, rows_seen, rows_inserted, imported_at
		FROM import_runs
		WHERE source = ? AND file_sha256 = ?
	`, source, fileHash).Scan(
		&result.Source,
		&result.FileSHA256,
		&result.RowsSeen,
		&result.RowsInserted,
		&importedAt,
	)
	if err == sql.ErrNoRows {
		return TradeImportResult{}, false, nil
	}
	if err != nil {
		return TradeImportResult{}, false, fmt.Errorf("query trade import: %w", err)
	}
	result.RowsDuplicated = result.RowsSeen - result.RowsInserted
	result.ImportedAt, err = parseTime(importedAt)
	if err != nil {
		return TradeImportResult{}, false, fmt.Errorf("parse trade import time: %w", err)
	}
	return result, true, nil
}

func instrumentIDByTicker(ctx context.Context, tx *sql.Tx, ticker string) (int64, error) {
	var instrumentID int64
	err := tx.QueryRowContext(ctx, `
		SELECT id
		FROM instruments
		WHERE ticker = ? COLLATE NOCASE OR yahoo_ticker = ? COLLATE NOCASE
		LIMIT 1
	`, ticker, ticker).Scan(&instrumentID)
	if err == sql.ErrNoRows {
		return 0, fmt.Errorf("%w: instrument %q", ErrNotFound, ticker)
	}
	if err != nil {
		return 0, fmt.Errorf("query instrument %q: %w", ticker, err)
	}
	return instrumentID, nil
}

func parseTradeDate(value string) (time.Time, string, error) {
	value = strings.TrimSpace(value)
	for _, candidate := range []struct {
		format    string
		precision string
	}{
		{format: "2006-01-02", precision: "day"},
		{format: "20060102", precision: "day"},
		{format: time.RFC3339, precision: "second"},
	} {
		if parsed, err := time.Parse(candidate.format, value); err == nil {
			return parsed.UTC(), candidate.precision, nil
		}
	}
	return time.Time{}, "", fmt.Errorf("invalid trade_date %q", value)
}

func parseRequiredDecimal(record []string, header map[string]int, name string) (int64, error) {
	value := csvField(record, header, name)
	if value == "" {
		return 0, fmt.Errorf("%s is required", name)
	}
	parsed, err := decimal.Parse(value)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", name, err)
	}
	return parsed, nil
}

func parseOptionalDecimal(
	record []string,
	header map[string]int,
	name string,
) (int64, bool, error) {
	value := csvField(record, header, name)
	if value == "" {
		return 0, false, nil
	}
	parsed, err := decimal.Parse(value)
	if err != nil {
		return 0, false, fmt.Errorf("invalid %s: %w", name, err)
	}
	return parsed, true, nil
}

func csvField(record []string, header map[string]int, name string) string {
	index := header[name]
	if index >= len(record) {
		return ""
	}
	return strings.TrimSpace(record[index])
}

func blankCSVRow(record []string) bool {
	for _, value := range record {
		if strings.TrimSpace(value) != "" {
			return false
		}
	}
	return true
}
