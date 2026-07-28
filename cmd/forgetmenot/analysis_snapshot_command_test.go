package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/analysis"
	"github.com/rowlet9g/stock-research-bot/internal/models"
	sqlitestore "github.com/rowlet9g/stock-research-bot/internal/storage/sqlite"
)

func TestAnalysisSnapshotCombinesStoredAndLiveInputs(t *testing.T) {
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
	if _, err := store.UpsertPosition(
		ctx,
		"005930",
		2_000_000,
		70_000_000_000,
		"KRW",
		time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC),
	); err != nil {
		t.Fatalf("seed position: %v", err)
	}
	fetchedAt := time.Date(2026, 7, 28, 1, 0, 0, 0, time.UTC)
	if _, err := store.SyncDARTDisclosures(
		ctx,
		"00126380",
		[]models.DARTDisclosure{commandDARTDisclosure(fetchedAt)},
	); err != nil {
		t.Fatalf("seed disclosure: %v", err)
	}
	if _, err := store.SyncDARTFinancialStatement(
		ctx,
		commandFinancialMetricStatement(),
	); err != nil {
		t.Fatalf("seed financial statement: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close seed store: %v", err)
	}

	originalPrice := analysisSnapshotPrice
	originalNow := analysisSnapshotNow
	t.Cleanup(func() {
		analysisSnapshotPrice = originalPrice
		analysisSnapshotNow = originalNow
	})
	observedAt := time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC)
	lastPrice := 72_000.0
	analysisSnapshotPrice = func(
		context.Context,
		string,
	) (models.PriceSnapshot, error) {
		return models.PriceSnapshot{
			YahooTicker: "005930.KS",
			Currency:    "KRW",
			Status:      models.DataStatusAvailable,
			LastPrice:   &lastPrice,
			Source: models.SourceMetadata{
				Provider:   "yahoo",
				SourceURL:  "https://query1.finance.yahoo.com",
				ObservedAt: &observedAt,
				FetchedAt:  fetchedAt,
			},
		}, nil
	}
	analysisSnapshotNow = func() time.Time {
		return time.Date(2026, 7, 28, 2, 0, 0, 0, time.UTC)
	}

	output := runCommand(
		t,
		"analysis-snapshot",
		"-db", databasePath,
		"-ticker", "005930",
		"-output", "json",
	)
	var snapshot analysis.AnalysisInputSnapshot
	if err := json.Unmarshal(output, &snapshot); err != nil {
		t.Fatalf("decode analysis snapshot: %v\n%s", err, output)
	}
	if snapshot.Status != models.DataStatusAvailable ||
		snapshot.SchemaVersion != analysis.AnalysisInputSchemaVersion ||
		snapshot.InputSHA256 == "" {
		t.Fatalf("unexpected analysis snapshot: %#v", snapshot)
	}
	if snapshot.Portfolio.Position == nil ||
		len(snapshot.Disclosures.Disclosures) != 1 ||
		snapshot.Financials.ReceiptNo != "20260310002820" ||
		snapshot.Price.LastPrice == nil ||
		*snapshot.Price.LastPrice != lastPrice {
		t.Fatalf("analysis components were not combined: %#v", snapshot)
	}
}
