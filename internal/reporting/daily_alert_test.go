package reporting

import (
	"strings"
	"testing"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/models"
)

func TestBuildDailyAlertReportSortsAndRendersFacts(t *testing.T) {
	location := time.FixedZone("Asia/Seoul", 9*60*60)
	generatedAt := time.Date(2026, 7, 28, 23, 30, 0, 0, time.UTC)
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
		"경고 1건 / 관찰 0건 / 정보 1건",
		"[WARNING] 단일 종목 집중도 확인 필요",
		"사실: AAPL의 USD 총 노출액 비중은 60.00%다.",
		"자동 매수·매도 지시가 아닙니다",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("daily report body missing %q:\n%s", expected, body)
		}
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
}
