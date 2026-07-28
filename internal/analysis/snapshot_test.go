package analysis

import (
	"testing"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/models"
)

func TestBuildAnalysisInputSnapshotHasStableHashForSameInput(t *testing.T) {
	input := completeAnalysisInput()
	first, err := BuildAnalysisInputSnapshot(
		time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC),
		input,
	)
	if err != nil {
		t.Fatalf("build first analysis snapshot: %v", err)
	}
	second, err := BuildAnalysisInputSnapshot(
		time.Date(2026, 7, 28, 12, 1, 0, 0, time.UTC),
		input,
	)
	if err != nil {
		t.Fatalf("build second analysis snapshot: %v", err)
	}
	if first.Status != models.DataStatusAvailable ||
		first.InputSHA256 == "" ||
		first.InputSHA256 != second.InputSHA256 {
		t.Fatalf("analysis hash is not stable: first=%#v second=%#v", first, second)
	}
	if first.GeneratedAt.Equal(second.GeneratedAt) {
		t.Fatal("generation times unexpectedly match")
	}
	if first.RuleVersions.PriceSignals != PriceSignalRuleVersion ||
		first.RuleVersions.FinancialMetrics != FinancialMetricRuleVersion {
		t.Fatalf("unexpected rule versions: %#v", first.RuleVersions)
	}
}

func TestBuildAnalysisInputSnapshotHashChangesWithPrice(t *testing.T) {
	input := completeAnalysisInput()
	first, err := BuildAnalysisInputSnapshot(time.Now(), input)
	if err != nil {
		t.Fatalf("build first analysis snapshot: %v", err)
	}
	changedPrice := 101.0
	input.Price.LastPrice = &changedPrice
	second, err := BuildAnalysisInputSnapshot(time.Now(), input)
	if err != nil {
		t.Fatalf("build changed analysis snapshot: %v", err)
	}
	if first.InputSHA256 == second.InputSHA256 {
		t.Fatal("analysis hash did not change with the price input")
	}
}

func TestBuildAnalysisInputSnapshotReturnsPartialForComponentIssue(t *testing.T) {
	input := completeAnalysisInput()
	input.Financials.Status = models.DataStatusPartial
	input.Issues = []AnalysisInputIssue{
		{
			Scope:   "financials",
			Kind:    "partial_data",
			Message: "one ratio is not comparable",
		},
	}

	snapshot, err := BuildAnalysisInputSnapshot(time.Now(), input)
	if err != nil {
		t.Fatalf("build partial analysis snapshot: %v", err)
	}
	if snapshot.Status != models.DataStatusPartial {
		t.Fatalf("unexpected partial snapshot status: %#v", snapshot)
	}
}

func TestBuildAnalysisInputSnapshotAllowsNotRequestedDARTComponents(
	t *testing.T,
) {
	input := completeAnalysisInput()
	input.Portfolio.Instrument.DARTCorpCode = ""
	input.Disclosures = AnalysisDisclosureInput{
		Status:      models.DataStatusNotRequested,
		Disclosures: []models.DARTDisclosure{},
	}
	input.Financials = FinancialMetricReport{
		Status:  models.DataStatusNotRequested,
		Metrics: []FinancialMetric{},
		Ratios:  []FinancialRatio{},
	}

	snapshot, err := BuildAnalysisInputSnapshot(time.Now(), input)
	if err != nil {
		t.Fatalf("build non-DART analysis snapshot: %v", err)
	}
	if snapshot.Status != models.DataStatusAvailable {
		t.Fatalf("not-requested DART data reduced status: %#v", snapshot)
	}
}

func completeAnalysisInput() AnalysisInput {
	observedAt := time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC)
	price := 100.0
	volume := int64(1000)
	statement := completeFinancialStatement("11011")
	return AnalysisInput{
		Portfolio: models.PortfolioRecord{
			Instrument: models.Instrument{
				ID:           1,
				Name:         "삼성전자",
				Ticker:       "005930",
				YahooTicker:  "005930.KS",
				DARTCorpCode: "00126380",
				Market:       "KOSPI",
				Currency:     "KRW",
			},
			Trades: []models.Trade{},
		},
		Price: models.PriceSnapshot{
			YahooTicker: "005930.KS",
			Currency:    "KRW",
			Status:      models.DataStatusAvailable,
			LastPrice:   &price,
			Volume:      &volume,
			Source: models.SourceMetadata{
				Provider:   "yahoo",
				SourceURL:  "https://query1.finance.yahoo.com",
				ObservedAt: &observedAt,
				FetchedAt:  observedAt.Add(time.Minute),
			},
		},
		Signals: []Signal{},
		Disclosures: AnalysisDisclosureInput{
			Status: models.DataStatusAvailable,
			Disclosures: []models.DARTDisclosure{
				{
					CorpCode:    "00126380",
					CorpName:    "삼성전자",
					ReportName:  "사업보고서",
					ReceiptNo:   "20260310002820",
					ReceiptDate: time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC),
					Submitter:   "삼성전자",
					ViewerURL:   "https://dart.fss.or.kr/dsaf001/main.do?rcpNo=20260310002820",
				},
			},
		},
		Financials: BuildFinancialMetricReport(statement),
		Issues:     []AnalysisInputIssue{},
	}
}
