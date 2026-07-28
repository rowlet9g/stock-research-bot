package analysis

import (
	"strings"
	"testing"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/models"
)

func TestEvaluateRiskSnapshotFindsFinancialPortfolioAndDisclosureRisks(
	t *testing.T,
) {
	evaluatedAt := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	input := completeAnalysisInput()
	input.Portfolio.Trades = []models.Trade{
		{
			ID:        1,
			TradeDate: evaluatedAt.AddDate(0, -1, 0),
			Action:    "buy",
		},
	}
	input.Disclosures.Disclosures = []models.DARTDisclosure{
		{
			CorpCode:    "00126380",
			CorpName:    "삼성전자",
			ReportName:  "전환사채권발행결정",
			ReceiptNo:   "20260728000123",
			ReceiptDate: evaluatedAt.AddDate(0, 0, -1),
			Submitter:   "삼성전자",
			ViewerURL:   "https://dart.fss.or.kr/dsaf001/main.do?rcpNo=20260728000123",
		},
	}
	setFinancialRatioValue(
		t,
		&input.Financials,
		"current_ratio_pct",
		"89.90",
	)
	setFinancialRatioValue(
		t,
		&input.Financials,
		"debt_to_equity_pct",
		"220.00",
	)
	setFinancialRatioValue(
		t,
		&input.Financials,
		"operating_income_growth_pct",
		"-35.00",
	)
	setFinancialMetricAmount(
		t,
		&input.Financials,
		FinancialMetricOperatingCashFlow,
		"-100",
	)
	snapshot, err := BuildAnalysisInputSnapshot(evaluatedAt, input)
	if err != nil {
		t.Fatalf("build analysis snapshot: %v", err)
	}

	assessment, err := EvaluateRiskSnapshot(
		snapshot,
		evaluatedAt,
		DefaultRiskRuleConfig(),
	)
	if err != nil {
		t.Fatalf("evaluate risk snapshot: %v", err)
	}
	expectedRuleIDs := []string{
		"financial.current_ratio_low",
		"financial.debt_to_equity_high",
		"financial.operating_income_growth_low",
		"financial.operating_cash_flow_negative",
		"portfolio.position_missing",
		"disclosure.capital_action_title",
	}
	for _, ruleID := range expectedRuleIDs {
		finding := riskFindingByRuleID(t, assessment.Findings, ruleID)
		if finding.Fingerprint == "" ||
			finding.Fact == "" ||
			finding.PossibleInterpretation == "" ||
			len(finding.ValidationQuestions) == 0 ||
			len(finding.Evidence) == 0 {
			t.Fatalf("incomplete risk finding %s: %#v", ruleID, finding)
		}
	}
	if assessment.Status != models.DataStatusAvailable ||
		assessment.RuleSetVersion != RiskRuleSetVersion ||
		assessment.InputSchemaVersion != AnalysisInputSchemaVersion ||
		assessment.Config.CurrentRatioWatchBelow != "100" ||
		assessment.InputSHA256 != snapshot.InputSHA256 {
		t.Fatalf("unexpected risk assessment: %#v", assessment)
	}
}

func TestEvaluateRiskSnapshotIgnoresOldDisclosure(t *testing.T) {
	evaluatedAt := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	input := completeAnalysisInput()
	input.Disclosures.Disclosures = []models.DARTDisclosure{
		{
			CorpCode:    "00126380",
			CorpName:    "삼성전자",
			ReportName:  "유상증자결정",
			ReceiptNo:   "20250101000123",
			ReceiptDate: evaluatedAt.AddDate(0, 0, -100),
			Submitter:   "삼성전자",
			ViewerURL:   "https://dart.fss.or.kr",
		},
	}
	snapshot, err := BuildAnalysisInputSnapshot(evaluatedAt, input)
	if err != nil {
		t.Fatalf("build analysis snapshot: %v", err)
	}
	assessment, err := EvaluateRiskSnapshot(
		snapshot,
		evaluatedAt,
		DefaultRiskRuleConfig(),
	)
	if err != nil {
		t.Fatalf("evaluate risk snapshot: %v", err)
	}
	for _, finding := range assessment.Findings {
		if finding.Category == RiskCategoryDisclosure {
			t.Fatalf("old disclosure produced a finding: %#v", finding)
		}
	}
}

func TestEvaluateRiskSnapshotCreatesStableFindingFingerprints(t *testing.T) {
	evaluatedAt := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	input := completeAnalysisInput()
	input.Portfolio.Trades = []models.Trade{{ID: 1}}
	snapshot, err := BuildAnalysisInputSnapshot(evaluatedAt, input)
	if err != nil {
		t.Fatalf("build analysis snapshot: %v", err)
	}
	first, err := EvaluateRiskSnapshot(
		snapshot,
		evaluatedAt,
		DefaultRiskRuleConfig(),
	)
	if err != nil {
		t.Fatalf("evaluate first risk snapshot: %v", err)
	}
	second, err := EvaluateRiskSnapshot(
		snapshot,
		evaluatedAt.Add(time.Minute),
		DefaultRiskRuleConfig(),
	)
	if err != nil {
		t.Fatalf("evaluate second risk snapshot: %v", err)
	}
	firstFinding := riskFindingByRuleID(
		t,
		first.Findings,
		"portfolio.position_missing",
	)
	secondFinding := riskFindingByRuleID(
		t,
		second.Findings,
		"portfolio.position_missing",
	)
	if firstFinding.Fingerprint != secondFinding.Fingerprint {
		t.Fatalf(
			"finding fingerprint changed with evaluation time: %s != %s",
			firstFinding.Fingerprint,
			secondFinding.Fingerprint,
		)
	}
}

func TestEvaluateRiskSnapshotRejectsInvalidConfig(t *testing.T) {
	input := completeAnalysisInput()
	snapshot, err := BuildAnalysisInputSnapshot(time.Now(), input)
	if err != nil {
		t.Fatalf("build analysis snapshot: %v", err)
	}
	config := DefaultRiskRuleConfig()
	config.CurrentRatioWatchBelow = "not-a-number"
	_, err = EvaluateRiskSnapshot(snapshot, time.Now(), config)
	if err == nil || !strings.Contains(err.Error(), "current ratio") {
		t.Fatalf("unexpected invalid config error: %v", err)
	}
}

func setFinancialRatioValue(
	t *testing.T,
	report *FinancialMetricReport,
	key string,
	value string,
) {
	t.Helper()
	for index := range report.Ratios {
		if report.Ratios[index].Key == key {
			report.Ratios[index].Status = FinancialMetricAvailable
			report.Ratios[index].Value = value
			return
		}
	}
	t.Fatalf("financial ratio %q not found", key)
}

func setFinancialMetricAmount(
	t *testing.T,
	report *FinancialMetricReport,
	key string,
	value string,
) {
	t.Helper()
	for index := range report.Metrics {
		if report.Metrics[index].Key == key {
			report.Metrics[index].Status = FinancialMetricAvailable
			report.Metrics[index].CurrentAmount = value
			return
		}
	}
	t.Fatalf("financial metric %q not found", key)
}

func riskFindingByRuleID(
	t *testing.T,
	findings []RiskFinding,
	ruleID string,
) RiskFinding {
	t.Helper()
	for _, finding := range findings {
		if finding.RuleID == ruleID {
			return finding
		}
	}
	t.Fatalf("risk finding %q not found in %#v", ruleID, findings)
	return RiskFinding{}
}
