package prompt

import (
	"fmt"
	"strings"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/analysis"
	"github.com/rowlet9g/stock-research-bot/internal/dart"
	"github.com/rowlet9g/stock-research-bot/internal/models"
)

const SystemPrompt = `You are an investment research assistant for education.
Do not provide guaranteed returns or definitive buy/sell orders.
Separate facts, interpretation, risks, and questions.
When data is insufficient, say so directly.
Write in Korean.`

type StockBriefInput struct {
	Name        string
	Snapshot    models.PriceSnapshot
	Signals     []analysis.Signal
	Disclosures dart.DisclosureResult
	UserThesis  string
}

func BuildStockBriefPrompt(input StockBriefInput) string {
	signalLines := make([]string, 0, len(input.Signals))
	for _, signal := range input.Signals {
		signalLines = append(signalLines, fmt.Sprintf("- [%s] %s: %s", signal.Level, signal.Title, signal.Detail))
	}
	if len(signalLines) == 0 {
		signalLines = append(signalLines, "- 특이 신호 없음")
	}

	disclosureLines := make([]string, 0, len(input.Disclosures.Disclosures))
	for i, item := range input.Disclosures.Disclosures {
		if i >= 10 {
			break
		}
		disclosureLines = append(
			disclosureLines,
			fmt.Sprintf(
				"- %s %s: %s (%s)",
				item.ReceiptDate.Format("2006-01-02"),
				item.CorpName,
				item.ReportName,
				item.ReceiptNo,
			),
		)
	}
	if len(disclosureLines) == 0 {
		disclosureLines = append(disclosureLines, "- 제공된 공시 없음")
	}

	userThesis := strings.TrimSpace(input.UserThesis)
	if userThesis == "" {
		userThesis = "아직 기록되지 않음"
	}

	return fmt.Sprintf(`다음 종목에 대한 투자 공부용 브리핑을 작성하라.

종목명: %s
Yahoo ticker: %s
통화: %s
가격 데이터 상태: %s
가격 기준시각: %s
가격 수집시각: %s
가격 출처: %s
현재가: %s
1일 변동률(%%): %s
20일 이동평균: %s
60일 이동평균: %s
거래량: %s

가격 신호:
%s

최근 DART 공시:
공시 데이터 상태: %s
공시 기준시각: %s
공시 수집시각: %s
공시 출처: %s
%s

사용자 투자 가설:
%s

출력 형식:
1. 확인된 사실
2. 가능한 해석
3. 주요 리스크
4. 사용자가 추가로 확인해야 할 질문
5. 관심종목 유지/주의/재검토 중 하나로 분류
`, input.Name,
		input.Snapshot.YahooTicker,
		valueOrNA(input.Snapshot.Currency),
		input.Snapshot.Status,
		formatObservedAt(input.Snapshot.Source.ObservedAt),
		formatFetchedAt(input.Snapshot.Source.FetchedAt),
		valueOrNA(input.Snapshot.Source.SourceURL),
		formatFloat(input.Snapshot.LastPrice),
		formatFloat(input.Snapshot.ChangePct1D),
		formatFloat(input.Snapshot.MA20),
		formatFloat(input.Snapshot.MA60),
		formatInt(input.Snapshot.Volume),
		strings.Join(signalLines, "\n"),
		input.Disclosures.Status,
		formatObservedAt(input.Disclosures.Source.ObservedAt),
		formatFetchedAt(input.Disclosures.Source.FetchedAt),
		valueOrNA(input.Disclosures.Source.SourceURL),
		strings.Join(disclosureLines, "\n"),
		userThesis,
	)
}

func formatObservedAt(value *time.Time) string {
	if value == nil {
		return "N/A"
	}
	return value.UTC().Format(time.RFC3339)
}

func formatFetchedAt(value time.Time) string {
	if value.IsZero() {
		return "N/A"
	}
	return value.UTC().Format(time.RFC3339)
}

func valueOrNA(value string) string {
	if strings.TrimSpace(value) == "" {
		return "N/A"
	}
	return value
}

func formatFloat(value *float64) string {
	if value == nil {
		return "N/A"
	}
	return fmt.Sprintf("%.2f", *value)
}

func formatInt(value *int64) string {
	if value == nil {
		return "N/A"
	}
	return fmt.Sprintf("%d", *value)
}
