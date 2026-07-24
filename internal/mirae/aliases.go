package mirae

import (
	"encoding/csv"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/models"
)

var aliasHeaders = []string{
	"source_name",
	"ticker",
	"yahoo_ticker",
	"market",
	"currency",
	"source_url",
	"verified_at",
}

type InstrumentAlias struct {
	SourceName string
	Instrument models.WatchlistItem
	SourceURL  string
	VerifiedAt time.Time
}

func LoadInstrumentAliases(path string) ([]InstrumentAlias, bool, error) {
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("open instrument alias CSV %q: %w", path, err)
	}
	defer file.Close()

	aliases, err := ParseInstrumentAliases(file)
	if err != nil {
		return nil, true, fmt.Errorf("parse instrument alias CSV %q: %w", path, err)
	}
	return aliases, true, nil
}

func ParseInstrumentAliases(reader io.Reader) ([]InstrumentAlias, error) {
	records, err := csv.NewReader(reader).ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read CSV: %w", err)
	}
	if len(records) < 2 {
		return nil, fmt.Errorf("CSV has no alias rows")
	}

	header := map[string]int{}
	for index, value := range records[0] {
		name := strings.TrimSpace(strings.TrimPrefix(value, "\uFEFF"))
		if _, exists := header[name]; exists {
			return nil, fmt.Errorf("duplicate header %q", name)
		}
		header[name] = index
	}
	for _, name := range aliasHeaders {
		if _, exists := header[name]; !exists {
			return nil, fmt.Errorf("missing required header %q", name)
		}
	}

	aliases := make([]InstrumentAlias, 0, len(records)-1)
	seen := map[string]int{}
	for index, record := range records[1:] {
		rowNumber := index + 2
		if blankRow(record) {
			continue
		}
		alias, err := parseInstrumentAlias(record, header)
		if err != nil {
			return nil, fmt.Errorf("row %d: %w", rowNumber, err)
		}
		key := strings.ToUpper(alias.Instrument.Currency) +
			"\x1f" +
			NormalizeInstrumentName(alias.SourceName)
		if previousRow, exists := seen[key]; exists {
			return nil, fmt.Errorf(
				"row %d duplicates source name and currency from row %d",
				rowNumber,
				previousRow,
			)
		}
		seen[key] = rowNumber
		aliases = append(aliases, alias)
	}
	if len(aliases) == 0 {
		return nil, fmt.Errorf("CSV has no non-empty alias rows")
	}
	return aliases, nil
}

func parseInstrumentAlias(
	record []string,
	header map[string]int,
) (InstrumentAlias, error) {
	sourceName := aliasField(record, header, "source_name")
	ticker := strings.ToUpper(aliasField(record, header, "ticker"))
	yahooTicker := strings.ToUpper(aliasField(record, header, "yahoo_ticker"))
	market := aliasField(record, header, "market")
	currency := strings.ToUpper(aliasField(record, header, "currency"))
	sourceURL := aliasField(record, header, "source_url")
	verifiedAtText := aliasField(record, header, "verified_at")
	for name, value := range map[string]string{
		"source_name":  sourceName,
		"ticker":       ticker,
		"yahoo_ticker": yahooTicker,
		"market":       market,
		"currency":     currency,
		"source_url":   sourceURL,
		"verified_at":  verifiedAtText,
	} {
		if value == "" {
			return InstrumentAlias{}, fmt.Errorf("%s is required", name)
		}
	}
	if currency != "KRW" && currency != "USD" {
		return InstrumentAlias{}, fmt.Errorf("currency must be KRW or USD")
	}
	parsedURL, err := url.ParseRequestURI(sourceURL)
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
		return InstrumentAlias{}, fmt.Errorf("source_url is invalid")
	}
	verifiedAt, err := time.Parse("2006-01-02", verifiedAtText)
	if err != nil {
		return InstrumentAlias{}, fmt.Errorf("verified_at must be YYYY-MM-DD")
	}

	return InstrumentAlias{
		SourceName: sourceName,
		Instrument: models.WatchlistItem{
			Name:        sourceName,
			Ticker:      ticker,
			YahooTicker: yahooTicker,
			Market:      market,
			Currency:    currency,
		},
		SourceURL:  sourceURL,
		VerifiedAt: verifiedAt.UTC(),
	}, nil
}

func aliasField(record []string, header map[string]int, name string) string {
	index := header[name]
	if index >= len(record) {
		return ""
	}
	return strings.TrimSpace(record[index])
}
