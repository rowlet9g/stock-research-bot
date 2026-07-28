package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/config"
	"github.com/rowlet9g/stock-research-bot/internal/emaildelivery"
	"github.com/rowlet9g/stock-research-bot/internal/models"
	"github.com/rowlet9g/stock-research-bot/internal/reporting"
	sqlitestore "github.com/rowlet9g/stock-research-bot/internal/storage/sqlite"
)

var dailyEmailNow = time.Now

type dailyEmailSender interface {
	Send(context.Context, emaildelivery.Message) error
}

var newDailyEmailSender = func(
	smtpConfig emaildelivery.SMTPConfig,
) (dailyEmailSender, error) {
	return emaildelivery.NewSMTPSender(smtpConfig)
}

type dailyEmailDeliveryResult struct {
	Requested      bool       `json:"requested"`
	Sent           bool       `json:"sent"`
	SkippedReason  string     `json:"skipped_reason,omitempty"`
	RecipientCount int        `json:"recipient_count,omitempty"`
	SentAt         *time.Time `json:"sent_at,omitempty"`
}

type dailyEmailCommandResult struct {
	Report   reporting.DailyAlertReport `json:"report"`
	Subject  string                     `json:"subject"`
	Body     string                     `json:"body"`
	Delivery dailyEmailDeliveryResult   `json:"delivery"`
}

func runDailyEmailReport(
	args []string,
	stdout io.Writer,
	stderr io.Writer,
) int {
	flags := flag.NewFlagSet("daily-email-report", flag.ContinueOnError)
	flags.SetOutput(stderr)
	databasePath := flags.String("db", defaultDatabasePath, "SQLite database path")
	envPath := flags.String("env", ".env", "environment file path")
	limit := flags.Int("limit", 100, "maximum pending alerts from 1 to 500")
	send := flags.Bool("send", false, "send the report through configured SMTP")
	sendEmpty := flags.Bool("send-empty", false, "send even when no alerts are pending")
	testAlert := flags.Bool("test-alert", false, "use one synthetic alert without changing SQLite")
	outputFormat := flags.String("output", "text", "output format: text or json")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if exitCode := validateCommandOutput(outputFormat, stdout, stderr); exitCode != 0 {
		return exitCode
	}
	switch {
	case *limit <= 0 || *limit > 500:
		return commandInputError(
			*outputFormat,
			stdout,
			stderr,
			"limit must be between 1 and 500",
		)
	case *sendEmpty && !*send:
		return commandInputError(
			*outputFormat,
			stdout,
			stderr,
			"send-empty requires send",
		)
	case *sendEmpty && *testAlert:
		return commandInputError(
			*outputFormat,
			stdout,
			stderr,
			"send-empty cannot be combined with test-alert",
		)
	}

	settings := config.Load(*envPath)
	timeZone := strings.TrimSpace(settings.EmailReportTimeZone)
	if timeZone == "" {
		timeZone = "Asia/Seoul"
	}
	location, err := time.LoadLocation(timeZone)
	if err != nil {
		return commandInputError(
			*outputFormat,
			stdout,
			stderr,
			fmt.Sprintf(
				"EMAIL_REPORT_TIMEZONE %q is invalid: %v",
				timeZone,
				err,
			),
		)
	}
	generatedAt := dailyEmailNow()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	var store *sqlitestore.Store
	var alerts []models.Alert
	var report reporting.DailyAlertReport
	if *testAlert {
		report, err = reporting.BuildDailyAlertTestReport(
			generatedAt,
			location,
		)
	} else {
		store, err = sqlitestore.Open(ctx, *databasePath)
		if err != nil {
			return writeRuntimeFailure(
				*outputFormat,
				stdout,
				stderr,
				"storage",
				err,
			)
		}
		defer store.Close()
		alerts, err = store.ListAlerts(
			ctx,
			models.AlertStatusPending,
			*limit,
		)
		if err != nil {
			return writeRuntimeFailure(
				*outputFormat,
				stdout,
				stderr,
				"alerts",
				err,
			)
		}
		report, err = reporting.BuildDailyAlertReport(
			alerts,
			generatedAt,
			location,
		)
	}
	if err != nil {
		return writeRuntimeFailure(
			*outputFormat,
			stdout,
			stderr,
			"daily_report",
			err,
		)
	}
	subject := report.Subject(settings.EmailSubjectPrefix)
	body, err := report.TextBody(location)
	if err != nil {
		return writeRuntimeFailure(
			*outputFormat,
			stdout,
			stderr,
			"daily_report",
			err,
		)
	}
	result := dailyEmailCommandResult{
		Report:  report,
		Subject: subject,
		Body:    body,
		Delivery: dailyEmailDeliveryResult{
			Requested: *send,
		},
	}
	if *send && !*testAlert && len(alerts) == 0 && !*sendEmpty {
		result.Delivery.SkippedReason = "no_pending_alerts"
		return writeDailyEmailResult(
			*outputFormat,
			stdout,
			stderr,
			result,
		)
	}
	if *send {
		deliveryConfig, err := parseDailyEmailConfig(settings)
		if err != nil {
			return commandInputError(
				*outputFormat,
				stdout,
				stderr,
				err.Error(),
			)
		}
		message, err := emaildelivery.NewMessage(
			deliveryConfig.From,
			deliveryConfig.To,
			subject,
			body,
			generatedAt,
		)
		if err != nil {
			return writeRuntimeFailure(
				*outputFormat,
				stdout,
				stderr,
				"email_message",
				err,
			)
		}
		sender, err := newDailyEmailSender(deliveryConfig.SMTP)
		if err != nil {
			return commandInputError(
				*outputFormat,
				stdout,
				stderr,
				err.Error(),
			)
		}
		if err := sender.Send(ctx, message); err != nil {
			return writeRuntimeFailure(
				*outputFormat,
				stdout,
				stderr,
				"email_delivery",
				err,
			)
		}
		if !*testAlert && len(alerts) > 0 {
			if err := store.MarkAlertsSent(
				ctx,
				alertIDs(alerts),
				generatedAt,
			); err != nil {
				return writeRuntimeFailure(
					*outputFormat,
					stdout,
					stderr,
					"email_delivery_state",
					fmt.Errorf(
						"email was accepted but alert state was not updated; check alert status before retrying: %w",
						err,
					),
				)
			}
		}
		sentAt := generatedAt.UTC()
		result.Delivery.Sent = true
		result.Delivery.RecipientCount = len(deliveryConfig.To)
		result.Delivery.SentAt = &sentAt
	}
	return writeDailyEmailResult(
		*outputFormat,
		stdout,
		stderr,
		result,
	)
}

func parseDailyEmailConfig(
	settings config.Settings,
) (emaildelivery.DeliveryConfig, error) {
	return emaildelivery.ParseConfig(emaildelivery.RawConfig{
		SMTPHost:       settings.SMTPHost,
		SMTPPort:       settings.SMTPPort,
		SMTPUsername:   settings.SMTPUsername,
		SMTPPassword:   settings.SMTPPassword,
		SMTPTLSMode:    settings.SMTPTLSMode,
		EmailFrom:      settings.EmailFrom,
		EmailTo:        settings.EmailTo,
		SubjectPrefix:  settings.EmailSubjectPrefix,
		ReportTimeZone: settings.EmailReportTimeZone,
	})
}

func alertIDs(alerts []models.Alert) []int64 {
	ids := make([]int64, 0, len(alerts))
	for _, alert := range alerts {
		ids = append(ids, alert.ID)
	}
	return ids
}

func writeDailyEmailResult(
	outputFormat string,
	stdout io.Writer,
	stderr io.Writer,
	result dailyEmailCommandResult,
) int {
	if outputFormat == "json" {
		if err := writeJSON(stdout, result); err != nil {
			return writeRuntimeFailure(
				"text",
				stdout,
				stderr,
				"output",
				err,
			)
		}
		return 0
	}
	fmt.Fprint(stdout, result.Body)
	switch {
	case result.Delivery.Sent:
		fmt.Fprintf(
			stdout,
			"\n이메일 전송 완료: 수신자 %d명 / 알림 %d건\n",
			result.Delivery.RecipientCount,
			len(result.Report.Alerts),
		)
	case result.Delivery.SkippedReason == "no_pending_alerts":
		fmt.Fprintln(
			stdout,
			"\n전송 생략: 확인할 미발송 알림이 없습니다.",
		)
	default:
		fmt.Fprintln(
			stdout,
			"\n미리보기 전용: 이메일을 전송하지 않았습니다.",
		)
	}
	return 0
}
