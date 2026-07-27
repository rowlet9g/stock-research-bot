package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rowlet9g/stock-research-bot/internal/models"
	sqlitestore "github.com/rowlet9g/stock-research-bot/internal/storage/sqlite"
)

func TestRunWritesStructuredJSONForInputError(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := run([]string{
		"-output", "json",
		"-watchlist", "does-not-exist.csv",
	}, &stdout, &stderr)

	if exitCode != 2 {
		t.Fatalf("expected exit code 2, got %d", exitCode)
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}

	var payload struct {
		Error outputIssue `json:"error"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode JSON output: %v\n%s", err, stdout.String())
	}
	if payload.Error.Scope != "watchlist" || payload.Error.Kind != "invalid_input" {
		t.Fatalf("unexpected error payload: %#v", payload.Error)
	}
}

func TestRunRejectsUnsupportedOutputFormat(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := run([]string{"-output", "yaml"}, &stdout, &stderr)

	if exitCode != 2 {
		t.Fatalf("expected exit code 2, got %d", exitCode)
	}
	if !strings.Contains(stderr.String(), `output must be "text" or "json"`) {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
}

func TestEnrichDARTCorporationCodeUsesStoredMapping(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "forgetmenot.db")
	store, err := sqlitestore.Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	item := models.WatchlistItem{
		Name:         "삼성전자",
		Ticker:       "005930",
		YahooTicker:  "005930.KS",
		DARTCorpCode: "00126380",
		Market:       "KOSPI",
		Currency:     "KRW",
	}
	if _, err := store.UpsertInstrument(ctx, item); err != nil {
		t.Fatalf("upsert instrument: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	item.DARTCorpCode = ""
	enriched, err := enrichDARTCorporationCode(ctx, item, databasePath)
	if err != nil {
		t.Fatalf("enrich corporation code: %v", err)
	}
	if enriched.DARTCorpCode != "00126380" {
		t.Fatalf("expected stored corporation code, got %#v", enriched)
	}
}
