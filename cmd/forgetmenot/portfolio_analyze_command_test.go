package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/analysis"
	"github.com/rowlet9g/stock-research-bot/internal/decimal"
	"github.com/rowlet9g/stock-research-bot/internal/models"
	sqlitestore "github.com/rowlet9g/stock-research-bot/internal/storage/sqlite"
)

func TestPortfolioAnalyzeCommandKeepsPartialPriceFailures(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "forgetmenot.db")
	store, err := sqlitestore.Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	items := []models.WatchlistItem{
		{Name: "Apple", Ticker: "AAPL", YahooTicker: "AAPL", Market: "NASDAQ", Currency: "USD"},
		{Name: "Intel", Ticker: "INTC", YahooTicker: "INTC", Market: "NASDAQ", Currency: "USD"},
		{Name: "Zero", Ticker: "ZERO", YahooTicker: "ZERO", Market: "NASDAQ", Currency: "USD"},
	}
	if _, err := store.SyncInstruments(ctx, items); err != nil {
		t.Fatalf("seed instruments: %v", err)
	}
	asOf := time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC)
	if _, err := store.UpsertPosition(
		ctx,
		"AAPL",
		2*decimal.Scale,
		150*decimal.Scale,
		"USD",
		asOf,
	); err != nil {
		t.Fatalf("seed Apple position: %v", err)
	}
	if _, err := store.UpsertPosition(
		ctx,
		"INTC",
		10*decimal.Scale,
		30*decimal.Scale,
		"USD",
		asOf,
	); err != nil {
		t.Fatalf("seed Intel position: %v", err)
	}
	if _, err := store.UpsertPosition(
		ctx,
		"ZERO",
		0,
		0,
		"USD",
		asOf,
	); err != nil {
		t.Fatalf("seed zero position: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close seed store: %v", err)
	}

	originalPrice := portfolioAnalyzePrice
	originalNow := portfolioAnalyzeNow
	t.Cleanup(func() {
		portfolioAnalyzePrice = originalPrice
		portfolioAnalyzeNow = originalNow
	})
	generatedAt := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	portfolioAnalyzeNow = func() time.Time { return generatedAt }
	var mutex sync.Mutex
	calls := map[string]int{}
	portfolioAnalyzePrice = func(
		_ context.Context,
		yahooTicker string,
	) (models.PriceSnapshot, error) {
		mutex.Lock()
		calls[yahooTicker]++
		mutex.Unlock()
		if yahooTicker == "INTC" {
			return models.PriceSnapshot{}, errors.New("fixture unavailable")
		}
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
		"portfolio-analyze",
		"-db", databasePath,
		"-workers", "2",
		"-output", "json",
	)
	var report analysis.PortfolioValuationReport
	if err := json.Unmarshal(output, &report); err != nil {
		t.Fatalf("decode portfolio valuation: %v\n%s", err, output)
	}
	if report.Status != models.DataStatusPartial ||
		report.Summary.StoredPositions != 3 ||
		report.Summary.ValuedPositions != 2 ||
		report.Summary.UnvaluedPositions != 1 ||
		report.Summary.ZeroPositions != 1 {
		t.Fatalf("unexpected valuation summary: %#v", report)
	}
	if calls["AAPL"] != 1 || calls["INTC"] != 1 || calls["ZERO"] != 0 {
		t.Fatalf("unexpected provider calls: %#v", calls)
	}
	intel := commandPositionValuation(t, report, "INTC")
	if intel.Status != models.DataStatusUnavailable ||
		len(intel.Issues) == 0 ||
		intel.MarketValueUnits != nil {
		t.Fatalf("provider failure was hidden: %#v", intel)
	}
}

func TestPortfolioAnalyzeCommandRejectsInvalidWorkers(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run(
		[]string{"portfolio-analyze", "-workers", "0"},
		&stdout,
		&stderr,
	)
	if exitCode != 2 || stderr.Len() == 0 {
		t.Fatalf(
			"unexpected invalid workers result: exit=%d stderr=%q",
			exitCode,
			stderr.String(),
		)
	}
}

func TestPortfolioAnalyzeCommandRejectsInvalidThresholds(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run(
		[]string{
			"portfolio-analyze",
			"-watch-bps", "4000",
			"-high-bps", "2500",
		},
		&stdout,
		&stderr,
	)
	if exitCode != 2 ||
		!strings.Contains(stderr.String(), "concentration thresholds") {
		t.Fatalf(
			"unexpected invalid threshold result: exit=%d stderr=%q",
			exitCode,
			stderr.String(),
		)
	}
}

func commandPositionValuation(
	t *testing.T,
	report analysis.PortfolioValuationReport,
	ticker string,
) analysis.PositionValuation {
	t.Helper()
	for _, item := range report.Positions {
		if item.Instrument.Ticker == ticker {
			return item
		}
	}
	t.Fatalf("position %s not found", ticker)
	return analysis.PositionValuation{}
}
