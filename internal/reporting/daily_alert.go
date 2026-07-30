package reporting

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/alerting"
	"github.com/rowlet9g/stock-research-bot/internal/models"
)

const DailyAlertReportVersion = "daily-alert-report/v3"

const maxDailyAlertSectionQuestions = 4

type DailyAlertCounts struct {
	Warning int `json:"warning"`
	Watch   int `json:"watch"`
	Info    int `json:"info"`
}

type DailyAlertItem struct {
	AlertID                int64                 `json:"alert_id,omitempty"`
	Severity               models.AlertSeverity  `json:"severity"`
	Status                 models.AlertStatus    `json:"status"`
	RuleID                 string                `json:"rule_id,omitempty"`
	Kind                   string                `json:"kind,omitempty"`
	Ticker                 string                `json:"ticker,omitempty"`
	Currency               string                `json:"currency,omitempty"`
	Title                  string                `json:"title"`
	Fact                   string                `json:"fact"`
	PossibleInterpretation string                `json:"possible_interpretation,omitempty"`
	ValidationQuestions    []string              `json:"validation_questions"`
	RebalanceReferences    []DailyAlertReference `json:"rebalance_references"`
	OccurrenceCount        int                   `json:"occurrence_count"`
	SourceRunID            int64                 `json:"source_run_id,omitempty"`
	LastDetectedAt         time.Time             `json:"last_detected_at"`
}

type DailyAlertReference struct {
	TargetLabel     string `json:"target_label"`
	TargetWeightPct string `json:"target_weight_pct"`
	Amount          string `json:"amount"`
	Currency        string `json:"currency"`
	Basis           string `json:"basis"`
}

type DailyAlertSection struct {
	Kind                string               `json:"kind"`
	Severity            models.AlertSeverity `json:"severity"`
	Title               string               `json:"title"`
	Summary             string               `json:"summary"`
	Alerts              []DailyAlertItem     `json:"alerts"`
	ValidationQuestions []string             `json:"validation_questions"`
}

type DailyAlertReport struct {
	Version     string              `json:"version"`
	Test        bool                `json:"test"`
	Status      models.DataStatus   `json:"status"`
	ReportDate  string              `json:"report_date"`
	TimeZone    string              `json:"time_zone"`
	GeneratedAt time.Time           `json:"generated_at"`
	Counts      DailyAlertCounts    `json:"counts"`
	Alerts      []DailyAlertItem    `json:"alerts"`
	Sections    []DailyAlertSection `json:"sections"`
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
		Sections:    []DailyAlertSection{},
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
			Kind:                strings.TrimSpace(alert.Kind),
			Title:               strings.TrimSpace(alert.Title),
			Fact:                strings.TrimSpace(alert.Fact),
			ValidationQuestions: []string{},
			RebalanceReferences: []DailyAlertReference{},
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
			if value := strings.TrimSpace(candidate.RuleID); value != "" {
				item.RuleID = value
			}
			if value := strings.TrimSpace(candidate.Kind); value != "" {
				item.Kind = value
			}
			if value := strings.TrimSpace(candidate.Ticker); value != "" {
				item.Ticker = value
			}
			if value := strings.TrimSpace(candidate.Currency); value != "" {
				item.Currency = strings.ToUpper(value)
			}
			for _, question := range candidate.ValidationQuestions {
				question = strings.TrimSpace(question)
				if question != "" {
					item.ValidationQuestions = append(
						item.ValidationQuestions,
						question,
					)
				}
			}
			for _, evidence := range candidate.Evidence {
				if evidence.Field != "rebalance_reference" {
					continue
				}
				item.RebalanceReferences = append(
					item.RebalanceReferences,
					DailyAlertReference{
						TargetLabel: strings.TrimSpace(
							evidence.Comparison,
						),
						TargetWeightPct: strings.TrimSpace(
							evidence.Threshold,
						),
						Amount: strings.TrimSpace(evidence.Value),
						Currency: strings.ToUpper(
							strings.TrimSpace(evidence.Unit),
						),
						Basis: strings.TrimSpace(evidence.Basis),
					},
				)
			}
		}
		report.Alerts = append(report.Alerts, item)
	}
	report.Sections = buildDailyAlertSections(report.Alerts)
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
		"요약: 경고 %d건 / 관찰 %d건 / 정보 %d건 / 점검 주제 %d개\n",
		r.Counts.Warning,
		r.Counts.Watch,
		r.Counts.Info,
		len(r.Sections),
	)
	writeDailyAlertSourceSummary(&builder, r.Alerts, location)
	if r.Test {
		builder.WriteString(
			"\n[TEST] 이 메일은 전송 경로 검증용이며 실제 투자 위험을 나타내지 않습니다.\n",
		)
	}
	if len(r.Sections) == 0 {
		builder.WriteString("\n확인할 미발송 알림이 없습니다.\n")
	} else {
		builder.WriteString("\n포트폴리오 점검 요약\n")
		for index, section := range r.Sections {
			fmt.Fprintf(
				&builder,
				"\n%d. [%s] %s (알림 %d건)\n진단: %s\n관측:\n",
				index+1,
				strings.ToUpper(string(section.Severity)),
				section.Title,
				len(section.Alerts),
				section.Summary,
			)
			for _, alert := range section.Alerts {
				if section.Kind == "portfolio_other" {
					fmt.Fprintf(
						&builder,
						"- %s: %s\n",
						alert.Title,
						alert.Fact,
					)
				} else {
					fmt.Fprintf(
						&builder,
						"- %s\n",
						alert.Fact,
					)
				}
			}
			references := dailyAlertSectionReferences(section)
			if len(references) > 0 {
				builder.WriteString("리밸런싱 참고 계산:\n")
				for _, reference := range references {
					fmt.Fprintf(&builder, "- %s\n", reference)
				}
			}
			if len(section.ValidationQuestions) > 0 {
				builder.WriteString("확인 질문:\n")
				for _, question := range section.ValidationQuestions {
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

func buildDailyAlertSections(
	alerts []DailyAlertItem,
) []DailyAlertSection {
	sectionsByKind := make(map[string]*DailyAlertSection)
	for _, alert := range alerts {
		kind, title, summary, _ := dailyAlertSectionMetadata(alert.Kind)
		section, exists := sectionsByKind[kind]
		if !exists {
			section = &DailyAlertSection{
				Kind:                kind,
				Severity:            alert.Severity,
				Title:               title,
				Summary:             summary,
				Alerts:              []DailyAlertItem{},
				ValidationQuestions: []string{},
			}
			if kind == "portfolio_other" &&
				alert.PossibleInterpretation != "" {
				section.Summary = alert.PossibleInterpretation
			}
			sectionsByKind[kind] = section
		}
		if alertSeverityOrder(alert.Severity) <
			alertSeverityOrder(section.Severity) {
			section.Severity = alert.Severity
		}
		section.Alerts = append(section.Alerts, alert)
		for _, question := range alert.ValidationQuestions {
			appendDailyAlertQuestion(section, question)
		}
	}

	sections := make([]DailyAlertSection, 0, len(sectionsByKind))
	for _, section := range sectionsByKind {
		sections = append(sections, *section)
	}
	sort.SliceStable(sections, func(left int, right int) bool {
		_, _, _, leftOrder := dailyAlertSectionMetadata(
			sections[left].Kind,
		)
		_, _, _, rightOrder := dailyAlertSectionMetadata(
			sections[right].Kind,
		)
		if leftOrder != rightOrder {
			return leftOrder < rightOrder
		}
		return sections[left].Title < sections[right].Title
	})
	return sections
}

func dailyAlertSectionMetadata(
	kind string,
) (string, string, string, int) {
	switch strings.TrimSpace(kind) {
	case "portfolio_concentration":
		return kind,
			"집중도와 리밸런싱",
			"같은 통화 안에서 일부 종목의 비중이 설정된 관찰 또는 경고 경계를 넘었다. 아래 계산은 총노출을 유지하는 기계적 참고값이며 매도 지시가 아니다.",
			0
	case "portfolio_cost_review":
		return kind,
			"평균단가와 손실 구간",
			"평균단가 대비 손실이 검토 경계를 넘었다. 손실률만으로 매수가의 적정성이나 매도 필요성을 확정할 수 없으며 투자 가설과 기업가치 변화를 함께 확인해야 한다.",
			1
	case "portfolio_price_review":
		return kind,
			"중단기 가격 추세",
			"일부 종목에서 중단기 가격 약세가 관측됐다. 이동평균 하회는 가격 상태이지 기업가치 하락의 증명이나 독립적인 매도 신호가 아니다.",
			2
	case "portfolio_thesis_quality":
		return kind,
			"투자 가설 완전성",
			"보유 종목의 매수 이유와 무효화 조건이 저장되지 않아 가격 변화가 기회인지 가설 훼손인지 재현 가능하게 판단하기 어렵다.",
			3
	case "portfolio_data_quality":
		return kind,
			"포트폴리오 데이터 완전성",
			"가격, 취득원가 또는 현재 포지션 데이터 일부가 불완전해 노출과 손익 판단에 제한이 있다.",
			4
	default:
		return "portfolio_other",
			"기타 확인 항목",
			"분류되지 않은 알림을 함께 검토해야 한다.",
			5
	}
}

func appendDailyAlertQuestion(
	section *DailyAlertSection,
	question string,
) {
	question = strings.TrimSpace(question)
	if question == "" ||
		len(section.ValidationQuestions) >=
			maxDailyAlertSectionQuestions {
		return
	}
	for _, existing := range section.ValidationQuestions {
		if existing == question {
			return
		}
	}
	section.ValidationQuestions = append(
		section.ValidationQuestions,
		question,
	)
}

func dailyAlertSectionReferences(
	section DailyAlertSection,
) []string {
	references := []string{}
	for _, alert := range section.Alerts {
		if len(alert.RebalanceReferences) == 0 {
			continue
		}
		label := alert.Ticker
		if label == "" {
			label = alert.Title
		}
		parts := make([]string, 0, len(alert.RebalanceReferences))
		for _, reference := range alert.RebalanceReferences {
			parts = append(parts, fmt.Sprintf(
				"%s%% 경계까지 %s %s",
				reference.TargetWeightPct,
				reference.Amount,
				reference.Currency,
			))
		}
		references = append(
			references,
			label+": "+strings.Join(parts, "; "),
		)
	}
	return references
}

func writeDailyAlertSourceSummary(
	builder *strings.Builder,
	alerts []DailyAlertItem,
	location *time.Location,
) {
	runIDs := []int64{}
	seenRuns := map[int64]struct{}{}
	var latest time.Time
	for _, alert := range alerts {
		if alert.SourceRunID > 0 {
			if _, exists := seenRuns[alert.SourceRunID]; !exists {
				seenRuns[alert.SourceRunID] = struct{}{}
				runIDs = append(runIDs, alert.SourceRunID)
			}
		}
		if alert.LastDetectedAt.After(latest) {
			latest = alert.LastDetectedAt
		}
	}
	if len(runIDs) > 0 {
		sort.Slice(runIDs, func(left int, right int) bool {
			return runIDs[left] < runIDs[right]
		})
		values := make([]string, 0, len(runIDs))
		for _, runID := range runIDs {
			values = append(values, strconv.FormatInt(runID, 10))
		}
		fmt.Fprintf(
			builder,
			"분석 실행 ID: %s\n",
			strings.Join(values, ", "),
		)
	}
	if !latest.IsZero() {
		fmt.Fprintf(
			builder,
			"최근 알림 관측: %s\n",
			latest.In(location).Format(time.RFC3339),
		)
	}
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
