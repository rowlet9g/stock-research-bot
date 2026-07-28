package sqlite

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/decimal"
)

var positionCSVHeaders = []string{
	"ticker",
	"quantity",
	"average_cost",
	"currency",
	"as_of",
}

type PositionImportRow struct {
	RowNumber        int
	Ticker           string
	QuantityUnits    int64
	AverageCostUnits int64
	Currency         string
	AsOf             time.Time
}

type PositionImportResult struct {
	File          string    `json:"file"`
	RowsSeen      int       `json:"rows_seen"`
	RowsChanged   int       `json:"rows_changed"`
	RowsUnchanged int       `json:"rows_unchanged"`
	ImportedAt    time.Time `json:"imported_at"`
}

func (s *Store) ImportPositionCSV(
	ctx context.Context,
	path string,
) (PositionImportResult, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return PositionImportResult{}, fmt.Errorf(
			"read position CSV %q: %w",
			path,
			err,
		)
	}
	rows, err := parsePositionCSV(content, path)
	if err != nil {
		return PositionImportResult{}, err
	}
	result, err := s.ImportPositionRows(ctx, rows)
	if err != nil {
		return PositionImportResult{}, err
	}
	result.File = path
	return result, nil
}

func (s *Store) ImportPositionRows(
	ctx context.Context,
	rows []PositionImportRow,
) (PositionImportResult, error) {
	if len(rows) == 0 {
		return PositionImportResult{}, fmt.Errorf(
			"position import has no rows",
		)
	}
	seenTickers := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if err := validatePositionImportRow(row); err != nil {
			return PositionImportResult{}, fmt.Errorf(
				"position row %d: %w",
				row.RowNumber,
				err,
			)
		}
		key := strings.ToUpper(strings.TrimSpace(row.Ticker))
		if _, exists := seenTickers[key]; exists {
			return PositionImportResult{}, fmt.Errorf(
				"position row %d: duplicate ticker %q",
				row.RowNumber,
				row.Ticker,
			)
		}
		seenTickers[key] = struct{}{}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PositionImportResult{}, fmt.Errorf(
			"begin position import: %w",
			err,
		)
	}
	defer tx.Rollback()

	importedAt := s.now().UTC()
	result := PositionImportResult{
		RowsSeen:   len(rows),
		ImportedAt: importedAt,
	}
	for _, row := range rows {
		instrumentID, err := instrumentIDByTicker(ctx, tx, row.Ticker)
		if err != nil {
			return PositionImportResult{}, fmt.Errorf(
				"position row %d: %w",
				row.RowNumber,
				err,
			)
		}
		sqlResult, err := tx.ExecContext(ctx, `
			INSERT INTO positions(
				instrument_id,
				quantity_units,
				average_cost_units,
				currency,
				as_of,
				updated_at
			)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(instrument_id) DO UPDATE SET
				quantity_units = excluded.quantity_units,
				average_cost_units = excluded.average_cost_units,
				currency = excluded.currency,
				as_of = excluded.as_of,
				updated_at = excluded.updated_at
			WHERE positions.quantity_units <> excluded.quantity_units
			   OR positions.average_cost_units <> excluded.average_cost_units
			   OR positions.currency <> excluded.currency
			   OR positions.as_of <> excluded.as_of
		`,
			instrumentID,
			row.QuantityUnits,
			row.AverageCostUnits,
			strings.ToUpper(strings.TrimSpace(row.Currency)),
			row.AsOf.UTC().Format(timeFormat),
			importedAt.Format(timeFormat),
		)
		if err != nil {
			return PositionImportResult{}, fmt.Errorf(
				"upsert position row %d: %w",
				row.RowNumber,
				err,
			)
		}
		affected, err := sqlResult.RowsAffected()
		if err != nil {
			return PositionImportResult{}, fmt.Errorf(
				"read position row %d result: %w",
				row.RowNumber,
				err,
			)
		}
		if affected == 0 {
			result.RowsUnchanged++
		} else {
			result.RowsChanged++
		}
	}
	if err := tx.Commit(); err != nil {
		return PositionImportResult{}, fmt.Errorf(
			"commit position import: %w",
			err,
		)
	}
	return result, nil
}

func parsePositionCSV(
	content []byte,
	path string,
) ([]PositionImportRow, error) {
	reader := csv.NewReader(bytes.NewReader(content))
	reader.TrimLeadingSpace = true
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parse position CSV %q: %w", path, err)
	}
	if len(records) < 2 {
		return nil, fmt.Errorf("position CSV %q has no data rows", path)
	}

	header := map[string]int{}
	for index, value := range records[0] {
		name := strings.TrimSpace(strings.TrimPrefix(value, "\uFEFF"))
		if _, exists := header[name]; exists {
			return nil, fmt.Errorf(
				"position CSV %q has duplicate header %q",
				path,
				name,
			)
		}
		header[name] = index
	}
	for _, name := range positionCSVHeaders {
		if _, exists := header[name]; !exists {
			return nil, fmt.Errorf(
				"position CSV %q is missing required header %q",
				path,
				name,
			)
		}
	}

	rows := make([]PositionImportRow, 0, len(records)-1)
	for index, record := range records[1:] {
		rowNumber := index + 2
		if blankCSVRow(record) {
			continue
		}
		quantityUnits, err := decimal.Parse(
			csvField(record, header, "quantity"),
		)
		if err != nil {
			return nil, fmt.Errorf(
				"position CSV %q row %d: invalid quantity: %w",
				path,
				rowNumber,
				err,
			)
		}
		averageCostUnits, err := decimal.Parse(
			csvField(record, header, "average_cost"),
		)
		if err != nil {
			return nil, fmt.Errorf(
				"position CSV %q row %d: invalid average cost: %w",
				path,
				rowNumber,
				err,
			)
		}
		asOf, err := parsePositionAsOf(csvField(record, header, "as_of"))
		if err != nil {
			return nil, fmt.Errorf(
				"position CSV %q row %d: %w",
				path,
				rowNumber,
				err,
			)
		}
		rows = append(rows, PositionImportRow{
			RowNumber:        rowNumber,
			Ticker:           csvField(record, header, "ticker"),
			QuantityUnits:    quantityUnits,
			AverageCostUnits: averageCostUnits,
			Currency:         csvField(record, header, "currency"),
			AsOf:             asOf,
		})
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("position CSV %q has no data rows", path)
	}
	return rows, nil
}

func validatePositionImportRow(row PositionImportRow) error {
	switch {
	case strings.TrimSpace(row.Ticker) == "":
		return fmt.Errorf("ticker is required")
	case row.AverageCostUnits < 0:
		return fmt.Errorf("average cost must not be negative")
	case strings.TrimSpace(row.Currency) == "":
		return fmt.Errorf("currency is required")
	case row.AsOf.IsZero():
		return fmt.Errorf("as-of time is required")
	}
	return nil
}

func parsePositionAsOf(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	for _, format := range []string{"2006-01-02", time.RFC3339} {
		if parsed, err := time.Parse(format, value); err == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf(
		"as_of must use YYYY-MM-DD or RFC3339",
	)
}
