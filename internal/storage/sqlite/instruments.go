package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/models"
)

var ErrNotFound = errors.New("record not found")

func (s *Store) UpsertInstrument(ctx context.Context, item models.WatchlistItem) (models.Instrument, error) {
	if err := validateWatchlistItem(item); err != nil {
		return models.Instrument{}, err
	}

	now := s.now().UTC().Format(timeFormat)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO instruments(
			name, ticker, yahoo_ticker, dart_corp_code, market, currency, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(ticker) DO UPDATE SET
			name = excluded.name,
			yahoo_ticker = excluded.yahoo_ticker,
			dart_corp_code = CASE
				WHEN trim(excluded.dart_corp_code) <> '' THEN excluded.dart_corp_code
				ELSE instruments.dart_corp_code
			END,
			market = excluded.market,
			currency = excluded.currency,
			updated_at = excluded.updated_at
	`,
		strings.TrimSpace(item.Name),
		strings.TrimSpace(item.Ticker),
		strings.TrimSpace(item.YahooTicker),
		strings.TrimSpace(item.DARTCorpCode),
		strings.TrimSpace(item.Market),
		strings.ToUpper(strings.TrimSpace(item.Currency)),
		now,
		now,
	)
	if err != nil {
		return models.Instrument{}, fmt.Errorf("upsert instrument %q: %w", item.Ticker, err)
	}
	return s.Instrument(ctx, item.Ticker)
}

func (s *Store) SyncInstruments(ctx context.Context, items []models.WatchlistItem) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin instrument sync: %w", err)
	}
	defer tx.Rollback()

	now := s.now().UTC().Format(timeFormat)
	for _, item := range items {
		if err := validateWatchlistItem(item); err != nil {
			return 0, err
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO instruments(
				name, ticker, yahoo_ticker, dart_corp_code, market, currency, created_at, updated_at
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(ticker) DO UPDATE SET
				name = excluded.name,
				yahoo_ticker = excluded.yahoo_ticker,
				dart_corp_code = CASE
					WHEN trim(excluded.dart_corp_code) <> '' THEN excluded.dart_corp_code
					ELSE instruments.dart_corp_code
				END,
				market = excluded.market,
				currency = excluded.currency,
				updated_at = excluded.updated_at
		`,
			strings.TrimSpace(item.Name),
			strings.TrimSpace(item.Ticker),
			strings.TrimSpace(item.YahooTicker),
			strings.TrimSpace(item.DARTCorpCode),
			strings.TrimSpace(item.Market),
			strings.ToUpper(strings.TrimSpace(item.Currency)),
			now,
			now,
		)
		if err != nil {
			return 0, fmt.Errorf("sync instrument %q: %w", item.Ticker, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit instrument sync: %w", err)
	}
	return len(items), nil
}

func (s *Store) Instrument(ctx context.Context, ticker string) (models.Instrument, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT
			id,
			name,
			ticker,
			yahoo_ticker,
			dart_corp_code,
			krx_standard_code,
			instrument_type,
			krx_verified_at,
			market,
			currency,
			created_at,
			updated_at
		FROM instruments
		WHERE ticker = ? COLLATE NOCASE OR yahoo_ticker = ? COLLATE NOCASE
		LIMIT 1
	`, strings.TrimSpace(ticker), strings.TrimSpace(ticker))

	instrument, err := scanInstrument(row)
	if err == sql.ErrNoRows {
		return models.Instrument{}, fmt.Errorf("%w: instrument %q", ErrNotFound, ticker)
	}
	if err != nil {
		return models.Instrument{}, fmt.Errorf("query instrument %q: %w", ticker, err)
	}
	return instrument, nil
}

func (s *Store) ListInstruments(ctx context.Context) ([]models.Instrument, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			id,
			name,
			ticker,
			yahoo_ticker,
			dart_corp_code,
			krx_standard_code,
			instrument_type,
			krx_verified_at,
			market,
			currency,
			created_at,
			updated_at
		FROM instruments
		ORDER BY name COLLATE NOCASE, ticker COLLATE NOCASE
	`)
	if err != nil {
		return nil, fmt.Errorf("list instruments: %w", err)
	}
	defer rows.Close()

	instruments := []models.Instrument{}
	for rows.Next() {
		instrument, err := scanInstrument(rows)
		if err != nil {
			return nil, fmt.Errorf("scan instrument: %w", err)
		}
		instruments = append(instruments, instrument)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate instruments: %w", err)
	}
	return instruments, nil
}

func (s *Store) DeleteInstrument(ctx context.Context, ticker string) (bool, error) {
	result, err := s.db.ExecContext(
		ctx,
		`DELETE FROM instruments WHERE ticker = ? COLLATE NOCASE OR yahoo_ticker = ? COLLATE NOCASE`,
		strings.TrimSpace(ticker),
		strings.TrimSpace(ticker),
	)
	if err != nil {
		return false, fmt.Errorf("delete instrument %q: %w", ticker, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read deleted instrument count: %w", err)
	}
	return affected > 0, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanInstrument(row scanner) (models.Instrument, error) {
	var instrument models.Instrument
	var krxVerifiedAt string
	var createdAt string
	var updatedAt string
	if err := row.Scan(
		&instrument.ID,
		&instrument.Name,
		&instrument.Ticker,
		&instrument.YahooTicker,
		&instrument.DARTCorpCode,
		&instrument.KRXStandardCode,
		&instrument.InstrumentType,
		&krxVerifiedAt,
		&instrument.Market,
		&instrument.Currency,
		&createdAt,
		&updatedAt,
	); err != nil {
		return models.Instrument{}, err
	}

	var err error
	if krxVerifiedAt != "" {
		verifiedAt, err := parseTime(krxVerifiedAt)
		if err != nil {
			return models.Instrument{}, fmt.Errorf("parse instrument krx_verified_at: %w", err)
		}
		instrument.KRXVerifiedAt = &verifiedAt
	}
	instrument.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return models.Instrument{}, fmt.Errorf("parse instrument created_at: %w", err)
	}
	instrument.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return models.Instrument{}, fmt.Errorf("parse instrument updated_at: %w", err)
	}
	return instrument, nil
}

func validateWatchlistItem(item models.WatchlistItem) error {
	required := map[string]string{
		"name":         item.Name,
		"ticker":       item.Ticker,
		"yahoo_ticker": item.YahooTicker,
		"market":       item.Market,
		"currency":     item.Currency,
	}
	for _, field := range []string{"name", "ticker", "yahoo_ticker", "market", "currency"} {
		if strings.TrimSpace(required[field]) == "" {
			return fmt.Errorf("instrument %s is required", field)
		}
	}
	return nil
}

func parseTime(value string) (time.Time, error) {
	return time.Parse(timeFormat, value)
}
