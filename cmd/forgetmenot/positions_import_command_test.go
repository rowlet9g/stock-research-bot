package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/rowlet9g/stock-research-bot/internal/models"
	sqlitestore "github.com/rowlet9g/stock-research-bot/internal/storage/sqlite"
)

func TestPositionsImportCommandStoresCurrentSnapshots(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "forgetmenot.db")
	store, err := sqlitestore.Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if _, err := store.SyncInstruments(ctx, []models.WatchlistItem{
		{
			Name:        "Apple",
			Ticker:      "AAPL",
			YahooTicker: "AAPL",
			Market:      "NASDAQ",
			Currency:    "USD",
		},
	}); err != nil {
		t.Fatalf("seed instrument: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close seed store: %v", err)
	}

	positionPath := filepath.Join(t.TempDir(), "positions.csv")
	if err := os.WriteFile(
		positionPath,
		[]byte("ticker,quantity,average_cost,currency,as_of\n"+
			"AAPL,2,210.50,USD,2026-07-28\n"),
		0o600,
	); err != nil {
		t.Fatalf("write position CSV: %v", err)
	}
	output := runCommand(
		t,
		"positions-import",
		"-db", databasePath,
		"-file", positionPath,
		"-output", "json",
	)
	var result sqlitestore.PositionImportResult
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("decode position import: %v\n%s", err, output)
	}
	if result.RowsSeen != 1 ||
		result.RowsChanged != 1 ||
		result.RowsUnchanged != 0 {
		t.Fatalf("unexpected position import result: %#v", result)
	}
}
