package watchlist

import (
	"encoding/csv"
	"fmt"
	"os"
	"strings"

	"github.com/rowlet9g/stock-research-bot/internal/models"
)

func Load(path string) ([]models.WatchlistItem, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.TrimLeadingSpace = true

	rows, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) < 2 {
		return nil, nil
	}

	header := map[string]int{}
	for i, column := range rows[0] {
		header[strings.TrimSpace(column)] = i
	}

	var items []models.WatchlistItem
	for _, row := range rows[1:] {
		items = append(items, models.WatchlistItem{
			Name:         field(row, header, "name"),
			Ticker:       field(row, header, "ticker"),
			YahooTicker:  field(row, header, "yahoo_ticker"),
			DARTCorpCode: field(row, header, "dart_corp_code"),
			Market:       field(row, header, "market"),
			Currency:     field(row, header, "currency"),
		})
	}
	return items, nil
}

func Select(items []models.WatchlistItem, name string, ticker string) (models.WatchlistItem, error) {
	name = strings.TrimSpace(name)
	ticker = strings.TrimSpace(ticker)

	if name == "" && ticker == "" {
		return items[0], nil
	}

	for _, item := range items {
		if name != "" && item.Name == name {
			return item, nil
		}
		if ticker != "" && (item.Ticker == ticker || item.YahooTicker == ticker) {
			return item, nil
		}
	}

	return models.WatchlistItem{}, fmt.Errorf("stock not found in watchlist: name=%q ticker=%q", name, ticker)
}

func field(row []string, header map[string]int, name string) string {
	index, ok := header[name]
	if !ok || index >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[index])
}
