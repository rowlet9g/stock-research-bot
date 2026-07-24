package mirae

import (
	"testing"

	"github.com/rowlet9g/stock-research-bot/internal/decimal"
	"github.com/xuri/excelize/v2"
)

func TestParseXLSXSelectsInventoryRowsAndDerivesTradeFields(t *testing.T) {
	content := buildLedgerWorkbook(t, [][]string{
		{"2026.07.01", "주식매수입고", "예시전자보통주", "2", "120,000", "0", "15", "0"},
		{"2026.07.01", "주식매수출금", "", "0", "120,000", "0", "15", "0"},
		{"2026.07.02", "해외주식매도출고", "EXAMPLE CORP", "3", "0", "301.5", "0.25", "0"},
		{"2026.07.02", "외화예탁금세금출금", "", "0", "0", "1.25", "0", "0"},
		{"2026.07.01", "주식매수입고", "예시전자보통주", "2", "120,000", "0", "15", "0"},
	})

	ledger, err := ParseXLSX(content)
	if err != nil {
		t.Fatalf("parse XLSX: %v", err)
	}
	if ledger.SheetName != "거래내역" {
		t.Fatalf("unexpected sheet: %q", ledger.SheetName)
	}
	if ledger.RowsSeen != 5 || ledger.RowsSkipped != 2 {
		t.Fatalf("unexpected row counts: seen=%d skipped=%d", ledger.RowsSeen, ledger.RowsSkipped)
	}
	if len(ledger.Trades) != 3 {
		t.Fatalf("expected 3 trades, got %d", len(ledger.Trades))
	}

	domestic := ledger.Trades[0]
	if domestic.Action != "BUY" || domestic.Currency != "KRW" {
		t.Fatalf("unexpected domestic trade: %#v", domestic)
	}
	if got := decimal.Format(domestic.PriceUnits); got != "60000" {
		t.Fatalf("expected derived price 60000, got %s", got)
	}
	if domestic.TaxesKnown {
		t.Fatal("expected taxes to be marked unknown")
	}
	if domestic.PriceSource != "derived_amount_div_quantity" {
		t.Fatalf("unexpected price source: %q", domestic.PriceSource)
	}
	if domestic.TimePrecision != "day" {
		t.Fatalf("unexpected time precision: %q", domestic.TimePrecision)
	}

	foreign := ledger.Trades[1]
	if foreign.Action != "SELL" || foreign.Currency != "USD" {
		t.Fatalf("unexpected foreign trade: %#v", foreign)
	}
	if got := decimal.Format(foreign.PriceUnits); got != "100.5" {
		t.Fatalf("expected derived price 100.5, got %s", got)
	}
	if domestic.ExternalID == ledger.Trades[2].ExternalID {
		t.Fatal("expected identical source rows to receive distinct occurrence IDs")
	}
	if ledger.IgnoredTypes["외화예탁금세금출금"] != 1 {
		t.Fatalf("expected tax ledger row to remain ignored: %#v", ledger.IgnoredTypes)
	}
}

func TestParseXLSXRequiresMiraeHeaders(t *testing.T) {
	workbook := excelize.NewFile()
	buffer, err := workbook.WriteToBuffer()
	if err != nil {
		t.Fatalf("write workbook: %v", err)
	}
	if _, err := ParseXLSX(buffer.Bytes()); err == nil {
		t.Fatal("expected missing header error")
	}
}

func TestNormalizeInstrumentName(t *testing.T) {
	tests := map[string]string{
		"삼성전자보통주":              "삼성전자",
		" 삼성 전자 보통주 ":          "삼성전자",
		"Vanguard Total Stock": "VANGUARDTOTALSTOCK",
	}
	for input, expected := range tests {
		if got := NormalizeInstrumentName(input); got != expected {
			t.Fatalf("normalize %q: expected %q, got %q", input, expected, got)
		}
	}
}

func buildLedgerWorkbook(t *testing.T, dataRows [][]string) []byte {
	t.Helper()
	workbook := excelize.NewFile()
	defer workbook.Close()
	if err := workbook.SetSheetName("Sheet1", "거래내역"); err != nil {
		t.Fatalf("rename sheet: %v", err)
	}
	for column, value := range ledgerHeaders {
		cell, err := excelize.CoordinatesToCellName(column+1, 5)
		if err != nil {
			t.Fatalf("header cell: %v", err)
		}
		if err := workbook.SetCellValue("거래내역", cell, value); err != nil {
			t.Fatalf("write header: %v", err)
		}
	}
	for rowIndex, row := range dataRows {
		for column, value := range row {
			cell, err := excelize.CoordinatesToCellName(column+1, rowIndex+6)
			if err != nil {
				t.Fatalf("data cell: %v", err)
			}
			if err := workbook.SetCellValue("거래내역", cell, value); err != nil {
				t.Fatalf("write data: %v", err)
			}
		}
	}
	buffer, err := workbook.WriteToBuffer()
	if err != nil {
		t.Fatalf("write workbook: %v", err)
	}
	return buffer.Bytes()
}
