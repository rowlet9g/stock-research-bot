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
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return models.Thesis{}, fmt.Errorf("thesis summary is required")
	}

	instrument, err := s.Instrument(ctx, ticker)
	if err != nil {
		return models.Thesis{}, err
	}
	metrics := normalizeMetrics(checkMetrics)
	metricsJSON, err := json.Marshal(metrics)
	if err != nil {
		return models.Thesis{}, fmt.Errorf("encode thesis metrics: %w", err)
	}
	now := s.now().UTC()

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO theses(
			instrument_id, summary, invalidation_condition, expected_holding_period,
			check_metrics_json, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(instrument_id) DO UPDATE SET
			summary = excluded.summary,
			invalidation_condition = excluded.invalidation_condition,
			expected_holding_period = excluded.expected_holding_period,
			check_metrics_json = excluded.check_metrics_json,
			updated_at = excluded.updated_at
	`,
		instrument.ID,
		summary,
		strings.TrimSpace(invalidationCondition),
		strings.TrimSpace(expectedHoldingPeriod),
		string(metricsJSON),
		now.Format(timeFormat),
		now.Format(timeFormat),
	)
	if err != nil {
		return models.Thesis{}, fmt.Errorf("upsert thesis %q: %w", ticker, err)
	}
	return s.thesis(ctx, instrument.ID)
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
		SELECT instrument_id, summary, invalidation_condition, expected_holding_period,
			check_metrics_json, created_at, updated_at
		FROM theses
		WHERE instrument_id = ?
	`, instrumentID).Scan(
		&thesis.InstrumentID,
		&thesis.Summary,
		&thesis.InvalidationCondition,
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
