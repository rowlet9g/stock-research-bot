package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/models"
	sqlitestore "github.com/rowlet9g/stock-research-bot/internal/storage/sqlite"
)

func TestResearchBriefCommandBuildsNonDARTPrompt(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "forgetmenot.db")
	store, err := sqlitestore.Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if _, err := store.SyncInstruments(ctx, []models.WatchlistItem{
		{
			Name:        "Intel",
			Ticker:      "INTC",
			YahooTicker: "INTC",
			Market:      "NASDAQ",
			Currency:    "USD",
		},
	}); err != nil {
		t.Fatalf("seed instrument: %v", err)
	}
	if _, err := store.UpsertPosition(
		ctx,
		"INTC",
		100_000_000,
		30_000_000_000,
		"USD",
		time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC),
	); err != nil {
		t.Fatalf("seed position: %v", err)
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
	lastPrice := 31.5
	analysisSnapshotPrice = func(
		context.Context,
		string,
	) (models.PriceSnapshot, error) {
		return models.PriceSnapshot{
			YahooTicker: "INTC",
			Currency:    "USD",
			Status:      models.DataStatusAvailable,
			LastPrice:   &lastPrice,
			Source: models.SourceMetadata{
				Provider:   "yahoo",
				SourceURL:  "https://query1.finance.yahoo.com",
				ObservedAt: &observedAt,
				FetchedAt:  observedAt.Add(time.Minute),
			},
		}, nil
	}
	analysisSnapshotNow = func() time.Time {
		return time.Date(2026, 7, 28, 2, 0, 0, 0, time.UTC)
	}

	output := runCommand(
		t,
		"research-brief",
		"-db", databasePath,
		"-ticker", "INTC",
		"-question", "투자 가설을 새로 세우려면 무엇을 확인해야 하나?",
		"-output", "json",
	)
	var result researchBriefCommandResult
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("decode research brief: %v\n%s", err, output)
	}
	if result.Snapshot.InputSHA256 == "" ||
		result.Assessment.InputSHA256 != result.Snapshot.InputSHA256 ||
		!strings.Contains(result.Prompt, result.Snapshot.InputSHA256) ||
		!strings.Contains(result.Prompt, "투자 가설을 새로 세우려면") ||
		!strings.Contains(result.Prompt, "상태: not_requested") {
		t.Fatalf("unexpected research brief result: %#v", result)
	}
}
