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

func TestDARTFinancialMetricsReadsCurrentStoredStatement(t *testing.T) {
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
	if _, err := store.SyncDARTFinancialStatement(
		ctx,
		commandFinancialMetricStatement(),
	); err != nil {
		t.Fatalf("seed financial statement: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close seed store: %v", err)
	}

	output := runCommand(
		t,
		"dart-financial-metrics",
		"-db", databasePath,
		"-ticker", "005930",
		"-year", "2025",
		"-report-code", "11011",
		"-fs-div", "CFS",
		"-output", "json",
	)
	var result dartFinancialMetricsResult
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("decode financial metrics: %v\n%s", err, output)
	}
	if result.Ticker != "005930" ||
		result.Status != models.DataStatusAvailable ||
		len(result.Metrics) != 9 ||
		len(result.Ratios) != 9 {
		t.Fatalf("unexpected financial metrics result: %#v", result)
	}
	revenueGrowth := commandFinancialRatio(
		t,
		result.Ratios,
		"revenue_growth_pct",
	)
	if revenueGrowth.Status != analysis.FinancialMetricAvailable ||
		revenueGrowth.Value != "20.00" {
		t.Fatalf("unexpected revenue growth: %#v", revenueGrowth)
	}
}

func TestFinancialRatioDisplayValueOmitsUnitWhenUnavailable(t *testing.T) {
	ratio := analysis.FinancialRatio{
		Status: analysis.FinancialMetricNotComparable,
		Unit:   "%",
	}
	if got := financialRatioDisplayValue(ratio); got != "N/A" {
		t.Fatalf("unexpected unavailable ratio display: %q", got)
	}
}

func commandFinancialMetricStatement() models.DARTFinancialStatement {
	observedAt := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
	statement := models.DARTFinancialStatement{
		CorpCode:      "00126380",
		BusinessYear:  2025,
		ReportCode:    "11011",
		FSKind:        "CFS",
		ReceiptNo:     "20260310002820",
		ContentSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		IsCurrent:     true,
		Source: models.SourceMetadata{
			Provider: "opendart",
			SourceURL: "https://opendart.fss.or.kr/api/fnlttSinglAcntAll.json?" +
				"bsns_year=2025&corp_code=00126380&fs_div=CFS&reprt_code=11011",
			ObservedAt: &observedAt,
			FetchedAt:  observedAt.Add(time.Hour),
		},
	}
	definitions := []struct {
		section   string
		accountID string
		name      string
		current   string
		previous  string
	}{
		{"IS", "ifrs-full_Revenue", "매출액", "120", "100"},
		{"IS", "dart_OperatingIncomeLoss", "영업이익", "24", "20"},
		{"IS", "ifrs-full_ProfitLoss", "당기순이익", "12", "10"},
		{"BS", "ifrs-full_Assets", "자산총계", "220", "200"},
		{"BS", "ifrs-full_Liabilities", "부채총계", "80", "70"},
		{"BS", "ifrs-full_Equity", "자본총계", "140", "130"},
		{"BS", "ifrs-full_CurrentAssets", "유동자산", "60", "50"},
		{"BS", "ifrs-full_CurrentLiabilities", "유동부채", "30", "25"},
		{
			"CF",
			"ifrs-full_CashFlowsFromUsedInOperatingActivities",
			"영업활동현금흐름",
			"18",
			"15",
		},
	}
	for index, definition := range definitions {
		statement.Accounts = append(
			statement.Accounts,
			models.DARTFinancialAccount{
				Index:            index,
				StatementKind:    definition.section,
				StatementName:    definition.section,
				AccountID:        definition.accountID,
				AccountName:      definition.name,
				CurrentTermName:  "당기",
				CurrentAmount:    definition.current,
				PreviousTermName: "전기",
				PreviousAmount:   definition.previous,
				Order:            index,
				Currency:         "KRW",
			},
		)
	}
	return statement
}

func commandFinancialRatio(
	t *testing.T,
	ratios []analysis.FinancialRatio,
	key string,
) analysis.FinancialRatio {
	t.Helper()
	for _, ratio := range ratios {
		if ratio.Key == key {
			return ratio
		}
	}
	t.Fatalf("financial ratio %q not found", key)
	return analysis.FinancialRatio{}
}
