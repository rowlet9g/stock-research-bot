package reporting

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/alerting"
	"github.com/rowlet9g/stock-research-bot/internal/models"
)

func TestBuildDailyAlertReportSortsAndRendersFacts(t *testing.T) {
	location := time.FixedZone("Asia/Seoul", 9*60*60)
	generatedAt := time.Date(2026, 7, 28, 23, 30, 0, 0, time.UTC)
	candidatePayload, err := json.Marshal(alerting.Candidate{
		RuleID:                 "portfolio.position_concentration",
		Kind:                   "portfolio_concentration",
		Ticker:                 "AAPL",
		Currency:               "USD",
		PossibleInterpretation: "한 종목의 가격 변화가 포트폴리오 결과에 미치는 영향이 크다.",
		ValidationQuestions: []string{
			"이 비중이 의도한 위험 한도 안에 있는가?",
			"투자 가설과 무효화 조건이 최신인가?",
		},
		Evidence: []alerting.CandidateEvidence{
			{
				Field:      "rebalance_reference",
				Value:      "200",
				Unit:       "USD",
				Comparison: "high_threshold",
				Threshold:  "40.00",
				Basis:      "gross_market_value_within_currency",
			},
		},
	})
	if err != nil {
		t.Fatalf("encode candidate payload: %v", err)
	}
	alerts := []models.Alert{
		{
			ID:              2,
			Severity:        models.AlertSeverityInfo,
			Status:          models.AlertStatusPending,
			Title:           "평균 취득단가 확인 필요",
			Fact:            "원가를 계산하지 못했다.",
			OccurrenceCount: 1,
			SourceRunID:     12,
			LastDetectedAt:  generatedAt.Add(-2 * time.Hour),
		},
		{
			ID:              1,
			Severity:        models.AlertSeverityWarning,
			Status:          models.AlertStatusPending,
			Title:           "단일 종목 집중도 확인 필요",
			Fact:            "AAPL의 USD 총 노출액 비중은 60.00%다.",
			OccurrenceCount: 2,
			SourceRunID:     11,
			Payload:         candidatePayload,
			LastDetectedAt:  generatedAt.Add(-time.Hour),
		},
	}
	report, err := BuildDailyAlertReport(alerts, generatedAt, location)
	if err != nil {
		t.Fatalf("build daily alert report: %v", err)
	}
	if report.Version != DailyAlertReportVersion ||
		report.Status != models.DataStatusAvailable ||
		report.ReportDate != "2026-07-29" ||
		report.TimeZone != "Asia/Seoul" ||
		report.Counts.Warning != 1 ||
		report.Counts.Info != 1 ||
		len(report.Alerts) != 2 ||
		len(report.Sections) != 2 ||
		report.Sections[0].Kind != "portfolio_concentration" ||
		report.Alerts[0].AlertID != 1 {
		t.Fatalf("unexpected daily report: %#v", report)
	}
	if alerts[0].ID != 2 {
		t.Fatal("input alerts were reordered")
	}
	body, err := report.TextBody(location)
	if err != nil {
		t.Fatalf("render daily report: %v", err)
	}
	for _, expected := range []string{
		"ForgetMeNot 일간 투자 점검",
		"경고 1건 / 관찰 0건 / 정보 1건 / 점검 주제 2개",
		"[WARNING] 집중도와 리밸런싱",
		"- AAPL의 USD 총 노출액 비중은 60.00%다.",
		"리밸런싱 참고 계산:",
		"AAPL: 40.00% 경계까지 200 USD",
		"확인 질문:",
		"- 이 비중이 의도한 위험 한도 안에 있는가?",
		"분석 실행 ID: 11, 12",
		"자동 매수·매도 지시가 아닙니다",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("daily report body missing %q:\n%s", expected, body)
		}
	}
	if strings.Contains(body, "발생 횟수:") {
		t.Fatalf("daily report repeated event metadata:\n%s", body)
	}
	if subject := report.Subject(""); subject !=
		"[ForgetMeNot] 일간 투자 점검 - 2026-07-29" {
		t.Fatalf("unexpected subject: %s", subject)
	}
}

func TestBuildDailyAlertReportHandlesEmptyAlerts(t *testing.T) {
	location := time.UTC
	report, err := BuildDailyAlertReport(
		nil,
		time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC),
		location,
	)
	if err != nil {
		t.Fatalf("build empty daily alert report: %v", err)
	}
	if report.Status != models.DataStatusEmpty ||
		len(report.Alerts) != 0 {
		t.Fatalf("unexpected empty report: %#v", report)
	}
	body, err := report.TextBody(location)
	if err != nil {
		t.Fatalf("render empty report: %v", err)
	}
	if !strings.Contains(body, "미발송 알림이 없습니다") {
		t.Fatalf("empty state missing:\n%s", body)
	}
}

func TestBuildDailyAlertReportGroupsRepeatedKinds(t *testing.T) {
	generatedAt := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	payloads := make([]json.RawMessage, 0, 2)
	for _, ticker := range []string{"AAPL", "NVDA"} {
		payload, err := json.Marshal(alerting.Candidate{
			RuleID:   "portfolio.position_weak_trend",
			Kind:     "portfolio_price_review",
			Ticker:   ticker,
			Currency: "USD",
			ValidationQuestions: []string{
				"시장 전체 움직임과 비교했는가?",
				"거래량 변화를 확인했는가?",
			},
		})
		if err != nil {
			t.Fatalf("encode %s payload: %v", ticker, err)
		}
		payloads = append(payloads, payload)
	}
	report, err := BuildDailyAlertReport(
		[]models.Alert{
			{
				ID:              1,
				Severity:        models.AlertSeverityWatch,
				Status:          models.AlertStatusPending,
				Title:           "중단기 가격 추세 약화 확인 필요",
				Fact:            "AAPL의 가격이 이동평균을 밑돈다.",
				Payload:         payloads[0],
				OccurrenceCount: 1,
				SourceRunID:     20,
				LastDetectedAt:  generatedAt,
			},
			{
				ID:              2,
				Severity:        models.AlertSeverityWatch,
				Status:          models.AlertStatusPending,
				Title:           "중단기 가격 추세 약화 확인 필요",
				Fact:            "NVDA의 가격이 이동평균을 밑돈다.",
				Payload:         payloads[1],
				OccurrenceCount: 1,
				SourceRunID:     20,
				LastDetectedAt:  generatedAt,
			},
		},
		generatedAt,
		time.UTC,
	)
	if err != nil {
		t.Fatalf("build grouped report: %v", err)
	}
	if len(report.Alerts) != 2 ||
		len(report.Sections) != 1 ||
		len(report.Sections[0].Alerts) != 2 ||
		len(report.Sections[0].ValidationQuestions) != 2 {
		t.Fatalf("unexpected grouped report: %#v", report)
	}
	body, err := report.TextBody(time.UTC)
	if err != nil {
		t.Fatalf("render grouped report: %v", err)
	}
	if strings.Count(body, "중단기 가격 추세 (알림 2건)") != 1 ||
		strings.Count(body, "시장 전체 움직임과 비교했는가?") != 1 {
		t.Fatalf("repeated section content was not deduplicated:\n%s", body)
	}
}

func TestBuildDailyAlertTestReportIsClearlySeparated(t *testing.T) {
	generatedAt := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	report, err := BuildDailyAlertTestReport(generatedAt, time.UTC)
	if err != nil {
		t.Fatalf("build test report: %v", err)
	}
	if !report.Test ||
		len(report.Alerts) != 1 ||
		report.Alerts[0].AlertID != 0 ||
		report.Alerts[0].SourceRunID != 0 ||
		report.Counts.Watch != 1 ||
		!strings.Contains(report.Alerts[0].Fact, "실제 포트폴리오 위험이 아니라") {
		t.Fatalf("unexpected test report: %#v", report)
	}
	if subject := report.Subject(""); subject !=
		"[ForgetMeNot] [TEST] 이메일 알림 점검 - 2026-07-28" {
		t.Fatalf("unexpected test subject: %s", subject)
	}
	body, err := report.TextBody(time.UTC)
	if err != nil {
		t.Fatalf("render test report: %v", err)
	}
	for _, expected := range []string{
		"이메일 알림 점검 [TEST]",
		"전송 경로 검증용",
		"[테스트] 이메일 알림 경로 확인",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("test report body missing %q:\n%s", expected, body)
		}
	}
	if strings.Contains(body, "분석 실행 ID: 0") {
		t.Fatalf("test report exposed synthetic run ID:\n%s", body)
	}
}

func TestBuildDailyAlertReportRejectsInvalidInputs(t *testing.T) {
	if _, err := BuildDailyAlertReport(nil, time.Time{}, time.UTC); err == nil {
		t.Fatal("zero generation time was accepted")
	}
	if _, err := BuildDailyAlertReport(
		nil,
		time.Now(),
		nil,
	); err == nil {
		t.Fatal("nil time zone was accepted")
	}
	if _, err := BuildDailyAlertReport(
		[]models.Alert{{
			ID:       1,
			Severity: "critical",
		}},
		time.Now(),
		time.UTC,
	); err == nil {
		t.Fatal("unsupported severity was accepted")
	}
	if _, err := BuildDailyAlertReport(
		[]models.Alert{{
			ID:             2,
			Severity:       models.AlertSeverityWatch,
			Payload:        []byte("{"),
			LastDetectedAt: time.Now(),
		}},
		time.Now(),
		time.UTC,
	); err == nil || !strings.Contains(err.Error(), "candidate payload") {
		t.Fatalf("invalid candidate payload was accepted: %v", err)
	}
}
