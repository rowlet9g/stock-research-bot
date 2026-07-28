package analysis

import (
	"fmt"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/decimal"
	"github.com/rowlet9g/stock-research-bot/internal/models"
)

const PortfolioValuationVersion = "portfolio-valuation/v1"

type ConcentrationLevel string

const (
	ConcentrationNotApplicable ConcentrationLevel = "not_applicable"
	ConcentrationNormal        ConcentrationLevel = "normal"
	ConcentrationWatch         ConcentrationLevel = "watch"
	ConcentrationHigh          ConcentrationLevel = "high"
)

type PortfolioValuationConfig struct {
	WatchThresholdBPS int64  `json:"watch_threshold_bps"`
	HighThresholdBPS  int64  `json:"high_threshold_bps"`
	WeightBasis       string `json:"weight_basis"`
}

func DefaultPortfolioValuationConfig() PortfolioValuationConfig {
	return PortfolioValuationConfig{
		WatchThresholdBPS: 2500,
		HighThresholdBPS:  4000,
		WeightBasis:       "gross_market_value_within_currency",
	}
}

type PortfolioValuationInput struct {
	Portfolio models.PortfolioRecord
	Price     models.PriceSnapshot
	Issues    []PortfolioValuationIssue
}

type PortfolioValuationIssue struct {
	Ticker  string `json:"ticker,omitempty"`
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

type PositionValuation struct {
	Instrument            models.Instrument         `json:"instrument"`
	Status                models.DataStatus         `json:"status"`
	QuantityUnits         *int64                    `json:"quantity_units,omitempty"`
	Quantity              string                    `json:"quantity,omitempty"`
	AverageCostUnits      *int64                    `json:"average_cost_units,omitempty"`
	AverageCost           string                    `json:"average_cost,omitempty"`
	Currency              string                    `json:"currency,omitempty"`
	AsOf                  *time.Time                `json:"as_of,omitempty"`
	LastPriceUnits        *int64                    `json:"last_price_units,omitempty"`
	LastPrice             string                    `json:"last_price,omitempty"`
	MarketValueUnits      *int64                    `json:"market_value_units,omitempty"`
	MarketValue           string                    `json:"market_value,omitempty"`
	GrossMarketValueUnits *int64                    `json:"gross_market_value_units,omitempty"`
	GrossMarketValue      string                    `json:"gross_market_value,omitempty"`
	CostBasisUnits        *int64                    `json:"cost_basis_units,omitempty"`
	CostBasis             string                    `json:"cost_basis,omitempty"`
	UnrealizedPLUnits     *int64                    `json:"unrealized_pl_units,omitempty"`
	UnrealizedPL          string                    `json:"unrealized_pl,omitempty"`
	WeightBPS             *int64                    `json:"weight_bps,omitempty"`
	WeightPct             string                    `json:"weight_pct,omitempty"`
	Concentration         ConcentrationLevel        `json:"concentration"`
	PriceSource           *models.SourceMetadata    `json:"price_source,omitempty"`
	Issues                []PortfolioValuationIssue `json:"issues"`
}

type CurrencyValuation struct {
	Currency              string `json:"currency"`
	Positions             int    `json:"positions"`
	ValuedPositions       int    `json:"valued_positions"`
	NetMarketValueUnits   int64  `json:"net_market_value_units"`
	NetMarketValue        string `json:"net_market_value"`
	GrossMarketValueUnits int64  `json:"gross_market_value_units"`
	GrossMarketValue      string `json:"gross_market_value"`
	CostBasisUnits        int64  `json:"cost_basis_units"`
	CostBasis             string `json:"cost_basis"`
	UnrealizedPLUnits     int64  `json:"unrealized_pl_units"`
	UnrealizedPL          string `json:"unrealized_pl"`
	CostBasisComplete     bool   `json:"cost_basis_complete"`
}

type PortfolioValuationSummary struct {
	InputInstruments  int `json:"input_instruments"`
	StoredPositions   int `json:"stored_positions"`
	MissingPositions  int `json:"missing_positions"`
	ZeroPositions     int `json:"zero_positions"`
	ValuedPositions   int `json:"valued_positions"`
	UnvaluedPositions int `json:"unvalued_positions"`
	CurrencyGroups    int `json:"currency_groups"`
}

type PortfolioValuationReport struct {
	Status                   models.DataStatus         `json:"status"`
	Version                  string                    `json:"version"`
	GeneratedAt              time.Time                 `json:"generated_at"`
	CrossCurrencyAggregation string                    `json:"cross_currency_aggregation"`
	Config                   PortfolioValuationConfig  `json:"config"`
	Summary                  PortfolioValuationSummary `json:"summary"`
	Currencies               []CurrencyValuation       `json:"currencies"`
	Positions                []PositionValuation       `json:"positions"`
	Issues                   []PortfolioValuationIssue `json:"issues"`
}

func ValuePortfolio(
	inputs []PortfolioValuationInput,
	generatedAt time.Time,
	config PortfolioValuationConfig,
) (PortfolioValuationReport, error) {
	if generatedAt.IsZero() {
		return PortfolioValuationReport{}, fmt.Errorf(
			"portfolio valuation generation time is required",
		)
	}
	if err := validatePortfolioValuationConfig(config); err != nil {
		return PortfolioValuationReport{}, err
	}

	ordered := append([]PortfolioValuationInput(nil), inputs...)
	sort.SliceStable(ordered, func(left int, right int) bool {
		return strings.ToUpper(ordered[left].Portfolio.Instrument.Ticker) <
			strings.ToUpper(ordered[right].Portfolio.Instrument.Ticker)
	})
	report := PortfolioValuationReport{
		Status:                   models.DataStatusAvailable,
		Version:                  PortfolioValuationVersion,
		GeneratedAt:              generatedAt.UTC(),
		CrossCurrencyAggregation: "not_performed",
		Config:                   config,
		Summary: PortfolioValuationSummary{
			InputInstruments: len(ordered),
		},
		Currencies: []CurrencyValuation{},
		Positions:  make([]PositionValuation, 0, len(ordered)),
		Issues:     []PortfolioValuationIssue{},
	}
	if len(ordered) == 0 {
		report.Status = models.DataStatusEmpty
		return report, nil
	}

	groups := map[string]*CurrencyValuation{}
	seenTickers := make(map[string]struct{}, len(ordered))
	for _, input := range ordered {
		ticker := strings.TrimSpace(input.Portfolio.Instrument.Ticker)
		tickerKey := strings.ToUpper(ticker)
		if ticker == "" {
			return PortfolioValuationReport{}, fmt.Errorf(
				"portfolio valuation instrument ticker is required",
			)
		}
		if _, exists := seenTickers[tickerKey]; exists {
			return PortfolioValuationReport{}, fmt.Errorf(
				"duplicate portfolio valuation ticker %q",
				ticker,
			)
		}
		seenTickers[tickerKey] = struct{}{}

		item, err := valuePortfolioPosition(input)
		if err != nil {
			return PortfolioValuationReport{}, fmt.Errorf(
				"value portfolio position %s: %w",
				ticker,
				err,
			)
		}
		updatePortfolioValuationSummary(&report, item)
		if item.Status != models.DataStatusAvailable {
			report.Status = models.DataStatusPartial
		}
		if item.Currency != "" && item.QuantityUnits != nil {
			group := groups[item.Currency]
			if group == nil {
				group = &CurrencyValuation{
					Currency:          item.Currency,
					CostBasisComplete: true,
				}
				groups[item.Currency] = group
			}
			if err := addPositionToCurrency(group, item); err != nil {
				return PortfolioValuationReport{}, fmt.Errorf(
					"aggregate portfolio currency %s: %w",
					item.Currency,
					err,
				)
			}
		}
		report.Positions = append(report.Positions, item)
		report.Issues = append(report.Issues, item.Issues...)
	}

	for _, item := range report.Positions {
		if item.GrossMarketValueUnits == nil {
			continue
		}
		group := groups[item.Currency]
		if group == nil || group.GrossMarketValueUnits == 0 {
			continue
		}
		weightBPS, err := ratioBPS(
			*item.GrossMarketValueUnits,
			group.GrossMarketValueUnits,
		)
		if err != nil {
			return PortfolioValuationReport{}, fmt.Errorf(
				"calculate portfolio weight %s: %w",
				item.Instrument.Ticker,
				err,
			)
		}
		item.WeightBPS = int64Pointer(weightBPS)
		item.WeightPct = formatBPS(weightBPS)
		item.Concentration = concentrationLevel(weightBPS, config)
		for index := range report.Positions {
			if strings.EqualFold(
				report.Positions[index].Instrument.Ticker,
				item.Instrument.Ticker,
			) {
				report.Positions[index] = item
				break
			}
		}
	}

	currencies := make([]string, 0, len(groups))
	for currency := range groups {
		currencies = append(currencies, currency)
	}
	sort.Strings(currencies)
	for _, currency := range currencies {
		group := groups[currency]
		group.NetMarketValue = decimal.Format(group.NetMarketValueUnits)
		group.GrossMarketValue = decimal.Format(group.GrossMarketValueUnits)
		group.CostBasis = decimal.Format(group.CostBasisUnits)
		group.UnrealizedPL = decimal.Format(group.UnrealizedPLUnits)
		report.Currencies = append(report.Currencies, *group)
	}
	report.Summary.CurrencyGroups = len(report.Currencies)
	return report, nil
}

func validatePortfolioValuationConfig(config PortfolioValuationConfig) error {
	if config.WatchThresholdBPS <= 0 ||
		config.HighThresholdBPS <= config.WatchThresholdBPS ||
		config.HighThresholdBPS > 10000 {
		return fmt.Errorf(
			"portfolio concentration thresholds must satisfy 0 < watch < high <= 10000 bps",
		)
	}
	if config.WeightBasis != "gross_market_value_within_currency" {
		return fmt.Errorf(
			"unsupported portfolio weight basis %q",
			config.WeightBasis,
		)
	}
	return nil
}

func valuePortfolioPosition(
	input PortfolioValuationInput,
) (PositionValuation, error) {
	item := PositionValuation{
		Instrument:    input.Portfolio.Instrument,
		Status:        models.DataStatusAvailable,
		Concentration: ConcentrationNotApplicable,
		Issues:        append([]PortfolioValuationIssue(nil), input.Issues...),
	}
	ticker := input.Portfolio.Instrument.Ticker
	for index := range item.Issues {
		if strings.TrimSpace(item.Issues[index].Ticker) == "" {
			item.Issues[index].Ticker = ticker
		}
	}
	if len(item.Issues) > 0 {
		item.Status = models.DataStatusPartial
	}
	if input.Portfolio.Position == nil {
		item.Status = models.DataStatusUnavailable
		item.Issues = append(item.Issues, PortfolioValuationIssue{
			Ticker:  ticker,
			Kind:    "position_missing",
			Message: "current position snapshot is not stored; absence is not treated as zero holdings",
		})
		return item, nil
	}

	position := *input.Portfolio.Position
	item.QuantityUnits = int64Pointer(position.QuantityUnits)
	item.Quantity = decimal.Format(position.QuantityUnits)
	item.AverageCostUnits = int64Pointer(position.AverageCostUnits)
	item.AverageCost = decimal.Format(position.AverageCostUnits)
	item.Currency = strings.ToUpper(strings.TrimSpace(position.Currency))
	asOf := position.AsOf.UTC()
	item.AsOf = &asOf
	if item.Currency == "" {
		item.Status = models.DataStatusUnavailable
		item.Issues = append(item.Issues, PortfolioValuationIssue{
			Ticker:  ticker,
			Kind:    "position_currency_missing",
			Message: "position currency is missing",
		})
		return item, nil
	}
	if position.QuantityUnits == 0 {
		zero := int64(0)
		item.MarketValueUnits = &zero
		item.MarketValue = "0"
		item.GrossMarketValueUnits = &zero
		item.GrossMarketValue = "0"
		item.CostBasisUnits = &zero
		item.CostBasis = "0"
		item.UnrealizedPLUnits = &zero
		item.UnrealizedPL = "0"
		return item, nil
	}

	priceUnits, issue := usablePortfolioPrice(input.Price, item.Currency)
	if issue != nil {
		issue.Ticker = ticker
		item.Status = models.DataStatusUnavailable
		item.Issues = append(item.Issues, *issue)
		return item, nil
	}
	item.LastPriceUnits = int64Pointer(priceUnits)
	item.LastPrice = decimal.Format(priceUnits)
	source := input.Price.Source
	item.PriceSource = &source
	if input.Price.Status != models.DataStatusAvailable {
		item.Status = models.DataStatusPartial
		item.Issues = append(item.Issues, PortfolioValuationIssue{
			Ticker: ticker,
			Kind:   "price_partial",
			Message: fmt.Sprintf(
				"price snapshot status is %q although a last price is available",
				input.Price.Status,
			),
		})
	}
	if strings.TrimSpace(source.Provider) == "" ||
		strings.TrimSpace(source.SourceURL) == "" ||
		source.FetchedAt.IsZero() {
		item.Status = models.DataStatusPartial
		item.Issues = append(item.Issues, PortfolioValuationIssue{
			Ticker:  ticker,
			Kind:    "price_source_incomplete",
			Message: "price source provider, source_url, or fetched_at is missing",
		})
	}

	marketValue, err := decimal.Multiply(position.QuantityUnits, priceUnits)
	if err != nil {
		return PositionValuation{}, err
	}
	grossMarketValue, err := absoluteInt64(marketValue)
	if err != nil {
		return PositionValuation{}, err
	}
	item.MarketValueUnits = int64Pointer(marketValue)
	item.MarketValue = decimal.Format(marketValue)
	item.GrossMarketValueUnits = int64Pointer(grossMarketValue)
	item.GrossMarketValue = decimal.Format(grossMarketValue)

	if position.AverageCostUnits == 0 {
		item.Status = models.DataStatusPartial
		item.Issues = append(item.Issues, PortfolioValuationIssue{
			Ticker:  ticker,
			Kind:    "average_cost_missing",
			Message: "average cost is zero; cost basis and unrealized profit/loss are not calculated",
		})
		return item, nil
	}
	costBasis, err := decimal.Multiply(
		position.QuantityUnits,
		position.AverageCostUnits,
	)
	if err != nil {
		return PositionValuation{}, err
	}
	unrealizedPL, err := subtractExactInt64(marketValue, costBasis)
	if err != nil {
		return PositionValuation{}, err
	}
	item.CostBasisUnits = int64Pointer(costBasis)
	item.CostBasis = decimal.Format(costBasis)
	item.UnrealizedPLUnits = int64Pointer(unrealizedPL)
	item.UnrealizedPL = decimal.Format(unrealizedPL)
	return item, nil
}

func usablePortfolioPrice(
	snapshot models.PriceSnapshot,
	positionCurrency string,
) (int64, *PortfolioValuationIssue) {
	if snapshot.LastPrice == nil {
		return 0, &PortfolioValuationIssue{
			Kind:    "price_missing",
			Message: "last price is not available",
		}
	}
	price := *snapshot.LastPrice
	if math.IsNaN(price) || math.IsInf(price, 0) || price <= 0 {
		return 0, &PortfolioValuationIssue{
			Kind:    "price_invalid",
			Message: "last price must be a finite positive number",
		}
	}
	priceCurrency := strings.ToUpper(strings.TrimSpace(snapshot.Currency))
	if priceCurrency == "" {
		return 0, &PortfolioValuationIssue{
			Kind:    "price_currency_missing",
			Message: "price currency is missing",
		}
	}
	if priceCurrency != positionCurrency {
		return 0, &PortfolioValuationIssue{
			Kind: "currency_mismatch",
			Message: fmt.Sprintf(
				"position currency %s does not match price currency %s",
				positionCurrency,
				priceCurrency,
			),
		}
	}
	priceUnits, err := decimal.Parse(strconv.FormatFloat(price, 'f', 8, 64))
	if err != nil {
		return 0, &PortfolioValuationIssue{
			Kind:    "price_precision_invalid",
			Message: err.Error(),
		}
	}
	return priceUnits, nil
}

func updatePortfolioValuationSummary(
	report *PortfolioValuationReport,
	item PositionValuation,
) {
	if item.QuantityUnits == nil {
		report.Summary.MissingPositions++
		return
	}
	report.Summary.StoredPositions++
	if *item.QuantityUnits == 0 {
		report.Summary.ZeroPositions++
	}
	if item.MarketValueUnits == nil {
		report.Summary.UnvaluedPositions++
	} else {
		report.Summary.ValuedPositions++
	}
}

func addPositionToCurrency(
	group *CurrencyValuation,
	item PositionValuation,
) error {
	group.Positions++
	if item.MarketValueUnits == nil ||
		item.GrossMarketValueUnits == nil {
		group.CostBasisComplete = false
		return nil
	}
	group.ValuedPositions++
	var err error
	group.NetMarketValueUnits, err = addExactInt64(
		group.NetMarketValueUnits,
		*item.MarketValueUnits,
	)
	if err != nil {
		return err
	}
	group.GrossMarketValueUnits, err = addExactInt64(
		group.GrossMarketValueUnits,
		*item.GrossMarketValueUnits,
	)
	if err != nil {
		return err
	}
	if item.CostBasisUnits == nil || item.UnrealizedPLUnits == nil {
		group.CostBasisComplete = false
		return nil
	}
	group.CostBasisUnits, err = addExactInt64(
		group.CostBasisUnits,
		*item.CostBasisUnits,
	)
	if err != nil {
		return err
	}
	group.UnrealizedPLUnits, err = addExactInt64(
		group.UnrealizedPLUnits,
		*item.UnrealizedPLUnits,
	)
	return err
}

func ratioBPS(value int64, total int64) (int64, error) {
	if value < 0 || total <= 0 {
		return 0, fmt.Errorf(
			"portfolio weight requires non-negative value and positive total",
		)
	}
	numerator := new(big.Int).Mul(big.NewInt(value), big.NewInt(10000))
	denominator := big.NewInt(total)
	quotient := new(big.Int)
	remainder := new(big.Int)
	quotient.QuoRem(numerator, denominator, remainder)
	if new(big.Int).Mul(remainder, big.NewInt(2)).Cmp(denominator) >= 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	if !quotient.IsInt64() {
		return 0, fmt.Errorf("portfolio weight exceeds int64 range")
	}
	return quotient.Int64(), nil
}

func formatBPS(value int64) string {
	return strconv.FormatFloat(float64(value)/100, 'f', 2, 64)
}

func concentrationLevel(
	weightBPS int64,
	config PortfolioValuationConfig,
) ConcentrationLevel {
	switch {
	case weightBPS >= config.HighThresholdBPS:
		return ConcentrationHigh
	case weightBPS >= config.WatchThresholdBPS:
		return ConcentrationWatch
	default:
		return ConcentrationNormal
	}
}

func absoluteInt64(value int64) (int64, error) {
	if value == math.MinInt64 {
		return 0, fmt.Errorf("absolute decimal value exceeds int64 range")
	}
	if value < 0 {
		return -value, nil
	}
	return value, nil
}

func int64Pointer(value int64) *int64 {
	return &value
}
