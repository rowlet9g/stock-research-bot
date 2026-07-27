package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/dart"
	"github.com/rowlet9g/stock-research-bot/internal/models"
	"github.com/xuri/excelize/v2"
)

func TestStorageCommandsPersistAndReturnPortfolioJSON(t *testing.T) {
	tempDirectory := t.TempDir()
	databasePath := filepath.Join(tempDirectory, "forgetmenot.db")
	watchlistPath := filepath.Join(tempDirectory, "watchlist.csv")
	tradesPath := filepath.Join(tempDirectory, "trades.csv")

	watchlistCSV := `name,ticker,yahoo_ticker,dart_corp_code,market,currency
삼성전자,005930,005930.KS,00126380,KOSPI,KRW
Apple,AAPL,AAPL,,NASDAQ,USD
`
	tradesCSV := `external_id,trade_date,ticker,action,quantity,price,fees,taxes,currency
AAPL-001,2026-07-02,AAPL,BUY,2,210.50,0.25,0,USD
`
	if err := os.WriteFile(watchlistPath, []byte(watchlistCSV), 0o600); err != nil {
		t.Fatalf("write watchlist: %v", err)
	}
	if err := os.WriteFile(tradesPath, []byte(tradesCSV), 0o600); err != nil {
		t.Fatalf("write trades: %v", err)
	}

	runCommand(t, "db-init", "-db", databasePath, "-output", "json")
	runCommand(
		t,
		"watchlist-sync",
		"-db", databasePath,
		"-watchlist", watchlistPath,
		"-output", "json",
	)
	runCommand(
		t,
		"position-set",
		"-db", databasePath,
		"-ticker", "AAPL",
		"-quantity", "2",
		"-average-cost", "210.50",
		"-currency", "USD",
		"-as-of", "2026-07-23",
		"-output", "json",
	)
	runCommand(
		t,
		"thesis-set",
		"-db", databasePath,
		"-ticker", "AAPL",
		"-summary", "서비스 매출 성장",
		"-invalidation", "서비스 성장률 둔화",
		"-horizon", "12개월",
		"-metrics", "서비스 매출,마진",
		"-output", "json",
	)

	firstImportJSON := runCommand(
		t,
		"trades-import",
		"-db", databasePath,
		"-file", tradesPath,
		"-source", "test",
		"-output", "json",
	)
	var firstImport struct {
		RowsInserted    int  `json:"rows_inserted"`
		AlreadyImported bool `json:"already_imported"`
	}
	if err := json.Unmarshal(firstImportJSON, &firstImport); err != nil {
		t.Fatalf("decode first import: %v\n%s", err, firstImportJSON)
	}
	if firstImport.RowsInserted != 1 || firstImport.AlreadyImported {
		t.Fatalf("unexpected first import: %#v", firstImport)
	}

	secondImportJSON := runCommand(
		t,
		"trades-import",
		"-db", databasePath,
		"-file", tradesPath,
		"-source", "test",
		"-output", "json",
	)
	var secondImport struct {
		AlreadyImported bool `json:"already_imported"`
	}
	if err := json.Unmarshal(secondImportJSON, &secondImport); err != nil {
		t.Fatalf("decode second import: %v\n%s", err, secondImportJSON)
	}
	if !secondImport.AlreadyImported {
		t.Fatalf("expected repeated import marker: %s", secondImportJSON)
	}

	portfolioJSON := runCommand(
		t,
		"portfolio-show",
		"-db", databasePath,
		"-ticker", "AAPL",
		"-output", "json",
	)
	var portfolio portfolioView
	if err := json.Unmarshal(portfolioJSON, &portfolio); err != nil {
		t.Fatalf("decode portfolio: %v\n%s", err, portfolioJSON)
	}
	if portfolio.Position == nil || portfolio.Position.Quantity != "2" {
		t.Fatalf("unexpected position: %#v", portfolio.Position)
	}
	if len(portfolio.Trades) != 1 || portfolio.Trades[0].Price != "210.5" {
		t.Fatalf("unexpected trades: %#v", portfolio.Trades)
	}
	if portfolio.Trades[0].PriceSource != "reported" ||
		!portfolio.Trades[0].TaxesKnown ||
		portfolio.Trades[0].TimePrecision != "day" {
		t.Fatalf("unexpected normalized trade quality: %#v", portfolio.Trades[0])
	}
	if portfolio.Thesis == nil || portfolio.Thesis.Summary != "서비스 매출 성장" {
		t.Fatalf("unexpected thesis: %#v", portfolio.Thesis)
	}

	deleteJSON := runCommand(
		t,
		"watchlist-delete",
		"-db", databasePath,
		"-ticker", "005930",
		"-output", "json",
	)
	var deletion struct {
		Deleted bool `json:"deleted"`
	}
	if err := json.Unmarshal(deleteJSON, &deletion); err != nil {
		t.Fatalf("decode deletion: %v\n%s", err, deleteJSON)
	}
	if !deletion.Deleted {
		t.Fatal("expected instrument deletion")
	}
}

func TestMiraeImportUsesVerifiedAliasesAndDeduplicatesFile(t *testing.T) {
	tempDirectory := t.TempDir()
	databasePath := filepath.Join(tempDirectory, "forgetmenot.db")
	ledgerPath := filepath.Join(tempDirectory, "ledger.xlsx")
	aliasPath := filepath.Join(tempDirectory, "aliases.csv")

	writeMiraeTestWorkbook(t, ledgerPath)
	aliasCSV := `source_name,ticker,yahoo_ticker,market,currency,source_url,verified_at
예시전자보통주,123456,123456.KS,KOSPI,KRW,https://example.com/123456,2026-07-24
EXAMPLE ETF,EXMP,EXMP,NASDAQ,USD,https://example.com/exmp,2026-07-24
`
	if err := os.WriteFile(aliasPath, []byte(aliasCSV), 0o600); err != nil {
		t.Fatalf("write alias CSV: %v", err)
	}

	firstImportJSON := runCommand(
		t,
		"mirae-import",
		"-db", databasePath,
		"-file", ledgerPath,
		"-aliases", aliasPath,
		"-output", "json",
	)
	var firstImport miraeImportResult
	if err := json.Unmarshal(firstImportJSON, &firstImport); err != nil {
		t.Fatalf("decode Mirae import: %v\n%s", err, firstImportJSON)
	}
	if firstImport.LedgerRows != 4 ||
		firstImport.TradeRows != 2 ||
		firstImport.SkippedRows != 2 ||
		firstImport.ResolvedCache != 2 ||
		firstImport.Import.RowsInserted != 2 {
		t.Fatalf("unexpected Mirae import: %#v", firstImport)
	}

	portfolioJSON := runCommand(
		t,
		"portfolio-show",
		"-db", databasePath,
		"-ticker", "123456",
		"-output", "json",
	)
	var portfolio portfolioView
	if err := json.Unmarshal(portfolioJSON, &portfolio); err != nil {
		t.Fatalf("decode portfolio: %v\n%s", err, portfolioJSON)
	}
	if len(portfolio.Trades) != 1 {
		t.Fatalf("expected one domestic trade, got %#v", portfolio.Trades)
	}
	trade := portfolio.Trades[0]
	if trade.Price != "60000" ||
		trade.PriceSource != "derived_amount_div_quantity" ||
		trade.TaxesKnown ||
		trade.TimePrecision != "day" {
		t.Fatalf("unexpected derived trade quality: %#v", trade)
	}

	secondImportJSON := runCommand(
		t,
		"mirae-import",
		"-db", databasePath,
		"-file", ledgerPath,
		"-aliases", aliasPath,
		"-output", "json",
	)
	var secondImport miraeImportResult
	if err := json.Unmarshal(secondImportJSON, &secondImport); err != nil {
		t.Fatalf("decode repeated Mirae import: %v\n%s", err, secondImportJSON)
	}
	if !secondImport.Import.AlreadyImported {
		t.Fatalf("expected repeated file marker: %#v", secondImport)
	}
}

func TestDARTCorporationSyncMapsStoredInstrument(t *testing.T) {
	tempDirectory := t.TempDir()
	databasePath := filepath.Join(tempDirectory, "forgetmenot.db")
	watchlistPath := filepath.Join(tempDirectory, "watchlist.csv")
	watchlistCSV := `name,ticker,yahoo_ticker,dart_corp_code,market,currency
삼성전자,005930,005930.KS,,KOSPI,KRW
`
	if err := os.WriteFile(watchlistPath, []byte(watchlistCSV), 0o600); err != nil {
		t.Fatalf("write watchlist: %v", err)
	}
	runCommand(
		t,
		"watchlist-sync",
		"-db", databasePath,
		"-watchlist", watchlistPath,
		"-output", "json",
	)

	fetchedAt := time.Date(2026, 7, 27, 11, 0, 0, 0, time.UTC)
	modifiedAt := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	originalFactory := newDARTCorporationClient
	newDARTCorporationClient = func(apiKey string) dartCorporationClient {
		if apiKey != "test-secret" {
			t.Fatalf("unexpected API key passed to client: %q", apiKey)
		}
		return stubDARTCorporationClient{
			result: dart.CorporationCodeResult{
				Status: models.DataStatusAvailable,
				Corporations: []models.DARTCorporation{
					{
						CorpCode:   "00126380",
						Name:       "삼성전자",
						StockCode:  "005930",
						ModifiedAt: modifiedAt,
						Source: models.SourceMetadata{
							Provider:   "opendart",
							SourceURL:  "https://opendart.fss.or.kr/api/corpCode.xml",
							ObservedAt: &modifiedAt,
							FetchedAt:  fetchedAt,
						},
					},
				},
				Source: models.SourceMetadata{
					Provider:   "opendart",
					SourceURL:  "https://opendart.fss.or.kr/api/corpCode.xml",
					ObservedAt: &modifiedAt,
					FetchedAt:  fetchedAt,
				},
			},
		}
	}
	t.Cleanup(func() {
		newDARTCorporationClient = originalFactory
	})
	t.Setenv("OPENDART_API_KEY", "test-secret")

	output := runCommand(
		t,
		"dart-corp-sync",
		"-db", databasePath,
		"-output", "json",
	)
	var syncResult dartCorporationSyncResult
	if err := json.Unmarshal(output, &syncResult); err != nil {
		t.Fatalf("decode OpenDART sync result: %v\n%s", err, output)
	}
	if syncResult.Corporations != 1 || syncResult.InstrumentsMapped != 1 {
		t.Fatalf("unexpected OpenDART sync result: %#v", syncResult)
	}

	instrumentOutput := runCommand(
		t,
		"watchlist-list",
		"-db", databasePath,
		"-output", "json",
	)
	var instruments []models.Instrument
	if err := json.Unmarshal(instrumentOutput, &instruments); err != nil {
		t.Fatalf("decode instruments: %v\n%s", err, instrumentOutput)
	}
	if len(instruments) != 1 || instruments[0].DARTCorpCode != "00126380" {
		t.Fatalf("expected mapped instrument, got %#v", instruments)
	}
}

type stubDARTCorporationClient struct {
	result dart.CorporationCodeResult
	err    error
}

func (c stubDARTCorporationClient) CorporationCodes(
	context.Context,
) (dart.CorporationCodeResult, error) {
	return c.result, c.err
}

func writeMiraeTestWorkbook(t *testing.T, path string) {
	t.Helper()
	workbook := excelize.NewFile()
	defer workbook.Close()
	if err := workbook.SetSheetName("Sheet1", "거래내역"); err != nil {
		t.Fatalf("rename sheet: %v", err)
	}
	headers := []string{
		"거래일자",
		"거래종류",
		"종목명",
		"거래수량",
		"거래금액",
		"외화거래금액",
		"수수료",
		"예수금잔고",
	}
	rows := [][]string{
		headers,
		{"2026.07.01", "주식매수입고", "예시전자보통주", "2", "120000", "0", "15", "0"},
		{"2026.07.01", "주식매수출금", "", "0", "120000", "0", "15", "0"},
		{"2026.07.02", "해외주식매도출고", "EXAMPLE ETF", "3", "0", "301.5", "0.25", "0"},
		{"2026.07.02", "해외주식매도입금", "EXAMPLE ETF", "0", "0", "301.5", "0.25", "0"},
	}
	for rowIndex, row := range rows {
		for columnIndex, value := range row {
			cell, err := excelize.CoordinatesToCellName(columnIndex+1, rowIndex+5)
			if err != nil {
				t.Fatalf("cell address: %v", err)
			}
			if err := workbook.SetCellValue("거래내역", cell, value); err != nil {
				t.Fatalf("write workbook: %v", err)
			}
		}
	}
	if err := workbook.SaveAs(path); err != nil {
		t.Fatalf("save workbook: %v", err)
	}
}

func runCommand(t *testing.T, args ...string) []byte {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exitCode := run(args, &stdout, &stderr); exitCode != 0 {
		t.Fatalf(
			"command %v failed with exit code %d\nstdout:\n%s\nstderr:\n%s",
			args,
			exitCode,
			stdout.String(),
			stderr.String(),
		)
	}
	return stdout.Bytes()
}
