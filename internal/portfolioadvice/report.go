package portfolioadvice

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/decimal"
)

const Version = "portfolio-advice/v1"

type HoldingAction string

const (
	HoldingActionBuyMore     HoldingAction = "buy_more"
	HoldingActionHold        HoldingAction = "hold"
	HoldingActionPartialSell HoldingAction = "partial_sell"
	HoldingActionFullExit    HoldingAction = "full_exit"
)

type Evidence struct {
	Claim      string `json:"claim"`
	SourceKind string `json:"source_kind"`
	SourceName string `json:"source_name"`
	SourceDate string `json:"source_date"`
	URL        string `json:"url"`
}

type HoldingRecommendation struct {
	Ticker                  string        `json:"ticker"`
	Action                  HoldingAction `json:"action"`
	Conviction              string        `json:"conviction"`
	QuantityChange          string        `json:"quantity_change"`
	TargetAdjustment        string        `json:"target_adjustment"`
	ThesisStatus            string        `json:"thesis_status"`
	IncreaseConditionStatus string        `json:"increase_condition_status"`
	Rationale               string        `json:"rationale"`
	AveragingDownAssessment string        `json:"averaging_down_assessment"`
	Counterargument         string        `json:"counterargument"`
	ActionTrigger           string        `json:"action_trigger"`
	Evidence                []Evidence    `json:"evidence"`
}

type CandidateRecommendation struct {
	Ticker             string     `json:"ticker"`
	Name               string     `json:"name"`
	AllocationCategory string     `json:"allocation_category"`
	Action             string     `json:"action"`
	ProposedRole       string     `json:"proposed_role"`
	EntryPlan          string     `json:"entry_plan"`
	Rationale          string     `json:"rationale"`
	Risks              string     `json:"risks"`
	Evidence           []Evidence `json:"evidence"`
}

type Report struct {
	Version             string                    `json:"version"`
	AsOf                string                    `json:"as_of"`
	ExecutiveSummary    []string                  `json:"executive_summary"`
	PortfolioActions    []string                  `json:"portfolio_actions"`
	Holdings            []HoldingRecommendation   `json:"holdings"`
	Candidates          []CandidateRecommendation `json:"candidates"`
	ResearchConclusions []string                  `json:"research_conclusions"`
	Limitations         []string                  `json:"limitations"`
}

type ValidationInput struct {
	ExpectedTickers                  []string
	QuantityUnits                    map[string]int64
	ProtectedQuantity                map[string]int64
	UnrealizedReturnBPS              map[string]int64
	EnforceHardRealizedLossLimit     bool
	HardMaxRealizedLossBPS           int64
	ThesisInvalidationOverridesLimit bool
}

func JSONSchema() []byte {
	return []byte(`{
  "type": "object",
  "properties": {
    "version": {
      "type": "string",
      "enum": ["portfolio-advice/v1"]
    },
    "as_of": {
      "type": "string"
    },
    "executive_summary": {
      "type": "array",
      "minItems": 3,
      "maxItems": 5,
      "items": {"type": "string"}
    },
    "portfolio_actions": {
      "type": "array",
      "minItems": 3,
      "maxItems": 8,
      "items": {"type": "string"}
    },
    "holdings": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "ticker": {"type": "string"},
          "action": {
            "type": "string",
            "enum": ["buy_more", "hold", "partial_sell", "full_exit"]
          },
          "conviction": {
            "type": "string",
            "enum": ["high", "medium", "low"]
          },
          "quantity_change": {"type": "string"},
          "target_adjustment": {"type": "string"},
          "thesis_status": {
            "type": "string",
            "enum": ["supported", "mixed", "broken"]
          },
          "increase_condition_status": {
            "type": "string",
            "enum": ["met", "partially_met", "not_met"]
          },
          "rationale": {"type": "string"},
          "averaging_down_assessment": {"type": "string"},
          "counterargument": {"type": "string"},
          "action_trigger": {"type": "string"},
          "evidence": {
            "type": "array",
            "minItems": 2,
            "maxItems": 5,
            "items": {
              "type": "object",
              "properties": {
                "claim": {"type": "string"},
                "source_kind": {
                  "type": "string",
                  "enum": ["primary", "secondary"]
                },
                "source_name": {"type": "string"},
                "source_date": {"type": "string"},
                "url": {"type": "string"}
              },
              "required": [
                "claim",
                "source_kind",
                "source_name",
                "source_date",
                "url"
              ],
              "additionalProperties": false
            }
          }
        },
        "required": [
          "ticker",
          "action",
          "conviction",
          "quantity_change",
          "target_adjustment",
          "thesis_status",
          "increase_condition_status",
          "rationale",
          "averaging_down_assessment",
          "counterargument",
          "action_trigger",
          "evidence"
        ],
        "additionalProperties": false
      }
    },
    "candidates": {
      "type": "array",
      "minItems": 2,
      "maxItems": 4,
      "items": {
        "type": "object",
        "properties": {
          "ticker": {"type": "string"},
          "name": {"type": "string"},
          "allocation_category": {
            "type": "string",
            "enum": ["core", "growth", "defensive"]
          },
          "action": {
            "type": "string",
            "enum": ["accumulate", "buy_once", "watch"]
          },
          "proposed_role": {"type": "string"},
          "entry_plan": {"type": "string"},
          "rationale": {"type": "string"},
          "risks": {"type": "string"},
          "evidence": {
            "type": "array",
            "minItems": 2,
            "maxItems": 5,
            "items": {
              "type": "object",
              "properties": {
                "claim": {"type": "string"},
                "source_kind": {
                  "type": "string",
                  "enum": ["primary", "secondary"]
                },
                "source_name": {"type": "string"},
                "source_date": {"type": "string"},
                "url": {"type": "string"}
              },
              "required": [
                "claim",
                "source_kind",
                "source_name",
                "source_date",
                "url"
              ],
              "additionalProperties": false
            }
          }
        },
        "required": [
          "ticker",
          "name",
          "allocation_category",
          "action",
          "proposed_role",
          "entry_plan",
          "rationale",
          "risks",
          "evidence"
        ],
        "additionalProperties": false
      }
    },
    "research_conclusions": {
      "type": "array",
      "minItems": 3,
      "maxItems": 8,
      "items": {"type": "string"}
    },
    "limitations": {
      "type": "array",
      "maxItems": 3,
      "items": {"type": "string"}
    }
  },
  "required": [
    "version",
    "as_of",
    "executive_summary",
    "portfolio_actions",
    "holdings",
    "candidates",
    "research_conclusions",
    "limitations"
  ],
  "additionalProperties": false
}`)
}

func ParseAndValidate(
	content []byte,
	input ValidationInput,
) (Report, error) {
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var report Report
	if err := decoder.Decode(&report); err != nil {
		return Report{}, fmt.Errorf("decode portfolio advice: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return Report{}, fmt.Errorf("portfolio advice contains trailing JSON")
		}
		return Report{}, fmt.Errorf(
			"decode trailing portfolio advice content: %w",
			err,
		)
	}
	if err := validateReport(report, input); err != nil {
		return Report{}, err
	}
	return report, nil
}

func validateReport(report Report, input ValidationInput) error {
	if report.Version != Version {
		return fmt.Errorf(
			"portfolio advice version must be %q, got %q",
			Version,
			report.Version,
		)
	}
	if err := validateAsOf(report.AsOf); err != nil {
		return err
	}
	if err := validateStringCount(
		"executive_summary",
		report.ExecutiveSummary,
		3,
		5,
	); err != nil {
		return err
	}
	if err := validateStringCount(
		"portfolio_actions",
		report.PortfolioActions,
		3,
		8,
	); err != nil {
		return err
	}
	if err := validateStringCount(
		"research_conclusions",
		report.ResearchConclusions,
		3,
		8,
	); err != nil {
		return err
	}
	if err := validateStringCount(
		"limitations",
		report.Limitations,
		0,
		3,
	); err != nil {
		return err
	}

	expected := make(map[string]struct{}, len(input.ExpectedTickers))
	for _, ticker := range input.ExpectedTickers {
		ticker = normalizeTicker(ticker)
		if ticker != "" {
			expected[ticker] = struct{}{}
		}
	}
	seen := make(map[string]struct{}, len(report.Holdings))
	for index, holding := range report.Holdings {
		ticker := normalizeTicker(holding.Ticker)
		if ticker == "" {
			return fmt.Errorf("holding %d ticker is required", index+1)
		}
		if _, exists := expected[ticker]; !exists {
			return fmt.Errorf(
				"holding %d contains unexpected ticker %q",
				index+1,
				holding.Ticker,
			)
		}
		if _, exists := seen[ticker]; exists {
			return fmt.Errorf("holding %d duplicates ticker %q", index+1, ticker)
		}
		seen[ticker] = struct{}{}
		if !validHoldingAction(holding.Action) {
			return fmt.Errorf(
				"holding %s has invalid action %q",
				ticker,
				holding.Action,
			)
		}
		if input.ProtectedQuantity[ticker] > 0 &&
			holding.Action == HoldingActionFullExit {
			return fmt.Errorf(
				"holding %s cannot use full_exit while protected quantity is positive",
				ticker,
			)
		}
		if err := validateQuantityChange(
			holding,
			input.QuantityUnits,
			input.ProtectedQuantity,
		); err != nil {
			return fmt.Errorf("holding %s: %w", ticker, err)
		}
		if err := validateRequiredHoldingFields(holding); err != nil {
			return fmt.Errorf("holding %s: %w", ticker, err)
		}
		if err := validateRealizedLossLimit(
			ticker,
			holding,
			input,
		); err != nil {
			return fmt.Errorf("holding %s: %w", ticker, err)
		}
		if err := validateEvidence("holding "+ticker, holding.Evidence); err != nil {
			return err
		}
	}
	for ticker := range expected {
		if _, exists := seen[ticker]; !exists {
			return fmt.Errorf(
				"portfolio advice is missing active holding %q",
				ticker,
			)
		}
	}

	if len(report.Candidates) < 2 || len(report.Candidates) > 4 {
		return fmt.Errorf(
			"portfolio advice candidates must contain between 2 and 4 items",
		)
	}
	seenCandidates := make(map[string]struct{}, len(report.Candidates))
	for index, candidate := range report.Candidates {
		ticker := normalizeTicker(candidate.Ticker)
		if ticker == "" || strings.TrimSpace(candidate.Name) == "" {
			return fmt.Errorf("candidate %d ticker and name are required", index+1)
		}
		if _, exists := seenCandidates[ticker]; exists {
			return fmt.Errorf("candidate %d duplicates ticker %q", index+1, ticker)
		}
		seenCandidates[ticker] = struct{}{}
		if !oneOf(
			candidate.AllocationCategory,
			"core",
			"growth",
			"defensive",
		) {
			return fmt.Errorf(
				"candidate %s has invalid allocation category %q",
				ticker,
				candidate.AllocationCategory,
			)
		}
		if !oneOf(candidate.Action, "accumulate", "buy_once", "watch") {
			return fmt.Errorf(
				"candidate %s has invalid action %q",
				ticker,
				candidate.Action,
			)
		}
		for name, value := range map[string]string{
			"proposed_role": candidate.ProposedRole,
			"entry_plan":    candidate.EntryPlan,
			"rationale":     candidate.Rationale,
			"risks":         candidate.Risks,
		} {
			if strings.TrimSpace(value) == "" {
				return fmt.Errorf("candidate %s %s is required", ticker, name)
			}
		}
		if err := validateEvidence("candidate "+ticker, candidate.Evidence); err != nil {
			return err
		}
	}
	return nil
}

func validateRealizedLossLimit(
	ticker string,
	holding HoldingRecommendation,
	input ValidationInput,
) error {
	if !input.EnforceHardRealizedLossLimit ||
		(holding.Action != HoldingActionPartialSell &&
			holding.Action != HoldingActionFullExit) {
		return nil
	}
	if input.HardMaxRealizedLossBPS <= 0 {
		return fmt.Errorf("hard realized loss limit must be positive")
	}
	returnBPS, exists := input.UnrealizedReturnBPS[ticker]
	if !exists {
		return fmt.Errorf(
			"cannot validate %s because unrealized return is unavailable",
			holding.Action,
		)
	}
	if returnBPS >= -input.HardMaxRealizedLossBPS {
		return nil
	}
	if input.ThesisInvalidationOverridesLimit &&
		holding.ThesisStatus == "broken" {
		return nil
	}
	return fmt.Errorf(
		"%s would realize an estimated %.2f%% loss, beyond the %.2f%% hard limit without a broken thesis",
		holding.Action,
		float64(-returnBPS)/100,
		float64(input.HardMaxRealizedLossBPS)/100,
	)
}

func validateAsOf(value string) error {
	value = strings.TrimSpace(value)
	if _, err := time.Parse("2006-01-02", value); err == nil {
		return nil
	}
	if _, err := time.Parse(time.RFC3339, value); err == nil {
		return nil
	}
	return fmt.Errorf(
		"portfolio advice as_of must be YYYY-MM-DD or RFC3339, got %q",
		value,
	)
}

func validateRequiredHoldingFields(holding HoldingRecommendation) error {
	if !oneOf(holding.Conviction, "high", "medium", "low") {
		return fmt.Errorf("conviction %q is invalid", holding.Conviction)
	}
	if !oneOf(holding.ThesisStatus, "supported", "mixed", "broken") {
		return fmt.Errorf("thesis_status %q is invalid", holding.ThesisStatus)
	}
	if !oneOf(
		holding.IncreaseConditionStatus,
		"met",
		"partially_met",
		"not_met",
	) {
		return fmt.Errorf(
			"increase_condition_status %q is invalid",
			holding.IncreaseConditionStatus,
		)
	}
	for name, value := range map[string]string{
		"target_adjustment":         holding.TargetAdjustment,
		"rationale":                 holding.Rationale,
		"averaging_down_assessment": holding.AveragingDownAssessment,
		"counterargument":           holding.Counterargument,
		"action_trigger":            holding.ActionTrigger,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	return nil
}

func validateEvidence(scope string, evidence []Evidence) error {
	if len(evidence) < 2 || len(evidence) > 5 {
		return fmt.Errorf("%s evidence must contain between 2 and 5 items", scope)
	}
	hasPrimary := false
	seenURLs := make(map[string]struct{}, len(evidence))
	for index, item := range evidence {
		if strings.TrimSpace(item.Claim) == "" ||
			strings.TrimSpace(item.SourceName) == "" {
			return fmt.Errorf(
				"%s evidence %d claim and source name are required",
				scope,
				index+1,
			)
		}
		if item.SourceKind == "primary" {
			hasPrimary = true
		} else if item.SourceKind != "secondary" {
			return fmt.Errorf(
				"%s evidence %d has invalid source kind %q",
				scope,
				index+1,
				item.SourceKind,
			)
		}
		if _, err := time.Parse("2006-01-02", item.SourceDate); err != nil {
			return fmt.Errorf(
				"%s evidence %d source date must be YYYY-MM-DD",
				scope,
				index+1,
			)
		}
		parsedURL, err := url.ParseRequestURI(strings.TrimSpace(item.URL))
		if err != nil ||
			(parsedURL.Scheme != "https" && parsedURL.Scheme != "http") ||
			parsedURL.Host == "" ||
			parsedURL.User != nil {
			return fmt.Errorf(
				"%s evidence %d URL is invalid",
				scope,
				index+1,
			)
		}
		normalizedURL := parsedURL.String()
		if _, exists := seenURLs[normalizedURL]; exists {
			return fmt.Errorf(
				"%s evidence %d duplicates URL %q",
				scope,
				index+1,
				normalizedURL,
			)
		}
		seenURLs[normalizedURL] = struct{}{}
	}
	if !hasPrimary {
		return fmt.Errorf("%s evidence requires at least one primary source", scope)
	}
	return nil
}

func (report Report) Text() string {
	var builder strings.Builder
	fmt.Fprintln(&builder, "1. 핵심 결론")
	for _, item := range report.ExecutiveSummary {
		fmt.Fprintf(&builder, "- %s\n", strings.TrimSpace(item))
	}

	fmt.Fprintln(&builder, "\n2. 포트폴리오 조정안")
	for index, item := range report.PortfolioActions {
		fmt.Fprintf(&builder, "%d) %s\n", index+1, strings.TrimSpace(item))
	}

	fmt.Fprintln(&builder, "\n3. 보유 종목 거래 의견")
	for _, holding := range report.Holdings {
		fmt.Fprintf(
			&builder,
			"\n%s: %s / 확신도 %s\n",
			normalizeTicker(holding.Ticker),
			holdingActionLabel(holding.Action),
			convictionLabel(holding.Conviction),
		)
		fmt.Fprintf(
			&builder,
			"권고 수량 변화: %s주\n권고 조정: %s\n가설 판정: %s / 증액 조건: %s\n",
			strings.TrimSpace(holding.QuantityChange),
			strings.TrimSpace(holding.TargetAdjustment),
			thesisStatusLabel(holding.ThesisStatus),
			increaseStatusLabel(holding.IncreaseConditionStatus),
		)
		fmt.Fprintf(
			&builder,
			"근거: %s\n평단 낮추기 판단: %s\n반론: %s\n실행 조건: %s\n",
			strings.TrimSpace(holding.Rationale),
			strings.TrimSpace(holding.AveragingDownAssessment),
			strings.TrimSpace(holding.Counterargument),
			strings.TrimSpace(holding.ActionTrigger),
		)
		writeEvidence(&builder, holding.Evidence)
	}

	fmt.Fprintln(&builder, "\n4. 신규 편입 후보")
	for _, candidate := range report.Candidates {
		fmt.Fprintf(
			&builder,
			"\n%s %s: %s / %s\n",
			normalizeTicker(candidate.Ticker),
			strings.TrimSpace(candidate.Name),
			candidateActionLabel(candidate.Action),
			allocationCategoryLabel(candidate.AllocationCategory),
		)
		fmt.Fprintf(
			&builder,
			"역할: %s\n편입안: %s\n선정 근거: %s\n위험: %s\n",
			strings.TrimSpace(candidate.ProposedRole),
			strings.TrimSpace(candidate.EntryPlan),
			strings.TrimSpace(candidate.Rationale),
			strings.TrimSpace(candidate.Risks),
		)
		writeEvidence(&builder, candidate.Evidence)
	}

	fmt.Fprintln(&builder, "\n5. 이번 조사에서 확인한 핵심 근거")
	for index, item := range report.ResearchConclusions {
		fmt.Fprintf(&builder, "%d) %s\n", index+1, strings.TrimSpace(item))
	}
	if len(report.Limitations) > 0 {
		fmt.Fprintln(&builder, "\n조사 한계")
		for _, item := range report.Limitations {
			fmt.Fprintf(&builder, "- %s\n", strings.TrimSpace(item))
		}
	}
	return strings.TrimSpace(builder.String())
}

func writeEvidence(builder *strings.Builder, evidence []Evidence) {
	sorted := append([]Evidence(nil), evidence...)
	sort.SliceStable(sorted, func(left int, right int) bool {
		return sorted[left].SourceKind < sorted[right].SourceKind
	})
	fmt.Fprintln(builder, "출처:")
	for _, item := range sorted {
		fmt.Fprintf(
			builder,
			"- [%s] %s (%s): %s - %s\n",
			sourceKindLabel(item.SourceKind),
			strings.TrimSpace(item.SourceName),
			item.SourceDate,
			strings.TrimSpace(item.Claim),
			strings.TrimSpace(item.URL),
		)
	}
}

func validHoldingAction(value HoldingAction) bool {
	switch value {
	case HoldingActionBuyMore,
		HoldingActionHold,
		HoldingActionPartialSell,
		HoldingActionFullExit:
		return true
	default:
		return false
	}
}

func validateQuantityChange(
	holding HoldingRecommendation,
	quantities map[string]int64,
	protected map[string]int64,
) error {
	ticker := normalizeTicker(holding.Ticker)
	change, err := decimal.Parse(strings.TrimSpace(holding.QuantityChange))
	if err != nil {
		return fmt.Errorf("quantity_change is invalid: %w", err)
	}
	switch holding.Action {
	case HoldingActionBuyMore:
		if change <= 0 {
			return fmt.Errorf("buy_more requires a positive quantity_change")
		}
	case HoldingActionHold:
		if change != 0 {
			return fmt.Errorf("hold requires quantity_change 0")
		}
	case HoldingActionPartialSell:
		if change >= 0 {
			return fmt.Errorf("partial_sell requires a negative quantity_change")
		}
	case HoldingActionFullExit:
		if change >= 0 {
			return fmt.Errorf("full_exit requires a negative quantity_change")
		}
	}

	quantity, exists := quantities[ticker]
	if !exists || quantity <= 0 {
		return nil
	}
	remaining := quantity + change
	switch holding.Action {
	case HoldingActionPartialSell:
		if remaining <= 0 {
			return fmt.Errorf(
				"partial_sell must leave a positive position quantity",
			)
		}
		if remaining < protected[ticker] {
			return fmt.Errorf(
				"partial_sell would reduce the position below its protected quantity",
			)
		}
	case HoldingActionFullExit:
		if remaining != 0 {
			return fmt.Errorf(
				"full_exit quantity_change must close the full position",
			)
		}
	}
	return nil
}

func holdingActionLabel(value HoldingAction) string {
	switch value {
	case HoldingActionBuyMore:
		return "추가 매수"
	case HoldingActionHold:
		return "보유"
	case HoldingActionPartialSell:
		return "부분 매도"
	case HoldingActionFullExit:
		return "전량 매도"
	default:
		return string(value)
	}
}

func candidateActionLabel(value string) string {
	switch value {
	case "accumulate":
		return "적립식 매수"
	case "buy_once":
		return "일시 매수"
	case "watch":
		return "관찰"
	default:
		return value
	}
}

func convictionLabel(value string) string {
	switch value {
	case "high":
		return "높음"
	case "medium":
		return "보통"
	case "low":
		return "낮음"
	default:
		return value
	}
}

func thesisStatusLabel(value string) string {
	switch value {
	case "supported":
		return "유효"
	case "mixed":
		return "혼재"
	case "broken":
		return "훼손"
	default:
		return value
	}
}

func increaseStatusLabel(value string) string {
	switch value {
	case "met":
		return "충족"
	case "partially_met":
		return "일부 충족"
	case "not_met":
		return "미충족"
	default:
		return value
	}
}

func allocationCategoryLabel(value string) string {
	switch value {
	case "core":
		return "코어"
	case "growth":
		return "성장"
	case "defensive":
		return "방어"
	default:
		return value
	}
}

func sourceKindLabel(value string) string {
	if value == "primary" {
		return "1차"
	}
	return "2차"
}

func normalizeTicker(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

func validateStringCount(
	name string,
	values []string,
	minimum int,
	maximum int,
) error {
	if len(values) < minimum || len(values) > maximum {
		return fmt.Errorf(
			"portfolio advice %s must contain between %d and %d items",
			name,
			minimum,
			maximum,
		)
	}
	for index, value := range values {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf(
				"portfolio advice %s item %d is empty",
				name,
				index+1,
			)
		}
	}
	return nil
}

func oneOf(value string, allowed ...string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}
