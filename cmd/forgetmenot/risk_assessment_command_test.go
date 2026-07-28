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

func TestRiskAssessmentCommandReturnsSnapshotAndFindings(t *testing.T) {
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
		"risk-assess",
		"-db", databasePath,
		"-ticker", "005930",
		"-output", "json",
	)
	var result riskAssessmentCommandResult
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("decode risk assessment: %v\n%s", err, output)
	}
	if result.Snapshot.InputSHA256 == "" ||
		result.Assessment.InputSHA256 != result.Snapshot.InputSHA256 ||
		result.Assessment.Status != models.DataStatusAvailable {
		t.Fatalf("unexpected risk assessment result: %#v", result)
	}
	finding := commandRiskFinding(
		t,
		result.Assessment.Findings,
		"portfolio.thesis_missing",
	)
	if finding.Severity != analysis.RiskSeverityWatch {
		t.Fatalf("unexpected thesis finding: %#v", finding)
	}
}

func commandRiskFinding(
	t *testing.T,
	findings []analysis.RiskFinding,
	ruleID string,
) analysis.RiskFinding {
	t.Helper()
	for _, finding := range findings {
		if finding.RuleID == ruleID {
			return finding
		}
	}
	t.Fatalf("risk finding %q not found in %#v", ruleID, findings)
	return analysis.RiskFinding{}
}
