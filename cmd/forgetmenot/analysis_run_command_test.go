package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/decimal"
	"github.com/rowlet9g/stock-research-bot/internal/models"
	sqlitestore "github.com/rowlet9g/stock-research-bot/internal/storage/sqlite"
)

func TestPortfolioBriefSaveAndAnalysisRunListAreIdempotent(t *testing.T) {
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
	if _, err := store.UpsertPosition(
		ctx,
		"AAPL",
		2*decimal.Scale,
		150*decimal.Scale,
		"USD",
		time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC),
	); err != nil {
		t.Fatalf("seed position: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close seed store: %v", err)
	}

	originalPrice := portfolioAnalyzePrice
	originalNow := portfolioBriefNow
	t.Cleanup(func() {
		portfolioAnalyzePrice = originalPrice
		portfolioBriefNow = originalNow
	})
	generatedAt := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	portfolioBriefNow = func() time.Time { return generatedAt }
	portfolioAnalyzePrice = func(
		_ context.Context,
		yahooTicker string,
	) (models.PriceSnapshot, error) {
		lastPrice := 200.0
		return models.PriceSnapshot{
			YahooTicker: yahooTicker,
			Currency:    "USD",
			Status:      models.DataStatusAvailable,
			LastPrice:   &lastPrice,
			Source: models.SourceMetadata{
				Provider:  "fixture",
				SourceURL: "https://example.test/" + yahooTicker,
				FetchedAt: generatedAt,
			},
		}, nil
	}

	firstOutput := runCommand(
		t,
		"portfolio-brief",
		"-db", databasePath,
		"-save",
		"-output", "json",
	)
	var first portfolioBriefCommandResult
	if err := json.Unmarshal(firstOutput, &first); err != nil {
		t.Fatalf("decode first saved brief: %v\n%s", err, firstOutput)
	}
	if first.AnalysisRun == nil || first.AlreadySaved {
		t.Fatalf("unexpected first save: %#v", first)
	}
	secondOutput := runCommand(
		t,
		"portfolio-brief",
		"-db", databasePath,
		"-save",
		"-output", "json",
	)
	var second portfolioBriefCommandResult
	if err := json.Unmarshal(secondOutput, &second); err != nil {
		t.Fatalf("decode second saved brief: %v\n%s", err, secondOutput)
	}
	if second.AnalysisRun == nil ||
		!second.AlreadySaved ||
		second.AnalysisRun.ID != first.AnalysisRun.ID {
		t.Fatalf("repeated brief was not deduplicated: %#v", second)
	}

	listOutput := runCommand(
		t,
		"analysis-run-list",
		"-db", databasePath,
		"-kind", "portfolio_brief",
		"-include-payload",
		"-output", "json",
	)
	var listed analysisRunListResult
	if err := json.Unmarshal(listOutput, &listed); err != nil {
		t.Fatalf("decode analysis run list: %v\n%s", err, listOutput)
	}
	if len(listed.Runs) != 1 ||
		!strings.Contains(
			string(listed.Runs[0].Payload),
			`"portfolio-research-brief/v1"`,
		) {
		t.Fatalf("unexpected stored analysis runs: %#v", listed)
	}
}
