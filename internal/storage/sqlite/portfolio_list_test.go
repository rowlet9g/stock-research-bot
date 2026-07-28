package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/decimal"
	"github.com/rowlet9g/stock-research-bot/internal/models"
)

func TestListPortfoliosIncludesTradesAndOptionalPosition(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "forgetmenot.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	if _, err := store.SyncInstruments(ctx, []models.WatchlistItem{
		{
			Name:        "Apple",
			Ticker:      "AAPL",
			YahooTicker: "AAPL",
			Market:      "NASDAQ",
			Currency:    "USD",
		},
		{
			Name:        "Intel",
			Ticker:      "INTC",
			YahooTicker: "INTC",
			Market:      "NASDAQ",
			Currency:    "USD",
		},
	}); err != nil {
		t.Fatalf("seed instruments: %v", err)
	}
	if _, err := store.AddTrade(ctx, "AAPL", TradeInput{
		ExternalID:     "AAPL-1",
		TradeDate:      time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		Action:         "BUY",
		QuantityUnits:  2 * decimal.Scale,
		PriceUnits:     200 * decimal.Scale,
		PriceSource:    "reported",
		TaxesKnown:     true,
		TimePrecision:  "day",
		Currency:       "USD",
		Source:         "test",
		IdempotencyKey: "AAPL-1",
	}); err != nil {
		t.Fatalf("seed trade: %v", err)
	}
	if _, err := store.UpsertPosition(
		ctx,
		"INTC",
		decimal.Scale,
		30*decimal.Scale,
		"USD",
		time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC),
	); err != nil {
		t.Fatalf("seed position: %v", err)
	}

	records, err := store.ListPortfolios(ctx)
	if err != nil {
		t.Fatalf("list portfolios: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("unexpected portfolio count: %#v", records)
	}
	byTicker := map[string]models.PortfolioRecord{}
	for _, record := range records {
		byTicker[record.Instrument.Ticker] = record
	}
	if len(byTicker["AAPL"].Trades) != 1 ||
		byTicker["AAPL"].Position != nil ||
		byTicker["INTC"].Position == nil ||
		byTicker["INTC"].Position.QuantityUnits != decimal.Scale {
		t.Fatalf("unexpected portfolio records: %#v", records)
	}
}
