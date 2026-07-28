package alerting

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/analysis"
	"github.com/rowlet9g/stock-research-bot/internal/models"
)

const PortfolioCandidateRuleVersion = "portfolio-alert-candidates/v1"

type PortfolioCandidateConfig struct {
	IncludeWatchConcentration bool `json:"include_watch_concentration"`
	IncludeMissingPositions   bool `json:"include_missing_positions"`
	IncludeUnvaluedPositions  bool `json:"include_unvalued_positions"`
	IncludeMissingCostBasis   bool `json:"include_missing_cost_basis"`
}

func DefaultPortfolioCandidateConfig() PortfolioCandidateConfig {
	return PortfolioCandidateConfig{
		IncludeWatchConcentration: true,
		IncludeMissingPositions:   true,
		IncludeUnvaluedPositions:  true,
		IncludeMissingCostBasis:   true,
	}
}

type CandidateEvidence struct {
	Field      string `json:"field"`
	Value      string `json:"value"`
	Unit       string `json:"unit,omitempty"`
	Comparison string `json:"comparison,omitempty"`
	Threshold  string `json:"threshold,omitempty"`
	Basis      string `json:"basis,omitempty"`
}

type Candidate struct {
	Fingerprint            string               `json:"fingerprint"`
	RuleID                 string               `json:"rule_id"`
	Kind                   string               `json:"kind"`
	Severity               models.AlertSeverity `json:"severity"`
	Ticker                 string               `json:"ticker,omitempty"`
	Currency               string               `json:"currency,omitempty"`
	Title                  string               `json:"title"`
	Fact                   string               `json:"fact"`
	PossibleInterpretation string               `json:"possible_interpretation"`
	ValidationQuestions    []string             `json:"validation_questions"`
	Evidence               []CandidateEvidence  `json:"evidence"`
}

type PortfolioCandidateReport struct {
	Status                models.DataStatus        `json:"status"`
	RuleVersion           string                   `json:"rule_version"`
	EvaluatedAt           time.Time                `json:"evaluated_at"`
	InputValuationVersion string                   `json:"input_valuation_version"`
	InputValuationSHA256  string                   `json:"input_valuation_sha256"`
	Config                PortfolioCandidateConfig `json:"config"`
	Candidates            []Candidate              `json:"candidates"`
}

func EvaluatePortfolioCandidates(
	valuation analysis.PortfolioValuationReport,
	evaluatedAt time.Time,
	config PortfolioCandidateConfig,
) (PortfolioCandidateReport, error) {
	if valuation.Version != analysis.PortfolioValuationVersion {
		return PortfolioCandidateReport{}, fmt.Errorf(
			"portfolio alerts require valuation version %q, got %q",
			analysis.PortfolioValuationVersion,
			valuation.Version,
		)
	}
	if evaluatedAt.IsZero() {
		return PortfolioCandidateReport{}, fmt.Errorf(
			"portfolio alert evaluation time is required",
		)
	}
	inputHash, err := analysis.PortfolioValuationSHA256(valuation)
	if err != nil {
		return PortfolioCandidateReport{}, fmt.Errorf(
			"hash portfolio alert input: %w",
			err,
		)
	}
	report := PortfolioCandidateReport{
		Status:                valuation.Status,
		RuleVersion:           PortfolioCandidateRuleVersion,
		EvaluatedAt:           evaluatedAt.UTC(),
		InputValuationVersion: valuation.Version,
		InputValuationSHA256:  inputHash,
		Config:                config,
		Candidates:            []Candidate{},
	}

	for _, position := range valuation.Positions {
		report.Candidates = append(
			report.Candidates,
			positionCandidates(position, valuation.Config, config)...,
		)
	}
	if config.IncludeMissingPositions &&
		valuation.Summary.MissingPositions > 0 {
		report.Candidates = append(
			report.Candidates,
			missingPositionsCandidate(valuation),
		)
	}
	sortCandidates(report.Candidates)
	return report, nil
}

func positionCandidates(
	position analysis.PositionValuation,
	valuationConfig analysis.PortfolioValuationConfig,
	config PortfolioCandidateConfig,
) []Candidate {
	candidates := []Candidate{}
	if position.WeightBPS != nil {
		switch position.Concentration {
		case analysis.ConcentrationHigh:
			candidates = append(candidates, concentrationCandidate(
				position,
				models.AlertSeverityWarning,
				valuationConfig.HighThresholdBPS,
			))
		case analysis.ConcentrationWatch:
			if config.IncludeWatchConcentration {
				candidates = append(candidates, concentrationCandidate(
					position,
					models.AlertSeverityWatch,
					valuationConfig.WatchThresholdBPS,
				))
			}
		}
	}
	if position.QuantityUnits == nil || *position.QuantityUnits == 0 {
		return candidates
	}
	if config.IncludeUnvaluedPositions &&
		position.MarketValueUnits == nil {
		candidates = append(candidates, newCandidate(
			"portfolio.position_unvalued",
			"portfolio_data_quality",
			models.AlertSeverityWatch,
			position.Instrument.Ticker,
			positionCurrency(position),
			"현재 포지션 평가 불가",
			fmt.Sprintf(
				"%s(%s)의 현재 포지션은 저장됐지만 평가금액을 계산하지 못했다.",
				position.Instrument.Name,
				position.Instrument.Ticker,
			),
			"가격 누락이나 통화 불일치가 해소되기 전에는 포트폴리오 노출과 손익을 완전하게 판단할 수 없다.",
			[]string{
				"해당 종목의 최근 가격과 통화가 정상적으로 수집됐는가?",
				"포지션 기준일과 종목 식별자가 최신 상태인가?",
			},
			[]CandidateEvidence{
				{
					Field: "position_status",
					Value: string(position.Status),
				},
			},
		))
	}
	if config.IncludeMissingCostBasis &&
		position.MarketValueUnits != nil &&
		position.CostBasisUnits == nil {
		candidates = append(candidates, newCandidate(
			"portfolio.cost_basis_missing",
			"portfolio_data_quality",
			models.AlertSeverityInfo,
			position.Instrument.Ticker,
			positionCurrency(position),
			"평균 취득단가 확인 필요",
			fmt.Sprintf(
				"%s(%s)의 평가금액은 계산됐지만 원가와 미실현손익은 계산되지 않았다.",
				position.Instrument.Name,
				position.Instrument.Ticker,
			),
			"평균 취득단가가 없으면 현재 가격 대비 손익을 확정할 수 없다.",
			[]string{
				"증권사 잔고에서 평균 취득단가를 다시 확인했는가?",
				"이전 또는 대체입고로 취득가 정보가 누락됐는가?",
			},
			[]CandidateEvidence{
				{
					Field: "average_cost",
					Value: position.AverageCost,
					Unit:  positionCurrency(position),
				},
			},
		))
	}
	return candidates
}

func concentrationCandidate(
	position analysis.PositionValuation,
	severity models.AlertSeverity,
	thresholdBPS int64,
) Candidate {
	return newCandidate(
		"portfolio.position_concentration",
		"portfolio_concentration",
		severity,
		position.Instrument.Ticker,
		position.Currency,
		"단일 종목 집중도 확인 필요",
		fmt.Sprintf(
			"%s(%s)의 %s 총 노출액 비중은 %s%%다.",
			position.Instrument.Name,
			position.Instrument.Ticker,
			position.Currency,
			position.WeightPct,
		),
		"한 종목의 가격 변화가 같은 통화 포트폴리오 결과에 미치는 영향이 클 수 있다.",
		[]string{
			"이 비중이 의도한 위험 한도 안에 있는가?",
			"해당 종목의 투자 가설과 무효화 조건이 최신인가?",
			"다른 통화 자산과 합산하지 않은 통화 내 비중이라는 점을 확인했는가?",
		},
		[]CandidateEvidence{
			{
				Field:      "weight_bps",
				Value:      fmt.Sprintf("%d", *position.WeightBPS),
				Unit:       "bp",
				Comparison: "greater_than_or_equal",
				Threshold:  fmt.Sprintf("%d", thresholdBPS),
				Basis:      "gross_market_value_within_currency",
			},
			{
				Field: "gross_market_value",
				Value: position.GrossMarketValue,
				Unit:  position.Currency,
			},
		},
	)
}

func missingPositionsCandidate(
	valuation analysis.PortfolioValuationReport,
) Candidate {
	return newCandidate(
		"portfolio.current_positions_missing",
		"portfolio_data_quality",
		models.AlertSeverityInfo,
		"",
		"",
		"현재 포지션 스냅샷 확인 필요",
		fmt.Sprintf(
			"저장된 %d개 종목 중 %d개 종목에 현재 포지션 스냅샷이 없다.",
			valuation.Summary.InputInstruments,
			valuation.Summary.MissingPositions,
		),
		"포지션 행이 없는 것은 보유수량 0을 뜻하지 않으므로 전체 포트폴리오 평가의 완전성을 확인할 수 없다.",
		[]string{
			"증권사 잔고 기준의 현재 포지션 CSV를 최근에 가져왔는가?",
			"관심종목과 실제 보유종목을 구분할 별도 상태가 필요한가?",
		},
		[]CandidateEvidence{
			{
				Field: "missing_positions",
				Value: fmt.Sprintf(
					"%d",
					valuation.Summary.MissingPositions,
				),
				Unit: "instruments",
			},
			{
				Field: "input_instruments",
				Value: fmt.Sprintf(
					"%d",
					valuation.Summary.InputInstruments,
				),
				Unit: "instruments",
			},
		},
	)
}

func newCandidate(
	ruleID string,
	kind string,
	severity models.AlertSeverity,
	ticker string,
	currency string,
	title string,
	fact string,
	interpretation string,
	questions []string,
	evidence []CandidateEvidence,
) Candidate {
	identity := strings.Join(
		[]string{
			PortfolioCandidateRuleVersion,
			ruleID,
			strings.ToUpper(strings.TrimSpace(ticker)),
			strings.ToUpper(strings.TrimSpace(currency)),
		},
		"\x1f",
	)
	hash := sha256.Sum256([]byte(identity))
	return Candidate{
		Fingerprint:            hex.EncodeToString(hash[:]),
		RuleID:                 ruleID,
		Kind:                   kind,
		Severity:               severity,
		Ticker:                 strings.TrimSpace(ticker),
		Currency:               strings.ToUpper(strings.TrimSpace(currency)),
		Title:                  title,
		Fact:                   fact,
		PossibleInterpretation: interpretation,
		ValidationQuestions:    append([]string(nil), questions...),
		Evidence:               append([]CandidateEvidence(nil), evidence...),
	}
}

func positionCurrency(
	position analysis.PositionValuation,
) string {
	if strings.TrimSpace(position.Currency) != "" {
		return position.Currency
	}
	return position.Instrument.Currency
}

func sortCandidates(candidates []Candidate) {
	severityOrder := map[models.AlertSeverity]int{
		models.AlertSeverityWarning: 0,
		models.AlertSeverityWatch:   1,
		models.AlertSeverityInfo:    2,
	}
	sort.SliceStable(candidates, func(left int, right int) bool {
		leftOrder := severityOrder[candidates[left].Severity]
		rightOrder := severityOrder[candidates[right].Severity]
		if leftOrder != rightOrder {
			return leftOrder < rightOrder
		}
		if candidates[left].RuleID != candidates[right].RuleID {
			return candidates[left].RuleID < candidates[right].RuleID
		}
		return strings.ToUpper(candidates[left].Ticker) <
			strings.ToUpper(candidates[right].Ticker)
	})
}
