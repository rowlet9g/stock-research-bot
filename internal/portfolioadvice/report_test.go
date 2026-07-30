package portfolioadvice

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/rowlet9g/stock-research-bot/internal/decimal"
)

func TestJSONSchemaIsValidJSON(t *testing.T) {
	if !json.Valid(JSONSchema()) {
		t.Fatal("portfolio advice JSON schema is invalid")
	}
}

func TestParseAndValidateRendersDirectAdviceWithSources(t *testing.T) {
	report, err := ParseAndValidate(
		[]byte(validReportJSON()),
		ValidationInput{
			ExpectedTickers: []string{"AAPL"},
			ProtectedQuantity: map[string]int64{
				"AAPL": 0,
			},
		},
	)
	if err != nil {
		t.Fatalf("parse valid advice: %v", err)
	}
	text := report.Text()
	for _, expected := range []string{
		"AAPL: 부분 매도",
		"평단 낮추기 판단:",
		"SPYM SPDR Portfolio S&P 500 ETF: 적립식 매수",
		"[1차] Apple 10-Q",
		"https://example.com/apple-10q",
		"이번 조사에서 확인한 핵심 근거",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("rendered advice missing %q:\n%s", expected, text)
		}
	}
}

func TestParseAndValidateRejectsMissingHolding(t *testing.T) {
	_, err := ParseAndValidate(
		[]byte(validReportJSON()),
		ValidationInput{
			ExpectedTickers: []string{"AAPL", "NVDA"},
		},
	)
	if err == nil || !strings.Contains(err.Error(), "missing active holding") {
		t.Fatalf("expected missing holding error, got %v", err)
	}
}

func TestParseAndValidateAcceptsDateOnlyAsOf(t *testing.T) {
	content := strings.Replace(
		validReportJSON(),
		`"as_of": "2026-07-30T01:00:00Z"`,
		`"as_of": "2026-07-30"`,
		1,
	)
	report, err := ParseAndValidate(
		[]byte(content),
		ValidationInput{ExpectedTickers: []string{"AAPL"}},
	)
	if err != nil {
		t.Fatalf("date-only as_of was rejected: %v", err)
	}
	if report.AsOf != "2026-07-30" {
		t.Fatalf("unexpected date-only as_of: %q", report.AsOf)
	}
}

func TestParseAndValidateProtectsPermanentQuantity(t *testing.T) {
	content := strings.Replace(
		validReportJSON(),
		`"action": "partial_sell"`,
		`"action": "full_exit"`,
		1,
	)
	_, err := ParseAndValidate(
		[]byte(content),
		ValidationInput{
			ExpectedTickers: []string{"AAPL"},
			ProtectedQuantity: map[string]int64{
				"AAPL": decimal.Scale,
			},
		},
	)
	if err == nil || !strings.Contains(err.Error(), "protected quantity") {
		t.Fatalf("expected protected quantity error, got %v", err)
	}
}

func TestParseAndValidateRejectsPartialSellWithoutUnprotectedQuantity(
	t *testing.T,
) {
	_, err := ParseAndValidate(
		[]byte(validReportJSON()),
		ValidationInput{
			ExpectedTickers: []string{"AAPL"},
			QuantityUnits: map[string]int64{
				"AAPL": decimal.Scale,
			},
			ProtectedQuantity: map[string]int64{
				"AAPL": decimal.Scale,
			},
		},
	)
	if err == nil || !strings.Contains(err.Error(), "partial_sell") {
		t.Fatalf("expected protected partial sell error, got %v", err)
	}
}

func TestParseAndValidateRequiresPrimaryEvidence(t *testing.T) {
	content := strings.ReplaceAll(
		validReportJSON(),
		`"source_kind": "primary"`,
		`"source_kind": "secondary"`,
	)
	_, err := ParseAndValidate(
		[]byte(content),
		ValidationInput{ExpectedTickers: []string{"AAPL"}},
	)
	if err == nil || !strings.Contains(err.Error(), "primary source") {
		t.Fatalf("expected primary source error, got %v", err)
	}
}

func validReportJSON() string {
	return `{
  "version": "portfolio-advice/v1",
  "as_of": "2026-07-30T01:00:00Z",
  "executive_summary": [
    "AAPL 비중을 줄이는 편이 낫다.",
    "코어 자산을 보완한다.",
    "방어 자산을 확보한다."
  ],
  "portfolio_actions": [
    "AAPL 일부를 매도한다.",
    "SPYM을 적립한다.",
    "단기 국채를 편입한다."
  ],
  "holdings": [
    {
      "ticker": "AAPL",
      "action": "partial_sell",
      "conviction": "medium",
      "quantity_change": "-1",
      "target_adjustment": "40%까지 부분 매도",
      "thesis_status": "mixed",
      "increase_condition_status": "not_met",
      "rationale": "집중도와 밸류에이션 부담을 함께 낮춘다.",
      "averaging_down_assessment": "실적 성장보다 집중 위험이 커서 평단 낮추기는 권하지 않는다.",
      "counterargument": "서비스 매출 성장이 예상보다 강할 수 있다.",
      "action_trigger": "현재 비중의 20%를 두 차례로 나누어 매도한다.",
      "evidence": [
        {
          "claim": "최근 분기 실적을 확인했다.",
          "source_kind": "primary",
          "source_name": "Apple 10-Q",
          "source_date": "2026-07-20",
          "url": "https://example.com/apple-10q"
        },
        {
          "claim": "시장 기대치를 확인했다.",
          "source_kind": "secondary",
          "source_name": "Example News",
          "source_date": "2026-07-21",
          "url": "https://example.com/apple-news"
        }
      ]
    }
  ],
  "candidates": [
    {
      "ticker": "SPYM",
      "name": "SPDR Portfolio S&P 500 ETF",
      "allocation_category": "core",
      "action": "accumulate",
      "proposed_role": "미국 주식 코어",
      "entry_plan": "신규 자금의 50%를 월 적립",
      "rationale": "광범위 지수 노출을 제공한다.",
      "risks": "주식시장 전체 하락에 노출된다.",
      "evidence": [
        {
          "claim": "지수 추종 방식과 보수를 확인했다.",
          "source_kind": "primary",
          "source_name": "SPDR Fund Page",
          "source_date": "2026-07-25",
          "url": "https://example.com/spym"
        },
        {
          "claim": "시장 구성을 확인했다.",
          "source_kind": "secondary",
          "source_name": "Example ETF Review",
          "source_date": "2026-07-26",
          "url": "https://example.com/spym-review"
        }
      ]
    },
    {
      "ticker": "SGOV",
      "name": "iShares 0-3 Month Treasury Bond ETF",
      "allocation_category": "defensive",
      "action": "accumulate",
      "proposed_role": "현금성 방어 자산",
      "entry_plan": "신규 자금의 30%를 분할 편입",
      "rationale": "단기 국채 노출을 제공한다.",
      "risks": "금리 하락 시 분배수익률이 낮아질 수 있다.",
      "evidence": [
        {
          "claim": "보유 채권과 만기를 확인했다.",
          "source_kind": "primary",
          "source_name": "iShares Fund Page",
          "source_date": "2026-07-25",
          "url": "https://example.com/sgov"
        },
        {
          "claim": "단기 금리 환경을 확인했다.",
          "source_kind": "secondary",
          "source_name": "Example Rates",
          "source_date": "2026-07-26",
          "url": "https://example.com/rates"
        }
      ]
    }
  ],
  "research_conclusions": [
    "AAPL의 최근 실적은 혼재됐다.",
    "코어 자산 비중이 부족하다.",
    "단기 국채가 방어 역할에 적합하다."
  ],
  "limitations": [
    "세금과 거래비용은 반영하지 않았다."
  ]
}`
}
