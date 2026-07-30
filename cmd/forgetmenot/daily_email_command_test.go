package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/emaildelivery"
	"github.com/rowlet9g/stock-research-bot/internal/models"
	sqlitestore "github.com/rowlet9g/stock-research-bot/internal/storage/sqlite"
)

func TestDailyEmailReportPreviewDoesNotRequireSMTPConfig(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "forgetmenot.db")
	originalNow := dailyEmailNow
	dailyEmailNow = func() time.Time {
		return time.Date(2026, 7, 28, 15, 0, 0, 0, time.UTC)
	}
	t.Cleanup(func() { dailyEmailNow = originalNow })

	output := runCommand(
		t,
		"daily-email-report",
		"-db", databasePath,
		"-env", filepath.Join(t.TempDir(), "missing.env"),
		"-output", "json",
	)
	var result dailyEmailCommandResult
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("decode daily email preview: %v\n%s", err, output)
	}
	if result.Report.Status != models.DataStatusEmpty ||
		result.Report.ReportDate != "2026-07-29" ||
		result.Delivery.Requested ||
		result.Delivery.Sent ||
		!strings.Contains(result.Body, "미발송 알림이 없습니다") {
		t.Fatalf("unexpected daily email preview: %#v", result)
	}
}

func TestDailyEmailReportSendsAndMarksPendingAlerts(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "forgetmenot.db")
	alert := seedDailyEmailAlert(t, ctx, databasePath)
	configureDailyEmailEnvironment(t)

	originalNow := dailyEmailNow
	originalSender := newDailyEmailSender
	generatedAt := time.Date(2026, 7, 28, 15, 0, 0, 0, time.UTC)
	dailyEmailNow = func() time.Time { return generatedAt }
	fake := &recordingDailyEmailSender{}
	newDailyEmailSender = func(
		_ emaildelivery.SMTPConfig,
	) (dailyEmailSender, error) {
		return fake, nil
	}
	t.Cleanup(func() {
		dailyEmailNow = originalNow
		newDailyEmailSender = originalSender
	})

	output := runCommand(
		t,
		"daily-email-report",
		"-db", databasePath,
		"-env", filepath.Join(t.TempDir(), "missing.env"),
		"-send",
		"-output", "json",
	)
	var result dailyEmailCommandResult
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("decode sent daily email: %v\n%s", err, output)
	}
	if !result.Delivery.Requested ||
		!result.Delivery.Sent ||
		result.Delivery.RecipientCount != 1 ||
		len(result.Report.Alerts) != 1 ||
		fake.Calls != 1 ||
		!strings.Contains(result.Body, "집중도와 리밸런싱") ||
		!strings.Contains(
			fake.Content,
			"Content-Type: text/plain; charset=UTF-8",
		) {
		t.Fatalf(
			"unexpected sent daily email: result=%#v fake=%#v",
			result,
			fake,
		)
	}

	store, err := sqlitestore.Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer store.Close()
	stored, err := store.Alert(ctx, alert.ID)
	if err != nil {
		t.Fatalf("load sent alert: %v", err)
	}
	if stored.Status != models.AlertStatusSent ||
		stored.SentAt == nil ||
		!stored.SentAt.Equal(generatedAt) {
		t.Fatalf("alert was not marked sent: %#v", stored)
	}
}

func TestDailyEmailReportDeliveryFailureKeepsAlertsPending(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "forgetmenot.db")
	alert := seedDailyEmailAlert(t, ctx, databasePath)
	configureDailyEmailEnvironment(t)

	originalSender := newDailyEmailSender
	newDailyEmailSender = func(
		_ emaildelivery.SMTPConfig,
	) (dailyEmailSender, error) {
		return failingDailyEmailSender{}, nil
	}
	t.Cleanup(func() { newDailyEmailSender = originalSender })

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run(
		[]string{
			"daily-email-report",
			"-db", databasePath,
			"-env", filepath.Join(t.TempDir(), "missing.env"),
			"-send",
			"-output", "json",
		},
		&stdout,
		&stderr,
	)
	if exitCode != 1 ||
		!strings.Contains(stdout.String(), "SMTP unavailable") {
		t.Fatalf(
			"unexpected delivery failure: code=%d stdout=%s stderr=%s",
			exitCode,
			stdout.String(),
			stderr.String(),
		)
	}
	store, err := sqlitestore.Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer store.Close()
	stored, err := store.Alert(ctx, alert.ID)
	if err != nil {
		t.Fatalf("load failed-delivery alert: %v", err)
	}
	if stored.Status != models.AlertStatusPending ||
		stored.SentAt != nil {
		t.Fatalf("failed delivery changed alert state: %#v", stored)
	}
}

func TestDailyEmailTestAlertDoesNotReadOrChangeStoredAlerts(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "forgetmenot.db")
	alert := seedDailyEmailAlert(t, ctx, databasePath)
	configureDailyEmailEnvironment(t)

	originalSender := newDailyEmailSender
	fake := &recordingDailyEmailSender{}
	newDailyEmailSender = func(
		_ emaildelivery.SMTPConfig,
	) (dailyEmailSender, error) {
		return fake, nil
	}
	t.Cleanup(func() { newDailyEmailSender = originalSender })

	output := runCommand(
		t,
		"daily-email-report",
		"-db", databasePath,
		"-env", filepath.Join(t.TempDir(), "missing.env"),
		"-test-alert",
		"-send",
		"-output", "json",
	)
	var result dailyEmailCommandResult
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("decode sent test alert: %v\n%s", err, output)
	}
	if !result.Report.Test ||
		len(result.Report.Alerts) != 1 ||
		result.Report.Alerts[0].AlertID != 0 ||
		!result.Delivery.Sent ||
		!strings.Contains(result.Subject, "[TEST]") ||
		!strings.Contains(result.Body, "실제 투자 위험을 나타내지 않습니다") ||
		fake.Calls != 1 {
		t.Fatalf(
			"unexpected test alert result: result=%#v fake=%#v",
			result,
			fake,
		)
	}
	store, err := sqlitestore.Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer store.Close()
	stored, err := store.Alert(ctx, alert.ID)
	if err != nil {
		t.Fatalf("load real pending alert: %v", err)
	}
	if stored.Status != models.AlertStatusPending ||
		stored.SentAt != nil {
		t.Fatalf("test email changed real alert: %#v", stored)
	}
}

func TestDailyEmailReportSkipsEmptySendWithoutConfig(t *testing.T) {
	output := runCommand(
		t,
		"daily-email-report",
		"-db", filepath.Join(t.TempDir(), "forgetmenot.db"),
		"-env", filepath.Join(t.TempDir(), "missing.env"),
		"-send",
		"-output", "json",
	)
	var result dailyEmailCommandResult
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("decode skipped daily email: %v\n%s", err, output)
	}
	if !result.Delivery.Requested ||
		result.Delivery.Sent ||
		result.Delivery.SkippedReason != "no_pending_alerts" {
		t.Fatalf("unexpected skipped delivery: %#v", result)
	}
}

type recordingDailyEmailSender struct {
	Calls   int
	Content string
}

func (s *recordingDailyEmailSender) Send(
	_ context.Context,
	message emaildelivery.Message,
) error {
	s.Calls++
	content, err := message.Bytes()
	if err != nil {
		return err
	}
	s.Content = string(content)
	return nil
}

type failingDailyEmailSender struct{}

func (failingDailyEmailSender) Send(
	context.Context,
	emaildelivery.Message,
) error {
	return errors.New("SMTP unavailable")
}

func configureDailyEmailEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("SMTP_HOST", "smtp.example.test")
	t.Setenv("SMTP_PORT", "587")
	t.Setenv("SMTP_USERNAME", "sender@example.test")
	t.Setenv("SMTP_PASSWORD", "secret")
	t.Setenv("SMTP_TLS_MODE", "starttls")
	t.Setenv("EMAIL_FROM", "sender@example.test")
	t.Setenv("EMAIL_TO", "receiver@example.test")
	t.Setenv("EMAIL_SUBJECT_PREFIX", "[ForgetMeNot]")
	t.Setenv("EMAIL_REPORT_TIMEZONE", "Asia/Seoul")
}

func seedDailyEmailAlert(
	t *testing.T,
	ctx context.Context,
	databasePath string,
) models.Alert {
	t.Helper()
	store, err := sqlitestore.Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	run, _, err := store.SaveAnalysisRun(
		ctx,
		sqlitestore.AnalysisRunInput{
			Kind:        "portfolio_brief",
			Status:      models.DataStatusAvailable,
			InputSHA256: strings.Repeat("a", 64),
			RuleVersion: "portfolio-research-brief/v1",
			Payload:     json.RawMessage(`{"brief":"test"}`),
			GeneratedAt: now,
		},
	)
	if err != nil {
		t.Fatalf("save analysis run: %v", err)
	}
	result, err := store.ObserveAlertCandidate(
		ctx,
		sqlitestore.AlertCandidateInput{
			Fingerprint: strings.Repeat("b", 64),
			Kind:        "portfolio_concentration",
			Severity:    models.AlertSeverityWarning,
			Title:       "단일 종목 집중도 확인 필요",
			Fact:        "AAPL의 USD 총 노출액 비중은 60.00%다.",
			SourceRunID: run.ID,
			Payload:     json.RawMessage(`{"weight_bps":6000}`),
			DetectedAt:  now,
		},
	)
	if err != nil {
		t.Fatalf("save alert candidate: %v", err)
	}
	return result.Alert
}
