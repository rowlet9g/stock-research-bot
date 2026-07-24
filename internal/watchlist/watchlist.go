package watchlist

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/rowlet9g/stock-research-bot/internal/models"
)

var requiredHeaders = []string{
	"name",
	"ticker",
	"yahoo_ticker",
	"dart_corp_code",
	"market",
	"currency",
}

type ValidationError struct {
	Path    string
	Row     int
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	location := e.Path
	if e.Row > 0 {
		location = fmt.Sprintf("%s:%d", location, e.Row)
	}
	if e.Field != "" {
		location += " field " + e.Field
	}
	return fmt.Sprintf("%s: %s", location, e.Message)
}

func Load(path string) ([]models.WatchlistItem, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open watchlist %q: %w", path, err)
	}
	defer file.Close()

	items, err := loadCSV(file, path)
	if err != nil {
		return nil, err
	}
	return items, nil
}

func loadCSV(input io.Reader, path string) ([]models.WatchlistItem, error) {
	reader := csv.NewReader(input)
	reader.TrimLeadingSpace = true

	rows, err := reader.ReadAll()
	if err != nil {
		return nil, &ValidationError{
			Path:    path,
			Message: fmt.Sprintf("invalid CSV: %v", err),
		}
	}
	if len(rows) == 0 {
		return nil, &ValidationError{
			Path:    path,
			Message: "CSV is empty",
		}
	}

	header := make(map[string]int, len(rows[0]))
	for i, column := range rows[0] {
		name := strings.TrimSpace(strings.TrimPrefix(column, "\uFEFF"))
		if name == "" {
			return nil, &ValidationError{
				Path:    path,
				Row:     1,
				Message: "header contains an empty column name",
			}
		}
		if _, exists := header[name]; exists {
			return nil, &ValidationError{
				Path:    path,
				Row:     1,
				Field:   name,
				Message: "duplicate header",
			}
		}
		header[name] = i
	}
	for _, name := range requiredHeaders {
		if _, ok := header[name]; !ok {
			return nil, &ValidationError{
				Path:    path,
				Row:     1,
				Field:   name,
				Message: "required header is missing",
			}
		}
	}

	items := make([]models.WatchlistItem, 0, len(rows)-1)
	seenTickers := map[string]int{}
	seenYahooTickers := map[string]int{}
	for rowIndex, row := range rows[1:] {
		csvRow := rowIndex + 2
		if isBlankRow(row) {
			continue
		}

		item := models.WatchlistItem{
			Name:         field(row, header, "name"),
			Ticker:       field(row, header, "ticker"),
			YahooTicker:  field(row, header, "yahoo_ticker"),
			DARTCorpCode: field(row, header, "dart_corp_code"),
			Market:       field(row, header, "market"),
			Currency:     field(row, header, "currency"),
		}
		requiredValues := map[string]string{
			"name":         item.Name,
			"ticker":       item.Ticker,
			"yahoo_ticker": item.YahooTicker,
			"market":       item.Market,
			"currency":     item.Currency,
		}
		for _, name := range []string{"name", "ticker", "yahoo_ticker", "market", "currency"} {
			if requiredValues[name] == "" {
				return nil, &ValidationError{
					Path:    path,
					Row:     csvRow,
					Field:   name,
					Message: "required value is empty",
				}
			}
		}

		tickerKey := strings.ToUpper(item.Ticker)
		if previousRow, exists := seenTickers[tickerKey]; exists {
			return nil, &ValidationError{
				Path:    path,
				Row:     csvRow,
				Field:   "ticker",
				Message: fmt.Sprintf("duplicate value %q; first seen on row %d", item.Ticker, previousRow),
			}
		}
		seenTickers[tickerKey] = csvRow

		yahooTickerKey := strings.ToUpper(item.YahooTicker)
		if previousRow, exists := seenYahooTickers[yahooTickerKey]; exists {
			return nil, &ValidationError{
				Path:    path,
				Row:     csvRow,
				Field:   "yahoo_ticker",
				Message: fmt.Sprintf("duplicate value %q; first seen on row %d", item.YahooTicker, previousRow),
			}
		}
		seenYahooTickers[yahooTickerKey] = csvRow
		items = append(items, item)
	}
	return items, nil
}

func Select(items []models.WatchlistItem, name string, ticker string) (models.WatchlistItem, error) {
	name = strings.TrimSpace(name)
	ticker = strings.TrimSpace(ticker)

	if len(items) == 0 {
		return models.WatchlistItem{}, &ValidationError{
			Path:    "watchlist",
			Message: "no data rows found",
		}
	}
	if name == "" && ticker == "" {
		return items[0], nil
	}

	for _, item := range items {
		if name != "" && item.Name == name {
			return item, nil
		}
		if ticker != "" && (strings.EqualFold(item.Ticker, ticker) || strings.EqualFold(item.YahooTicker, ticker)) {
			return item, nil
		}
	}

	return models.WatchlistItem{}, &ValidationError{
		Path:    "watchlist selection",
		Message: fmt.Sprintf("stock not found: name=%q ticker=%q", name, ticker),
	}
}

func field(row []string, header map[string]int, name string) string {
	index := header[name]
	if index >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[index])
}

func isBlankRow(row []string) bool {
	for _, value := range row {
		if strings.TrimSpace(value) != "" {
			return false
		}
	}
	return true
}
