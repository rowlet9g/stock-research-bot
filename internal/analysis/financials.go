package analysis

import (
	"fmt"
	"math/big"

	"github.com/rowlet9g/stock-research-bot/internal/models"
)

type FinancialMetricStatus string

const (
	FinancialMetricAvailable     FinancialMetricStatus = "available"
	FinancialMetricMissing       FinancialMetricStatus = "missing"
	FinancialMetricAmbiguous     FinancialMetricStatus = "ambiguous"
	FinancialMetricNotComparable FinancialMetricStatus = "not_comparable"
)

const (
	FinancialMetricRevenue            = "revenue"
	FinancialMetricOperatingIncome    = "operating_income"
	FinancialMetricNetIncome          = "net_income"
	FinancialMetricTotalAssets        = "total_assets"
	FinancialMetricTotalLiabilities   = "total_liabilities"
	FinancialMetricTotalEquity        = "total_equity"
	FinancialMetricCurrentAssets      = "current_assets"
	FinancialMetricCurrentLiabilities = "current_liabilities"
	FinancialMetricOperatingCashFlow  = "operating_cash_flow"
)

const (
	FinancialPeriodAnnual      = "annual"
	FinancialPeriodPointInTime = "point_in_time"
	FinancialPeriodCumulative  = "cumulative"
	FinancialPeriodPeriod      = "period"
)

type FinancialMetric struct {
	Key                string                `json:"key"`
	Label              string                `json:"label"`
	Status             FinancialMetricStatus `json:"status"`
	StatementKind      string                `json:"statement_kind,omitempty"`
	PeriodBasis        string                `json:"period_basis,omitempty"`
	AccountID          string                `json:"account_id,omitempty"`
	AccountName        string                `json:"account_name,omitempty"`
	CurrentTermName    string                `json:"current_term_name,omitempty"`
	CurrentAmount      string                `json:"current_amount,omitempty"`
	PreviousTermName   string                `json:"previous_term_name,omitempty"`
	PreviousAmount     string                `json:"previous_amount,omitempty"`
	Currency           string                `json:"currency,omitempty"`
	Candidates         int                   `json:"candidates,omitempty"`
	ExpectedAccountIDs []string              `json:"expected_account_ids,omitempty"`
	Message            string                `json:"message,omitempty"`
}

type FinancialRatio struct {
	Key     string                `json:"key"`
	Label   string                `json:"label"`
	Status  FinancialMetricStatus `json:"status"`
	Value   string                `json:"value,omitempty"`
	Unit    string                `json:"unit"`
	Formula string                `json:"formula"`
	Message string                `json:"message,omitempty"`
}

type FinancialMetricReport struct {
	Status        models.DataStatus     `json:"status"`
	CorpCode      string                `json:"corp_code"`
	BusinessYear  int                   `json:"business_year"`
	ReportCode    string                `json:"report_code"`
	FSKind        string                `json:"fs_kind"`
	ReceiptNo     string                `json:"receipt_no"`
	ContentSHA256 string                `json:"content_sha256"`
	Metrics       []FinancialMetric     `json:"metrics"`
	Ratios        []FinancialRatio      `json:"ratios"`
	Source        models.SourceMetadata `json:"source"`
}

type financialMetricDefinition struct {
	key        string
	label      string
	sections   []string
	accountIDs []string
}

var financialMetricDefinitions = []financialMetricDefinition{
	{
		key:      FinancialMetricRevenue,
		label:    "매출액",
		sections: []string{"IS", "CIS"},
		accountIDs: []string{
			"ifrs-full_Revenue",
			"ifrs-full_RevenueFromContractsWithCustomers",
		},
	},
	{
		key:      FinancialMetricOperatingIncome,
		label:    "영업이익",
		sections: []string{"IS", "CIS"},
		accountIDs: []string{
			"dart_OperatingIncomeLoss",
			"ifrs-full_ProfitLossFromOperatingActivities",
		},
	},
	{
		key:        FinancialMetricNetIncome,
		label:      "당기순이익",
		sections:   []string{"IS", "CIS"},
		accountIDs: []string{"ifrs-full_ProfitLoss"},
	},
	{
		key:        FinancialMetricTotalAssets,
		label:      "자산총계",
		sections:   []string{"BS"},
		accountIDs: []string{"ifrs-full_Assets"},
	},
	{
		key:        FinancialMetricTotalLiabilities,
		label:      "부채총계",
		sections:   []string{"BS"},
		accountIDs: []string{"ifrs-full_Liabilities"},
	},
	{
		key:        FinancialMetricTotalEquity,
		label:      "자본총계",
		sections:   []string{"BS"},
		accountIDs: []string{"ifrs-full_Equity"},
	},
	{
		key:        FinancialMetricCurrentAssets,
		label:      "유동자산",
		sections:   []string{"BS"},
		accountIDs: []string{"ifrs-full_CurrentAssets"},
	},
	{
		key:        FinancialMetricCurrentLiabilities,
		label:      "유동부채",
		sections:   []string{"BS"},
		accountIDs: []string{"ifrs-full_CurrentLiabilities"},
	},
	{
		key:      FinancialMetricOperatingCashFlow,
		label:    "영업활동현금흐름",
		sections: []string{"CF"},
		accountIDs: []string{
			"ifrs-full_CashFlowsFromUsedInOperatingActivities",
		},
	},
}

func BuildFinancialMetricReport(
	statement models.DARTFinancialStatement,
) FinancialMetricReport {
	report := FinancialMetricReport{
		Status:        models.DataStatusEmpty,
		CorpCode:      statement.CorpCode,
		BusinessYear:  statement.BusinessYear,
		ReportCode:    statement.ReportCode,
		FSKind:        statement.FSKind,
		ReceiptNo:     statement.ReceiptNo,
		ContentSHA256: statement.ContentSHA256,
		Metrics:       make([]FinancialMetric, 0, len(financialMetricDefinitions)),
		Ratios:        []FinancialRatio{},
		Source:        statement.Source,
	}
	availableMetrics := 0
	metricIndex := make(map[string]FinancialMetric, len(financialMetricDefinitions))
	for _, definition := range financialMetricDefinitions {
		metric := resolveFinancialMetric(statement, definition)
		if metric.Status == FinancialMetricAvailable {
			availableMetrics++
		}
		report.Metrics = append(report.Metrics, metric)
		metricIndex[metric.Key] = metric
	}
	report.Ratios = buildFinancialRatios(metricIndex)

	complete := availableMetrics == len(financialMetricDefinitions)
	for _, ratio := range report.Ratios {
		if ratio.Status != FinancialMetricAvailable {
			complete = false
			break
		}
	}
	switch {
	case complete:
		report.Status = models.DataStatusAvailable
	case availableMetrics > 0:
		report.Status = models.DataStatusPartial
	}
	return report
}

func resolveFinancialMetric(
	statement models.DARTFinancialStatement,
	definition financialMetricDefinition,
) FinancialMetric {
	metric := FinancialMetric{
		Key:                definition.key,
		Label:              definition.label,
		Status:             FinancialMetricMissing,
		ExpectedAccountIDs: append([]string(nil), definition.accountIDs...),
		Message:            "matching standard account ID was not found",
	}
	for _, section := range definition.sections {
		for _, accountID := range definition.accountIDs {
			candidates := financialAccountCandidates(
				statement.Accounts,
				section,
				accountID,
			)
			if len(candidates) == 0 {
				continue
			}
			if len(candidates) > 1 {
				metric.Status = FinancialMetricAmbiguous
				metric.StatementKind = section
				metric.AccountID = accountID
				metric.Candidates = len(candidates)
				metric.Message = fmt.Sprintf(
					"%d accounts matched the same preferred section and account ID",
					len(candidates),
				)
				return metric
			}

			account := candidates[0]
			currentTerm, currentAmount, previousTerm, previousAmount, periodBasis :=
				financialAccountPeriods(statement.ReportCode, account)
			metric.StatementKind = account.StatementKind
			metric.PeriodBasis = periodBasis
			metric.AccountID = account.AccountID
			metric.AccountName = account.AccountName
			metric.CurrentTermName = currentTerm
			metric.CurrentAmount = currentAmount
			metric.PreviousTermName = previousTerm
			metric.PreviousAmount = previousAmount
			metric.Currency = account.Currency
			if currentAmount == "" {
				metric.Message = "current amount is missing"
				return metric
			}
			metric.Status = FinancialMetricAvailable
			if previousAmount == "" {
				metric.Message = "comparable previous amount is missing"
			} else {
				metric.Message = ""
			}
			return metric
		}
	}
	return metric
}

func financialAccountCandidates(
	accounts []models.DARTFinancialAccount,
	statementKind string,
	accountID string,
) []models.DARTFinancialAccount {
	candidates := []models.DARTFinancialAccount{}
	for _, account := range accounts {
		if account.StatementKind == statementKind &&
			account.AccountID == accountID {
			candidates = append(candidates, account)
		}
	}
	return candidates
}

func financialAccountPeriods(
	reportCode string,
	account models.DARTFinancialAccount,
) (string, string, string, string, string) {
	currentTerm := account.CurrentTermName
	previousTerm := account.PreviousTermName
	if account.StatementKind == "BS" {
		return currentTerm,
			account.CurrentAmount,
			previousTerm,
			account.PreviousAmount,
			FinancialPeriodPointInTime
	}
	if reportCode == "11011" {
		return currentTerm,
			account.CurrentAmount,
			previousTerm,
			account.PreviousAmount,
			FinancialPeriodAnnual
	}

	if account.StatementKind == "CF" {
		currentAmount := account.CurrentAmount
		if account.CurrentAddAmount != "" {
			currentAmount = account.CurrentAddAmount
		}
		previousAmount := account.PreviousInterimAmount
		if account.PreviousAddAmount != "" {
			previousAmount = account.PreviousAddAmount
		}
		if account.PreviousInterimTermName != "" {
			previousTerm = account.PreviousInterimTermName
		}
		return currentTerm,
			currentAmount,
			previousTerm,
			previousAmount,
			FinancialPeriodCumulative
	}

	if account.CurrentAddAmount != "" && account.PreviousAddAmount != "" {
		if account.PreviousInterimTermName != "" {
			previousTerm = account.PreviousInterimTermName
		}
		return currentTerm,
			account.CurrentAddAmount,
			previousTerm,
			account.PreviousAddAmount,
			FinancialPeriodCumulative
	}
	if account.CurrentAmount != "" && account.PreviousInterimAmount != "" {
		if account.PreviousInterimTermName != "" {
			previousTerm = account.PreviousInterimTermName
		}
		return currentTerm,
			account.CurrentAmount,
			previousTerm,
			account.PreviousInterimAmount,
			FinancialPeriodPeriod
	}
	if account.CurrentAddAmount != "" {
		return currentTerm,
			account.CurrentAddAmount,
			previousTerm,
			"",
			FinancialPeriodCumulative
	}
	return currentTerm,
		account.CurrentAmount,
		previousTerm,
		"",
		FinancialPeriodPeriod
}

func buildFinancialRatios(
	metrics map[string]FinancialMetric,
) []FinancialRatio {
	return []FinancialRatio{
		growthRatio(
			"revenue_growth_pct",
			"매출액 증감률",
			metrics[FinancialMetricRevenue],
		),
		growthRatio(
			"operating_income_growth_pct",
			"영업이익 증감률",
			metrics[FinancialMetricOperatingIncome],
		),
		growthRatio(
			"net_income_growth_pct",
			"당기순이익 증감률",
			metrics[FinancialMetricNetIncome],
		),
		growthRatio(
			"total_assets_growth_pct",
			"자산총계 증감률",
			metrics[FinancialMetricTotalAssets],
		),
		growthRatio(
			"operating_cash_flow_growth_pct",
			"영업활동현금흐름 증감률",
			metrics[FinancialMetricOperatingCashFlow],
		),
		metricRatio(
			"operating_margin_pct",
			"영업이익률",
			metrics[FinancialMetricOperatingIncome],
			metrics[FinancialMetricRevenue],
			"operating_income / revenue * 100",
		),
		metricRatio(
			"net_margin_pct",
			"순이익률",
			metrics[FinancialMetricNetIncome],
			metrics[FinancialMetricRevenue],
			"net_income / revenue * 100",
		),
		metricRatio(
			"debt_to_equity_pct",
			"부채비율",
			metrics[FinancialMetricTotalLiabilities],
			metrics[FinancialMetricTotalEquity],
			"total_liabilities / total_equity * 100",
		),
		metricRatio(
			"current_ratio_pct",
			"유동비율",
			metrics[FinancialMetricCurrentAssets],
			metrics[FinancialMetricCurrentLiabilities],
			"current_assets / current_liabilities * 100",
		),
	}
}

func growthRatio(
	key string,
	label string,
	metric FinancialMetric,
) FinancialRatio {
	ratio := FinancialRatio{
		Key:     key,
		Label:   label,
		Status:  FinancialMetricMissing,
		Unit:    "%",
		Formula: "(current - previous) / previous * 100",
	}
	if metric.Status != FinancialMetricAvailable {
		ratio.Message = "source metric is unavailable: " + metric.Key
		return ratio
	}
	if metric.PreviousAmount == "" {
		ratio.Message = "previous amount is missing"
		return ratio
	}
	current, ok := new(big.Int).SetString(metric.CurrentAmount, 10)
	if !ok {
		ratio.Message = "current amount is not an integer"
		return ratio
	}
	previous, ok := new(big.Int).SetString(metric.PreviousAmount, 10)
	if !ok {
		ratio.Message = "previous amount is not an integer"
		return ratio
	}
	if previous.Sign() <= 0 {
		ratio.Status = FinancialMetricNotComparable
		ratio.Message = "growth rate is not meaningful when the previous amount is zero or negative"
		return ratio
	}
	change := new(big.Int).Sub(current, previous)
	value, err := formatPercent(change, previous)
	if err != nil {
		ratio.Message = err.Error()
		return ratio
	}
	ratio.Status = FinancialMetricAvailable
	ratio.Value = value
	return ratio
}

func metricRatio(
	key string,
	label string,
	numeratorMetric FinancialMetric,
	denominatorMetric FinancialMetric,
	formula string,
) FinancialRatio {
	ratio := FinancialRatio{
		Key:     key,
		Label:   label,
		Status:  FinancialMetricMissing,
		Unit:    "%",
		Formula: formula,
	}
	if numeratorMetric.Status != FinancialMetricAvailable ||
		denominatorMetric.Status != FinancialMetricAvailable {
		ratio.Message = fmt.Sprintf(
			"required metrics are unavailable: %s, %s",
			numeratorMetric.Key,
			denominatorMetric.Key,
		)
		return ratio
	}
	if numeratorMetric.Currency != denominatorMetric.Currency {
		ratio.Status = FinancialMetricNotComparable
		ratio.Message = "metric currencies do not match"
		return ratio
	}
	if numeratorMetric.PeriodBasis != denominatorMetric.PeriodBasis {
		ratio.Status = FinancialMetricNotComparable
		ratio.Message = "metric period bases do not match"
		return ratio
	}
	numerator, ok := new(big.Int).SetString(numeratorMetric.CurrentAmount, 10)
	if !ok {
		ratio.Message = "numerator is not an integer"
		return ratio
	}
	denominator, ok := new(big.Int).SetString(
		denominatorMetric.CurrentAmount,
		10,
	)
	if !ok {
		ratio.Message = "denominator is not an integer"
		return ratio
	}
	if denominator.Sign() <= 0 {
		ratio.Status = FinancialMetricNotComparable
		ratio.Message = "ratio denominator must be positive"
		return ratio
	}
	value, err := formatPercent(numerator, denominator)
	if err != nil {
		ratio.Message = err.Error()
		return ratio
	}
	ratio.Status = FinancialMetricAvailable
	ratio.Value = value
	return ratio
}

func formatPercent(numerator *big.Int, denominator *big.Int) (string, error) {
	if denominator.Sign() == 0 {
		return "", fmt.Errorf("percentage denominator must not be zero")
	}
	scaledNumerator := new(big.Int).Mul(
		new(big.Int).Set(numerator),
		big.NewInt(10000),
	)
	quotient := new(big.Int)
	remainder := new(big.Int)
	quotient.QuoRem(scaledNumerator, denominator, remainder)

	doubleRemainder := new(big.Int).Mul(
		new(big.Int).Abs(remainder),
		big.NewInt(2),
	)
	absoluteDenominator := new(big.Int).Abs(new(big.Int).Set(denominator))
	if doubleRemainder.Cmp(absoluteDenominator) >= 0 {
		if scaledNumerator.Sign() == denominator.Sign() {
			quotient.Add(quotient, big.NewInt(1))
		} else {
			quotient.Sub(quotient, big.NewInt(1))
		}
	}

	sign := ""
	if quotient.Sign() < 0 {
		sign = "-"
	}
	magnitude := new(big.Int).Abs(quotient)
	whole := new(big.Int)
	fraction := new(big.Int)
	whole.QuoRem(magnitude, big.NewInt(100), fraction)
	fractionText := fraction.String()
	if len(fractionText) == 1 {
		fractionText = "0" + fractionText
	}
	return fmt.Sprintf(
		"%s%s.%s",
		sign,
		whole.String(),
		fractionText,
	), nil
}
