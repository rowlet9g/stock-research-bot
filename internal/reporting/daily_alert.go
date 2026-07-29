package reporting

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/alerting"
	"github.com/rowlet9g/stock-research-bot/internal/models"
)

const DailyAlertReportVersion = "daily-alert-report/v2"

type DailyAlertCounts struct {
	Warning int `json:"warning"`
	Watch   int `json:"watch"`
	Info    int `json:"info"`
}

type DailyAlertItem struct {
	AlertID                int64                `json:"alert_id,omitempty"`
	Severity               models.AlertSeverity `json:"severity"`
	Status                 models.AlertStatus   `json:"status"`
	Title                  string               `json:"title"`
	Fact                   string               `json:"fact"`
	PossibleInterpretation string               `json:"possible_interpretation,omitempty"`
	ValidationQuestions    []string             `json:"validation_questions"`
	OccurrenceCount        int                  `json:"occurrence_count"`
	SourceRunID            int64                `json:"source_run_id,omitempty"`
	LastDetectedAt         time.Time            `json:"last_detected_at"`
}

type DailyAlertReport struct {
	Version     string            `json:"version"`
	Test        bool              `json:"test"`
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
		item := DailyAlertItem{
			AlertID:             alert.ID,
			Severity:            alert.Severity,
			Status:              alert.Status,
			Title:               strings.TrimSpace(alert.Title),
			Fact:                strings.TrimSpace(alert.Fact),
			ValidationQuestions: []string{},
			OccurrenceCount:     alert.OccurrenceCount,
			SourceRunID:         alert.SourceRunID,
			LastDetectedAt:      alert.LastDetectedAt.UTC(),
		}
		if len(alert.Payload) > 0 {
			var candidate alerting.Candidate
			if err := json.Unmarshal(alert.Payload, &candidate); err != nil {
				return DailyAlertReport{}, fmt.Errorf(
					"decode alert %d candidate payload: %w",
					alert.ID,
					err,
				)
			}
			item.PossibleInterpretation = strings.TrimSpace(
				candidate.PossibleInterpretation,
			)
			for _, question := range candidate.ValidationQuestions {
				question = strings.TrimSpace(question)
				if question != "" {
					item.ValidationQuestions = append(
						item.ValidationQuestions,
						question,
					)
				}
			}
		}
		report.Alerts = append(report.Alerts, item)
	}
	return report, nil
}

func BuildDailyAlertTestReport(
	generatedAt time.Time,
	location *time.Location,
) (DailyAlertReport, error) {
	report, err := BuildDailyAlertReport(
		[]models.Alert{
			{
				Severity: models.AlertSeverityWatch,
				Status:   models.AlertStatusPending,
				Title:    "[테스트] 이메일 알림 경로 확인",
				Fact: "이 항목은 실제 포트폴리오 위험이 아니라 " +
					"이메일 보고서 전송을 검증하기 위해 생성됐다.",
				OccurrenceCount: 1,
				LastDetectedAt:  generatedAt,
			},
		},
		generatedAt,
		location,
	)
	if err != nil {
		return DailyAlertReport{}, err
	}
	report.Test = true
	return report, nil
}

func (r DailyAlertReport) Subject(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "[ForgetMeNot]"
	}
	if r.Test {
		return fmt.Sprintf(
			"%s [TEST] 이메일 알림 점검 - %s",
			prefix,
			r.ReportDate,
		)
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
	title := "ForgetMeNot 일간 투자 점검"
	if r.Test {
		title = "ForgetMeNot 이메일 알림 점검 [TEST]"
	}
	fmt.Fprintf(
		&builder,
		"%s\n\n보고서 날짜: %s\n생성시각: %s\n기준 시간대: %s\n",
		title,
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
	if r.Test {
		builder.WriteString(
			"\n[TEST] 이 메일은 전송 경로 검증용이며 실제 투자 위험을 나타내지 않습니다.\n",
		)
	}
	if len(r.Alerts) == 0 {
		builder.WriteString("\n확인할 미발송 알림이 없습니다.\n")
	} else {
		builder.WriteString("\n확인 항목\n")
		for index, alert := range r.Alerts {
			fmt.Fprintf(
				&builder,
				"\n%d. [%s] %s\n사실: %s\n발생 횟수: %d\n최근 관측: %s\n",
				index+1,
				strings.ToUpper(string(alert.Severity)),
				alert.Title,
				alert.Fact,
				alert.OccurrenceCount,
				alert.LastDetectedAt.In(location).Format(time.RFC3339),
			)
			if alert.SourceRunID > 0 {
				fmt.Fprintf(
					&builder,
					"분석 실행 ID: %d\n",
					alert.SourceRunID,
				)
			}
			if alert.PossibleInterpretation != "" {
				fmt.Fprintf(
					&builder,
					"가능한 해석: %s\n",
					alert.PossibleInterpretation,
				)
			}
			if len(alert.ValidationQuestions) > 0 {
				builder.WriteString("확인 질문:\n")
				for _, question := range alert.ValidationQuestions {
					fmt.Fprintf(&builder, "- %s\n", question)
				}
			}
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
