package sqlite

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/models"
)

var validKRXDatasets = map[string]struct{}{
	"kospi":  {},
	"kosdaq": {},
	"konex":  {},
	"etf":    {},
	"etn":    {},
}

var krxDatasetMarkets = map[string]string{
	"kospi":  "KOSPI",
	"kosdaq": "KOSDAQ",
	"konex":  "KONEX",
	"etf":    "KRX",
	"etn":    "KRX",
}

type KRXInstrumentSyncResult struct {
	Dataset             string    `json:"dataset"`
	InstrumentsSeen     int       `json:"instruments_seen"`
	Inactive            int       `json:"inactive"`
	InstrumentsMapped   int       `json:"instruments_mapped"`
	InstrumentsUnmapped int       `json:"instruments_unmapped"`
	DARTMapped          int       `json:"dart_mapped"`
	DARTConflicts       int       `json:"dart_conflicts"`
	SyncedAt            time.Time `json:"synced_at"`
}

func (s *Store) SyncKRXInstruments(
	ctx context.Context,
	dataset string,
	instruments []models.KRXInstrument,
) (KRXInstrumentSyncResult, error) {
	dataset = strings.ToLower(strings.TrimSpace(dataset))
	result := KRXInstrumentSyncResult{
		Dataset:         dataset,
		InstrumentsSeen: len(instruments),
		SyncedAt:        s.now().UTC(),
	}
	if err := validateKRXInstruments(dataset, instruments); err != nil {
		return result, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, fmt.Errorf("begin KRX instrument sync for %s: %w", dataset, err)
	}
	defer tx.Rollback()

	now := result.SyncedAt.Format(timeFormat)
	if _, err := tx.ExecContext(
		ctx,
		`UPDATE krx_instruments SET is_current = 0, updated_at = ? WHERE dataset = ? AND is_current = 1`,
		now,
		dataset,
	); err != nil {
		return result, fmt.Errorf("mark previous KRX %s instruments inactive: %w", dataset, err)
	}

	statement, err := tx.PrepareContext(ctx, `
		INSERT INTO krx_instruments(
			short_code,
			standard_code,
			name,
			abbreviated_name,
			english_name,
			market,
			security_group,
			section,
			share_type,
			instrument_type,
			listing_date,
			dataset,
			source_url,
			observed_at,
			fetched_at,
			is_current,
			created_at,
			updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)
		ON CONFLICT(short_code) DO UPDATE SET
			standard_code = excluded.standard_code,
			name = excluded.name,
			abbreviated_name = excluded.abbreviated_name,
			english_name = excluded.english_name,
			market = excluded.market,
			security_group = excluded.security_group,
			section = excluded.section,
			share_type = excluded.share_type,
			instrument_type = excluded.instrument_type,
			listing_date = excluded.listing_date,
			dataset = excluded.dataset,
			source_url = excluded.source_url,
			observed_at = excluded.observed_at,
			fetched_at = excluded.fetched_at,
			is_current = 1,
			updated_at = excluded.updated_at
	`)
	if err != nil {
		return result, fmt.Errorf("prepare KRX %s instrument upsert: %w", dataset, err)
	}
	defer statement.Close()

	for _, instrument := range instruments {
		listingDate := ""
		if instrument.ListingDate != nil {
			listingDate = instrument.ListingDate.UTC().Format("2006-01-02")
		}
		observedAt := instrument.Source.ObservedAt
		if observedAt == nil {
			return result, fmt.Errorf(
				"KRX %s instrument %q has no observed time",
				dataset,
				instrument.ShortCode,
			)
		}
		if _, err := statement.ExecContext(
			ctx,
			instrument.ShortCode,
			instrument.StandardCode,
			instrument.Name,
			instrument.AbbreviatedName,
			instrument.EnglishName,
			instrument.Market,
			instrument.SecurityGroup,
			instrument.Section,
			instrument.ShareType,
			string(instrument.InstrumentType),
			listingDate,
			dataset,
			instrument.Source.SourceURL,
			observedAt.UTC().Format(timeFormat),
			instrument.Source.FetchedAt.UTC().Format(timeFormat),
			now,
			now,
		); err != nil {
			return result, fmt.Errorf(
				"upsert KRX %s instrument %q: %w",
				dataset,
				instrument.ShortCode,
				err,
			)
		}
	}

	if err := tx.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM krx_instruments WHERE dataset = ? AND is_current = 0`,
		dataset,
	).Scan(&result.Inactive); err != nil {
		return result, fmt.Errorf("count inactive KRX %s instruments: %w", dataset, err)
	}

	unmappingResult, err := tx.ExecContext(ctx, `
		UPDATE instruments
		SET
			krx_standard_code = '',
			instrument_type = 'unknown',
			krx_verified_at = '',
			updated_at = ?
		WHERE currency COLLATE NOCASE = 'KRW'
		  AND EXISTS (
				SELECT 1
				FROM krx_instruments AS krx
				WHERE krx.short_code = upper(instruments.ticker)
				  AND krx.is_current = 0
		  )
		  AND NOT EXISTS (
				SELECT 1
				FROM krx_instruments AS krx
				WHERE krx.short_code = upper(instruments.ticker)
				  AND krx.is_current = 1
		  )
		  AND (
				krx_standard_code <> ''
				OR instrument_type <> 'unknown'
				OR krx_verified_at <> ''
		  )
	`, now)
	if err != nil {
		return result, fmt.Errorf("clear inactive KRX %s instrument mappings: %w", dataset, err)
	}
	result.InstrumentsUnmapped, err = rowsAffectedInt(unmappingResult)
	if err != nil {
		return result, fmt.Errorf("read KRX %s unmapped instrument count: %w", dataset, err)
	}

	mappingResult, err := tx.ExecContext(ctx, `
		UPDATE instruments
		SET
			krx_standard_code = (
				SELECT krx.standard_code
				FROM krx_instruments AS krx
				WHERE krx.is_current = 1
				  AND krx.short_code = upper(instruments.ticker)
				LIMIT 1
			),
			instrument_type = (
				SELECT krx.instrument_type
				FROM krx_instruments AS krx
				WHERE krx.is_current = 1
				  AND krx.short_code = upper(instruments.ticker)
				LIMIT 1
			),
			krx_verified_at = ?,
			updated_at = ?
		WHERE currency COLLATE NOCASE = 'KRW'
		  AND EXISTS (
				SELECT 1
				FROM krx_instruments AS krx
				WHERE krx.is_current = 1
				  AND krx.short_code = upper(instruments.ticker)
		  )
		  AND (
				krx_standard_code <> (
					SELECT krx.standard_code
					FROM krx_instruments AS krx
					WHERE krx.is_current = 1
					  AND krx.short_code = upper(instruments.ticker)
					LIMIT 1
				)
				OR instrument_type <> (
					SELECT krx.instrument_type
					FROM krx_instruments AS krx
					WHERE krx.is_current = 1
					  AND krx.short_code = upper(instruments.ticker)
					LIMIT 1
				)
				OR krx_verified_at = ''
		  )
	`, now, now)
	if err != nil {
		return result, fmt.Errorf("map stored instruments to KRX %s identities: %w", dataset, err)
	}
	result.InstrumentsMapped, err = rowsAffectedInt(mappingResult)
	if err != nil {
		return result, fmt.Errorf("read KRX %s mapped instrument count: %w", dataset, err)
	}

	dartMappingResult, err := tx.ExecContext(ctx, `
		UPDATE instruments
		SET
			dart_corp_code = (
				SELECT dart.corp_code
				FROM dart_corporations AS dart
				JOIN krx_instruments AS krx
				  ON krx.short_code = dart.stock_code
				WHERE dart.is_current = 1
				  AND krx.is_current = 1
				  AND krx.instrument_type = 'common_stock'
				  AND krx.short_code = upper(instruments.ticker)
				LIMIT 1
			),
			updated_at = ?
		WHERE currency COLLATE NOCASE = 'KRW'
		  AND EXISTS (
				SELECT 1
				FROM dart_corporations AS dart
				JOIN krx_instruments AS krx
				  ON krx.short_code = dart.stock_code
				WHERE dart.is_current = 1
				  AND krx.is_current = 1
				  AND krx.instrument_type = 'common_stock'
				  AND krx.short_code = upper(instruments.ticker)
		  )
		  AND dart_corp_code <> (
				SELECT dart.corp_code
				FROM dart_corporations AS dart
				JOIN krx_instruments AS krx
				  ON krx.short_code = dart.stock_code
				WHERE dart.is_current = 1
				  AND krx.is_current = 1
				  AND krx.instrument_type = 'common_stock'
				  AND krx.short_code = upper(instruments.ticker)
				LIMIT 1
		  )
	`, now)
	if err != nil {
		return result, fmt.Errorf("cross-map KRX %s and OpenDART identities: %w", dataset, err)
	}
	result.DARTMapped, err = rowsAffectedInt(dartMappingResult)
	if err != nil {
		return result, fmt.Errorf("read KRX %s DART mapped count: %w", dataset, err)
	}

	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM instruments AS instrument
		JOIN krx_instruments AS krx
		  ON krx.is_current = 1
		 AND krx.short_code = upper(instrument.ticker)
		JOIN dart_corporations AS dart
		  ON dart.is_current = 1
		 AND dart.corp_code = instrument.dart_corp_code
		WHERE instrument.currency COLLATE NOCASE = 'KRW'
		  AND krx.instrument_type = 'common_stock'
		  AND dart.stock_code <> krx.short_code
	`).Scan(&result.DARTConflicts); err != nil {
		return result, fmt.Errorf("count KRX and OpenDART identifier conflicts: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return result, fmt.Errorf("commit KRX %s instrument sync: %w", dataset, err)
	}
	return result, nil
}

func validateKRXInstruments(dataset string, instruments []models.KRXInstrument) error {
	if _, ok := validKRXDatasets[dataset]; !ok {
		return fmt.Errorf("unsupported KRX dataset %q", dataset)
	}
	if len(instruments) == 0 {
		return fmt.Errorf("KRX %s instrument list is empty", dataset)
	}

	seenShortCodes := make(map[string]struct{}, len(instruments))
	seenStandardCodes := make(map[string]struct{}, len(instruments))
	var snapshotObservedAt time.Time
	snapshotSourceURL := ""
	for index, instrument := range instruments {
		isStockDataset := dataset == "kospi" ||
			dataset == "kosdaq" ||
			dataset == "konex"
		switch {
		case instrument.Dataset != dataset:
			return fmt.Errorf(
				"KRX %s instrument %d belongs to dataset %q",
				dataset,
				index+1,
				instrument.Dataset,
			)
		case !fixedUpperAlphanumeric(instrument.ShortCode, 6):
			return fmt.Errorf(
				"KRX %s instrument %d has invalid short code %q",
				dataset,
				index+1,
				instrument.ShortCode,
			)
		case instrument.StandardCode != "" &&
			!fixedUpperAlphanumeric(instrument.StandardCode, 12):
			return fmt.Errorf(
				"KRX %s instrument %d has invalid standard code %q",
				dataset,
				index+1,
				instrument.StandardCode,
			)
		case strings.TrimSpace(instrument.Name) == "":
			return fmt.Errorf("KRX %s instrument %d has no name", dataset, index+1)
		case strings.ToUpper(strings.TrimSpace(instrument.Market)) !=
			krxDatasetMarkets[dataset]:
			return fmt.Errorf(
				"KRX %s instrument %d has market %q",
				dataset,
				index+1,
				instrument.Market,
			)
		case isStockDataset && instrument.StandardCode == "":
			return fmt.Errorf(
				"KRX %s instrument %d has no standard code",
				dataset,
				index+1,
			)
		case isStockDataset && instrument.ListingDate == nil:
			return fmt.Errorf(
				"KRX %s instrument %d has no listing date",
				dataset,
				index+1,
			)
		case (dataset == "etf" && instrument.InstrumentType != models.InstrumentTypeETF) ||
			(dataset == "etn" && instrument.InstrumentType != models.InstrumentTypeETN) ||
			(isStockDataset &&
				(instrument.InstrumentType == models.InstrumentTypeETF ||
					instrument.InstrumentType == models.InstrumentTypeETN)):
			return fmt.Errorf(
				"KRX %s instrument %d has type %q inconsistent with its dataset",
				dataset,
				index+1,
				instrument.InstrumentType,
			)
		case !validInstrumentType(instrument.InstrumentType):
			return fmt.Errorf(
				"KRX %s instrument %d has invalid instrument type %q",
				dataset,
				index+1,
				instrument.InstrumentType,
			)
		case instrument.Source.Provider != "krx":
			return fmt.Errorf(
				"KRX %s instrument %d has unexpected provider %q",
				dataset,
				index+1,
				instrument.Source.Provider,
			)
		case strings.TrimSpace(instrument.Source.SourceURL) == "":
			return fmt.Errorf("KRX %s instrument %d has no source URL", dataset, index+1)
		case instrument.Source.ObservedAt == nil:
			return fmt.Errorf("KRX %s instrument %d has no observed time", dataset, index+1)
		case instrument.Source.FetchedAt.IsZero():
			return fmt.Errorf("KRX %s instrument %d has no fetched time", dataset, index+1)
		}
		if index == 0 {
			snapshotObservedAt = instrument.Source.ObservedAt.UTC()
			snapshotSourceURL = instrument.Source.SourceURL
		} else if !instrument.Source.ObservedAt.UTC().Equal(snapshotObservedAt) ||
			instrument.Source.SourceURL != snapshotSourceURL {
			return fmt.Errorf(
				"KRX %s instrument %d belongs to a different source snapshot",
				dataset,
				index+1,
			)
		}
		if _, exists := seenShortCodes[instrument.ShortCode]; exists {
			return fmt.Errorf(
				"KRX %s instrument %d duplicates short code %q",
				dataset,
				index+1,
				instrument.ShortCode,
			)
		}
		seenShortCodes[instrument.ShortCode] = struct{}{}
		if instrument.StandardCode != "" {
			if _, exists := seenStandardCodes[instrument.StandardCode]; exists {
				return fmt.Errorf(
					"KRX %s instrument %d duplicates standard code %q",
					dataset,
					index+1,
					instrument.StandardCode,
				)
			}
			seenStandardCodes[instrument.StandardCode] = struct{}{}
		}
	}
	return nil
}

func validInstrumentType(value models.InstrumentType) bool {
	switch value {
	case models.InstrumentTypeCommonStock,
		models.InstrumentTypePreferredStock,
		models.InstrumentTypeETF,
		models.InstrumentTypeETN,
		models.InstrumentTypeOtherEquity:
		return true
	default:
		return false
	}
}
