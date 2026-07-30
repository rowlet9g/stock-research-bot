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

const PortfolioCandidateRuleVersion = "portfolio-alert-candidates/v2"

type PortfolioCandidateConfig struct {
	IncludeWatchConcentration bool  `json:"include_watch_concentration"`
	IncludeMissingPositions   bool  `json:"include_missing_positions"`
	IncludeUnvaluedPositions  bool  `json:"include_unvalued_positions"`
	IncludeMissingCostBasis   bool  `json:"include_missing_cost_basis"`
	IncludeLossFromCost       bool  `json:"include_loss_from_cost"`
	IncludeWeakTrend          bool  `json:"include_weak_trend"`
	IncludeMissingThesis      bool  `json:"include_missing_thesis"`
	LossWatchThresholdBPS     int64 `json:"loss_watch_threshold_bps"`
	LossWarningThresholdBPS   int64 `json:"loss_warning_threshold_bps"`
}

func DefaultPortfolioCandidateConfig() PortfolioCandidateConfig {
	return PortfolioCandidateConfig{
		IncludeWatchConcentration: true,
		IncludeMissingPositions:   true,
		IncludeUnvaluedPositions:  true,
		IncludeMissingCostBasis:   true,
		IncludeLossFromCost:       true,
		IncludeWeakTrend:          true,
		IncludeMissingThesis:      true,
		LossWatchThresholdBPS:     -1000,
		LossWarningThresholdBPS:   -2000,
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
	if err := validatePortfolioCandidateConfig(config); err != nil {
		return PortfolioCandidateReport{}, err
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
	if config.IncludeLossFromCost &&
		position.UnrealizedReturnBPS != nil {
		switch {
		case *position.UnrealizedReturnBPS <= config.LossWarningThresholdBPS:
			candidates = append(candidates, lossFromCostCandidate(
				position,
				models.AlertSeverityWarning,
				config.LossWarningThresholdBPS,
			))
		case *position.UnrealizedReturnBPS <= config.LossWatchThresholdBPS:
			candidates = append(candidates, lossFromCostCandidate(
				position,
				models.AlertSeverityWatch,
				config.LossWatchThresholdBPS,
			))
		}
	}
	if config.IncludeWeakTrend && weakPriceTrend(position) {
		candidates = append(candidates, weakTrendCandidate(position))
	}
	if config.IncludeMissingThesis && position.Thesis == nil {
		candidates = append(candidates, missingThesisCandidate(position))
	}
	return candidates
}

func validatePortfolioCandidateConfig(
	config PortfolioCandidateConfig,
) error {
	if config.LossWarningThresholdBPS >= config.LossWatchThresholdBPS ||
		config.LossWatchThresholdBPS >= 0 ||
		config.LossWarningThresholdBPS < -10000 {
		return fmt.Errorf(
			"portfolio loss thresholds must satisfy -10000 <= warning < watch < 0 bps",
		)
	}
	return nil
}

func concentrationCandidate(
	position analysis.PositionValuation,
	severity models.AlertSeverity,
	thresholdBPS int64,
) Candidate {
	evidence := []CandidateEvidence{
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
	}
	interpretation := "한 종목의 가격 변화가 같은 통화 포트폴리오 결과에 미치는 영향이 클 수 있다."
	if len(position.RebalanceReferences) > 0 {
		parts := make([]string, 0, len(position.RebalanceReferences))
		for _, reference := range position.RebalanceReferences {
			parts = append(parts, fmt.Sprintf(
				"%s%% 경계까지 같은 통화 안에서 재배분하는 기계적 참고액은 %s %s",
				reference.TargetWeightPct,
				reference.ReallocationValue,
				position.Currency,
			))
			evidence = append(evidence, CandidateEvidence{
				Field:      "rebalance_reference",
				Value:      reference.ReallocationValue,
				Unit:       position.Currency,
				Comparison: reference.TargetLabel,
				Threshold:  reference.TargetWeightPct,
				Basis:      reference.Basis,
			})
		}
		interpretation += " " + strings.Join(parts, "; ") +
			"이다. 이는 매도 지시가 아니라 총노출 유지 가정의 민감도 계산이다."
	}
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
		interpretation,
		[]string{
			"이 비중이 의도한 위험 한도 안에 있는가?",
			"해당 종목의 투자 가설과 무효화 조건이 최신인가?",
			"다른 통화 자산과 합산하지 않은 통화 내 비중이라는 점을 확인했는가?",
		},
		evidence,
	)
}

func lossFromCostCandidate(
	position analysis.PositionValuation,
	severity models.AlertSeverity,
	thresholdBPS int64,
) Candidate {
	return newCandidate(
		"portfolio.position_loss_from_cost",
		"portfolio_cost_review",
		severity,
		position.Instrument.Ticker,
		positionCurrency(position),
		"평균단가 대비 손실 구간 재검토",
		fmt.Sprintf(
			"%s(%s)의 최근가격은 %s %s, 평균단가는 %s %s이며 평단 대비 수익률은 %s%%, 미실현손익은 %s %s다.",
			position.Instrument.Name,
			position.Instrument.Ticker,
			position.LastPrice,
			positionCurrency(position),
			position.AverageCost,
			positionCurrency(position),
			position.UnrealizedReturnPct,
			position.UnrealizedPL,
			positionCurrency(position),
		),
		"손실률만으로 매수가가 틀렸거나 매도가 필요하다고 결론낼 수 없다. 현재 가격 추세와 투자 가설의 무효화 조건이 함께 충족되는지 확인해야 한다.",
		[]string{
			"매수 당시 가설을 훼손하는 실적, 공시 또는 산업 변화가 확인됐는가?",
			"손실이 가격 변동인지 기업가치 변화인지 구분할 근거가 있는가?",
			"추가매수는 기존 가설의 검증 결과인가, 평단을 낮추려는 행동인가?",
		},
		[]CandidateEvidence{
			{
				Field:      "unrealized_return_bps",
				Value:      fmt.Sprintf("%d", *position.UnrealizedReturnBPS),
				Unit:       "bp",
				Comparison: "less_than_or_equal",
				Threshold:  fmt.Sprintf("%d", thresholdBPS),
				Basis:      "current_price_vs_stored_average_cost",
			},
			{
				Field: "unrealized_pl",
				Value: position.UnrealizedPL,
				Unit:  positionCurrency(position),
			},
			{
				Field: "market_trend",
				Value: position.MarketMetrics.Trend,
			},
		},
	)
}

func weakPriceTrend(position analysis.PositionValuation) bool {
	if position.MarketMetrics.Trend != "below_ma20_and_ma60" {
		return false
	}
	return optionalFloatNegative(position.MarketMetrics.ReturnPct20D) ||
		optionalFloatNegative(position.MarketMetrics.ReturnPct60D)
}

func optionalFloatNegative(value *float64) bool {
	return value != nil && *value < 0
}

func weakTrendCandidate(
	position analysis.PositionValuation,
) Candidate {
	return newCandidate(
		"portfolio.position_weak_trend",
		"portfolio_price_review",
		models.AlertSeverityWatch,
		position.Instrument.Ticker,
		positionCurrency(position),
		"중단기 가격 추세 약화 확인 필요",
		fmt.Sprintf(
			"%s(%s)의 최근가격 %s %s는 20일 이동평균 %s와 60일 이동평균 %s를 모두 밑돌며, 20일 수익률은 %s%%, 60일 수익률은 %s%%다.",
			position.Instrument.Name,
			position.Instrument.Ticker,
			position.LastPrice,
			positionCurrency(position),
			formatOptionalMetric(position.MarketMetrics.MA20),
			formatOptionalMetric(position.MarketMetrics.MA60),
			formatOptionalMetric(position.MarketMetrics.ReturnPct20D),
			formatOptionalMetric(position.MarketMetrics.ReturnPct60D),
		),
		"이동평균 하회는 관측된 가격 약세이지 기업가치 하락의 증명이나 매도 신호가 아니다. 거래량, 변동성, 공시와 투자 가설을 함께 확인해야 한다.",
		[]string{
			"약세가 시장 전체 움직임과 비교해 종목 고유 현상인가?",
			"거래량 증가와 함께 하락해 수급 변화가 커졌는가?",
			"예상 보유기간에서 이 가격 추세가 실제 무효화 조건에 해당하는가?",
		},
		[]CandidateEvidence{
			{
				Field: "trend",
				Value: position.MarketMetrics.Trend,
			},
			{
				Field: "max_drawdown_pct_6m",
				Value: formatOptionalMetric(
					position.MarketMetrics.MaxDrawdownPct6M,
				),
				Unit: "%",
			},
			{
				Field: "volume_ratio_20d",
				Value: formatOptionalMetric(
					position.MarketMetrics.VolumeRatio20D,
				),
				Unit: "x",
			},
		},
	)
}

func missingThesisCandidate(
	position analysis.PositionValuation,
) Candidate {
	return newCandidate(
		"portfolio.position_thesis_missing",
		"portfolio_thesis_quality",
		models.AlertSeverityInfo,
		position.Instrument.Ticker,
		positionCurrency(position),
		"투자 가설 등록 필요",
		fmt.Sprintf(
			"%s(%s)는 현재 보유 중이지만 매수 이유, 무효화 조건, 예상 보유기간과 확인 지표가 저장되어 있지 않다.",
			position.Instrument.Name,
			position.Instrument.Ticker,
		),
		"투자 가설이 없으면 가격 하락이 기회인지, 보유 근거가 훼손된 것인지 재현 가능하게 판단할 수 없다.",
		[]string{
			"이 종목을 매수한 핵심 이유를 한 문장으로 설명할 수 있는가?",
			"어떤 사실이 확인되면 기존 가설을 폐기하거나 비중을 재검토할 것인가?",
			"예상 보유기간과 정기적으로 확인할 재무·산업 지표는 무엇인가?",
		},
		[]CandidateEvidence{
			{
				Field: "thesis_status",
				Value: "missing",
			},
		},
	)
}

func formatOptionalMetric(value *float64) string {
	if value == nil {
		return "N/A"
	}
	return fmt.Sprintf("%.2f", *value)
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
