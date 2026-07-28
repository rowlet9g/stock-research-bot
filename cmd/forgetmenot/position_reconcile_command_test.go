package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/analysis"
	"github.com/rowlet9g/stock-research-bot/internal/decimal"
	"github.com/rowlet9g/stock-research-bot/internal/models"
	sqlitestore "github.com/rowlet9g/stock-research-bot/internal/storage/sqlite"
)

func TestPositionReconcileCommandReportsPeriodNetChange(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "forgetmenot.db")
	store, err := sqlitestore.Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if _, err := store.SyncInstruments(ctx, []models.WatchlistItem{
		{
			Name:         "삼성전자",
			Ticker:       "005930",
			YahooTicker:  "005930.KS",
			DARTCorpCode: "00126380",
			Market:       "KOSPI",
			Currency:     "KRW",
		},
	}); err != nil {
		t.Fatalf("seed instrument: %v", err)
	}
	for index, trade := range []struct {
		action   string
		quantity int64
	}{
		{action: "BUY", quantity: 9 * decimal.Scale},
		{action: "SELL", quantity: 4 * decimal.Scale},
	} {
		key := "trade-" + string(rune('1'+index))
		if _, err := store.AddTrade(ctx, "005930", sqlitestore.TradeInput{
			ExternalID:     key,
			TradeDate:      time.Date(2026, 7, index+1, 0, 0, 0, 0, time.UTC),
			Action:         trade.action,
			QuantityUnits:  trade.quantity,
			PriceUnits:     100 * decimal.Scale,
			PriceSource:    "reported",
			TaxesKnown:     true,
			TimePrecision:  "day",
			Currency:       "KRW",
			Source:         "test",
			IdempotencyKey: key,
		}); err != nil {
			t.Fatalf("seed trade %d: %v", index, err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close seed store: %v", err)
	}

	output := runCommand(
		t,
		"position-reconcile",
		"-db", databasePath,
		"-ticker", "005930",
		"-output", "json",
	)
	var report analysis.PositionReconciliationReport
	if err := json.Unmarshal(output, &report); err != nil {
		t.Fatalf("decode position reconciliation: %v\n%s", err, output)
	}
	if report.Status != models.DataStatusPartial ||
		report.Summary.MissingStoredPosition != 1 ||
		len(report.Items) != 1 ||
		report.Items[0].NetQuantityChange != "5" ||
		report.Items[0].StoredPositionUnits != nil {
		t.Fatalf("unexpected position reconciliation: %#v", report)
	}
}
