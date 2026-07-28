package prompt

import (
	"strings"
	"testing"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/analysis"
	"github.com/rowlet9g/stock-research-bot/internal/models"
)

func TestBuildResearchBriefPromptIncludesAuditableInputs(t *testing.T) {
	observedAt := time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC)
	price := 72_000.0
	snapshot := analysis.AnalysisInputSnapshot{
		SchemaVersion: analysis.AnalysisInputSchemaVersion,
		Status:        models.DataStatusAvailable,
		GeneratedAt:   observedAt.Add(time.Hour),
		InputSHA256:   strings.Repeat("a", 64),
		RuleVersions: analysis.AnalysisRuleVersions{
			PriceSignals:     analysis.PriceSignalRuleVersion,
			FinancialMetrics: analysis.FinancialMetricRuleVersion,
		},
		Portfolio: models.PortfolioRecord{
			Instrument: models.Instrument{
				Name:         "삼성전자",
				Ticker:       "005930",
				YahooTicker:  "005930.KS",
				DARTCorpCode: "00126380",
				Market:       "KOSPI",
				Currency:     "KRW",
			},
			Position: &models.Position{
				QuantityUnits:    200_000_000,
				AverageCostUnits: 70_000_000_000,
				Currency:         "KRW",
				AsOf:             observedAt,
			},
			Trades: []models.Trade{
				{TradeDate: observedAt.AddDate(0, 0, -1)},
			},
			Thesis: &models.Thesis{
				Summary:               "메모리 업황 회복",
				InvalidationCondition: "영업이익률 악화",
				CheckMetrics:          []string{"영업이익률"},
			},
		},
		Price: models.PriceSnapshot{
			YahooTicker: "005930.KS",
			Currency:    "KRW",
			Status:      models.DataStatusAvailable,
			LastPrice:   &price,
			Source: models.SourceMetadata{
				Provider:   "yahoo",
				SourceURL:  "https://query1.finance.yahoo.com",
				ObservedAt: &observedAt,
				FetchedAt:  observedAt.Add(time.Minute),
			},
		},
		Signals: []analysis.Signal{
			{
				Level:  "warning",
				Title:  "1일 하락 폭 확대",
				Detail: "전일 대비 -6.00% 변동했습니다.",
			},
		},
		Disclosures: analysis.AnalysisDisclosureInput{
			Status: models.DataStatusAvailable,
			Disclosures: []models.DARTDisclosure{
				{
					ReportName:  "사업보고서",
					ReceiptNo:   "20260310002820",
					ReceiptDate: time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC),
					Submitter:   "삼성전자",
					ViewerURL:   "https://dart.fss.or.kr",
				},
			},
		},
		Financials: analysis.FinancialMetricReport{
			Status:        models.DataStatusAvailable,
			BusinessYear:  2025,
			ReportCode:    "11011",
			FSKind:        "CFS",
			ReceiptNo:     "20260310002820",
			ContentSHA256: strings.Repeat("b", 64),
			Metrics: []analysis.FinancialMetric{
				{
					Key:           analysis.FinancialMetricRevenue,
					Label:         "매출액",
					Status:        analysis.FinancialMetricAvailable,
					CurrentAmount: "100",
					Currency:      "KRW",
					AccountID:     "ifrs-full_Revenue",
				},
			},
			Ratios: []analysis.FinancialRatio{
				{
					Key:     "operating_margin_pct",
					Label:   "영업이익률",
					Status:  analysis.FinancialMetricAvailable,
					Value:   "10.00",
					Unit:    "%",
					Formula: "operating_income / revenue * 100",
				},
			},
			Source: models.SourceMetadata{
				Provider:   "opendart",
				SourceURL:  "https://opendart.fss.or.kr/api/fnlttSinglAcntAll.json",
				ObservedAt: &observedAt,
				FetchedAt:  observedAt.Add(time.Minute),
			},
		},
		Issues: []analysis.AnalysisInputIssue{},
	}
	assessment := analysis.RiskAssessment{
		Status:             models.DataStatusAvailable,
		RuleSetVersion:     analysis.RiskRuleSetVersion,
		InputSchemaVersion: snapshot.SchemaVersion,
		InputSHA256:        snapshot.InputSHA256,
		EvaluatedAt:        snapshot.GeneratedAt,
		Config:             analysis.DefaultRiskRuleConfig(),
		Findings: []analysis.RiskFinding{
			{
				Fingerprint:            strings.Repeat("c", 64),
				RuleID:                 "price.signal",
				Category:               analysis.RiskCategoryPrice,
				Severity:               analysis.RiskSeverityWatch,
				Title:                  "가격 변동 확인",
				Fact:                   "전일 대비 -6.00% 변동했다.",
				PossibleInterpretation: "공시와 시장 요인을 확인해야 한다.",
				ValidationQuestions: []string{
					"같은 시점에 공시가 있었는가?",
				},
				Evidence: []analysis.RiskEvidence{
					{
						Kind:       "price_signal",
						Field:      "change_pct_1d",
						Value:      "-6.00",
						Unit:       "%",
						SourceURL:  "https://query1.finance.yahoo.com",
						ObservedAt: &observedAt,
					},
				},
			},
		},
		InputIssues: []analysis.AnalysisInputIssue{},
	}

	result, err := BuildResearchBriefPrompt(ResearchBriefInput{
		Snapshot:     snapshot,
		Assessment:   assessment,
		UserQuestion: "가설이 아직 유효한가?",
	})
	if err != nil {
		t.Fatalf("build research brief prompt: %v", err)
	}
	required := []string{
		snapshot.InputSHA256,
		"20260310002820",
		"ifrs-full_Revenue",
		"https://query1.finance.yahoo.com",
		"메모리 업황 회복",
		"가격 변동 확인",
		"가설이 아직 유효한가?",
		"확정적 매수·매도 지시를 하지 않는다",
	}
	for _, value := range required {
		if !strings.Contains(result, value) {
			t.Fatalf("research prompt missing %q:\n%s", value, result)
		}
	}
}

func TestBuildResearchBriefPromptRejectsMismatchedInput(t *testing.T) {
	_, err := BuildResearchBriefPrompt(ResearchBriefInput{
		Snapshot: analysis.AnalysisInputSnapshot{
			InputSHA256: strings.Repeat("a", 64),
		},
		Assessment: analysis.RiskAssessment{
			InputSHA256: strings.Repeat("b", 64),
		},
	})
	if err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("unexpected hash mismatch error: %v", err)
	}
}

func TestFormatPromptFloatUnitOmitsUnitForMissingValue(t *testing.T) {
	if got := formatPromptFloatUnit(nil, "%"); got != "N/A" {
		t.Fatalf("unexpected missing value format: %q", got)
	}
}
