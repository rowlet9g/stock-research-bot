package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/models"
)

type ThesisInput struct {
	AllocationCategory     string
	ProtectedQuantityUnits int64
	Summary                string
	InvalidationCondition  string
	IncreaseCondition      string
	ExpectedHoldingPeriod  string
	CheckMetrics           []string
}

type ThesisSyncRow struct {
	RowNumber int
	Ticker    string
	Thesis    ThesisInput
}

type ThesisSyncResult struct {
	RowsSeen      int       `json:"rows_seen"`
	RowsChanged   int       `json:"rows_changed"`
	RowsUnchanged int       `json:"rows_unchanged"`
	SyncedAt      time.Time `json:"synced_at"`
}

func (s *Store) UpsertPosition(
	ctx context.Context,
	ticker string,
	quantityUnits int64,
	averageCostUnits int64,
	currency string,
	asOf time.Time,
) (models.Position, error) {
	if averageCostUnits < 0 {
		return models.Position{}, fmt.Errorf("average cost must not be negative")
	}
	if strings.TrimSpace(currency) == "" {
		return models.Position{}, fmt.Errorf("position currency is required")
	}
	if asOf.IsZero() {
		return models.Position{}, fmt.Errorf("position as-of time is required")
	}

	instrument, err := s.Instrument(ctx, ticker)
	if err != nil {
		return models.Position{}, err
	}
	updatedAt := s.now().UTC()
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO positions(
			instrument_id, quantity_units, average_cost_units, currency, as_of, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(instrument_id) DO UPDATE SET
			quantity_units = excluded.quantity_units,
			average_cost_units = excluded.average_cost_units,
			currency = excluded.currency,
			as_of = excluded.as_of,
			updated_at = excluded.updated_at
	`,
		instrument.ID,
		quantityUnits,
		averageCostUnits,
		strings.ToUpper(strings.TrimSpace(currency)),
		asOf.UTC().Format(timeFormat),
		updatedAt.Format(timeFormat),
	)
	if err != nil {
		return models.Position{}, fmt.Errorf("upsert position %q: %w", ticker, err)
	}
	return models.Position{
		InstrumentID:     instrument.ID,
		QuantityUnits:    quantityUnits,
		AverageCostUnits: averageCostUnits,
		Currency:         strings.ToUpper(strings.TrimSpace(currency)),
		AsOf:             asOf.UTC(),
		UpdatedAt:        updatedAt,
	}, nil
}

func (s *Store) UpsertThesis(
	ctx context.Context,
	ticker string,
	summary string,
	invalidationCondition string,
	expectedHoldingPeriod string,
	checkMetrics []string,
) (models.Thesis, error) {
	return s.UpsertThesisDetails(ctx, ticker, ThesisInput{
		Summary:               summary,
		InvalidationCondition: invalidationCondition,
		ExpectedHoldingPeriod: expectedHoldingPeriod,
		CheckMetrics:          checkMetrics,
	})
}

func (s *Store) UpsertThesisDetails(
	ctx context.Context,
	ticker string,
	input ThesisInput,
) (models.Thesis, error) {
	input, metricsJSON, err := prepareThesisInput(input)
	if err != nil {
		return models.Thesis{}, err
	}
	instrument, err := s.Instrument(ctx, ticker)
	if err != nil {
		return models.Thesis{}, err
	}
	now := s.now().UTC()

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO theses(
			instrument_id, allocation_category, protected_quantity_units, summary,
			invalidation_condition, increase_condition, expected_holding_period,
			check_metrics_json, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(instrument_id) DO UPDATE SET
			allocation_category = excluded.allocation_category,
			protected_quantity_units = excluded.protected_quantity_units,
			summary = excluded.summary,
			invalidation_condition = excluded.invalidation_condition,
			increase_condition = excluded.increase_condition,
			expected_holding_period = excluded.expected_holding_period,
			check_metrics_json = excluded.check_metrics_json,
			updated_at = excluded.updated_at
	`,
		instrument.ID,
		input.AllocationCategory,
		input.ProtectedQuantityUnits,
		input.Summary,
		input.InvalidationCondition,
		input.IncreaseCondition,
		input.ExpectedHoldingPeriod,
		metricsJSON,
		now.Format(timeFormat),
		now.Format(timeFormat),
	)
	if err != nil {
		return models.Thesis{}, fmt.Errorf("upsert thesis %q: %w", ticker, err)
	}
	return s.thesis(ctx, instrument.ID)
}

func (s *Store) SyncTheses(
	ctx context.Context,
	rows []ThesisSyncRow,
) (ThesisSyncResult, error) {
	result := ThesisSyncResult{
		RowsSeen: len(rows),
		SyncedAt: s.now().UTC(),
	}
	type preparedRow struct {
		row         ThesisSyncRow
		metricsJSON string
	}
	prepared := make([]preparedRow, 0, len(rows))
	seenTickers := make(map[string]struct{}, len(rows))
	for index, row := range rows {
		if row.RowNumber <= 0 {
			row.RowNumber = index + 1
		}
		row.Ticker = strings.TrimSpace(row.Ticker)
		key := strings.ToLower(row.Ticker)
		if key == "" {
			return result, fmt.Errorf("thesis row %d: ticker is required", row.RowNumber)
		}
		if _, exists := seenTickers[key]; exists {
			return result, fmt.Errorf(
				"thesis row %d: duplicate ticker %q",
				row.RowNumber,
				row.Ticker,
			)
		}
		seenTickers[key] = struct{}{}
		input, metricsJSON, err := prepareThesisInput(row.Thesis)
		if err != nil {
			return result, fmt.Errorf("thesis row %d: %w", row.RowNumber, err)
		}
		row.Thesis = input
		prepared = append(prepared, preparedRow{
			row:         row,
			metricsJSON: metricsJSON,
		})
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, fmt.Errorf("begin thesis sync: %w", err)
	}
	defer tx.Rollback()

	now := result.SyncedAt.Format(timeFormat)
	for _, item := range prepared {
		instrumentID, err := instrumentIDByTicker(ctx, tx, item.row.Ticker)
		if err != nil {
			return result, fmt.Errorf(
				"thesis row %d: %w",
				item.row.RowNumber,
				err,
			)
		}
		input := item.row.Thesis
		sqlResult, err := tx.ExecContext(ctx, `
			INSERT INTO theses(
				instrument_id, allocation_category, protected_quantity_units, summary,
				invalidation_condition, increase_condition, expected_holding_period,
				check_metrics_json, created_at, updated_at
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(instrument_id) DO UPDATE SET
				allocation_category = excluded.allocation_category,
				protected_quantity_units = excluded.protected_quantity_units,
				summary = excluded.summary,
				invalidation_condition = excluded.invalidation_condition,
				increase_condition = excluded.increase_condition,
				expected_holding_period = excluded.expected_holding_period,
				check_metrics_json = excluded.check_metrics_json,
				updated_at = excluded.updated_at
			WHERE theses.allocation_category <> excluded.allocation_category
			   OR theses.protected_quantity_units <> excluded.protected_quantity_units
			   OR theses.summary <> excluded.summary
			   OR theses.invalidation_condition <> excluded.invalidation_condition
			   OR theses.increase_condition <> excluded.increase_condition
			   OR theses.expected_holding_period <> excluded.expected_holding_period
			   OR theses.check_metrics_json <> excluded.check_metrics_json
		`,
			instrumentID,
			input.AllocationCategory,
			input.ProtectedQuantityUnits,
			input.Summary,
			input.InvalidationCondition,
			input.IncreaseCondition,
			input.ExpectedHoldingPeriod,
			item.metricsJSON,
			now,
			now,
		)
		if err != nil {
			return result, fmt.Errorf(
				"upsert thesis row %d ticker %q: %w",
				item.row.RowNumber,
				item.row.Ticker,
				err,
			)
		}
		affected, err := sqlResult.RowsAffected()
		if err != nil {
			return result, fmt.Errorf(
				"read thesis row %d result: %w",
				item.row.RowNumber,
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
		return result, fmt.Errorf("commit thesis sync: %w", err)
	}
	return result, nil
}

func (s *Store) Portfolio(ctx context.Context, ticker string) (models.PortfolioRecord, error) {
	instrument, err := s.Instrument(ctx, ticker)
	if err != nil {
		return models.PortfolioRecord{}, err
	}
	record := models.PortfolioRecord{
		Instrument: instrument,
		Trades:     []models.Trade{},
	}

	position, err := s.position(ctx, instrument.ID)
	if err != nil && err != sql.ErrNoRows {
		return models.PortfolioRecord{}, err
	}
	if err == nil {
		record.Position = &position
	}

	trades, err := listTrades(ctx, s.db, instrument.ID)
	if err != nil {
		return models.PortfolioRecord{}, err
	}
	record.Trades = trades

	thesis, err := s.thesis(ctx, instrument.ID)
	if err != nil && err != sql.ErrNoRows {
		return models.PortfolioRecord{}, err
	}
	if err == nil {
		record.Thesis = &thesis
	}
	return record, nil
}

func (s *Store) ListPortfolios(
	ctx context.Context,
) ([]models.PortfolioRecord, error) {
	instruments, err := s.ListInstruments(ctx)
	if err != nil {
		return nil, err
	}
	records := make([]models.PortfolioRecord, 0, len(instruments))
	for _, instrument := range instruments {
		record, err := s.Portfolio(ctx, instrument.Ticker)
		if err != nil {
			return nil, fmt.Errorf(
				"load portfolio %q: %w",
				instrument.Ticker,
				err,
			)
		}
		records = append(records, record)
	}
	return records, nil
}

func (s *Store) position(ctx context.Context, instrumentID int64) (models.Position, error) {
	var position models.Position
	var asOf string
	var updatedAt string
	err := s.db.QueryRowContext(ctx, `
		SELECT instrument_id, quantity_units, average_cost_units, currency, as_of, updated_at
		FROM positions
		WHERE instrument_id = ?
	`, instrumentID).Scan(
		&position.InstrumentID,
		&position.QuantityUnits,
		&position.AverageCostUnits,
		&position.Currency,
		&asOf,
		&updatedAt,
	)
	if err != nil {
		return models.Position{}, err
	}
	position.AsOf, err = parseTime(asOf)
	if err != nil {
		return models.Position{}, fmt.Errorf("parse position as_of: %w", err)
	}
	position.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return models.Position{}, fmt.Errorf("parse position updated_at: %w", err)
	}
	return position, nil
}

func (s *Store) thesis(ctx context.Context, instrumentID int64) (models.Thesis, error) {
	var thesis models.Thesis
	var metricsJSON string
	var createdAt string
	var updatedAt string
	err := s.db.QueryRowContext(ctx, `
		SELECT instrument_id, allocation_category, protected_quantity_units, summary,
			invalidation_condition, increase_condition, expected_holding_period,
			check_metrics_json, created_at, updated_at
		FROM theses
		WHERE instrument_id = ?
	`, instrumentID).Scan(
		&thesis.InstrumentID,
		&thesis.AllocationCategory,
		&thesis.ProtectedQuantityUnits,
		&thesis.Summary,
		&thesis.InvalidationCondition,
		&thesis.IncreaseCondition,
		&thesis.ExpectedHoldingPeriod,
		&metricsJSON,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		return models.Thesis{}, err
	}
	if err := json.Unmarshal([]byte(metricsJSON), &thesis.CheckMetrics); err != nil {
		return models.Thesis{}, fmt.Errorf("decode thesis metrics: %w", err)
	}
	thesis.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return models.Thesis{}, fmt.Errorf("parse thesis created_at: %w", err)
	}
	thesis.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return models.Thesis{}, fmt.Errorf("parse thesis updated_at: %w", err)
	}
	return thesis, nil
}

func prepareThesisInput(input ThesisInput) (ThesisInput, string, error) {
	input.AllocationCategory = strings.ToLower(
		strings.TrimSpace(input.AllocationCategory),
	)
	input.Summary = strings.TrimSpace(input.Summary)
	input.InvalidationCondition = strings.TrimSpace(input.InvalidationCondition)
	input.IncreaseCondition = strings.TrimSpace(input.IncreaseCondition)
	input.ExpectedHoldingPeriod = strings.TrimSpace(input.ExpectedHoldingPeriod)
	input.CheckMetrics = normalizeMetrics(input.CheckMetrics)
	if input.Summary == "" {
		return ThesisInput{}, "", fmt.Errorf("thesis summary is required")
	}
	if input.ProtectedQuantityUnits < 0 {
		return ThesisInput{}, "", fmt.Errorf(
			"thesis protected quantity must not be negative",
		)
	}
	metricsJSON, err := json.Marshal(input.CheckMetrics)
	if err != nil {
		return ThesisInput{}, "", fmt.Errorf("encode thesis metrics: %w", err)
	}
	return input, string(metricsJSON), nil
}

func normalizeMetrics(metrics []string) []string {
	normalized := make([]string, 0, len(metrics))
	seen := map[string]struct{}{}
	for _, metric := range metrics {
		metric = strings.TrimSpace(metric)
		if metric == "" {
			continue
		}
		key := strings.ToLower(metric)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, metric)
	}
	return normalized
}
