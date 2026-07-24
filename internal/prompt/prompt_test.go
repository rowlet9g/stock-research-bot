package prompt

import (
	"strings"
	"testing"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/dart"
	"github.com/rowlet9g/stock-research-bot/internal/models"
)

func TestBuildStockBriefPromptIncludesStatusAndSourceTimes(t *testing.T) {
	observedAt := time.Date(2026, 7, 23, 20, 0, 0, 0, time.UTC)
	fetchedAt := time.Date(2026, 7, 24, 1, 0, 0, 0, time.UTC)
	lastPrice := 100.0
	volume := int64(1200)

	result := BuildStockBriefPrompt(StockBriefInput{
		Name: "Test",
		Snapshot: models.PriceSnapshot{
			YahooTicker: "TEST",
			Currency:    "USD",
			Status:      models.DataStatusAvailable,
			LastPrice:   &lastPrice,
			Volume:      &volume,
			Source: models.SourceMetadata{
				Provider:   "test_provider",
				SourceURL:  "https://example.com/prices",
				ObservedAt: &observedAt,
				FetchedAt:  fetchedAt,
			},
		},
		Disclosures: dart.DisclosureResult{
			Status:      models.DataStatusNotRequested,
			Disclosures: []dart.Disclosure{},
			Source: models.SourceMetadata{
				Provider:  "opendart",
				SourceURL: "https://opendart.fss.or.kr/api/list.json",
				FetchedAt: fetchedAt,
			},
		},
	})

	for _, expected := range []string{
		"가격 데이터 상태: available",
		"가격 기준시각: 2026-07-23T20:00:00Z",
		"가격 수집시각: 2026-07-24T01:00:00Z",
		"가격 출처: https://example.com/prices",
		"공시 데이터 상태: not_requested",
	} {
		if !strings.Contains(result, expected) {
			t.Fatalf("prompt does not contain %q:\n%s", expected, result)
		}
	}
}
