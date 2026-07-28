package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/decimal"
	"github.com/rowlet9g/stock-research-bot/internal/models"
)

func TestImportPositionRowsIsAtomicAndIdempotent(t *testing.T) {
	ctx := context.Background()
	fixedNow := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	store, err := openWithClock(
		ctx,
		filepath.Join(t.TempDir(), "forgetmenot.db"),
		func() time.Time { return fixedNow },
	)
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
	rows := []PositionImportRow{
		{
			RowNumber:        2,
			Ticker:           "AAPL",
			QuantityUnits:    2 * decimal.Scale,
			AverageCostUnits: 210 * decimal.Scale,
			Currency:         "USD",
			AsOf:             time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC),
		},
		{
			RowNumber:        3,
			Ticker:           "INTC",
			QuantityUnits:    decimal.Scale,
			AverageCostUnits: 30 * decimal.Scale,
			Currency:         "usd",
			AsOf:             time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC),
		},
	}
	first, err := store.ImportPositionRows(ctx, rows)
	if err != nil {
		t.Fatalf("import positions: %v", err)
	}
	if first.RowsSeen != 2 ||
		first.RowsChanged != 2 ||
		first.RowsUnchanged != 0 {
		t.Fatalf("unexpected first position import: %#v", first)
	}
	second, err := store.ImportPositionRows(ctx, rows)
	if err != nil {
		t.Fatalf("repeat position import: %v", err)
	}
	if second.RowsChanged != 0 || second.RowsUnchanged != 2 {
		t.Fatalf("position import was not idempotent: %#v", second)
	}
	intel, err := store.Portfolio(ctx, "INTC")
	if err != nil {
		t.Fatalf("query Intel portfolio: %v", err)
	}
	if intel.Position == nil ||
		intel.Position.Currency != "USD" ||
		intel.Position.UpdatedAt != fixedNow {
		t.Fatalf("unexpected imported position: %#v", intel.Position)
	}

	invalidRows := append([]PositionImportRow(nil), rows...)
	invalidRows[0].QuantityUnits = 3 * decimal.Scale
	invalidRows[1].Ticker = "MISSING"
	if _, err := store.ImportPositionRows(ctx, invalidRows); err == nil {
		t.Fatal("expected missing instrument error")
	}
	apple, err := store.Portfolio(ctx, "AAPL")
	if err != nil {
		t.Fatalf("query Apple portfolio: %v", err)
	}
	if apple.Position == nil ||
		apple.Position.QuantityUnits != 2*decimal.Scale {
		t.Fatalf("failed import changed prior rows: %#v", apple.Position)
	}
}

func TestParsePositionCSVRejectsDuplicateTicker(t *testing.T) {
	content := []byte(`ticker,quantity,average_cost,currency,as_of
AAPL,2,210.5,USD,2026-07-28
aapl,3,220,USD,2026-07-28
`)
	rows, err := parsePositionCSV(content, "positions.csv")
	if err != nil {
		t.Fatalf("parse position CSV: %v", err)
	}
	_, err = (&Store{}).ImportPositionRows(context.Background(), rows)
	if err == nil || !strings.Contains(err.Error(), "duplicate ticker") {
		t.Fatalf("unexpected duplicate ticker error: %v", err)
	}
}
