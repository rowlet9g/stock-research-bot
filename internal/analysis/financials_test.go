package analysis

import (
	"math/big"
	"testing"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/models"
)

func TestBuildFinancialMetricReportCalculatesCoreRatios(t *testing.T) {
	statement := completeFinancialStatement("11011")
	report := BuildFinancialMetricReport(statement)
	if report.Status != models.DataStatusAvailable {
		t.Fatalf("unexpected financial report status: %#v", report)
	}
	if report.ContentSHA256 != statement.ContentSHA256 {
		t.Fatalf("content hash was not propagated: %#v", report)
	}

	revenue := findFinancialMetric(t, report.Metrics, FinancialMetricRevenue)
	if revenue.Status != FinancialMetricAvailable ||
		revenue.CurrentAmount != "120" ||
		revenue.PreviousAmount != "100" ||
		revenue.PeriodBasis != FinancialPeriodAnnual ||
		revenue.AccountID != "ifrs-full_Revenue" {
		t.Fatalf("unexpected revenue metric: %#v", revenue)
	}
	expectedRatios := map[string]string{
		"revenue_growth_pct":             "20.00",
		"operating_income_growth_pct":    "20.00",
		"net_income_growth_pct":          "20.00",
		"total_assets_growth_pct":        "10.00",
		"operating_cash_flow_growth_pct": "20.00",
		"operating_margin_pct":           "20.00",
		"net_margin_pct":                 "10.00",
		"debt_to_equity_pct":             "57.14",
		"current_ratio_pct":              "200.00",
	}
	for key, expected := range expectedRatios {
		ratio := findFinancialRatio(t, report.Ratios, key)
		if ratio.Status != FinancialMetricAvailable ||
			ratio.Value != expected {
			t.Fatalf(
				"unexpected ratio %s: got %#v, want %s",
				key,
				ratio,
				expected,
			)
		}
	}
}

func TestBuildFinancialMetricReportRejectsAmbiguousPreferredAccount(t *testing.T) {
	statement := completeFinancialStatement("11011")
	duplicate := statement.Accounts[0]
	duplicate.Index = len(statement.Accounts)
	duplicate.AccountName = "중복 매출액"
	statement.Accounts = append(statement.Accounts, duplicate)

	report := BuildFinancialMetricReport(statement)
	revenue := findFinancialMetric(t, report.Metrics, FinancialMetricRevenue)
	if report.Status != models.DataStatusPartial ||
		revenue.Status != FinancialMetricAmbiguous ||
		revenue.Candidates != 2 {
		t.Fatalf("ambiguous revenue was accepted: %#v", revenue)
	}
	margin := findFinancialRatio(t, report.Ratios, "operating_margin_pct")
	if margin.Status != FinancialMetricMissing {
		t.Fatalf("ratio used ambiguous revenue: %#v", margin)
	}
}

func TestBuildFinancialMetricReportDoesNotGuessByAccountName(t *testing.T) {
	statement := completeFinancialStatement("11011")
	statement.Accounts[0].AccountID = "custom_Revenue"
	statement.Accounts[0].AccountName = "매출액"

	report := BuildFinancialMetricReport(statement)
	revenue := findFinancialMetric(t, report.Metrics, FinancialMetricRevenue)
	if revenue.Status != FinancialMetricMissing {
		t.Fatalf("custom account was guessed by name: %#v", revenue)
	}
}

func TestBuildFinancialMetricReportUsesInterimCumulativeAmounts(t *testing.T) {
	statement := completeFinancialStatement("11014")
	statement.Accounts[0].CurrentAmount = "30"
	statement.Accounts[0].CurrentAddAmount = "120"
	statement.Accounts[0].PreviousAmount = ""
	statement.Accounts[0].PreviousInterimTermName = "전기 3분기"
	statement.Accounts[0].PreviousInterimAmount = "25"
	statement.Accounts[0].PreviousAddAmount = "100"

	report := BuildFinancialMetricReport(statement)
	revenue := findFinancialMetric(t, report.Metrics, FinancialMetricRevenue)
	if revenue.CurrentAmount != "120" ||
		revenue.PreviousAmount != "100" ||
		revenue.PeriodBasis != FinancialPeriodCumulative ||
		revenue.PreviousTermName != "전기 3분기" {
		t.Fatalf("interim cumulative amounts were not selected: %#v", revenue)
	}
	ratio := findFinancialRatio(t, report.Ratios, "revenue_growth_pct")
	if ratio.Status != FinancialMetricAvailable || ratio.Value != "20.00" {
		t.Fatalf("unexpected interim revenue growth: %#v", ratio)
	}
}

func TestBuildFinancialMetricReportDoesNotMixInterimPeriodBases(t *testing.T) {
	statement := completeFinancialStatement("11014")
	statement.Accounts[0].CurrentAmount = "30"
	statement.Accounts[0].CurrentAddAmount = "120"
	statement.Accounts[0].PreviousAmount = ""
	statement.Accounts[0].PreviousInterimTermName = "전기 3분기"
	statement.Accounts[0].PreviousInterimAmount = "25"
	statement.Accounts[0].PreviousAddAmount = ""

	report := BuildFinancialMetricReport(statement)
	revenue := findFinancialMetric(t, report.Metrics, FinancialMetricRevenue)
	if revenue.CurrentAmount != "30" ||
		revenue.PreviousAmount != "25" ||
		revenue.PeriodBasis != FinancialPeriodPeriod {
		t.Fatalf("interim period and cumulative amounts were mixed: %#v", revenue)
	}
}

func TestBuildFinancialMetricReportTreatsInterimCashFlowAsCumulative(
	t *testing.T,
) {
	statement := completeFinancialStatement("11014")
	cashFlow := &statement.Accounts[8]
	cashFlow.PreviousAmount = ""
	cashFlow.PreviousInterimTermName = "전기 3분기"
	cashFlow.PreviousInterimAmount = "15"

	report := BuildFinancialMetricReport(statement)
	metric := findFinancialMetric(
		t,
		report.Metrics,
		FinancialMetricOperatingCashFlow,
	)
	if metric.CurrentAmount != "18" ||
		metric.PreviousAmount != "15" ||
		metric.PeriodBasis != FinancialPeriodCumulative {
		t.Fatalf("interim cash flow was not treated as cumulative: %#v", metric)
	}
}

func TestBuildFinancialMetricReportKeepsCurrentMetricWithoutPreviousAmount(
	t *testing.T,
) {
	statement := completeFinancialStatement("11011")
	statement.Accounts[0].PreviousAmount = ""

	report := BuildFinancialMetricReport(statement)
	revenue := findFinancialMetric(t, report.Metrics, FinancialMetricRevenue)
	if revenue.Status != FinancialMetricAvailable ||
		revenue.CurrentAmount != "120" ||
		revenue.PreviousAmount != "" {
		t.Fatalf("current revenue was discarded: %#v", revenue)
	}
	growth := findFinancialRatio(t, report.Ratios, "revenue_growth_pct")
	if growth.Status != FinancialMetricMissing {
		t.Fatalf("growth used a missing previous amount: %#v", growth)
	}
	margin := findFinancialRatio(t, report.Ratios, "operating_margin_pct")
	if margin.Status != FinancialMetricAvailable || margin.Value != "20.00" {
		t.Fatalf("current-period margin was not calculated: %#v", margin)
	}
}

func TestMetricRatioRejectsMismatchedInterimPeriodBases(t *testing.T) {
	statement := completeFinancialStatement("11014")
	statement.Accounts[0].CurrentAmount = "30"
	statement.Accounts[0].PreviousInterimAmount = "25"
	statement.Accounts[1].CurrentAddAmount = "24"
	statement.Accounts[1].PreviousAddAmount = "20"

	report := BuildFinancialMetricReport(statement)
	margin := findFinancialRatio(t, report.Ratios, "operating_margin_pct")
	if margin.Status != FinancialMetricNotComparable {
		t.Fatalf("ratio mixed interim period bases: %#v", margin)
	}
}

func TestGrowthRatioRejectsNonpositivePreviousAmount(t *testing.T) {
	statement := completeFinancialStatement("11011")
	statement.Accounts[1].PreviousAmount = "-5"

	report := BuildFinancialMetricReport(statement)
	ratio := findFinancialRatio(
		t,
		report.Ratios,
		"operating_income_growth_pct",
	)
	if ratio.Status != FinancialMetricNotComparable {
		t.Fatalf("negative-base growth was treated as comparable: %#v", ratio)
	}
}

func TestFormatPercentRoundsHalfAwayFromZero(t *testing.T) {
	tests := []struct {
		numerator   int64
		denominator int64
		expected    string
	}{
		{numerator: 1, denominator: 6, expected: "16.67"},
		{numerator: -1, denominator: 6, expected: "-16.67"},
		{numerator: 1, denominator: 8, expected: "12.50"},
	}
	for _, test := range tests {
		actual, err := formatPercent(
			big.NewInt(test.numerator),
			big.NewInt(test.denominator),
		)
		if err != nil {
			t.Fatalf("format percentage: %v", err)
		}
		if actual != test.expected {
			t.Fatalf(
				"format %d/%d: got %s, want %s",
				test.numerator,
				test.denominator,
				actual,
				test.expected,
			)
		}
	}
}

func completeFinancialStatement(
	reportCode string,
) models.DARTFinancialStatement {
	observedAt := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
	return models.DARTFinancialStatement{
		CorpCode:      "00126380",
		BusinessYear:  2025,
		ReportCode:    reportCode,
		FSKind:        "CFS",
		ReceiptNo:     "20260310002820",
		ContentSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Accounts: []models.DARTFinancialAccount{
			financialAccount(
				0,
				"IS",
				"ifrs-full_Revenue",
				"매출액",
				"120",
				"100",
			),
			financialAccount(
				1,
				"IS",
				"dart_OperatingIncomeLoss",
				"영업이익",
				"24",
				"20",
			),
			financialAccount(
				2,
				"IS",
				"ifrs-full_ProfitLoss",
				"당기순이익",
				"12",
				"10",
			),
			financialAccount(
				3,
				"BS",
				"ifrs-full_Assets",
				"자산총계",
				"220",
				"200",
			),
			financialAccount(
				4,
				"BS",
				"ifrs-full_Liabilities",
				"부채총계",
				"80",
				"70",
			),
			financialAccount(
				5,
				"BS",
				"ifrs-full_Equity",
				"자본총계",
				"140",
				"130",
			),
			financialAccount(
				6,
				"BS",
				"ifrs-full_CurrentAssets",
				"유동자산",
				"60",
				"50",
			),
			financialAccount(
				7,
				"BS",
				"ifrs-full_CurrentLiabilities",
				"유동부채",
				"30",
				"25",
			),
			financialAccount(
				8,
				"CF",
				"ifrs-full_CashFlowsFromUsedInOperatingActivities",
				"영업활동현금흐름",
				"18",
				"15",
			),
		},
		Source: models.SourceMetadata{
			Provider:   "opendart",
			SourceURL:  "https://opendart.fss.or.kr/api/fnlttSinglAcntAll.json",
			ObservedAt: &observedAt,
			FetchedAt:  observedAt.Add(time.Hour),
		},
	}
}

func financialAccount(
	index int,
	statementKind string,
	accountID string,
	accountName string,
	current string,
	previous string,
) models.DARTFinancialAccount {
	return models.DARTFinancialAccount{
		Index:            index,
		StatementKind:    statementKind,
		StatementName:    statementKind,
		AccountID:        accountID,
		AccountName:      accountName,
		CurrentTermName:  "당기",
		CurrentAmount:    current,
		PreviousTermName: "전기",
		PreviousAmount:   previous,
		Order:            index,
		Currency:         "KRW",
	}
}

func findFinancialMetric(
	t *testing.T,
	metrics []FinancialMetric,
	key string,
) FinancialMetric {
	t.Helper()
	for _, metric := range metrics {
		if metric.Key == key {
			return metric
		}
	}
	t.Fatalf("financial metric %q not found", key)
	return FinancialMetric{}
}

func findFinancialRatio(
	t *testing.T,
	ratios []FinancialRatio,
	key string,
) FinancialRatio {
	t.Helper()
	for _, ratio := range ratios {
		if ratio.Key == key {
			return ratio
		}
	}
	t.Fatalf("financial ratio %q not found", key)
	return FinancialRatio{}
}
