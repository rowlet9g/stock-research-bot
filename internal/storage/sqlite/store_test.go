package sqlite

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/decimal"
	"github.com/rowlet9g/stock-research-bot/internal/models"
)

func TestStorePersistsPortfolioAndDeduplicatesTradeImport(t *testing.T) {
	context := context.Background()
	databasePath := filepath.Join(t.TempDir(), "forgetmenot.db")
	fixedNow := time.Date(2026, 7, 24, 9, 30, 0, 0, time.UTC)

	store, err := openWithClock(context, databasePath, func() time.Time { return fixedNow })
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	items := []models.WatchlistItem{
		{
			Name:         "삼성전자",
			Ticker:       "005930",
			YahooTicker:  "005930.KS",
			DARTCorpCode: "00126380",
			Market:       "KOSPI",
			Currency:     "KRW",
		},
		{
			Name:        "Apple",
			Ticker:      "AAPL",
			YahooTicker: "AAPL",
			Market:      "NASDAQ",
			Currency:    "USD",
		},
	}
	if count, err := store.SyncInstruments(context, items); err != nil || count != 2 {
		t.Fatalf("sync instruments: count=%d err=%v", count, err)
	}

	quantity := mustParseDecimal(t, "2")
	averageCost := mustParseDecimal(t, "210.50")
	if _, err := store.UpsertPosition(
		context,
		"AAPL",
		quantity,
		averageCost,
		"USD",
		time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC),
	); err != nil {
		t.Fatalf("upsert position: %v", err)
	}
	if _, err := store.UpsertThesis(
		context,
		"AAPL",
		"서비스 매출 성장",
		"서비스 성장률 둔화",
		"12개월",
		[]string{"서비스 매출", "마진", "서비스 매출"},
	); err != nil {
		t.Fatalf("upsert thesis: %v", err)
	}

	tradeCSVPath := filepath.Join(t.TempDir(), "trades.csv")
	tradeCSV := `external_id,trade_date,ticker,action,quantity,price,fees,taxes,currency
AAPL-001,2026-07-02,AAPL,BUY,2,210.50,0.25,0,USD
`
	if err := os.WriteFile(tradeCSVPath, []byte(tradeCSV), 0o600); err != nil {
		t.Fatalf("write trade CSV: %v", err)
	}

	firstImport, err := store.ImportTradeCSV(context, tradeCSVPath, "mirae-normalized")
	if err != nil {
		t.Fatalf("first import: %v", err)
	}
	if firstImport.RowsSeen != 1 || firstImport.RowsInserted != 1 || firstImport.AlreadyImported {
		t.Fatalf("unexpected first import: %#v", firstImport)
	}

	secondImport, err := store.ImportTradeCSV(context, tradeCSVPath, "mirae-normalized")
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if !secondImport.AlreadyImported || secondImport.RowsInserted != 1 {
		t.Fatalf("unexpected repeated import: %#v", secondImport)
	}

	variantTradeCSVPath := filepath.Join(t.TempDir(), "trades-variant.csv")
	if err := os.WriteFile(variantTradeCSVPath, []byte(tradeCSV+"\n"), 0o600); err != nil {
		t.Fatalf("write variant trade CSV: %v", err)
	}
	variantImport, err := store.ImportTradeCSV(context, variantTradeCSVPath, "mirae-normalized")
	if err != nil {
		t.Fatalf("variant import: %v", err)
	}
	if variantImport.AlreadyImported || variantImport.RowsInserted != 0 || variantImport.RowsDuplicated != 1 {
		t.Fatalf("unexpected variant import: %#v", variantImport)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	reopened, err := openWithClock(context, databasePath, func() time.Time {
		return fixedNow.Add(time.Hour)
	})
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer reopened.Close()

	portfolio, err := reopened.Portfolio(context, "AAPL")
	if err != nil {
		t.Fatalf("query portfolio: %v", err)
	}
	if portfolio.Position == nil || portfolio.Position.QuantityUnits != quantity {
		t.Fatalf("unexpected position: %#v", portfolio.Position)
	}
	if len(portfolio.Trades) != 1 {
		t.Fatalf("expected 1 trade, got %d", len(portfolio.Trades))
	}
	if portfolio.Thesis == nil || portfolio.Thesis.Summary != "서비스 매출 성장" {
		t.Fatalf("unexpected thesis: %#v", portfolio.Thesis)
	}
	if len(portfolio.Thesis.CheckMetrics) != 2 {
		t.Fatalf("expected normalized metrics, got %#v", portfolio.Thesis.CheckMetrics)
	}

	var foreignKeys int
	if err := reopened.db.QueryRowContext(context, `PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
		t.Fatalf("query foreign_keys pragma: %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("expected foreign_keys=1, got %d", foreignKeys)
	}
}

func TestTradeImportRollsBackWhenInstrumentIsMissing(t *testing.T) {
	context := context.Background()
	store, err := Open(context, filepath.Join(t.TempDir(), "forgetmenot.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	_, err = store.UpsertInstrument(context, models.WatchlistItem{
		Name:        "Apple",
		Ticker:      "AAPL",
		YahooTicker: "AAPL",
		Market:      "NASDAQ",
		Currency:    "USD",
	})
	if err != nil {
		t.Fatalf("upsert instrument: %v", err)
	}

	tradeCSVPath := filepath.Join(t.TempDir(), "trades.csv")
	tradeCSV := `external_id,trade_date,ticker,action,quantity,price,fees,taxes,currency
AAPL-001,2026-07-02,AAPL,BUY,2,210.50,0,0,USD
UNKNOWN-001,2026-07-03,UNKNOWN,BUY,1,10,0,0,USD
`
	if err := os.WriteFile(tradeCSVPath, []byte(tradeCSV), 0o600); err != nil {
		t.Fatalf("write trade CSV: %v", err)
	}

	if _, err := store.ImportTradeCSV(context, tradeCSVPath, "test"); err == nil {
		t.Fatal("expected missing instrument error")
	}
	portfolio, err := store.Portfolio(context, "AAPL")
	if err != nil {
		t.Fatalf("query portfolio: %v", err)
	}
	if len(portfolio.Trades) != 0 {
		t.Fatalf("expected transaction rollback, got %d trades", len(portfolio.Trades))
	}
}

func mustParseDecimal(t *testing.T, value string) int64 {
	t.Helper()
	parsed, err := decimal.Parse(value)
	if err != nil {
		t.Fatalf("parse decimal %q: %v", value, err)
	}
	return parsed
}
