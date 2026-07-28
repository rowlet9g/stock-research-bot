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

func TestAnalysisRunSaveIsIdempotentAndPreservesPayload(t *testing.T) {
	ctx := context.Background()
	createdAt := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	store, err := openWithClock(
		ctx,
		filepath.Join(t.TempDir(), "forgetmenot.db"),
		func() time.Time { return createdAt },
	)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	input := AnalysisRunInput{
		Kind:        "portfolio_brief",
		Status:      models.DataStatusPartial,
		InputSHA256: strings.Repeat("a", 64),
		RuleVersion: "portfolio-research-brief/v1",
		Payload: json.RawMessage(`{
			"version": "portfolio-research-brief/v1",
			"amount": "9223372036854775807"
		}`),
		GeneratedAt: createdAt.Add(-time.Minute),
	}
	first, duplicate, err := store.SaveAnalysisRun(ctx, input)
	if err != nil {
		t.Fatalf("save first analysis run: %v", err)
	}
	if duplicate ||
		first.ID == 0 ||
		first.OutputSHA256 == "" ||
		first.IdempotencyKey == "" ||
		first.CreatedAt != createdAt {
		t.Fatalf("unexpected first analysis run: %#v duplicate=%t", first, duplicate)
	}
	second, duplicate, err := store.SaveAnalysisRun(ctx, input)
	if err != nil {
		t.Fatalf("save duplicate analysis run: %v", err)
	}
	if !duplicate || second.ID != first.ID {
		t.Fatalf(
			"analysis run was not idempotent: first=%#v second=%#v duplicate=%t",
			first,
			second,
			duplicate,
		)
	}
	runs, err := store.ListAnalysisRuns(ctx, "portfolio_brief", 10)
	if err != nil {
		t.Fatalf("list analysis runs: %v", err)
	}
	if len(runs) != 1 ||
		!strings.Contains(
			string(runs[0].Payload),
			`"amount":"9223372036854775807"`,
		) {
		t.Fatalf("analysis payload precision was not preserved: %#v", runs)
	}
}

func TestAnalysisRunSaveDistinguishesRuleVersions(t *testing.T) {
	ctx := context.Background()
	store, err := Open(
		ctx,
		filepath.Join(t.TempDir(), "forgetmenot.db"),
	)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	input := AnalysisRunInput{
		Kind:        "risk_assessment",
		Status:      models.DataStatusAvailable,
		InputSHA256: strings.Repeat("b", 64),
		RuleVersion: "risk-rules/v1",
		Payload:     json.RawMessage(`{"findings":[]}`),
		GeneratedAt: time.Now(),
	}
	first, _, err := store.SaveAnalysisRun(ctx, input)
	if err != nil {
		t.Fatalf("save first rule version: %v", err)
	}
	input.RuleVersion = "risk-rules/v2"
	second, duplicate, err := store.SaveAnalysisRun(ctx, input)
	if err != nil {
		t.Fatalf("save second rule version: %v", err)
	}
	if duplicate || second.ID == first.ID {
		t.Fatalf("different rule version was deduplicated: %#v %#v", first, second)
	}
}

func TestAnalysisRunSaveRejectsInvalidInput(t *testing.T) {
	ctx := context.Background()
	store, err := Open(
		ctx,
		filepath.Join(t.TempDir(), "forgetmenot.db"),
	)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	tests := map[string]AnalysisRunInput{
		"invalid hash": {
			Kind:        "test",
			Status:      models.DataStatusAvailable,
			InputSHA256: "invalid",
			RuleVersion: "v1",
			Payload:     json.RawMessage(`{}`),
			GeneratedAt: time.Now(),
		},
		"invalid JSON": {
			Kind:        "test",
			Status:      models.DataStatusAvailable,
			InputSHA256: strings.Repeat("a", 64),
			RuleVersion: "v1",
			Payload:     json.RawMessage(`{`),
			GeneratedAt: time.Now(),
		},
		"invalid status": {
			Kind:        "test",
			Status:      models.DataStatus("unknown"),
			InputSHA256: strings.Repeat("a", 64),
			RuleVersion: "v1",
			Payload:     json.RawMessage(`{}`),
			GeneratedAt: time.Now(),
		},
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			if _, _, err := store.SaveAnalysisRun(ctx, input); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
