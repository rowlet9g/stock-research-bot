package main

import (
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

func TestDARTFinancialSyncPreservesSuccessOnPartialFailure(t *testing.T) {
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
	}); err != nil {
		t.Fatalf("seed instrument: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close seed store: %v", err)
	}

	fetchedAt := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	client := &stubDARTFinancialClient{
		results: map[string]dart.FinancialStatementResult{
			"CFS": {
				Status:    models.DataStatusAvailable,
				Statement: commandDARTFinancialStatement(fetchedAt),
			},
		},
		errors: map[string]error{
			"OFS": &provider.Error{
				Provider:  "opendart",
				Operation: "fetch_financial_statement",
				Kind:      provider.ErrorKindUnavailable,
				Message:   "temporary failure",
			},
		},
	}
	originalFactory := newDARTFinancialClient
	newDARTFinancialClient = func(apiKey string) dartFinancialClient {
		if apiKey != "test-secret" {
			t.Fatalf("unexpected OpenDART API key: %q", apiKey)
		}
		return client
	}
	t.Cleanup(func() {
		newDARTFinancialClient = originalFactory
	})
	t.Setenv("OPENDART_API_KEY", "test-secret")

	output := runCommand(
		t,
		"dart-financial-sync",
		"-db", databasePath,
		"-ticker", "005930",
		"-year", "2025",
		"-report-code", "11011",
		"-fs-div", "both",
		"-output", "json",
	)
	var syncResult dartFinancialSyncCommandResult
	if err := json.Unmarshal(output, &syncResult); err != nil {
		t.Fatalf("decode financial sync result: %v\n%s", err, output)
	}
	if syncResult.Status != models.DataStatusPartial ||
		syncResult.Queries != 2 ||
		syncResult.Statements != 1 ||
		syncResult.Accounts != 1 ||
		syncResult.Inserted != 1 ||
		len(syncResult.Issues) != 1 {
		t.Fatalf("unexpected financial sync result: %#v", syncResult)
	}

	repeatOutput := runCommand(
		t,
		"dart-financial-sync",
		"-db", databasePath,
		"-ticker", "005930",
		"-year", "2025",
		"-report-code", "11011",
		"-fs-div", "CFS",
		"-output", "json",
	)
	var repeatResult dartFinancialSyncCommandResult
	if err := json.Unmarshal(repeatOutput, &repeatResult); err != nil {
		t.Fatalf("decode repeated financial sync: %v\n%s", err, repeatOutput)
	}
	if repeatResult.Status != models.DataStatusAvailable ||
		repeatResult.Inserted != 0 ||
		repeatResult.VersionsChanged != 0 {
		t.Fatalf("unexpected repeated financial sync: %#v", repeatResult)
	}

	listOutput := runCommand(
		t,
		"dart-financial-list",
		"-db", databasePath,
		"-ticker", "005930",
		"-year", "2025",
		"-report-code", "11011",
		"-fs-div", "CFS",
		"-versions", "10",
		"-output", "json",
	)
	var listResult dartFinancialListResult
	if err := json.Unmarshal(listOutput, &listResult); err != nil {
		t.Fatalf("decode financial list: %v\n%s", err, listOutput)
	}
	if listResult.CorpCode != "00126380" ||
		len(listResult.Versions) != 1 ||
		!listResult.Versions[0].IsCurrent ||
		listResult.Versions[0].AccountsTotal != 1 ||
		listResult.Versions[0].Accounts[0].CurrentAmount !=
			"500000000000000" {
		t.Fatalf("unexpected financial list result: %#v", listResult)
	}
}

type stubDARTFinancialClient struct {
	results map[string]dart.FinancialStatementResult
	errors  map[string]error
}

func (c *stubDARTFinancialClient) FullFinancialStatement(
	_ context.Context,
	_ string,
	_ int,
	_ string,
	fsKind string,
) (dart.FinancialStatementResult, error) {
	return c.results[fsKind], c.errors[fsKind]
}

func commandDARTFinancialStatement(
	fetchedAt time.Time,
) models.DARTFinancialStatement {
	observedAt := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
	return models.DARTFinancialStatement{
		CorpCode:      "00126380",
		BusinessYear:  2025,
		ReportCode:    "11011",
		FSKind:        "CFS",
		ReceiptNo:     "20260310000777",
		ContentSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		IsCurrent:     true,
		Accounts: []models.DARTFinancialAccount{
			{
				Index:            0,
				StatementKind:    "BS",
				StatementName:    "연결 재무상태표",
				AccountID:        "ifrs-full_Assets",
				AccountName:      "자산총계",
				CurrentTermName:  "제 57 기",
				CurrentAmount:    "500000000000000",
				PreviousTermName: "제 56 기말",
				PreviousAmount:   "450000000000000",
				Order:            1,
				Currency:         "KRW",
			},
		},
		Source: models.SourceMetadata{
			Provider: "opendart",
			SourceURL: "https://opendart.fss.or.kr/api/fnlttSinglAcntAll.json?" +
				"bsns_year=2025&corp_code=00126380&fs_div=CFS&reprt_code=11011",
			ObservedAt: &observedAt,
			FetchedAt:  fetchedAt,
		},
	}
}
