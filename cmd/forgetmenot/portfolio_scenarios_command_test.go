package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/analysis"
	"github.com/rowlet9g/stock-research-bot/internal/decimal"
	"github.com/rowlet9g/stock-research-bot/internal/models"
	sqlitestore "github.com/rowlet9g/stock-research-bot/internal/storage/sqlite"
)

func TestPortfolioScenariosCommandBuildsStressResults(t *testing.T) {
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
	originalNow := portfolioScenarioNow
	t.Cleanup(func() {
		portfolioAnalyzePrice = originalPrice
		portfolioScenarioNow = originalNow
	})
	generatedAt := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	portfolioScenarioNow = func() time.Time { return generatedAt }
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

	output := runCommand(
		t,
		"portfolio-scenarios",
		"-db", databasePath,
		"-downside-bps", "-1500",
		"-upside-bps", "2500",
		"-output", "json",
	)
	var report analysis.PortfolioScenarioReport
	if err := json.Unmarshal(output, &report); err != nil {
		t.Fatalf("decode portfolio scenarios: %v\n%s", err, output)
	}
	if report.Status != models.DataStatusAvailable ||
		report.InputValuationStatus != models.DataStatusAvailable ||
		report.Summary.IncludedPositions != 1 ||
		len(report.Scenarios) != 3 {
		t.Fatalf("unexpected scenario report: %#v", report)
	}
	downside := commandPortfolioScenario(t, report, "downside")
	upside := commandPortfolioScenario(t, report, "upside")
	if downside.ReturnPct != "-15.00" ||
		downside.Currencies[0].Change != "-60" ||
		upside.ReturnPct != "25.00" ||
		upside.Currencies[0].Change != "100" {
		t.Fatalf("unexpected scenario calculations: %#v %#v", downside, upside)
	}
}

func commandPortfolioScenario(
	t *testing.T,
	report analysis.PortfolioScenarioReport,
	id string,
) analysis.PortfolioScenarioResult {
	t.Helper()
	for _, scenario := range report.Scenarios {
		if scenario.ID == id {
			return scenario
		}
	}
	t.Fatalf("scenario %s not found", id)
	return analysis.PortfolioScenarioResult{}
}

func TestPortfolioScenariosCommandRejectsInvalidBounds(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run(
		[]string{
			"portfolio-scenarios",
			"-downside-bps", "0",
		},
		&stdout,
		&stderr,
	)
	if exitCode != 2 ||
		!strings.Contains(stderr.String(), "scenario returns") {
		t.Fatalf(
			"unexpected invalid scenario result: exit=%d stderr=%q",
			exitCode,
			stderr.String(),
		)
	}
}
