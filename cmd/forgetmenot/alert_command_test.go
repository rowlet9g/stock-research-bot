package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/alerting"
	"github.com/rowlet9g/stock-research-bot/internal/decimal"
	"github.com/rowlet9g/stock-research-bot/internal/models"
	sqlitestore "github.com/rowlet9g/stock-research-bot/internal/storage/sqlite"
)

func TestAlertEvaluateStoresAndDeduplicatesPortfolioCandidates(t *testing.T) {
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
	if _, err := store.UpsertThesis(
		ctx,
		"AAPL",
		"services growth",
		"margin contracts",
		"3 years",
		[]string{"services revenue"},
	); err != nil {
		t.Fatalf("seed thesis: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close seed store: %v", err)
	}

	originalPrice := portfolioAnalyzePrice
	originalBriefNow := portfolioBriefNow
	originalAlertNow := alertEvaluateNow
	t.Cleanup(func() {
		portfolioAnalyzePrice = originalPrice
		portfolioBriefNow = originalBriefNow
		alertEvaluateNow = originalAlertNow
	})
	generatedAt := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	portfolioBriefNow = func() time.Time { return generatedAt }
	alertEvaluateNow = func() time.Time {
		return generatedAt.Add(5 * time.Minute)
	}
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

	briefOutput := runCommand(
		t,
		"portfolio-brief",
		"-db", databasePath,
		"-save",
		"-output", "json",
	)
	var brief portfolioBriefCommandResult
	if err := json.Unmarshal(briefOutput, &brief); err != nil {
		t.Fatalf("decode saved portfolio brief: %v\n%s", err, briefOutput)
	}
	if brief.AnalysisRun == nil {
		t.Fatalf("portfolio brief run was not saved: %#v", brief)
	}

	firstOutput := runCommand(
		t,
		"alert-evaluate",
		"-db", databasePath,
		"-run-id", strconv.FormatInt(brief.AnalysisRun.ID, 10),
		"-output", "json",
	)
	var first alertEvaluateCommandResult
	if err := json.Unmarshal(firstOutput, &first); err != nil {
		t.Fatalf("decode first alert evaluation: %v\n%s", err, firstOutput)
	}
	if len(first.Report.Candidates) != 1 ||
		first.NewAlerts != 1 ||
		first.NewObservations != 1 ||
		len(first.Observations) != 1 ||
		first.Observations[0].Alert.OccurrenceCount != 1 ||
		first.Observations[0].Alert.Status != models.AlertStatusPending {
		t.Fatalf("unexpected first alert evaluation: %#v", first)
	}

	secondOutput := runCommand(
		t,
		"alert-evaluate",
		"-db", databasePath,
		"-run-id", strconv.FormatInt(brief.AnalysisRun.ID, 10),
		"-output", "json",
	)
	var second alertEvaluateCommandResult
	if err := json.Unmarshal(secondOutput, &second); err != nil {
		t.Fatalf("decode second alert evaluation: %v\n%s", err, secondOutput)
	}
	if second.NewAlerts != 0 ||
		second.NewObservations != 0 ||
		len(second.Observations) != 1 ||
		second.Observations[0].Alert.OccurrenceCount != 1 {
		t.Fatalf("same source run was not deduplicated: %#v", second)
	}

	listOutput := runCommand(
		t,
		"alert-list",
		"-db", databasePath,
		"-status", "pending",
		"-output", "json",
	)
	var listed alertListCommandResult
	if err := json.Unmarshal(listOutput, &listed); err != nil {
		t.Fatalf("decode alert list: %v\n%s", err, listOutput)
	}
	if listed.Status != models.AlertStatusPending ||
		len(listed.Alerts) != 1 ||
		len(listed.Alerts[0].Payload) != 0 {
		t.Fatalf("unexpected alert list: %#v", listed)
	}

	payloadOutput := runCommand(
		t,
		"alert-list",
		"-db", databasePath,
		"-include-payload",
		"-output", "json",
	)
	var withPayload alertListCommandResult
	if err := json.Unmarshal(payloadOutput, &withPayload); err != nil {
		t.Fatalf("decode alert payload list: %v\n%s", err, payloadOutput)
	}
	if len(withPayload.Alerts) != 1 {
		t.Fatalf("alert evidence payload missing: %#v", withPayload)
	}
	var candidate alerting.Candidate
	if err := json.Unmarshal(
		withPayload.Alerts[0].Payload,
		&candidate,
	); err != nil {
		t.Fatalf("decode alert evidence payload: %v", err)
	}
	if candidate.RuleID != "portfolio.position_concentration" {
		t.Fatalf("unexpected alert evidence: %#v", candidate)
	}
}
