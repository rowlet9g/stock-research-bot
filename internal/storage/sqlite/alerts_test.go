package sqlite

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/models"
)

func TestAlertObservationDeduplicatesSameAnalysisRun(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	store, err := openWithClock(
		ctx,
		filepath.Join(t.TempDir(), "forgetmenot.db"),
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	run := saveAlertTestAnalysisRun(t, store, ctx, "a", now)
	input := AlertCandidateInput{
		Fingerprint: strings.Repeat("b", 64),
		Kind:        "portfolio.high_concentration",
		Severity:    models.AlertSeverityWarning,
		Title:       "단일 종목 집중도 높음",
		Fact:        "AAPL의 USD 총 노출액 비중이 60.00%다.",
		SourceRunID: run.ID,
		Payload:     json.RawMessage(`{"weight_bps":6000}`),
		DetectedAt:  now,
	}
	first, err := store.ObserveAlertCandidate(ctx, input)
	if err != nil {
		t.Fatalf("observe first alert: %v", err)
	}
	if !first.AlertCreated ||
		!first.ObservationCreated ||
		first.Alert.OccurrenceCount != 1 ||
		first.Alert.Status != models.AlertStatusPending {
		t.Fatalf("unexpected first alert result: %#v", first)
	}
	second, err := store.ObserveAlertCandidate(ctx, input)
	if err != nil {
		t.Fatalf("observe duplicate alert: %v", err)
	}
	if second.AlertCreated ||
		second.ObservationCreated ||
		second.Alert.ID != first.Alert.ID ||
		second.Alert.OccurrenceCount != 1 {
		t.Fatalf("same run was counted twice: %#v", second)
	}
}

func TestAlertObservationCountsNewAnalysisRun(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	store, err := openWithClock(
		ctx,
		filepath.Join(t.TempDir(), "forgetmenot.db"),
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	firstRun := saveAlertTestAnalysisRun(t, store, ctx, "a", now)
	secondRun := saveAlertTestAnalysisRun(
		t,
		store,
		ctx,
		"c",
		now.Add(time.Minute),
	)
	input := AlertCandidateInput{
		Fingerprint: strings.Repeat("b", 64),
		Kind:        "portfolio.high_concentration",
		Severity:    models.AlertSeverityWarning,
		Title:       "단일 종목 집중도 높음",
		Fact:        "AAPL의 USD 총 노출액 비중이 60.00%다.",
		SourceRunID: firstRun.ID,
		Payload:     json.RawMessage(`{"weight_bps":6000}`),
		DetectedAt:  now,
	}
	if _, err := store.ObserveAlertCandidate(ctx, input); err != nil {
		t.Fatalf("observe first run: %v", err)
	}
	input.SourceRunID = secondRun.ID
	input.Fact = "AAPL의 USD 총 노출액 비중이 65.00%다."
	input.Payload = json.RawMessage(`{"weight_bps":6500}`)
	input.DetectedAt = now.Add(time.Minute)
	result, err := store.ObserveAlertCandidate(ctx, input)
	if err != nil {
		t.Fatalf("observe second run: %v", err)
	}
	if result.AlertCreated ||
		!result.ObservationCreated ||
		result.Alert.OccurrenceCount != 2 ||
		result.Alert.SourceRunID != secondRun.ID ||
		!strings.Contains(result.Alert.Fact, "65.00%") {
		t.Fatalf("new run was not recorded: %#v", result)
	}
	alerts, err := store.ListAlerts(ctx, models.AlertStatusPending, 10)
	if err != nil {
		t.Fatalf("list alerts: %v", err)
	}
	if len(alerts) != 1 || alerts[0].OccurrenceCount != 2 {
		t.Fatalf("unexpected alert list: %#v", alerts)
	}
}

func TestAlertDeliveryMarksSentAndNewRunReopensPending(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	store, err := openWithClock(
		ctx,
		filepath.Join(t.TempDir(), "forgetmenot.db"),
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	firstRun := saveAlertTestAnalysisRun(t, store, ctx, "a", now)
	secondRun := saveAlertTestAnalysisRun(
		t,
		store,
		ctx,
		"c",
		now.Add(24*time.Hour),
	)
	input := AlertCandidateInput{
		Fingerprint: strings.Repeat("b", 64),
		Kind:        "portfolio.high_concentration",
		Severity:    models.AlertSeverityWarning,
		Title:       "단일 종목 집중도 높음",
		Fact:        "AAPL의 비중이 높다.",
		SourceRunID: firstRun.ID,
		Payload:     json.RawMessage(`{"weight_bps":6000}`),
		DetectedAt:  now,
	}
	first, err := store.ObserveAlertCandidate(ctx, input)
	if err != nil {
		t.Fatalf("observe first alert: %v", err)
	}
	sentAt := now.Add(time.Minute)
	if err := store.MarkAlertsSent(
		ctx,
		[]int64{first.Alert.ID},
		sentAt,
	); err != nil {
		t.Fatalf("mark alert sent: %v", err)
	}
	sent, err := store.Alert(ctx, first.Alert.ID)
	if err != nil {
		t.Fatalf("load sent alert: %v", err)
	}
	if sent.Status != models.AlertStatusSent ||
		sent.SentAt == nil ||
		!sent.SentAt.Equal(sentAt) {
		t.Fatalf("unexpected sent alert: %#v", sent)
	}

	duplicate, err := store.ObserveAlertCandidate(ctx, input)
	if err != nil {
		t.Fatalf("observe duplicate source run: %v", err)
	}
	if duplicate.Alert.Status != models.AlertStatusSent ||
		duplicate.ObservationCreated {
		t.Fatalf("same run reopened sent alert: %#v", duplicate)
	}

	input.SourceRunID = secondRun.ID
	input.DetectedAt = now.Add(24 * time.Hour)
	reopened, err := store.ObserveAlertCandidate(ctx, input)
	if err != nil {
		t.Fatalf("observe next run: %v", err)
	}
	if reopened.Alert.Status != models.AlertStatusPending ||
		reopened.Alert.SentAt != nil ||
		reopened.Alert.OccurrenceCount != 2 {
		t.Fatalf("new run did not reopen alert: %#v", reopened)
	}
}

func TestMarkAlertsSentIsAtomic(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	store, err := openWithClock(
		ctx,
		filepath.Join(t.TempDir(), "forgetmenot.db"),
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	run := saveAlertTestAnalysisRun(t, store, ctx, "a", now)
	input := AlertCandidateInput{
		Fingerprint: strings.Repeat("b", 64),
		Kind:        "test",
		Severity:    models.AlertSeverityInfo,
		Title:       "test",
		Fact:        "test fact",
		SourceRunID: run.ID,
		Payload:     json.RawMessage(`{}`),
		DetectedAt:  now,
	}
	first, err := store.ObserveAlertCandidate(ctx, input)
	if err != nil {
		t.Fatalf("observe first alert: %v", err)
	}
	input.Fingerprint = strings.Repeat("c", 64)
	second, err := store.ObserveAlertCandidate(ctx, input)
	if err != nil {
		t.Fatalf("observe second alert: %v", err)
	}
	if err := store.MarkAlertsSent(
		ctx,
		[]int64{first.Alert.ID, 999},
		now,
	); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unexpected atomic mark error: %v", err)
	}
	firstAfter, err := store.Alert(ctx, first.Alert.ID)
	if err != nil {
		t.Fatalf("load first alert after rollback: %v", err)
	}
	secondAfter, err := store.Alert(ctx, second.Alert.ID)
	if err != nil {
		t.Fatalf("load second alert after rollback: %v", err)
	}
	if firstAfter.Status != models.AlertStatusPending ||
		secondAfter.Status != models.AlertStatusPending {
		t.Fatalf(
			"mark sent was not atomic: first=%#v second=%#v",
			firstAfter,
			secondAfter,
		)
	}
	if err := store.MarkAlertsSent(
		ctx,
		[]int64{first.Alert.ID, first.Alert.ID},
		now,
	); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate alert IDs were accepted: %v", err)
	}
}

func TestAlertObservationRequiresStoredAnalysisRun(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "forgetmenot.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	_, err = store.ObserveAlertCandidate(ctx, AlertCandidateInput{
		Fingerprint: strings.Repeat("b", 64),
		Kind:        "test",
		Severity:    models.AlertSeverityInfo,
		Title:       "test",
		Fact:        "test fact",
		SourceRunID: 999,
		Payload:     json.RawMessage(`{}`),
		DetectedAt:  time.Now(),
	})
	if err == nil || !strings.Contains(err.Error(), "FOREIGN KEY") {
		t.Fatalf("unexpected missing run error: %v", err)
	}
}

func saveAlertTestAnalysisRun(
	t *testing.T,
	store *Store,
	ctx context.Context,
	hashCharacter string,
	generatedAt time.Time,
) models.AnalysisRun {
	t.Helper()
	run, _, err := store.SaveAnalysisRun(ctx, AnalysisRunInput{
		Kind:        "portfolio_brief",
		Status:      models.DataStatusAvailable,
		InputSHA256: strings.Repeat(hashCharacter, 64),
		RuleVersion: "portfolio-research-brief/v1",
		Payload:     json.RawMessage(`{"brief":"test"}`),
		GeneratedAt: generatedAt,
	})
	if err != nil {
		t.Fatalf("save analysis run: %v", err)
	}
	return run
}
