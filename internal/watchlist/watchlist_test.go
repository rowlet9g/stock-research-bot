package watchlist

import (
	"errors"
	"strings"
	"testing"
)

const validCSV = `name,ticker,yahoo_ticker,dart_corp_code,market,currency
삼성전자,005930,005930.KS,00126380,KOSPI,KRW
Apple,AAPL,AAPL,,NASDAQ,USD
`

func TestLoadCSVValidatesAndParsesRows(t *testing.T) {
	items, err := loadCSV(strings.NewReader(validCSV), "watchlist.csv")
	if err != nil {
		t.Fatalf("load CSV: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].DARTCorpCode != "00126380" {
		t.Fatalf("unexpected corporation code: %q", items[0].DARTCorpCode)
	}
}

func TestLoadCSVRejectsMissingHeader(t *testing.T) {
	input := `name,ticker,yahoo_ticker,dart_corp_code,market
Apple,AAPL,AAPL,,NASDAQ
`
	_, err := loadCSV(strings.NewReader(input), "watchlist.csv")
	assertValidationError(t, err, 1, "currency")
}

func TestLoadCSVRejectsMissingRequiredValue(t *testing.T) {
	input := `name,ticker,yahoo_ticker,dart_corp_code,market,currency
Apple,AAPL,,,NASDAQ,USD
`
	_, err := loadCSV(strings.NewReader(input), "watchlist.csv")
	assertValidationError(t, err, 2, "yahoo_ticker")
}

func TestLoadCSVRejectsDuplicateTicker(t *testing.T) {
	input := `name,ticker,yahoo_ticker,dart_corp_code,market,currency
Apple,AAPL,AAPL,,NASDAQ,USD
Apple duplicate,aapl,AAPL2,,NASDAQ,USD
`
	_, err := loadCSV(strings.NewReader(input), "watchlist.csv")
	assertValidationError(t, err, 3, "ticker")
}

func TestSelectRejectsEmptyWatchlist(t *testing.T) {
	_, err := Select(nil, "", "")
	assertValidationError(t, err, 0, "")
}

func assertValidationError(t *testing.T, err error, row int, field string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected validation error")
	}
	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected ValidationError, got %T: %v", err, err)
	}
	if validationErr.Row != row {
		t.Fatalf("expected row %d, got %d", row, validationErr.Row)
	}
	if validationErr.Field != field {
		t.Fatalf("expected field %q, got %q", field, validationErr.Field)
	}
}
