package reporting

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/models"
)

const DailyAlertReportVersion = "daily-alert-report/v1"

type DailyAlertCounts struct {
	Warning int `json:"warning"`
	Watch   int `json:"watch"`
	Info    int `json:"info"`
}

type DailyAlertItem struct {
	AlertID         int64                `json:"alert_id"`
	Severity        models.AlertSeverity `json:"severity"`
	Status          models.AlertStatus   `json:"status"`
	Title           string               `json:"title"`
	Fact            string               `json:"fact"`
	OccurrenceCount int                  `json:"occurrence_count"`
	SourceRunID     int64                `json:"source_run_id"`
	LastDetectedAt  time.Time            `json:"last_detected_at"`
}

type DailyAlertReport struct {
	Version     string            `json:"version"`
	Status      models.DataStatus `json:"status"`
	ReportDate  string            `json:"report_date"`
	TimeZone    string            `json:"time_zone"`
	GeneratedAt time.Time         `json:"generated_at"`
	Counts      DailyAlertCounts  `json:"counts"`
	Alerts      []DailyAlertItem  `json:"alerts"`
}

func BuildDailyAlertReport(
	alerts []models.Alert,
	generatedAt time.Time,
	location *time.Location,
) (DailyAlertReport, error) {
	if generatedAt.IsZero() {
		return DailyAlertReport{}, fmt.Errorf(
			"daily alert report generation time is required",
		)
	}
	if location == nil {
		return DailyAlertReport{}, fmt.Errorf(
			"daily alert report time zone is required",
		)
	}
	sorted := append([]models.Alert(nil), alerts...)
	sort.SliceStable(sorted, func(left int, right int) bool {
		leftOrder := alertSeverityOrder(sorted[left].Severity)
		rightOrder := alertSeverityOrder(sorted[right].Severity)
		if leftOrder != rightOrder {
			return leftOrder < rightOrder
		}
		if !sorted[left].LastDetectedAt.Equal(
			sorted[right].LastDetectedAt,
		) {
			return sorted[left].LastDetectedAt.After(
				sorted[right].LastDetectedAt,
			)
		}
		return sorted[left].ID < sorted[right].ID
	})

	report := DailyAlertReport{
		Version:     DailyAlertReportVersion,
		Status:      models.DataStatusEmpty,
		ReportDate:  generatedAt.In(location).Format("2006-01-02"),
		TimeZone:    location.String(),
		GeneratedAt: generatedAt.UTC(),
		Alerts:      []DailyAlertItem{},
	}
	if len(sorted) > 0 {
		report.Status = models.DataStatusAvailable
	}
	for _, alert := range sorted {
		switch alert.Severity {
		case models.AlertSeverityWarning:
			report.Counts.Warning++
		case models.AlertSeverityWatch:
			report.Counts.Watch++
		case models.AlertSeverityInfo:
			report.Counts.Info++
		default:
			return DailyAlertReport{}, fmt.Errorf(
				"alert %d has unsupported severity %q",
				alert.ID,
				alert.Severity,
			)
		}
		report.Alerts = append(report.Alerts, DailyAlertItem{
			AlertID:         alert.ID,
			Severity:        alert.Severity,
			Status:          alert.Status,
			Title:           strings.TrimSpace(alert.Title),
			Fact:            strings.TrimSpace(alert.Fact),
			OccurrenceCount: alert.OccurrenceCount,
			SourceRunID:     alert.SourceRunID,
			LastDetectedAt:  alert.LastDetectedAt.UTC(),
		})
	}
	return report, nil
}

func (r DailyAlertReport) Subject(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "[ForgetMeNot]"
	}
	return fmt.Sprintf("%s 일간 투자 점검 - %s", prefix, r.ReportDate)
}

func (r DailyAlertReport) TextBody(
	location *time.Location,
) (string, error) {
	if r.Version != DailyAlertReportVersion {
		return "", fmt.Errorf(
			"daily alert report version must be %q, got %q",
			DailyAlertReportVersion,
			r.Version,
		)
	}
	if location == nil {
		return "", fmt.Errorf(
			"daily alert report time zone is required",
		)
	}
	var builder strings.Builder
	fmt.Fprintf(
		&builder,
		"ForgetMeNot 일간 투자 점검\n\n보고서 날짜: %s\n생성시각: %s\n기준 시간대: %s\n",
		r.ReportDate,
		r.GeneratedAt.In(location).Format(time.RFC3339),
		r.TimeZone,
	)
	fmt.Fprintf(
		&builder,
		"요약: 경고 %d건 / 관찰 %d건 / 정보 %d건\n",
		r.Counts.Warning,
		r.Counts.Watch,
		r.Counts.Info,
	)
	if len(r.Alerts) == 0 {
		builder.WriteString("\n확인할 미발송 알림이 없습니다.\n")
	} else {
		builder.WriteString("\n확인 항목\n")
		for index, alert := range r.Alerts {
			fmt.Fprintf(
				&builder,
				"\n%d. [%s] %s\n사실: %s\n발생 횟수: %d\n최근 관측: %s\n분석 실행 ID: %d\n",
				index+1,
				strings.ToUpper(string(alert.Severity)),
				alert.Title,
				alert.Fact,
				alert.OccurrenceCount,
				alert.LastDetectedAt.In(location).Format(time.RFC3339),
				alert.SourceRunID,
			)
		}
	}
	builder.WriteString(
		"\n이 보고서는 투자 검토를 위한 사실 요약이며 자동 매수·매도 지시가 아닙니다.\n",
	)
	return builder.String(), nil
}

func alertSeverityOrder(severity models.AlertSeverity) int {
	switch severity {
	case models.AlertSeverityWarning:
		return 0
	case models.AlertSeverityWatch:
		return 1
	case models.AlertSeverityInfo:
		return 2
	default:
		return 3
	}
}
