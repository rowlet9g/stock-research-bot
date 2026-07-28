package analysis

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/decimal"
	"github.com/rowlet9g/stock-research-bot/internal/models"
)

const RiskRuleSetVersion = "risk-rules/v1"

type RiskSeverity string

const (
	RiskSeverityInfo    RiskSeverity = "info"
	RiskSeverityWatch   RiskSeverity = "watch"
	RiskSeverityWarning RiskSeverity = "warning"
)

type RiskCategory string

const (
	RiskCategoryPrice       RiskCategory = "price"
	RiskCategoryDisclosure  RiskCategory = "disclosure"
	RiskCategoryFinancial   RiskCategory = "financial"
	RiskCategoryPortfolio   RiskCategory = "portfolio"
	RiskCategoryDataQuality RiskCategory = "data_quality"
)

type RiskEvidence struct {
	Kind          string     `json:"kind"`
	Field         string     `json:"field"`
	Value         string     `json:"value"`
	Unit          string     `json:"unit,omitempty"`
	SourceURL     string     `json:"source_url,omitempty"`
	ObservedAt    *time.Time `json:"observed_at,omitempty"`
	ReceiptNo     string     `json:"receipt_no,omitempty"`
	AccountID     string     `json:"account_id,omitempty"`
	ContentSHA256 string     `json:"content_sha256,omitempty"`
}

type RiskFinding struct {
	Fingerprint            string         `json:"fingerprint"`
	RuleID                 string         `json:"rule_id"`
	Category               RiskCategory   `json:"category"`
	Severity               RiskSeverity   `json:"severity"`
	Title                  string         `json:"title"`
	Fact                   string         `json:"fact"`
	PossibleInterpretation string         `json:"possible_interpretation"`
	ValidationQuestions    []string       `json:"validation_questions"`
	Evidence               []RiskEvidence `json:"evidence"`
}

type RiskAssessment struct {
	Status             models.DataStatus    `json:"status"`
	RuleSetVersion     string               `json:"rule_set_version"`
	InputSchemaVersion string               `json:"input_schema_version"`
	InputSHA256        string               `json:"input_sha256"`
	EvaluatedAt        time.Time            `json:"evaluated_at"`
	Config             RiskRuleConfig       `json:"config"`
	Findings           []RiskFinding        `json:"findings"`
	InputIssues        []AnalysisInputIssue `json:"input_issues"`
}

type RiskRuleConfig struct {
	CurrentRatioWatchBelow            string `json:"current_ratio_watch_below"`
	DebtToEquityWatchAbove            string `json:"debt_to_equity_watch_above"`
	RevenueGrowthWatchBelow           string `json:"revenue_growth_watch_below"`
	OperatingIncomeGrowthWarningBelow string `json:"operating_income_growth_warning_below"`
	NetIncomeGrowthWatchBelow         string `json:"net_income_growth_watch_below"`
	OperatingMarginWarningBelow       string `json:"operating_margin_warning_below"`
	NetMarginWatchBelow               string `json:"net_margin_watch_below"`
	PositionStaleAfterDays            int    `json:"position_stale_after_days"`
	DisclosureLookbackDays            int    `json:"disclosure_lookback_days"`
	MaxDisclosureFindings             int    `json:"max_disclosure_findings"`
}

func DefaultRiskRuleConfig() RiskRuleConfig {
	return RiskRuleConfig{
		CurrentRatioWatchBelow:            "100",
		DebtToEquityWatchAbove:            "200",
		RevenueGrowthWatchBelow:           "-10",
		OperatingIncomeGrowthWarningBelow: "-30",
		NetIncomeGrowthWatchBelow:         "-30",
		OperatingMarginWarningBelow:       "0",
		NetMarginWatchBelow:               "0",
		PositionStaleAfterDays:            30,
		DisclosureLookbackDays:            90,
		MaxDisclosureFindings:             20,
	}
}

func EvaluateRiskSnapshot(
	snapshot AnalysisInputSnapshot,
	evaluatedAt time.Time,
	config RiskRuleConfig,
) (RiskAssessment, error) {
	if strings.TrimSpace(snapshot.InputSHA256) == "" {
		return RiskAssessment{}, fmt.Errorf(
			"risk assessment input hash is required",
		)
	}
	if evaluatedAt.IsZero() {
		return RiskAssessment{}, fmt.Errorf(
			"risk assessment evaluation time is required",
		)
	}
	if err := validateRiskRuleConfig(config); err != nil {
		return RiskAssessment{}, err
	}

	findings := []RiskFinding{}
	findings = append(findings, evaluatePriceSignalRisks(snapshot)...)
	financialFindings, err := evaluateFinancialRisks(snapshot, config)
	if err != nil {
		return RiskAssessment{}, err
	}
	findings = append(findings, financialFindings...)
	findings = append(
		findings,
		evaluatePortfolioRisks(snapshot, evaluatedAt.UTC(), config)...,
	)
	findings = append(
		findings,
		evaluateDisclosureRisks(snapshot, evaluatedAt.UTC(), config)...,
	)
	sortRiskFindings(findings)

	status := models.DataStatusAvailable
	if snapshot.Status != models.DataStatusAvailable {
		status = models.DataStatusPartial
	}
	inputIssues := append([]AnalysisInputIssue(nil), snapshot.Issues...)
	if inputIssues == nil {
		inputIssues = []AnalysisInputIssue{}
	}
	return RiskAssessment{
		Status:             status,
		RuleSetVersion:     RiskRuleSetVersion,
		InputSchemaVersion: snapshot.SchemaVersion,
		InputSHA256:        snapshot.InputSHA256,
		EvaluatedAt:        evaluatedAt.UTC(),
		Config:             config,
		Findings:           findings,
		InputIssues:        inputIssues,
	}, nil
}

func validateRiskRuleConfig(config RiskRuleConfig) error {
	thresholds := map[string]string{
		"current ratio":           config.CurrentRatioWatchBelow,
		"debt to equity":          config.DebtToEquityWatchAbove,
		"revenue growth":          config.RevenueGrowthWatchBelow,
		"operating income growth": config.OperatingIncomeGrowthWarningBelow,
		"net income growth":       config.NetIncomeGrowthWatchBelow,
		"operating margin":        config.OperatingMarginWarningBelow,
		"net margin":              config.NetMarginWatchBelow,
	}
	for label, value := range thresholds {
		if _, ok := new(big.Rat).SetString(strings.TrimSpace(value)); !ok {
			return fmt.Errorf("invalid %s risk threshold %q", label, value)
		}
	}
	if config.PositionStaleAfterDays <= 0 {
		return fmt.Errorf("position stale days must be greater than zero")
	}
	if config.DisclosureLookbackDays <= 0 {
		return fmt.Errorf("disclosure lookback days must be greater than zero")
	}
	if config.MaxDisclosureFindings <= 0 {
		return fmt.Errorf("maximum disclosure findings must be greater than zero")
	}
	return nil
}

func evaluatePriceSignalRisks(
	snapshot AnalysisInputSnapshot,
) []RiskFinding {
	findings := []RiskFinding{}
	for _, signal := range snapshot.Signals {
		severity := RiskSeverity("")
		switch signal.Level {
		case "error":
			severity = RiskSeverityWarning
		case "warning":
			severity = RiskSeverityWatch
		default:
			continue
		}
		observedAt := snapshot.Price.Source.ObservedAt
		identity := signal.Title
		if observedAt != nil {
			identity += "|" + observedAt.UTC().Format(time.RFC3339Nano)
		}
		findings = append(findings, newRiskFinding(
			"price.signal",
			RiskCategoryPrice,
			severity,
			signal.Title,
			signal.Detail,
			"가격 신호만으로 원인을 확정할 수 없으며 공시, 뉴스, 시장 요인을 함께 확인해야 한다.",
			[]string{
				"같은 시점에 기업 공시나 주요 뉴스가 있었는가?",
				"시장 또는 섹터 전체에서도 비슷한 움직임이 나타났는가?",
			},
			[]RiskEvidence{
				{
					Kind:       "price_signal",
					Field:      "signal",
					Value:      signal.Detail,
					SourceURL:  snapshot.Price.Source.SourceURL,
					ObservedAt: observedAt,
				},
			},
			identity,
		))
	}
	return findings
}

type financialRatioRiskRule struct {
	ruleID         string
	ratioKey       string
	severity       RiskSeverity
	threshold      string
	triggerBelow   bool
	title          string
	interpretation string
	questions      []string
}

func evaluateFinancialRisks(
	snapshot AnalysisInputSnapshot,
	config RiskRuleConfig,
) ([]RiskFinding, error) {
	if snapshot.Financials.Status == models.DataStatusNotRequested ||
		snapshot.Financials.Status == models.DataStatusEmpty ||
		snapshot.Financials.Status == models.DataStatusUnavailable {
		return []RiskFinding{}, nil
	}

	rules := []financialRatioRiskRule{
		{
			ruleID:         "financial.current_ratio_low",
			ratioKey:       "current_ratio_pct",
			severity:       RiskSeverityWarning,
			threshold:      config.CurrentRatioWatchBelow,
			triggerBelow:   true,
			title:          "유동비율 기준 미달",
			interpretation: "단기 채무 대응 여력이 제한적일 가능성이 있어 부채 만기와 현금성 자산을 함께 확인해야 한다.",
			questions: []string{
				"유동부채 중 단기간에 실제 상환해야 하는 항목은 무엇인가?",
				"현금및현금성자산과 영업현금흐름으로 부족분을 감당할 수 있는가?",
			},
		},
		{
			ruleID:         "financial.debt_to_equity_high",
			ratioKey:       "debt_to_equity_pct",
			severity:       RiskSeverityWarning,
			threshold:      config.DebtToEquityWatchAbove,
			triggerBelow:   false,
			title:          "부채비율 기준 초과",
			interpretation: "재무 레버리지와 이자·상환 부담을 추가로 검토할 필요가 있다.",
			questions: []string{
				"차입금 만기 구조와 평균 조달금리는 어떻게 변하고 있는가?",
				"부채 증가가 영업 확장, 인수, 운전자본 중 무엇에서 발생했는가?",
			},
		},
		{
			ruleID:         "financial.revenue_growth_low",
			ratioKey:       "revenue_growth_pct",
			severity:       RiskSeverityWatch,
			threshold:      config.RevenueGrowthWatchBelow,
			triggerBelow:   true,
			title:          "매출 감소 폭 확인 필요",
			interpretation: "수요, 가격, 환율 또는 사업 구조 변화 중 어떤 요인이 매출 감소를 만들었는지 분해해야 한다.",
			questions: []string{
				"매출 감소가 판매량과 판매가격 중 어디에서 발생했는가?",
				"일회성 기저효과나 환율 영향을 제외해도 감소 추세가 남는가?",
			},
		},
		{
			ruleID:         "financial.operating_income_growth_low",
			ratioKey:       "operating_income_growth_pct",
			severity:       RiskSeverityWarning,
			threshold:      config.OperatingIncomeGrowthWarningBelow,
			triggerBelow:   true,
			title:          "영업이익 감소 폭 확인 필요",
			interpretation: "본업 수익성 악화 가능성이 있으므로 매출총이익률과 비용 항목을 확인해야 한다.",
			questions: []string{
				"원가율과 판매관리비 중 어느 항목이 영업이익을 훼손했는가?",
				"회사가 제시한 수익성 회복 근거와 시점이 있는가?",
			},
		},
		{
			ruleID:         "financial.net_income_growth_low",
			ratioKey:       "net_income_growth_pct",
			severity:       RiskSeverityWatch,
			threshold:      config.NetIncomeGrowthWatchBelow,
			triggerBelow:   true,
			title:          "순이익 감소 폭 확인 필요",
			interpretation: "영업 외 손익, 금융비용, 세금 또는 일회성 항목의 영향을 분리해 확인해야 한다.",
			questions: []string{
				"순이익 감소가 영업이익 변화로 설명되는가?",
				"평가손익이나 처분손익 같은 일회성 항목이 포함됐는가?",
			},
		},
		{
			ruleID:         "financial.operating_margin_negative",
			ratioKey:       "operating_margin_pct",
			severity:       RiskSeverityWarning,
			threshold:      config.OperatingMarginWarningBelow,
			triggerBelow:   true,
			title:          "영업이익률 음수",
			interpretation: "현재 기간의 본업이 영업손실 상태일 가능성이 있다.",
			questions: []string{
				"손실이 구조적인지 일회성 비용 때문인지 구분할 수 있는가?",
				"손익분기점 도달을 위해 필요한 매출 또는 비용 개선 폭은 얼마인가?",
			},
		},
		{
			ruleID:         "financial.net_margin_negative",
			ratioKey:       "net_margin_pct",
			severity:       RiskSeverityWatch,
			threshold:      config.NetMarginWatchBelow,
			triggerBelow:   true,
			title:          "순이익률 음수",
			interpretation: "최종 손익이 적자일 가능성이 있어 영업 외 항목과 현금흐름을 함께 봐야 한다.",
			questions: []string{
				"적자의 주된 원인이 영업손실인가 영업 외 손실인가?",
				"회계상 적자와 실제 현금 유출의 차이는 무엇인가?",
			},
		},
	}

	ratioIndex := make(map[string]FinancialRatio, len(snapshot.Financials.Ratios))
	for _, ratio := range snapshot.Financials.Ratios {
		ratioIndex[ratio.Key] = ratio
	}
	findings := []RiskFinding{}
	for _, rule := range rules {
		ratio, exists := ratioIndex[rule.ratioKey]
		if !exists || ratio.Status != FinancialMetricAvailable {
			continue
		}
		triggered, err := decimalThresholdTriggered(
			ratio.Value,
			rule.threshold,
			rule.triggerBelow,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"evaluate financial ratio %s: %w",
				rule.ratioKey,
				err,
			)
		}
		if !triggered {
			continue
		}
		comparison := "미만"
		if !rule.triggerBelow {
			comparison = "초과"
		}
		findings = append(findings, newRiskFinding(
			rule.ruleID,
			RiskCategoryFinancial,
			rule.severity,
			rule.title,
			fmt.Sprintf(
				"%s은 %s%%로 규칙 기준 %s%% %s이다.",
				ratio.Label,
				ratio.Value,
				rule.threshold,
				comparison,
			),
			rule.interpretation,
			rule.questions,
			[]RiskEvidence{
				{
					Kind:          "financial_ratio",
					Field:         ratio.Key,
					Value:         ratio.Value,
					Unit:          ratio.Unit,
					SourceURL:     snapshot.Financials.Source.SourceURL,
					ObservedAt:    snapshot.Financials.Source.ObservedAt,
					ReceiptNo:     snapshot.Financials.ReceiptNo,
					ContentSHA256: snapshot.Financials.ContentSHA256,
				},
			},
			snapshot.Financials.ContentSHA256,
			ratio.Key,
			ratio.Value,
		))
	}

	operatingCashFlow, exists := financialMetricByKey(
		snapshot.Financials.Metrics,
		FinancialMetricOperatingCashFlow,
	)
	if exists &&
		operatingCashFlow.Status == FinancialMetricAvailable &&
		strings.HasPrefix(operatingCashFlow.CurrentAmount, "-") {
		findings = append(findings, newRiskFinding(
			"financial.operating_cash_flow_negative",
			RiskCategoryFinancial,
			RiskSeverityWarning,
			"영업활동현금흐름 음수",
			fmt.Sprintf(
				"%s의 현재 금액은 %s %s이다.",
				operatingCashFlow.Label,
				operatingCashFlow.CurrentAmount,
				operatingCashFlow.Currency,
			),
			"회계상 이익과 실제 현금창출력 사이에 차이가 있을 수 있어 원인을 확인해야 한다.",
			[]string{
				"운전자본 증가와 일회성 현금 유출 중 어느 요인이 큰가?",
				"영업활동현금흐름 음수가 반복되는가?",
			},
			[]RiskEvidence{
				financialMetricEvidence(snapshot.Financials, operatingCashFlow),
			},
			snapshot.Financials.ContentSHA256,
			operatingCashFlow.AccountID,
			operatingCashFlow.CurrentAmount,
		))
	}

	for _, metric := range snapshot.Financials.Metrics {
		if metric.Status != FinancialMetricMissing &&
			metric.Status != FinancialMetricAmbiguous {
			continue
		}
		findings = append(findings, newRiskFinding(
			"data.financial_metric_unavailable",
			RiskCategoryDataQuality,
			RiskSeverityWatch,
			"핵심 재무항목 확인 불가",
			fmt.Sprintf(
				"%s 항목 상태는 %s이다: %s",
				metric.Label,
				metric.Status,
				metric.Message,
			),
			"이 항목에 의존하는 비율과 해석은 현재 입력만으로 확정할 수 없다.",
			[]string{
				"개별 재무제표 또는 다른 보고서에는 해당 표준계정이 존재하는가?",
				"기업 확장계정 원문을 직접 확인해야 하는가?",
			},
			[]RiskEvidence{
				financialMetricEvidence(snapshot.Financials, metric),
			},
			snapshot.Financials.ContentSHA256,
			metric.Key,
			string(metric.Status),
		))
	}
	for _, ratio := range snapshot.Financials.Ratios {
		if ratio.Status != FinancialMetricNotComparable {
			continue
		}
		findings = append(findings, newRiskFinding(
			"data.financial_ratio_not_comparable",
			RiskCategoryDataQuality,
			RiskSeverityInfo,
			"재무비율 기간 비교 불가",
			fmt.Sprintf(
				"%s은 비교 불가 상태이다: %s",
				ratio.Label,
				ratio.Message,
			),
			"음수 또는 0인 비교 기준처럼 단순 증감률이 의미를 잃는 경우다.",
			[]string{
				"절대 금액 변화와 흑자·적자 전환 여부를 직접 확인했는가?",
			},
			[]RiskEvidence{
				{
					Kind:          "financial_ratio",
					Field:         ratio.Key,
					Value:         string(ratio.Status),
					SourceURL:     snapshot.Financials.Source.SourceURL,
					ObservedAt:    snapshot.Financials.Source.ObservedAt,
					ReceiptNo:     snapshot.Financials.ReceiptNo,
					ContentSHA256: snapshot.Financials.ContentSHA256,
				},
			},
			snapshot.Financials.ContentSHA256,
			ratio.Key,
			string(ratio.Status),
		))
	}
	return findings, nil
}

func evaluatePortfolioRisks(
	snapshot AnalysisInputSnapshot,
	evaluatedAt time.Time,
	config RiskRuleConfig,
) []RiskFinding {
	portfolio := snapshot.Portfolio
	findings := []RiskFinding{}
	if portfolio.Position == nil && len(portfolio.Trades) > 0 {
		findings = append(findings, newRiskFinding(
			"portfolio.position_missing",
			RiskCategoryDataQuality,
			RiskSeverityWarning,
			"현재 포지션 확인 필요",
			fmt.Sprintf(
				"저장된 거래는 %d건이지만 현재 포지션 스냅샷이 없다.",
				len(portfolio.Trades),
			),
			"전량 매도 여부와 현재 보유수량을 구분할 수 없어 평가금액과 손익 계산을 확정할 수 없다.",
			[]string{
				"해당 종목을 전량 매도한 상태인가?",
				"아직 보유 중이라면 최신 수량과 평균단가를 기록했는가?",
			},
			[]RiskEvidence{
				{
					Kind:  "portfolio",
					Field: "trade_count",
					Value: strconv.Itoa(len(portfolio.Trades)),
				},
			},
			portfolio.Instrument.Ticker,
			strconv.Itoa(len(portfolio.Trades)),
		))
	}
	if portfolio.Position != nil {
		position := portfolio.Position
		quantity := decimal.Format(position.QuantityUnits)
		if position.QuantityUnits != 0 && position.AverageCostUnits == 0 {
			findings = append(findings, newRiskFinding(
				"portfolio.average_cost_missing",
				RiskCategoryDataQuality,
				RiskSeverityWarning,
				"평균단가 확인 필요",
				fmt.Sprintf(
					"현재 수량은 %s이지만 평균단가는 0으로 저장되어 있다.",
					quantity,
				),
				"평가손익과 투자 가설의 가격 기준을 계산할 수 없다.",
				[]string{
					"증권사 잔고 기준 평균단가를 최신 값으로 기록했는가?",
				},
				[]RiskEvidence{
					{
						Kind:  "portfolio",
						Field: "average_cost",
						Value: decimal.Format(position.AverageCostUnits),
						Unit:  position.Currency,
					},
				},
				portfolio.Instrument.Ticker,
				position.UpdatedAt.UTC().Format(time.RFC3339Nano),
			))
		}
		if position.QuantityUnits != 0 &&
			!strings.EqualFold(position.Currency, portfolio.Instrument.Currency) {
			findings = append(findings, newRiskFinding(
				"portfolio.currency_mismatch",
				RiskCategoryDataQuality,
				RiskSeverityWarning,
				"포지션 통화 불일치",
				fmt.Sprintf(
					"포지션 통화는 %s, 종목 기준 통화는 %s이다.",
					position.Currency,
					portfolio.Instrument.Currency,
				),
				"통화 단위가 다르면 평균단가와 손익 계산이 잘못될 수 있다.",
				[]string{
					"포지션 평균단가가 실제 거래 통화로 저장되어 있는가?",
				},
				[]RiskEvidence{
					{
						Kind:  "portfolio",
						Field: "position_currency",
						Value: position.Currency,
					},
					{
						Kind:  "instrument",
						Field: "currency",
						Value: portfolio.Instrument.Currency,
					},
				},
				portfolio.Instrument.Ticker,
				position.Currency,
				portfolio.Instrument.Currency,
			))
		}
		staleAt := position.AsOf.AddDate(
			0,
			0,
			config.PositionStaleAfterDays,
		)
		if position.QuantityUnits != 0 && evaluatedAt.After(staleAt) {
			ageDays := int(evaluatedAt.Sub(position.AsOf).Hours() / 24)
			findings = append(findings, newRiskFinding(
				"portfolio.position_stale",
				RiskCategoryDataQuality,
				RiskSeverityWatch,
				"포지션 기준일 경과",
				fmt.Sprintf(
					"현재 포지션 기준일은 %s로 평가시각보다 %d일 전이다.",
					position.AsOf.UTC().Format("2006-01-02"),
					ageDays,
				),
				"이후 거래가 반영되지 않았다면 보유수량과 평균단가가 실제 계좌와 다를 수 있다.",
				[]string{
					"기준일 이후 매수, 매도, 분할 또는 배당 재투자가 있었는가?",
				},
				[]RiskEvidence{
					{
						Kind:       "portfolio",
						Field:      "position_as_of",
						Value:      position.AsOf.UTC().Format(time.RFC3339),
						ObservedAt: timePointer(position.AsOf.UTC()),
					},
				},
				portfolio.Instrument.Ticker,
				position.AsOf.UTC().Format(time.RFC3339Nano),
			))
		}
		if position.QuantityUnits != 0 && portfolio.Thesis == nil {
			findings = append(findings, newRiskFinding(
				"portfolio.thesis_missing",
				RiskCategoryPortfolio,
				RiskSeverityWatch,
				"투자 가설 미기록",
				fmt.Sprintf(
					"현재 수량 %s에 대응하는 투자 가설이 저장되어 있지 않다.",
					quantity,
				),
				"보유 근거와 매도·재검토 조건을 사후에 일관되게 평가하기 어렵다.",
				[]string{
					"처음 보유한 핵심 근거는 무엇인가?",
					"어떤 조건에서 가설이 무효화되는가?",
				},
				[]RiskEvidence{
					{
						Kind:  "portfolio",
						Field: "quantity",
						Value: quantity,
					},
				},
				portfolio.Instrument.Ticker,
				position.UpdatedAt.UTC().Format(time.RFC3339Nano),
			))
		}
	}
	if portfolio.Thesis != nil {
		thesis := portfolio.Thesis
		if strings.TrimSpace(thesis.InvalidationCondition) == "" {
			findings = append(findings, newRiskFinding(
				"portfolio.thesis_invalidation_missing",
				RiskCategoryPortfolio,
				RiskSeverityWatch,
				"가설 무효화 조건 미기록",
				"투자 가설은 있지만 무효화 조건이 비어 있다.",
				"불리한 증거가 나타나도 가설을 계속 유지하는 확증편향 위험이 커질 수 있다.",
				[]string{
					"어떤 실적, 가격, 경쟁 또는 규제 변화가 가설을 무효화하는가?",
				},
				[]RiskEvidence{
					{
						Kind:  "portfolio",
						Field: "thesis_summary",
						Value: thesis.Summary,
					},
				},
				portfolio.Instrument.Ticker,
				thesis.UpdatedAt.UTC().Format(time.RFC3339Nano),
			))
		}
		if len(thesis.CheckMetrics) == 0 {
			findings = append(findings, newRiskFinding(
				"portfolio.thesis_metrics_missing",
				RiskCategoryPortfolio,
				RiskSeverityInfo,
				"가설 확인 지표 미기록",
				"투자 가설을 정기적으로 검증할 확인 지표가 비어 있다.",
				"가설을 정량적으로 추적하기 어렵다.",
				[]string{
					"다음 실적 발표에서 반드시 확인할 지표는 무엇인가?",
				},
				[]RiskEvidence{
					{
						Kind:  "portfolio",
						Field: "thesis_summary",
						Value: thesis.Summary,
					},
				},
				portfolio.Instrument.Ticker,
				thesis.UpdatedAt.UTC().Format(time.RFC3339Nano),
			))
		}
	}
	return findings
}

type disclosureRiskRule struct {
	ruleID         string
	severity       RiskSeverity
	title          string
	keywords       []string
	interpretation string
	questions      []string
}

var disclosureRiskRules = []disclosureRiskRule{
	{
		ruleID:   "disclosure.severe_event_title",
		severity: RiskSeverityWarning,
		title:    "중대 사건 공시 제목 확인",
		keywords: []string{
			"상장폐지",
			"관리종목",
			"회생절차",
			"파산",
			"부도",
			"횡령",
			"배임",
			"감사의견거절",
			"감사의견부적정",
			"영업정지",
		},
		interpretation: "제목만으로 사건의 확정 범위나 재무 영향을 판단할 수 없으므로 공시 본문을 우선 확인해야 한다.",
		questions: []string{
			"공시 본문에서 발생 금액, 적용 범위와 회사 대응은 무엇인가?",
			"거래정지, 계속기업 또는 현금흐름에 미치는 영향이 명시되어 있는가?",
		},
	},
	{
		ruleID:   "disclosure.capital_action_title",
		severity: RiskSeverityWatch,
		title:    "자본조달·희석 관련 공시 제목 확인",
		keywords: []string{
			"유상증자",
			"무상감자",
			"감자결정",
			"전환사채",
			"신주인수권부사채",
			"교환사채",
			"제3자배정",
			"자기주식처분",
		},
		interpretation: "자금조달 목적과 조건에 따라 주당 가치 희석 또는 재무구조 개선 가능성이 함께 존재한다.",
		questions: []string{
			"조달 자금의 사용 목적과 납입 일정은 무엇인가?",
			"잠재 주식 수와 기존 주주 기준 최대 희석률은 얼마인가?",
		},
	},
	{
		ruleID:   "disclosure.control_change_title",
		severity: RiskSeverityWatch,
		title:    "지배구조 변경 관련 공시 제목 확인",
		keywords: []string{
			"최대주주변경",
			"경영권변경",
			"주식양수도",
		},
		interpretation: "지배구조와 경영 전략이 바뀔 수 있으나 실제 영향은 거래 조건과 후속 공시 확인이 필요하다.",
		questions: []string{
			"새 최대주주의 자금조달 구조와 의무보유 조건은 무엇인가?",
			"사업 전략, 이사회 또는 기존 계약에 어떤 변화가 예상되는가?",
		},
	},
	{
		ruleID:   "disclosure.litigation_title",
		severity: RiskSeverityWatch,
		title:    "소송·법적 분쟁 관련 공시 제목 확인",
		keywords: []string{
			"소송",
			"중재",
			"손해배상",
			"가압류",
			"압류",
		},
		interpretation: "청구 금액, 승소 가능성 및 사업 지속 영향은 제목만으로 판단할 수 없다.",
		questions: []string{
			"청구 금액은 자본과 현금성 자산 대비 어느 정도인가?",
			"충당부채 설정과 회사의 법률적 대응이 공시되어 있는가?",
		},
	},
}

func evaluateDisclosureRisks(
	snapshot AnalysisInputSnapshot,
	evaluatedAt time.Time,
	config RiskRuleConfig,
) []RiskFinding {
	if snapshot.Disclosures.Status != models.DataStatusAvailable {
		return []RiskFinding{}
	}
	cutoff := evaluatedAt.AddDate(0, 0, -config.DisclosureLookbackDays)
	findings := []RiskFinding{}
	for _, rule := range disclosureRiskRules {
		for _, disclosure := range snapshot.Disclosures.Disclosures {
			if len(findings) >= config.MaxDisclosureFindings {
				return findings
			}
			if disclosure.ReceiptDate.Before(cutoff) {
				continue
			}
			keyword := matchingDisclosureKeyword(
				disclosure.ReportName,
				rule.keywords,
			)
			if keyword == "" {
				continue
			}
			observedAt := disclosure.ReceiptDate.UTC()
			findings = append(findings, newRiskFinding(
				rule.ruleID,
				RiskCategoryDisclosure,
				rule.severity,
				rule.title,
				fmt.Sprintf(
					"%s 공시 제목 %q에 규칙 키워드 %q이 포함되어 있다.",
					disclosure.ReceiptDate.Format("2006-01-02"),
					disclosure.ReportName,
					keyword,
				),
				rule.interpretation,
				rule.questions,
				[]RiskEvidence{
					{
						Kind:       "opendart_disclosure",
						Field:      "report_name",
						Value:      disclosure.ReportName,
						SourceURL:  disclosure.ViewerURL,
						ObservedAt: &observedAt,
						ReceiptNo:  disclosure.ReceiptNo,
					},
				},
				disclosure.ReceiptNo,
				keyword,
			))
		}
	}
	return findings
}

func matchingDisclosureKeyword(title string, keywords []string) string {
	normalizedTitle := normalizeDisclosureTitle(title)
	for _, keyword := range keywords {
		if strings.Contains(
			normalizedTitle,
			normalizeDisclosureTitle(keyword),
		) {
			return keyword
		}
	}
	return ""
}

func normalizeDisclosureTitle(value string) string {
	replacer := strings.NewReplacer(
		" ", "",
		"\t", "",
		"\r", "",
		"\n", "",
		"-", "",
		"_", "",
		"(", "",
		")", "",
		"[", "",
		"]", "",
	)
	return strings.ToLower(replacer.Replace(strings.TrimSpace(value)))
}

func decimalThresholdTriggered(
	value string,
	threshold string,
	triggerBelow bool,
) (bool, error) {
	parsedValue, ok := new(big.Rat).SetString(strings.TrimSpace(value))
	if !ok {
		return false, fmt.Errorf("invalid decimal value %q", value)
	}
	parsedThreshold, ok := new(big.Rat).SetString(
		strings.TrimSpace(threshold),
	)
	if !ok {
		return false, fmt.Errorf("invalid decimal threshold %q", threshold)
	}
	comparison := parsedValue.Cmp(parsedThreshold)
	if triggerBelow {
		return comparison < 0, nil
	}
	return comparison > 0, nil
}

func financialMetricByKey(
	metrics []FinancialMetric,
	key string,
) (FinancialMetric, bool) {
	for _, metric := range metrics {
		if metric.Key == key {
			return metric, true
		}
	}
	return FinancialMetric{}, false
}

func financialMetricEvidence(
	report FinancialMetricReport,
	metric FinancialMetric,
) RiskEvidence {
	return RiskEvidence{
		Kind:          "financial_metric",
		Field:         metric.Key,
		Value:         metric.CurrentAmount,
		Unit:          metric.Currency,
		SourceURL:     report.Source.SourceURL,
		ObservedAt:    report.Source.ObservedAt,
		ReceiptNo:     report.ReceiptNo,
		AccountID:     metric.AccountID,
		ContentSHA256: report.ContentSHA256,
	}
}

func newRiskFinding(
	ruleID string,
	category RiskCategory,
	severity RiskSeverity,
	title string,
	fact string,
	interpretation string,
	questions []string,
	evidence []RiskEvidence,
	identity ...string,
) RiskFinding {
	fingerprintSource := strings.Join(
		append([]string{RiskRuleSetVersion, ruleID}, identity...),
		"\x00",
	)
	hash := sha256.Sum256([]byte(fingerprintSource))
	return RiskFinding{
		Fingerprint:            hex.EncodeToString(hash[:]),
		RuleID:                 ruleID,
		Category:               category,
		Severity:               severity,
		Title:                  title,
		Fact:                   fact,
		PossibleInterpretation: interpretation,
		ValidationQuestions:    append([]string(nil), questions...),
		Evidence:               append([]RiskEvidence(nil), evidence...),
	}
}

func sortRiskFindings(findings []RiskFinding) {
	severityRank := map[RiskSeverity]int{
		RiskSeverityWarning: 0,
		RiskSeverityWatch:   1,
		RiskSeverityInfo:    2,
	}
	sort.SliceStable(findings, func(left int, right int) bool {
		leftRank := severityRank[findings[left].Severity]
		rightRank := severityRank[findings[right].Severity]
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		if findings[left].Category != findings[right].Category {
			return findings[left].Category < findings[right].Category
		}
		if findings[left].RuleID != findings[right].RuleID {
			return findings[left].RuleID < findings[right].RuleID
		}
		return findings[left].Fingerprint < findings[right].Fingerprint
	})
}

func timePointer(value time.Time) *time.Time {
	return &value
}
