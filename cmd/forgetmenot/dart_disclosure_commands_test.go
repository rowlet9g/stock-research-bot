package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/dart"
	"github.com/rowlet9g/stock-research-bot/internal/models"
	"github.com/rowlet9g/stock-research-bot/internal/provider"
	sqlitestore "github.com/rowlet9g/stock-research-bot/internal/storage/sqlite"
)

func TestDARTDisclosureSyncPreservesSuccessfulCorporationOnPartialFailure(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "forgetmenot.db")
	store, err := sqlitestore.Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if _, err := store.SyncInstruments(ctx, []models.WatchlistItem{
		{
			Name:         "삼성전자",
			Ticker:       "005930",
			YahooTicker:  "005930.KS",
			DARTCorpCode: "00126380",
			Market:       "KOSPI",
			Currency:     "KRW",
		},
		{
			Name:         "현대자동차",
			Ticker:       "005380",
			YahooTicker:  "005380.KS",
			DARTCorpCode: "00164742",
			Market:       "KOSPI",
			Currency:     "KRW",
		},
	}); err != nil {
		t.Fatalf("seed instruments: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close seed store: %v", err)
	}

	fetchedAt := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	client := stubDARTDisclosureClient{
		results: map[string]dart.DisclosureResult{
			"00126380": {
				Status:       models.DataStatusAvailable,
				Disclosures:  []dart.Disclosure{commandDARTDisclosure(fetchedAt)},
				TotalCount:   1,
				PagesFetched: 1,
				BeginDate:    "2026-07-01",
				EndDate:      "2026-07-27",
				Source: models.SourceMetadata{
					Provider:  "opendart",
					SourceURL: "https://opendart.fss.or.kr/api/list.json?corp_code=00126380",
					FetchedAt: fetchedAt,
				},
			},
		},
		errors: map[string]error{
			"00164742": &provider.Error{
				Provider:  "opendart",
				Operation: "fetch_disclosures",
				Kind:      provider.ErrorKindUnavailable,
				Message:   "temporary failure",
			},
		},
	}
	originalFactory := newDARTDisclosureClient
	newDARTDisclosureClient = func(apiKey string) dartDisclosureClient {
		if apiKey != "test-secret" {
			t.Fatalf("unexpected OpenDART API key: %q", apiKey)
		}
		return client
	}
	originalNow := dartDisclosureNow
	dartDisclosureNow = func() time.Time {
		return time.Date(
			2026,
			7,
			27,
			16,
			0,
			0,
			0,
			dartDisclosureSeoulLocation,
		)
	}
	t.Cleanup(func() {
		newDARTDisclosureClient = originalFactory
		dartDisclosureNow = originalNow
	})
	t.Setenv("OPENDART_API_KEY", "test-secret")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run(
		[]string{
			"dart-disclosure-sync",
			"-db", databasePath,
			"-days", "30",
			"-output", "json",
		},
		&stdout,
		&stderr,
	)
	if exitCode != 0 {
		t.Fatalf(
			"disclosure sync failed: code=%d stdout=%s stderr=%s",
			exitCode,
			stdout.String(),
			stderr.String(),
		)
	}

	var result dartDisclosureSyncCommandResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode disclosure sync result: %v\n%s", err, stdout.String())
	}
	if result.Status != models.DataStatusPartial ||
		result.Disclosures != 1 ||
		result.Inserted != 1 ||
		len(result.Issues) != 1 {
		t.Fatalf("unexpected disclosure sync result: %#v", result)
	}

	store, err = sqlitestore.Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	stored, err := store.ListDARTDisclosures(ctx, "00126380", 10)
	if err != nil {
		t.Fatalf("list stored disclosures: %v", err)
	}
	if len(stored) != 1 || stored[0].ReceiptNo != "20260724000123" {
		t.Fatalf("successful corporation was not stored: %#v", stored)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close verified store: %v", err)
	}

	listOutput := runCommand(
		t,
		"dart-disclosure-list",
		"-db", databasePath,
		"-ticker", "005930",
		"-limit", "10",
		"-output", "json",
	)
	var listResult dartDisclosureListResult
	if err := json.Unmarshal(listOutput, &listResult); err != nil {
		t.Fatalf("decode disclosure list: %v\n%s", err, listOutput)
	}
	if listResult.CorpCode != "00126380" ||
		len(listResult.Disclosures) != 1 ||
		listResult.Disclosures[0].ReceiptNo != "20260724000123" {
		t.Fatalf("unexpected disclosure list result: %#v", listResult)
	}
}

type stubDARTDisclosureClient struct {
	results map[string]dart.DisclosureResult
	errors  map[string]error
}

func (c stubDARTDisclosureClient) DisclosureHistory(
	_ context.Context,
	corpCode string,
	_ time.Time,
	_ time.Time,
	_ int,
) (dart.DisclosureResult, error) {
	return c.results[corpCode], c.errors[corpCode]
}

func commandDARTDisclosure(fetchedAt time.Time) models.DARTDisclosure {
	receiptDate := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	observedAt := receiptDate
	return models.DARTDisclosure{
		CorpClass:   "Y",
		CorpCode:    "00126380",
		CorpName:    "삼성전자",
		StockCode:   "005930",
		ReportName:  "주요사항보고서",
		ReceiptNo:   "20260724000123",
		ReceiptDate: receiptDate,
		Submitter:   "삼성전자",
		Remark:      "유",
		ViewerURL: "https://dart.fss.or.kr/dsaf001/main.do?rcpNo=" +
			"20260724000123",
		Source: models.SourceMetadata{
			Provider:   "opendart",
			SourceURL:  "https://opendart.fss.or.kr/api/list.json?corp_code=00126380",
			ObservedAt: &observedAt,
			FetchedAt:  fetchedAt,
		},
	}
}
