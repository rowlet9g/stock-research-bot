package analysis

import (
	"testing"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/decimal"
	"github.com/rowlet9g/stock-research-bot/internal/models"
)

func TestValuePortfolioGroupsCurrenciesAndUsesGrossExposure(t *testing.T) {
	generatedAt := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	inputs := []PortfolioValuationInput{
		portfolioValuationInput(t, "AAPL", "USD", "2", "150", 200, generatedAt),
		portfolioValuationInput(t, "INTC", "USD", "10", "30", 40, generatedAt),
		portfolioValuationInput(t, "005930", "KRW", "5", "70000", 80000, generatedAt),
	}
	report, err := ValuePortfolio(
		inputs,
		generatedAt,
		DefaultPortfolioValuationConfig(),
	)
	if err != nil {
		t.Fatalf("value portfolio: %v", err)
	}
	if report.Status != models.DataStatusAvailable ||
		report.Summary.ValuedPositions != 3 ||
		report.Summary.CurrencyGroups != 2 ||
		len(report.Currencies) != 2 {
		t.Fatalf("unexpected valuation summary: %#v", report)
	}
	usd := currencyValuation(t, report, "USD")
	if usd.NetMarketValue != "800" ||
		usd.GrossMarketValue != "800" ||
		usd.UnrealizedPL != "200" {
		t.Fatalf("unexpected USD valuation: %#v", usd)
	}
	apple := positionValuation(t, report, "AAPL")
	intel := positionValuation(t, report, "INTC")
	if apple.WeightPct != "50.00" ||
		apple.Concentration != ConcentrationHigh ||
		intel.WeightPct != "50.00" ||
		intel.Concentration != ConcentrationHigh {
		t.Fatalf("unexpected USD concentration: %#v %#v", apple, intel)
	}
}

func TestValuePortfolioDoesNotNetLongAndShortConcentration(t *testing.T) {
	generatedAt := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	report, err := ValuePortfolio(
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
	usd := currencyValuation(t, report, "USD")
	if usd.NetMarketValue != "50" || usd.GrossMarketValue != "150" {
		t.Fatalf("long and short exposure was netted: %#v", usd)
	}
	long := positionValuation(t, report, "LONG")
	short := positionValuation(t, report, "SHORT")
	if long.WeightPct != "66.67" || short.WeightPct != "33.33" {
		t.Fatalf("unexpected gross exposure weights: %#v %#v", long, short)
	}
}

func TestValuePortfolioPreservesUnknownAndPartialStates(t *testing.T) {
	generatedAt := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	missingPosition := PortfolioValuationInput{
		Portfolio: models.PortfolioRecord{
			Instrument: models.Instrument{
				Ticker:   "MISSING",
				Currency: "USD",
			},
		},
	}
	missingPrice := portfolioValuationInput(
		t,
		"NOPRICE",
		"USD",
		"1",
		"10",
		20,
		generatedAt,
	)
	missingPrice.Price.LastPrice = nil
	unknownCost := portfolioValuationInput(
		t,
		"NOCOST",
		"USD",
		"2",
		"0",
		20,
		generatedAt,
	)
	unknownCost.Issues = []PortfolioValuationIssue{
		{
			Kind:    "partial_data",
			Message: "fixture warning",
		},
	}
	report, err := ValuePortfolio(
		[]PortfolioValuationInput{
			missingPosition,
			missingPrice,
			unknownCost,
		},
		generatedAt,
		DefaultPortfolioValuationConfig(),
	)
	if err != nil {
		t.Fatalf("value portfolio: %v", err)
	}
	if report.Status != models.DataStatusPartial ||
		report.Summary.MissingPositions != 1 ||
		report.Summary.UnvaluedPositions != 1 ||
		report.Summary.ValuedPositions != 1 {
		t.Fatalf("unknown states were hidden: %#v", report)
	}
	noCost := positionValuation(t, report, "NOCOST")
	if noCost.Status != models.DataStatusPartial ||
		noCost.MarketValue != "40" ||
		noCost.CostBasisUnits != nil ||
		len(noCost.Issues) != 2 ||
		noCost.Issues[0].Ticker != "NOCOST" {
		t.Fatalf("unexpected missing cost valuation: %#v", noCost)
	}
	usd := currencyValuation(t, report, "USD")
	if usd.CostBasisComplete {
		t.Fatalf("incomplete cost basis was reported complete: %#v", usd)
	}
}

func TestValuePortfolioRejectsCrossCurrencyPrice(t *testing.T) {
	generatedAt := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	input := portfolioValuationInput(
		t,
		"AAPL",
		"USD",
		"2",
		"150",
		200,
		generatedAt,
	)
	input.Price.Currency = "KRW"
	report, err := ValuePortfolio(
		[]PortfolioValuationInput{input},
		generatedAt,
		DefaultPortfolioValuationConfig(),
	)
	if err != nil {
		t.Fatalf("value portfolio: %v", err)
	}
	item := report.Positions[0]
	if report.Status != models.DataStatusPartial ||
		item.Status != models.DataStatusUnavailable ||
		item.MarketValueUnits != nil ||
		len(item.Issues) != 1 ||
		item.Issues[0].Kind != "currency_mismatch" {
		t.Fatalf("currency mismatch was accepted: %#v", report)
	}
}

func portfolioValuationInput(
	t *testing.T,
	ticker string,
	currency string,
	quantity string,
	averageCost string,
	lastPrice float64,
	fetchedAt time.Time,
) PortfolioValuationInput {
	t.Helper()
	quantityUnits, err := decimal.Parse(quantity)
	if err != nil {
		t.Fatalf("parse quantity: %v", err)
	}
	averageCostUnits, err := decimal.Parse(averageCost)
	if err != nil {
		t.Fatalf("parse average cost: %v", err)
	}
	return PortfolioValuationInput{
		Portfolio: models.PortfolioRecord{
			Instrument: models.Instrument{
				Ticker:      ticker,
				Name:        ticker,
				YahooTicker: ticker,
				Currency:    currency,
			},
			Position: &models.Position{
				QuantityUnits:    quantityUnits,
				AverageCostUnits: averageCostUnits,
				Currency:         currency,
				AsOf:             fetchedAt.Add(-time.Hour),
			},
		},
		Price: models.PriceSnapshot{
			YahooTicker: ticker,
			Currency:    currency,
			Status:      models.DataStatusAvailable,
			LastPrice:   &lastPrice,
			Source: models.SourceMetadata{
				Provider:  "fixture",
				SourceURL: "https://example.test/" + ticker,
				FetchedAt: fetchedAt,
			},
		},
	}
}

func currencyValuation(
	t *testing.T,
	report PortfolioValuationReport,
	currency string,
) CurrencyValuation {
	t.Helper()
	for _, group := range report.Currencies {
		if group.Currency == currency {
			return group
		}
	}
	t.Fatalf("currency %s not found", currency)
	return CurrencyValuation{}
}

func positionValuation(
	t *testing.T,
	report PortfolioValuationReport,
	ticker string,
) PositionValuation {
	t.Helper()
	for _, item := range report.Positions {
		if item.Instrument.Ticker == ticker {
			return item
		}
	}
	t.Fatalf("position %s not found", ticker)
	return PositionValuation{}
}
