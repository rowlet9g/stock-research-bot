package sqlite

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/models"
)

type DARTCorporationSyncResult struct {
	CorporationsSeen  int       `json:"corporations_seen"`
	Listed            int       `json:"listed"`
	Inactive          int       `json:"inactive"`
	InstrumentsMapped int       `json:"instruments_mapped"`
	SyncedAt          time.Time `json:"synced_at"`
}

func (s *Store) SyncDARTCorporations(
	ctx context.Context,
	corporations []models.DARTCorporation,
) (DARTCorporationSyncResult, error) {
	result := DARTCorporationSyncResult{
		CorporationsSeen: len(corporations),
		SyncedAt:         s.now().UTC(),
	}
	if err := validateDARTCorporations(corporations); err != nil {
		return result, err
	}
	for _, corporation := range corporations {
		if corporation.StockCode != "" {
			result.Listed++
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, fmt.Errorf("begin OpenDART corporation sync: %w", err)
	}
	defer tx.Rollback()

	now := result.SyncedAt.Format(timeFormat)
	if _, err := tx.ExecContext(
		ctx,
		`UPDATE dart_corporations SET is_current = 0, updated_at = ? WHERE is_current = 1`,
		now,
	); err != nil {
		return result, fmt.Errorf("mark previous OpenDART corporations inactive: %w", err)
	}

	statement, err := tx.PrepareContext(ctx, `
		INSERT INTO dart_corporations(
			corp_code,
			corp_name,
			corp_english_name,
			stock_code,
			modify_date,
			source_url,
			observed_at,
			fetched_at,
			is_current,
			created_at,
			updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)
		ON CONFLICT(corp_code) DO UPDATE SET
			corp_name = excluded.corp_name,
			corp_english_name = excluded.corp_english_name,
			stock_code = excluded.stock_code,
			modify_date = excluded.modify_date,
			source_url = excluded.source_url,
			observed_at = excluded.observed_at,
			fetched_at = excluded.fetched_at,
			is_current = 1,
			updated_at = excluded.updated_at
	`)
	if err != nil {
		return result, fmt.Errorf("prepare OpenDART corporation upsert: %w", err)
	}
	defer statement.Close()

	for _, corporation := range corporations {
		observedAt := corporation.ModifiedAt.UTC()
		if corporation.Source.ObservedAt != nil {
			observedAt = corporation.Source.ObservedAt.UTC()
		}
		if _, err := statement.ExecContext(
			ctx,
			corporation.CorpCode,
			corporation.Name,
			corporation.EnglishName,
			corporation.StockCode,
			corporation.ModifiedAt.UTC().Format("2006-01-02"),
			corporation.Source.SourceURL,
			observedAt.Format(timeFormat),
			corporation.Source.FetchedAt.UTC().Format(timeFormat),
			now,
			now,
		); err != nil {
			return result, fmt.Errorf(
				"upsert OpenDART corporation %q: %w",
				corporation.CorpCode,
				err,
			)
		}
	}

	if err := tx.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM dart_corporations WHERE is_current = 0`,
	).Scan(&result.Inactive); err != nil {
		return result, fmt.Errorf("count inactive OpenDART corporations: %w", err)
	}

	mappingResult, err := tx.ExecContext(ctx, `
		UPDATE instruments
		SET
			dart_corp_code = (
				SELECT corporation.corp_code
				FROM dart_corporations AS corporation
				WHERE corporation.is_current = 1
				  AND corporation.stock_code = upper(instruments.ticker)
				LIMIT 1
			),
			updated_at = ?
		WHERE currency COLLATE NOCASE = 'KRW'
		  AND length(ticker) = 6
		  AND upper(ticker) NOT GLOB '*[^0-9A-Z]*'
		  AND (
				NOT EXISTS (
					SELECT 1
					FROM krx_instruments AS krx
					WHERE krx.is_current = 1
					  AND krx.short_code = upper(instruments.ticker)
				)
				OR EXISTS (
					SELECT 1
					FROM krx_instruments AS krx
					WHERE krx.is_current = 1
					  AND krx.short_code = upper(instruments.ticker)
					  AND krx.instrument_type = 'common_stock'
				)
		  )
		  AND EXISTS (
				SELECT 1
				FROM dart_corporations AS corporation
				WHERE corporation.is_current = 1
				  AND corporation.stock_code = upper(instruments.ticker)
		  )
		  AND dart_corp_code <> (
				SELECT corporation.corp_code
				FROM dart_corporations AS corporation
				WHERE corporation.is_current = 1
				  AND corporation.stock_code = upper(instruments.ticker)
				LIMIT 1
		  )
	`, now)
	if err != nil {
		return result, fmt.Errorf("map instruments to OpenDART corporations: %w", err)
	}
	result.InstrumentsMapped, err = rowsAffectedInt(mappingResult)
	if err != nil {
		return result, fmt.Errorf("read mapped instrument count: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return result, fmt.Errorf("commit OpenDART corporation sync: %w", err)
	}
	return result, nil
}

func validateDARTCorporations(corporations []models.DARTCorporation) error {
	if len(corporations) == 0 {
		return fmt.Errorf("OpenDART corporation list is empty")
	}
	seenCorpCodes := make(map[string]struct{}, len(corporations))
	seenStockCodes := make(map[string]struct{}, len(corporations))
	for index, corporation := range corporations {
		switch {
		case !fixedDigits(corporation.CorpCode, 8):
			return fmt.Errorf(
				"OpenDART corporation %d has invalid corporation code %q",
				index+1,
				corporation.CorpCode,
			)
		case strings.TrimSpace(corporation.Name) == "":
			return fmt.Errorf("OpenDART corporation %d has no name", index+1)
		case corporation.StockCode != "" &&
			!fixedUpperAlphanumeric(corporation.StockCode, 6):
			return fmt.Errorf(
				"OpenDART corporation %d has invalid stock code %q",
				index+1,
				corporation.StockCode,
			)
		case corporation.ModifiedAt.IsZero():
			return fmt.Errorf("OpenDART corporation %d has no modify date", index+1)
		case corporation.Source.Provider != "opendart":
			return fmt.Errorf(
				"OpenDART corporation %d has unexpected provider %q",
				index+1,
				corporation.Source.Provider,
			)
		case strings.TrimSpace(corporation.Source.SourceURL) == "":
			return fmt.Errorf("OpenDART corporation %d has no source URL", index+1)
		case corporation.Source.FetchedAt.IsZero():
			return fmt.Errorf("OpenDART corporation %d has no fetched time", index+1)
		}
		if _, exists := seenCorpCodes[corporation.CorpCode]; exists {
			return fmt.Errorf(
				"OpenDART corporation %d duplicates corporation code %q",
				index+1,
				corporation.CorpCode,
			)
		}
		seenCorpCodes[corporation.CorpCode] = struct{}{}
		if corporation.StockCode != "" {
			if _, exists := seenStockCodes[corporation.StockCode]; exists {
				return fmt.Errorf(
					"OpenDART corporation %d duplicates stock code %q",
					index+1,
					corporation.StockCode,
				)
			}
			seenStockCodes[corporation.StockCode] = struct{}{}
		}
	}
	return nil
}

func fixedDigits(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func fixedUpperAlphanumeric(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') &&
			(character < 'A' || character > 'Z') {
			return false
		}
	}
	return true
}

type rowsAffectedResult interface {
	RowsAffected() (int64, error)
}

func rowsAffectedInt(result rowsAffectedResult) (int, error) {
	count, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(count), nil
}
