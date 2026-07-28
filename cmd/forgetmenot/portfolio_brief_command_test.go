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
	"github.com/rowlet9g/stock-research-bot/internal/prompt"
	sqlitestore "github.com/rowlet9g/stock-research-bot/internal/storage/sqlite"
)

func TestPortfolioBriefCommandBuildsHashLinkedPrompt(t *testing.T) {
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
		observedAt := generatedAt.Add(-time.Hour)
		return models.PriceSnapshot{
			YahooTicker: yahooTicker,
			Currency:    "USD",
			Status:      models.DataStatusAvailable,
			LastPrice:   &lastPrice,
			Source: models.SourceMetadata{
				Provider:   "fixture",
				SourceURL:  "https://example.test/" + yahooTicker,
				ObservedAt: &observedAt,
				FetchedAt:  generatedAt,
			},
		}, nil
	}

	output := runCommand(
		t,
		"portfolio-brief",
		"-db", databasePath,
		"-question", "가장 큰 집중 위험은?",
		"-output", "json",
	)
	var result portfolioBriefCommandResult
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("decode portfolio brief: %v\n%s", err, output)
	}
	if result.Brief.Version != prompt.PortfolioResearchBriefVersion ||
		result.Brief.PromptSHA256 == "" ||
		result.Brief.ValuationSHA256 !=
			result.Scenarios.InputValuationSHA256 ||
		!strings.Contains(result.Brief.Prompt, "가장 큰 집중 위험은?") ||
		!strings.Contains(result.Brief.Prompt, "평가금액=400") ||
		!strings.Contains(result.Brief.Prompt, "통화내비중=100.00%") ||
		!strings.Contains(result.Brief.Prompt, "not_estimated") {
		t.Fatalf("unexpected portfolio brief result: %#v", result)
	}
}
