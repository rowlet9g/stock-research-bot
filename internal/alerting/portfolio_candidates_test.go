package alerting

import (
	"strings"
	"testing"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/analysis"
	"github.com/rowlet9g/stock-research-bot/internal/models"
)

func TestEvaluatePortfolioCandidatesBuildsStableConcentrationAlerts(
	t *testing.T,
) {
	weightA := int64(6000)
	weightB := int64(4000)
	marketA := int64(60_000_000_000)
	marketB := int64(40_000_000_000)
	valuation := portfolioCandidateValuation([]analysis.PositionValuation{
		{
			Instrument: models.Instrument{
				Name:     "Apple",
				Ticker:   "AAPL",
				Currency: "USD",
			},
			Status:                models.DataStatusAvailable,
			QuantityUnits:         int64Pointer(100_000_000),
			Quantity:              "1",
			Currency:              "USD",
			MarketValueUnits:      &marketA,
			MarketValue:           "600",
			GrossMarketValueUnits: &marketA,
			GrossMarketValue:      "600",
			CostBasisUnits:        int64Pointer(50_000_000_000),
			WeightBPS:             &weightA,
			WeightPct:             "60.00",
			Concentration:         analysis.ConcentrationHigh,
			Thesis:                &models.Thesis{Summary: "services growth"},
			Issues:                []analysis.PortfolioValuationIssue{},
		},
		{
			Instrument: models.Instrument{
				Name:     "Intel",
				Ticker:   "INTC",
				Currency: "USD",
			},
			Status:                models.DataStatusAvailable,
			QuantityUnits:         int64Pointer(100_000_000),
			Quantity:              "1",
			Currency:              "USD",
			MarketValueUnits:      &marketB,
			MarketValue:           "400",
			GrossMarketValueUnits: &marketB,
			GrossMarketValue:      "400",
			CostBasisUnits:        int64Pointer(30_000_000_000),
			WeightBPS:             &weightB,
			WeightPct:             "40.00",
			Concentration:         analysis.ConcentrationHigh,
			Thesis:                &models.Thesis{Summary: "margin recovery"},
			Issues:                []analysis.PortfolioValuationIssue{},
		},
	})
	report, err := EvaluatePortfolioCandidates(
		valuation,
		time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC),
		DefaultPortfolioCandidateConfig(),
	)
	if err != nil {
		t.Fatalf("evaluate portfolio candidates: %v", err)
	}
	if report.Status != models.DataStatusAvailable ||
		report.RuleVersion != PortfolioCandidateRuleVersion ||
		report.InputValuationSHA256 == "" ||
		len(report.Candidates) != 2 {
		t.Fatalf("unexpected candidate report: %#v", report)
	}
	apple := candidateByTicker(t, report.Candidates, "AAPL")
	if apple.Severity != models.AlertSeverityWarning ||
		apple.RuleID != "portfolio.position_concentration" ||
		apple.Fingerprint == "" ||
		len(apple.Evidence) != 2 ||
		apple.Evidence[0].Threshold != "4000" {
		t.Fatalf("unexpected concentration candidate: %#v", apple)
	}

	valuation.Positions[0].WeightPct = "65.00"
	*valuation.Positions[0].WeightBPS = 6500
	changed, err := EvaluatePortfolioCandidates(
		valuation,
		time.Date(2026, 7, 28, 12, 1, 0, 0, time.UTC),
		DefaultPortfolioCandidateConfig(),
	)
	if err != nil {
		t.Fatalf("evaluate changed candidates: %v", err)
	}
	if candidateByTicker(
		t,
		changed.Candidates,
		"AAPL",
	).Fingerprint != apple.Fingerprint {
		t.Fatal("concentration fingerprint changed with observed weight")
	}
}

func TestEvaluatePortfolioCandidatesAddsCostTrendAndThesisReviews(
	t *testing.T,
) {
	quantity := int64(100_000_000)
	market := int64(75_000_000_000)
	cost := int64(100_000_000_000)
	returnBPS := int64(-2500)
	return20D := -12.5
	return60D := -18.0
	ma20 := 80.0
	ma60 := 90.0
	drawdown := -32.0
	volumeRatio := 1.7
	valuation := portfolioCandidateValuation([]analysis.PositionValuation{
		{
			Instrument: models.Instrument{
				Name:     "Apple",
				Ticker:   "AAPL",
				Currency: "USD",
			},
			Status:              models.DataStatusAvailable,
			QuantityUnits:       &quantity,
			Quantity:            "1",
			AverageCost:         "1000",
			Currency:            "USD",
			LastPrice:           "750",
			MarketValueUnits:    &market,
			MarketValue:         "750",
			CostBasisUnits:      &cost,
			CostBasis:           "1000",
			UnrealizedPL:        "-250",
			UnrealizedReturnBPS: &returnBPS,
			UnrealizedReturnPct: "-25.00",
			MarketMetrics: analysis.PositionMarketMetrics{
				ReturnPct20D:     &return20D,
				ReturnPct60D:     &return60D,
				MaxDrawdownPct6M: &drawdown,
				MA20:             &ma20,
				MA60:             &ma60,
				VolumeRatio20D:   &volumeRatio,
				Trend:            "below_ma20_and_ma60",
			},
			Issues: []analysis.PortfolioValuationIssue{},
		},
	})
	report, err := EvaluatePortfolioCandidates(
		valuation,
		time.Now(),
		DefaultPortfolioCandidateConfig(),
	)
	if err != nil {
		t.Fatalf("evaluate decision candidates: %v", err)
	}
	loss := candidateByRule(
		t,
		report.Candidates,
		"portfolio.position_loss_from_cost",
	)
	if loss.Severity != models.AlertSeverityWarning ||
		loss.Evidence[0].Threshold != "-2000" ||
		!strings.Contains(loss.Fact, "-25.00%") {
		t.Fatalf("unexpected loss candidate: %#v", loss)
	}
	trend := candidateByRule(
		t,
		report.Candidates,
		"portfolio.position_weak_trend",
	)
	if trend.Severity != models.AlertSeverityWatch ||
		!strings.Contains(trend.Fact, "20일 이동평균") {
		t.Fatalf("unexpected trend candidate: %#v", trend)
	}
	thesis := candidateByRule(
		t,
		report.Candidates,
		"portfolio.position_thesis_missing",
	)
	if thesis.Severity != models.AlertSeverityInfo ||
		len(thesis.ValidationQuestions) != 3 {
		t.Fatalf("unexpected thesis candidate: %#v", thesis)
	}
}

func TestEvaluatePortfolioCandidatesSeparatesDataQualityStates(t *testing.T) {
	quantity := int64(100_000_000)
	market := int64(20_000_000_000)
	valuation := portfolioCandidateValuation([]analysis.PositionValuation{
		{
			Instrument: models.Instrument{
				Name:     "No Price",
				Ticker:   "NOPRICE",
				Currency: "USD",
			},
			Status:        models.DataStatusUnavailable,
			QuantityUnits: &quantity,
			Quantity:      "1",
			Currency:      "USD",
			Issues: []analysis.PortfolioValuationIssue{
				{Kind: "price_missing", Message: "missing"},
			},
		},
		{
			Instrument: models.Instrument{
				Name:     "No Cost",
				Ticker:   "NOCOST",
				Currency: "USD",
			},
			Status:                models.DataStatusPartial,
			QuantityUnits:         &quantity,
			Quantity:              "1",
			AverageCost:           "0",
			Currency:              "USD",
			MarketValueUnits:      &market,
			MarketValue:           "200",
			GrossMarketValueUnits: &market,
			GrossMarketValue:      "200",
			Issues: []analysis.PortfolioValuationIssue{
				{Kind: "average_cost_missing", Message: "missing"},
			},
		},
	})
	valuation.Status = models.DataStatusPartial
	valuation.Summary.MissingPositions = 3
	report, err := EvaluatePortfolioCandidates(
		valuation,
		time.Now(),
		DefaultPortfolioCandidateConfig(),
	)
	if err != nil {
		t.Fatalf("evaluate portfolio candidates: %v", err)
	}
	for _, ruleID := range []string{
		"portfolio.position_unvalued",
		"portfolio.cost_basis_missing",
		"portfolio.current_positions_missing",
	} {
		if !hasCandidateRule(report.Candidates, ruleID) {
			t.Fatalf("candidate rule %s missing: %#v", ruleID, report)
		}
	}
}

func TestEvaluatePortfolioCandidatesRejectsVersionMismatch(t *testing.T) {
	_, err := EvaluatePortfolioCandidates(
		analysis.PortfolioValuationReport{
			Version: "portfolio-valuation/v0",
		},
		time.Now(),
		DefaultPortfolioCandidateConfig(),
	)
	if err == nil || !strings.Contains(err.Error(), "require valuation version") {
		t.Fatalf("unexpected version error: %v", err)
	}
}

func TestEvaluatePortfolioCandidatesRejectsInvalidLossThresholds(t *testing.T) {
	config := DefaultPortfolioCandidateConfig()
	config.LossWarningThresholdBPS = -500
	_, err := EvaluatePortfolioCandidates(
		portfolioCandidateValuation(nil),
		time.Now(),
		config,
	)
	if err == nil || !strings.Contains(err.Error(), "loss thresholds") {
		t.Fatalf("unexpected threshold validation result: %v", err)
	}
}

func portfolioCandidateValuation(
	positions []analysis.PositionValuation,
) analysis.PortfolioValuationReport {
	return analysis.PortfolioValuationReport{
		Status:                   models.DataStatusAvailable,
		Version:                  analysis.PortfolioValuationVersion,
		GeneratedAt:              time.Date(2026, 7, 28, 11, 0, 0, 0, time.UTC),
		CrossCurrencyAggregation: "not_performed",
		Config:                   analysis.DefaultPortfolioValuationConfig(),
		Summary: analysis.PortfolioValuationSummary{
			InputInstruments: len(positions),
			StoredPositions:  len(positions),
			ValuedPositions:  len(positions),
			CurrencyGroups:   1,
		},
		Currencies: []analysis.CurrencyValuation{},
		Positions:  positions,
		Issues:     []analysis.PortfolioValuationIssue{},
	}
}

func candidateByTicker(
	t *testing.T,
	candidates []Candidate,
	ticker string,
) Candidate {
	t.Helper()
	for _, candidate := range candidates {
		if candidate.Ticker == ticker {
			return candidate
		}
	}
	t.Fatalf("candidate %s not found", ticker)
	return Candidate{}
}

func hasCandidateRule(
	candidates []Candidate,
	ruleID string,
) bool {
	for _, candidate := range candidates {
		if candidate.RuleID == ruleID {
			return true
		}
	}
	return false
}

func candidateByRule(
	t *testing.T,
	candidates []Candidate,
	ruleID string,
) Candidate {
	t.Helper()
	for _, candidate := range candidates {
		if candidate.RuleID == ruleID {
			return candidate
		}
	}
	t.Fatalf("candidate rule %s not found", ruleID)
	return Candidate{}
}

func int64Pointer(value int64) *int64 {
	return &value
}
