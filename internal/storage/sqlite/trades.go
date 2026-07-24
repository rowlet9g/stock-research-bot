package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/models"
)

type TradeInput struct {
	ExternalID     string
	TradeDate      time.Time
	Action         string
	QuantityUnits  int64
	PriceUnits     int64
	FeesUnits      int64
	TaxesUnits     int64
	PriceSource    string
	TaxesKnown     bool
	TimePrecision  string
	Currency       string
	Source         string
	IdempotencyKey string
}

func (s *Store) AddTrade(ctx context.Context, ticker string, input TradeInput) (bool, error) {
	if err := validateTradeInput(input); err != nil {
		return false, err
	}
	instrument, err := s.Instrument(ctx, ticker)
	if err != nil {
		return false, err
	}
	return insertTrade(ctx, s.db, instrument.ID, input, s.now().UTC())
}

func insertTrade(
	ctx context.Context,
	executor interface {
		ExecContext(context.Context, string, ...any) (sql.Result, error)
	},
	instrumentID int64,
	input TradeInput,
	createdAt time.Time,
) (bool, error) {
	result, err := executor.ExecContext(ctx, `
		INSERT INTO trades(
			instrument_id, external_id, trade_date, action, quantity_units, price_units,
			fees_units, taxes_units, price_source, taxes_known, time_precision,
			currency, source, idempotency_key, created_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(idempotency_key) DO NOTHING
	`,
		instrumentID,
		strings.TrimSpace(input.ExternalID),
		input.TradeDate.UTC().Format(timeFormat),
		strings.ToUpper(strings.TrimSpace(input.Action)),
		input.QuantityUnits,
		input.PriceUnits,
		input.FeesUnits,
		input.TaxesUnits,
		input.PriceSource,
		input.TaxesKnown,
		input.TimePrecision,
		strings.ToUpper(strings.TrimSpace(input.Currency)),
		strings.TrimSpace(input.Source),
		input.IdempotencyKey,
		createdAt.Format(timeFormat),
	)
	if err != nil {
		return false, fmt.Errorf("insert trade: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read inserted trade count: %w", err)
	}
	return affected > 0, nil
}

func validateTradeInput(input TradeInput) error {
	action := strings.ToUpper(strings.TrimSpace(input.Action))
	if action != "BUY" && action != "SELL" {
		return fmt.Errorf("trade action must be BUY or SELL")
	}
	if input.TradeDate.IsZero() {
		return fmt.Errorf("trade date is required")
	}
	if input.QuantityUnits <= 0 {
		return fmt.Errorf("trade quantity must be greater than zero")
	}
	if input.PriceUnits < 0 || input.FeesUnits < 0 || input.TaxesUnits < 0 {
		return fmt.Errorf("trade price, fees, and taxes must not be negative")
	}
	if input.PriceSource != "reported" && input.PriceSource != "derived_amount_div_quantity" {
		return fmt.Errorf("trade price source is invalid")
	}
	if input.TimePrecision != "day" && input.TimePrecision != "second" {
		return fmt.Errorf("trade time precision is invalid")
	}
	if strings.TrimSpace(input.Currency) == "" {
		return fmt.Errorf("trade currency is required")
	}
	if strings.TrimSpace(input.Source) == "" {
		return fmt.Errorf("trade source is required")
	}
	if strings.TrimSpace(input.IdempotencyKey) == "" {
		return fmt.Errorf("trade idempotency key is required")
	}
	return nil
}

func listTrades(ctx context.Context, db *sql.DB, instrumentID int64) ([]models.Trade, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, instrument_id, external_id, trade_date, action, quantity_units, price_units,
			fees_units, taxes_units, price_source, taxes_known, time_precision,
			currency, source, idempotency_key, created_at
		FROM trades
		WHERE instrument_id = ?
		ORDER BY trade_date DESC, id DESC
	`, instrumentID)
	if err != nil {
		return nil, fmt.Errorf("list trades: %w", err)
	}
	defer rows.Close()

	trades := []models.Trade{}
	for rows.Next() {
		var trade models.Trade
		var tradeDate string
		var createdAt string
		if err := rows.Scan(
			&trade.ID,
			&trade.InstrumentID,
			&trade.ExternalID,
			&tradeDate,
			&trade.Action,
			&trade.QuantityUnits,
			&trade.PriceUnits,
			&trade.FeesUnits,
			&trade.TaxesUnits,
			&trade.PriceSource,
			&trade.TaxesKnown,
			&trade.TimePrecision,
			&trade.Currency,
			&trade.Source,
			&trade.IdempotencyKey,
			&createdAt,
		); err != nil {
			return nil, fmt.Errorf("scan trade: %w", err)
		}
		trade.TradeDate, err = parseTime(tradeDate)
		if err != nil {
			return nil, fmt.Errorf("parse trade_date: %w", err)
		}
		trade.CreatedAt, err = parseTime(createdAt)
		if err != nil {
			return nil, fmt.Errorf("parse trade created_at: %w", err)
		}
		trades = append(trades, trade)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate trades: %w", err)
	}
	return trades, nil
}
