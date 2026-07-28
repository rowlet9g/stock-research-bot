package analysis

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/decimal"
	"github.com/rowlet9g/stock-research-bot/internal/models"
)

const PortfolioScenarioVersion = "portfolio-scenarios/v1"

type PortfolioScenarioConfig struct {
	DownsideReturnBPS int64  `json:"downside_return_bps"`
	NeutralReturnBPS  int64  `json:"neutral_return_bps"`
	UpsideReturnBPS   int64  `json:"upside_return_bps"`
	ShockBasis        string `json:"shock_basis"`
	ProbabilityPolicy string `json:"probability_policy"`
}

func DefaultPortfolioScenarioConfig() PortfolioScenarioConfig {
	return PortfolioScenarioConfig{
		DownsideReturnBPS: -2000,
		NeutralReturnBPS:  0,
		UpsideReturnBPS:   2000,
		ShockBasis:        "uniform_position_price_return",
		ProbabilityPolicy: "not_estimated",
	}
}

type PortfolioScenarioIssue struct {
	Ticker  string `json:"ticker,omitempty"`
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

type PortfolioScenarioSummary struct {
	InputPositions    int `json:"input_positions"`
	IncludedPositions int `json:"included_positions"`
	ExcludedPositions int `json:"excluded_positions"`
	CurrencyGroups    int `json:"currency_groups"`
}

type ScenarioPositionImpact struct {
	Instrument         models.Instrument `json:"instrument"`
	Currency           string            `json:"currency"`
	CurrentValueUnits  int64             `json:"current_value_units"`
	CurrentValue       string            `json:"current_value"`
	ScenarioValueUnits int64             `json:"scenario_value_units"`
	ScenarioValue      string            `json:"scenario_value"`
	ChangeUnits        int64             `json:"change_units"`
	Change             string            `json:"change"`
}

type ScenarioCurrencyResult struct {
	Currency                   string `json:"currency"`
	CurrentNetValueUnits       int64  `json:"current_net_value_units"`
	CurrentNetValue            string `json:"current_net_value"`
	ScenarioNetValueUnits      int64  `json:"scenario_net_value_units"`
	ScenarioNetValue           string `json:"scenario_net_value"`
	ChangeUnits                int64  `json:"change_units"`
	Change                     string `json:"change"`
	CurrentGrossExposureUnits  int64  `json:"current_gross_exposure_units"`
	CurrentGrossExposure       string `json:"current_gross_exposure"`
	ScenarioGrossExposureUnits int64  `json:"scenario_gross_exposure_units"`
	ScenarioGrossExposure      string `json:"scenario_gross_exposure"`
}

type PortfolioScenarioResult struct {
	ID                     string                   `json:"id"`
	Label                  string                   `json:"label"`
	ReturnBPS              int64                    `json:"return_bps"`
	ReturnPct              string                   `json:"return_pct"`
	ProbabilityStatus      string                   `json:"probability_status"`
	Assumption             string                   `json:"assumption"`
	MechanicalResult       string                   `json:"mechanical_result"`
	PossibleInterpretation string                   `json:"possible_interpretation"`
	ValidationQuestions    []string                 `json:"validation_questions"`
	Currencies             []ScenarioCurrencyResult `json:"currencies"`
	PositionImpacts        []ScenarioPositionImpact `json:"position_impacts"`
}

type PortfolioScenarioReport struct {
	Status                    models.DataStatus         `json:"status"`
	Version                   string                    `json:"version"`
	GeneratedAt               time.Time                 `json:"generated_at"`
	InputValuationVersion     string                    `json:"input_valuation_version"`
	InputValuationGeneratedAt time.Time                 `json:"input_valuation_generated_at"`
	InputValuationSHA256      string                    `json:"input_valuation_sha256"`
	CrossCurrencyAggregation  string                    `json:"cross_currency_aggregation"`
	Config                    PortfolioScenarioConfig   `json:"config"`
	Summary                   PortfolioScenarioSummary  `json:"summary"`
	Scenarios                 []PortfolioScenarioResult `json:"scenarios"`
	Issues                    []PortfolioScenarioIssue  `json:"issues"`
}

type portfolioScenarioDefinition struct {
	ID        string
	Label     string
	ReturnBPS int64
}

func StressPortfolio(
	valuation PortfolioValuationReport,
	generatedAt time.Time,
	config PortfolioScenarioConfig,
) (PortfolioScenarioReport, error) {
	if generatedAt.IsZero() {
		return PortfolioScenarioReport{}, fmt.Errorf(
			"portfolio scenario generation time is required",
		)
	}
	if valuation.Version != PortfolioValuationVersion {
		return PortfolioScenarioReport{}, fmt.Errorf(
			"portfolio scenario requires valuation version %q, got %q",
			PortfolioValuationVersion,
			valuation.Version,
		)
	}
	if valuation.GeneratedAt.IsZero() {
		return PortfolioScenarioReport{}, fmt.Errorf(
			"portfolio valuation generation time is required",
		)
	}
	if err := validatePortfolioScenarioConfig(config); err != nil {
		return PortfolioScenarioReport{}, err
	}
	inputHash, err := hashPortfolioValuation(valuation)
	if err != nil {
		return PortfolioScenarioReport{}, err
	}

	report := PortfolioScenarioReport{
		Status:                    valuation.Status,
		Version:                   PortfolioScenarioVersion,
		GeneratedAt:               generatedAt.UTC(),
		InputValuationVersion:     valuation.Version,
		InputValuationGeneratedAt: valuation.GeneratedAt.UTC(),
		InputValuationSHA256:      inputHash,
		CrossCurrencyAggregation:  "not_performed",
		Config:                    config,
		Summary: PortfolioScenarioSummary{
			InputPositions: len(valuation.Positions),
		},
		Scenarios: []PortfolioScenarioResult{},
		Issues:    make([]PortfolioScenarioIssue, 0, len(valuation.Issues)),
	}
	for _, issue := range valuation.Issues {
		report.Issues = append(report.Issues, PortfolioScenarioIssue{
			Ticker:  issue.Ticker,
			Kind:    issue.Kind,
			Message: issue.Message,
		})
	}

	valuedPositions := make([]PositionValuation, 0, len(valuation.Positions))
	currencySet := map[string]struct{}{}
	for _, position := range valuation.Positions {
		if position.MarketValueUnits == nil {
			report.Summary.ExcludedPositions++
			continue
		}
		valuedPositions = append(valuedPositions, position)
		report.Summary.IncludedPositions++
		currencySet[position.Currency] = struct{}{}
	}
	report.Summary.CurrencyGroups = len(currencySet)
	if len(valuedPositions) == 0 {
		if valuation.Status == models.DataStatusEmpty {
			report.Status = models.DataStatusEmpty
		} else {
			report.Status = models.DataStatusPartial
		}
		report.Issues = append(report.Issues, PortfolioScenarioIssue{
			Kind:    "no_valued_positions",
			Message: "no valued positions are available for scenario analysis",
		})
		return report, nil
	}

	definitions := []portfolioScenarioDefinition{
		{
			ID:        "downside",
			Label:     "Downside",
			ReturnBPS: config.DownsideReturnBPS,
		},
		{
			ID:        "neutral",
			Label:     "Neutral",
			ReturnBPS: config.NeutralReturnBPS,
		},
		{
			ID:        "upside",
			Label:     "Upside",
			ReturnBPS: config.UpsideReturnBPS,
		},
	}
	for _, definition := range definitions {
		result, err := buildPortfolioScenario(
			valuedPositions,
			definition,
			config,
		)
		if err != nil {
			return PortfolioScenarioReport{}, fmt.Errorf(
				"build %s portfolio scenario: %w",
				definition.ID,
				err,
			)
		}
		report.Scenarios = append(report.Scenarios, result)
	}
	return report, nil
}

func validatePortfolioScenarioConfig(config PortfolioScenarioConfig) error {
	if config.DownsideReturnBPS < -10000 ||
		config.DownsideReturnBPS >= config.NeutralReturnBPS ||
		config.NeutralReturnBPS != 0 ||
		config.UpsideReturnBPS <= config.NeutralReturnBPS ||
		config.UpsideReturnBPS > 100000 {
		return fmt.Errorf(
			"scenario returns must satisfy -10000 <= downside < neutral=0 < upside <= 100000 bps",
		)
	}
	if config.ShockBasis != "uniform_position_price_return" {
		return fmt.Errorf(
			"unsupported scenario shock basis %q",
			config.ShockBasis,
		)
	}
	if config.ProbabilityPolicy != "not_estimated" {
		return fmt.Errorf(
			"unsupported scenario probability policy %q",
			config.ProbabilityPolicy,
		)
	}
	return nil
}

func hashPortfolioValuation(
	valuation PortfolioValuationReport,
) (string, error) {
	encoded, err := json.Marshal(valuation)
	if err != nil {
		return "", fmt.Errorf("encode portfolio valuation hash payload: %w", err)
	}
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:]), nil
}

func buildPortfolioScenario(
	positions []PositionValuation,
	definition portfolioScenarioDefinition,
	config PortfolioScenarioConfig,
) (PortfolioScenarioResult, error) {
	result := PortfolioScenarioResult{
		ID:                definition.ID,
		Label:             definition.Label,
		ReturnBPS:         definition.ReturnBPS,
		ReturnPct:         formatBPS(definition.ReturnBPS),
		ProbabilityStatus: config.ProbabilityPolicy,
		Assumption: fmt.Sprintf(
			"all valued position prices change by %s%% from the current snapshot",
			formatBPS(definition.ReturnBPS),
		),
		PossibleInterpretation: "This is a mechanical sensitivity test, not a forecast or a buy/sell signal.",
		ValidationQuestions: []string{
			"Which positions are likely to move differently from the uniform shock assumption?",
			"Which thesis invalidation conditions would be triggered before this scenario is reached?",
			"Would currency moves materially change the result after a verified FX source is added?",
		},
		Currencies:      []ScenarioCurrencyResult{},
		PositionImpacts: make([]ScenarioPositionImpact, 0, len(positions)),
	}
	returnUnits := definition.ReturnBPS * decimal.Scale / 10000
	currencies := map[string]*ScenarioCurrencyResult{}
	for _, position := range positions {
		currentValue := *position.MarketValueUnits
		change, err := decimal.Multiply(currentValue, returnUnits)
		if err != nil {
			return PortfolioScenarioResult{}, err
		}
		scenarioValue, err := addExactInt64(currentValue, change)
		if err != nil {
			return PortfolioScenarioResult{}, err
		}
		currentGross, err := absoluteInt64(currentValue)
		if err != nil {
			return PortfolioScenarioResult{}, err
		}
		scenarioGross, err := absoluteInt64(scenarioValue)
		if err != nil {
			return PortfolioScenarioResult{}, err
		}
		result.PositionImpacts = append(
			result.PositionImpacts,
			ScenarioPositionImpact{
				Instrument:         position.Instrument,
				Currency:           position.Currency,
				CurrentValueUnits:  currentValue,
				CurrentValue:       decimal.Format(currentValue),
				ScenarioValueUnits: scenarioValue,
				ScenarioValue:      decimal.Format(scenarioValue),
				ChangeUnits:        change,
				Change:             decimal.Format(change),
			},
		)

		currency := currencies[position.Currency]
		if currency == nil {
			currency = &ScenarioCurrencyResult{
				Currency: position.Currency,
			}
			currencies[position.Currency] = currency
		}
		currency.CurrentNetValueUnits, err = addExactInt64(
			currency.CurrentNetValueUnits,
			currentValue,
		)
		if err != nil {
			return PortfolioScenarioResult{}, err
		}
		currency.ScenarioNetValueUnits, err = addExactInt64(
			currency.ScenarioNetValueUnits,
			scenarioValue,
		)
		if err != nil {
			return PortfolioScenarioResult{}, err
		}
		currency.ChangeUnits, err = addExactInt64(
			currency.ChangeUnits,
			change,
		)
		if err != nil {
			return PortfolioScenarioResult{}, err
		}
		currency.CurrentGrossExposureUnits, err = addExactInt64(
			currency.CurrentGrossExposureUnits,
			currentGross,
		)
		if err != nil {
			return PortfolioScenarioResult{}, err
		}
		currency.ScenarioGrossExposureUnits, err = addExactInt64(
			currency.ScenarioGrossExposureUnits,
			scenarioGross,
		)
		if err != nil {
			return PortfolioScenarioResult{}, err
		}
	}
	sort.SliceStable(
		result.PositionImpacts,
		func(left int, right int) bool {
			leftMagnitude := scenarioChangeMagnitude(
				result.PositionImpacts[left].ChangeUnits,
			)
			rightMagnitude := scenarioChangeMagnitude(
				result.PositionImpacts[right].ChangeUnits,
			)
			if leftMagnitude == rightMagnitude {
				return strings.ToUpper(
					result.PositionImpacts[left].Instrument.Ticker,
				) < strings.ToUpper(
					result.PositionImpacts[right].Instrument.Ticker,
				)
			}
			return leftMagnitude > rightMagnitude
		},
	)

	currencyNames := make([]string, 0, len(currencies))
	for currency := range currencies {
		currencyNames = append(currencyNames, currency)
	}
	sort.Strings(currencyNames)
	mechanicalParts := make([]string, 0, len(currencyNames))
	for _, name := range currencyNames {
		currency := currencies[name]
		currency.CurrentNetValue = decimal.Format(
			currency.CurrentNetValueUnits,
		)
		currency.ScenarioNetValue = decimal.Format(
			currency.ScenarioNetValueUnits,
		)
		currency.Change = decimal.Format(currency.ChangeUnits)
		currency.CurrentGrossExposure = decimal.Format(
			currency.CurrentGrossExposureUnits,
		)
		currency.ScenarioGrossExposure = decimal.Format(
			currency.ScenarioGrossExposureUnits,
		)
		result.Currencies = append(result.Currencies, *currency)
		mechanicalParts = append(mechanicalParts, fmt.Sprintf(
			"%s net value changes by %s",
			name,
			currency.Change,
		))
	}
	result.MechanicalResult = strings.Join(mechanicalParts, "; ")
	return result, nil
}

func scenarioChangeMagnitude(value int64) uint64 {
	if value < 0 {
		return uint64(-(value + 1)) + 1
	}
	return uint64(value)
}
