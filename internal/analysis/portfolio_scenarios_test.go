package analysis

import (
	"strings"
	"testing"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/models"
)

func TestStressPortfolioBuildsDeterministicCurrencyScenarios(t *testing.T) {
	generatedAt := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	valuation, err := ValuePortfolio(
		[]PortfolioValuationInput{
			portfolioValuationInput(t, "AAPL", "USD", "2", "150", 200, generatedAt),
			portfolioValuationInput(t, "005930", "KRW", "5", "70000", 80000, generatedAt),
		},
		generatedAt,
		DefaultPortfolioValuationConfig(),
	)
	if err != nil {
		t.Fatalf("value portfolio: %v", err)
	}
	report, err := StressPortfolio(
		valuation,
		generatedAt.Add(time.Minute),
		DefaultPortfolioScenarioConfig(),
	)
	if err != nil {
		t.Fatalf("stress portfolio: %v", err)
	}
	if report.Status != models.DataStatusAvailable ||
		report.InputValuationStatus != models.DataStatusAvailable ||
		report.Summary.IncludedPositions != 2 ||
		report.Summary.CurrencyGroups != 2 ||
		len(report.Scenarios) != 3 ||
		report.InputValuationSHA256 == "" {
		t.Fatalf("unexpected scenario report: %#v", report)
	}
	downside := portfolioScenario(t, report, "downside")
	if downside.ProbabilityStatus != "not_estimated" ||
		downside.ReturnPct != "-20.00" {
		t.Fatalf("scenario was presented as a forecast: %#v", downside)
	}
	usd := scenarioCurrency(t, downside, "USD")
	krw := scenarioCurrency(t, downside, "KRW")
	if usd.CurrentNetValue != "400" ||
		usd.ScenarioNetValue != "320" ||
		usd.Change != "-80" ||
		krw.Change != "-80000" {
		t.Fatalf("unexpected downside result: %#v %#v", usd, krw)
	}
}

func TestStressPortfolioPreservesLongShortDirection(t *testing.T) {
	generatedAt := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	valuation, err := ValuePortfolio(
		[]PortfolioValuationInput{
			portfolioValuationInput(t, "LONG", "USD", "10", "8", 10, generatedAt),
			portfolioValuationInput(t, "SHORT", "USD", "-5", "9", 10, generatedAt),
		},
		generatedAt,
		DefaultPortfolioValuationConfig(),
	)
	if err != nil {
		t.Fatalf("value portfolio: %v", err)
	}
	report, err := StressPortfolio(
		valuation,
		generatedAt.Add(time.Minute),
		DefaultPortfolioScenarioConfig(),
	)
	if err != nil {
		t.Fatalf("stress portfolio: %v", err)
	}
	downside := portfolioScenario(t, report, "downside")
	long := scenarioImpact(t, downside, "LONG")
	short := scenarioImpact(t, downside, "SHORT")
	if long.Change != "-20" || short.Change != "10" {
		t.Fatalf("short direction was lost: %#v %#v", long, short)
	}
	usd := scenarioCurrency(t, downside, "USD")
	if usd.CurrentNetValue != "50" ||
		usd.ScenarioNetValue != "40" ||
		usd.Change != "-10" {
		t.Fatalf("unexpected long-short net result: %#v", usd)
	}
}

func TestStressPortfolioReportsNoValuedPositions(t *testing.T) {
	generatedAt := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	valuation := PortfolioValuationReport{
		Status:      models.DataStatusPartial,
		Version:     PortfolioValuationVersion,
		GeneratedAt: generatedAt,
		Positions: []PositionValuation{
			{
				Instrument: models.Instrument{Ticker: "MISSING"},
				Status:     models.DataStatusUnavailable,
				Issues: []PortfolioValuationIssue{
					{Ticker: "MISSING", Kind: "price_missing", Message: "missing"},
				},
			},
		},
		Issues: []PortfolioValuationIssue{
			{Ticker: "MISSING", Kind: "price_missing", Message: "missing"},
		},
	}
	report, err := StressPortfolio(
		valuation,
		generatedAt.Add(time.Minute),
		DefaultPortfolioScenarioConfig(),
	)
	if err != nil {
		t.Fatalf("stress portfolio: %v", err)
	}
	if report.Status != models.DataStatusPartial ||
		len(report.Scenarios) != 0 ||
		report.Summary.ExcludedPositions != 1 ||
		len(report.Issues) != 2 ||
		report.Issues[1].Kind != "no_valued_positions" {
		t.Fatalf("missing valuation was hidden: %#v", report)
	}
}

func TestStressPortfolioRejectsInvalidScenarioBounds(t *testing.T) {
	config := DefaultPortfolioScenarioConfig()
	config.DownsideReturnBPS = -10001
	_, err := StressPortfolio(
		PortfolioValuationReport{
			Version:     PortfolioValuationVersion,
			GeneratedAt: time.Now(),
		},
		time.Now(),
		config,
	)
	if err == nil || !strings.Contains(err.Error(), "scenario returns") {
		t.Fatalf("unexpected config error: %v", err)
	}
}

func TestStressPortfolioRejectsValuationVersionMismatch(t *testing.T) {
	_, err := StressPortfolio(
		PortfolioValuationReport{
			Version:     "portfolio-valuation/v0",
			GeneratedAt: time.Now(),
		},
		time.Now(),
		DefaultPortfolioScenarioConfig(),
	)
	if err == nil || !strings.Contains(err.Error(), "requires valuation version") {
		t.Fatalf("unexpected version error: %v", err)
	}
}

func portfolioScenario(
	t *testing.T,
	report PortfolioScenarioReport,
	id string,
) PortfolioScenarioResult {
	t.Helper()
	for _, scenario := range report.Scenarios {
		if scenario.ID == id {
			return scenario
		}
	}
	t.Fatalf("scenario %s not found", id)
	return PortfolioScenarioResult{}
}

func scenarioCurrency(
	t *testing.T,
	scenario PortfolioScenarioResult,
	currency string,
) ScenarioCurrencyResult {
	t.Helper()
	for _, result := range scenario.Currencies {
		if result.Currency == currency {
			return result
		}
	}
	t.Fatalf("scenario currency %s not found", currency)
	return ScenarioCurrencyResult{}
}

func scenarioImpact(
	t *testing.T,
	scenario PortfolioScenarioResult,
	ticker string,
) ScenarioPositionImpact {
	t.Helper()
	for _, impact := range scenario.PositionImpacts {
		if impact.Instrument.Ticker == ticker {
			return impact
		}
	}
	t.Fatalf("scenario impact %s not found", ticker)
	return ScenarioPositionImpact{}
}
