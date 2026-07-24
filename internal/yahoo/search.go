package yahoo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"unicode"

	"github.com/rowlet9g/stock-research-bot/internal/models"
	"github.com/rowlet9g/stock-research-bot/internal/provider"
)

const defaultSearchURL = "https://query2.finance.yahoo.com/v1/finance/search"

type InstrumentResolution struct {
	Instrument models.WatchlistItem
	MatchKind  string
	SourceURL  string
}

type searchQuote struct {
	Exchange  string `json:"exchange"`
	ShortName string `json:"shortname"`
	LongName  string `json:"longname"`
	QuoteType string `json:"quoteType"`
	Symbol    string `json:"symbol"`
	ExchDisp  string `json:"exchDisp"`
}

type searchResponse struct {
	Quotes []searchQuote `json:"quotes"`
}

func (c *Client) ResolveInstrument(
	ctx context.Context,
	name string,
	currency string,
) (InstrumentResolution, error) {
	name = strings.TrimSpace(name)
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if name == "" {
		return InstrumentResolution{}, instrumentSearchError(
			provider.ErrorKindInvalidRequest,
			0,
			"instrument name is required",
			nil,
		)
	}
	if currency != "KRW" && currency != "USD" {
		return InstrumentResolution{}, instrumentSearchError(
			provider.ErrorKindInvalidRequest,
			0,
			"instrument currency must be KRW or USD",
			nil,
		)
	}

	var lastSourceURL string
	for _, query := range instrumentSearchQueries(name) {
		quotes, sourceURL, err := c.searchInstruments(ctx, query)
		lastSourceURL = sourceURL
		if err != nil {
			return InstrumentResolution{}, err
		}
		candidate, matchKind, found := selectInstrumentCandidate(query, currency, quotes)
		if !found {
			continue
		}
		return InstrumentResolution{
			Instrument: watchlistItemFromCandidate(name, currency, candidate),
			MatchKind:  matchKind,
			SourceURL:  sourceURL,
		}, nil
	}
	return InstrumentResolution{}, instrumentSearchError(
		provider.ErrorKindBadResponse,
		0,
		fmt.Sprintf("no %s instrument matched %q", currency, name),
		fmt.Errorf("source: %s", lastSourceURL),
	)
}

func (c *Client) searchInstruments(
	ctx context.Context,
	query string,
) ([]searchQuote, string, error) {
	searchURL := c.searchURL
	if searchURL == "" {
		searchURL = defaultSearchURL
	}
	parsedURL, err := url.Parse(searchURL)
	if err != nil {
		return nil, "", instrumentSearchError(
			provider.ErrorKindInvalidRequest,
			0,
			"invalid instrument search URL",
			err,
		)
	}
	parameters := parsedURL.Query()
	parameters.Set("q", query)
	parameters.Set("quotesCount", "10")
	parameters.Set("newsCount", "0")
	parameters.Set("listsCount", "0")
	parsedURL.RawQuery = parameters.Encode()
	sourceURL := parsedURL.String()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return nil, sourceURL, instrumentSearchError(
			provider.ErrorKindInvalidRequest,
			0,
			"could not build instrument search request",
			err,
		)
	}
	request.Header.Set("User-Agent", "forgetmenot-research-bot/0.1")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, sourceURL, instrumentSearchError(
			provider.ErrorKindUnavailable,
			0,
			"instrument search request failed",
			err,
		)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		kind := provider.ErrorKindBadResponse
		if response.StatusCode >= 500 {
			kind = provider.ErrorKindUnavailable
		}
		return nil, sourceURL, instrumentSearchError(
			kind,
			response.StatusCode,
			"instrument search returned an unexpected HTTP status",
			nil,
		)
	}

	var payload searchResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, sourceURL, instrumentSearchError(
			provider.ErrorKindBadResponse,
			response.StatusCode,
			"instrument search returned invalid JSON",
			err,
		)
	}
	return payload.Quotes, sourceURL, nil
}

func instrumentSearchQueries(name string) []string {
	queries := []string{strings.TrimSpace(name)}
	withoutCommonStock := strings.TrimSpace(strings.TrimSuffix(name, "보통주"))
	if withoutCommonStock != "" && withoutCommonStock != queries[0] {
		queries = append(queries, withoutCommonStock)
	}
	if strings.Contains(name, "KODEX") && strings.Contains(name, "인버스") {
		queries = append(queries, "KODEX 인버스")
	}

	seen := map[string]struct{}{}
	result := make([]string, 0, len(queries))
	for _, query := range queries {
		key := strings.ToUpper(strings.TrimSpace(query))
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, query)
	}
	return result
}

func selectInstrumentCandidate(
	query string,
	currency string,
	quotes []searchQuote,
) (searchQuote, string, bool) {
	type rankedQuote struct {
		quote     searchQuote
		score     int
		matchKind string
		index     int
	}
	ranked := []rankedQuote{}
	for index, quote := range quotes {
		if !candidateMatchesCurrency(quote, currency) {
			continue
		}
		score, matchKind := instrumentNameScore(query, quote)
		ranked = append(ranked, rankedQuote{
			quote:     quote,
			score:     score,
			matchKind: matchKind,
			index:     index,
		})
	}
	if len(ranked) == 0 {
		return searchQuote{}, "", false
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score == ranked[j].score {
			return ranked[i].index < ranked[j].index
		}
		return ranked[i].score > ranked[j].score
	})
	return ranked[0].quote, ranked[0].matchKind, true
}

func candidateMatchesCurrency(quote searchQuote, currency string) bool {
	symbol := strings.ToUpper(strings.TrimSpace(quote.Symbol))
	quoteType := strings.ToUpper(strings.TrimSpace(quote.QuoteType))
	if quoteType != "EQUITY" && quoteType != "ETF" {
		return false
	}
	if currency == "KRW" {
		return strings.HasSuffix(symbol, ".KS") || strings.HasSuffix(symbol, ".KQ")
	}
	exchange := strings.ToUpper(strings.TrimSpace(quote.Exchange))
	switch exchange {
	case "ASE", "BTS", "NCM", "NGM", "NMS", "NYQ", "PCX":
		return !strings.HasSuffix(symbol, ".KS") && !strings.HasSuffix(symbol, ".KQ")
	default:
		return false
	}
}

func instrumentNameScore(query string, quote searchQuote) (int, string) {
	normalizedQuery := normalizeSearchText(query)
	names := []string{
		normalizeSearchText(quote.ShortName),
		normalizeSearchText(quote.LongName),
	}
	for _, name := range names {
		if normalizedQuery != "" && normalizedQuery == name {
			return 100, "exact_name"
		}
	}
	for _, name := range names {
		if normalizedQuery != "" && name != "" &&
			(strings.Contains(name, normalizedQuery) || strings.Contains(normalizedQuery, name)) {
			return 80, "partial_name"
		}
	}
	return 10, "provider_rank"
}

func watchlistItemFromCandidate(
	sourceName string,
	currency string,
	quote searchQuote,
) models.WatchlistItem {
	symbol := strings.ToUpper(strings.TrimSpace(quote.Symbol))
	ticker := symbol
	market := strings.TrimSpace(quote.ExchDisp)
	if strings.HasSuffix(symbol, ".KS") {
		ticker = strings.TrimSuffix(symbol, ".KS")
		market = "KOSPI"
	}
	if strings.HasSuffix(symbol, ".KQ") {
		ticker = strings.TrimSuffix(symbol, ".KQ")
		market = "KOSDAQ"
	}
	if market == "" {
		market = strings.ToUpper(strings.TrimSpace(quote.Exchange))
	}
	return models.WatchlistItem{
		Name:        strings.TrimSpace(sourceName),
		Ticker:      ticker,
		YahooTicker: symbol,
		Market:      market,
		Currency:    currency,
	}
}

func normalizeSearchText(value string) string {
	var normalized strings.Builder
	for _, char := range strings.ToUpper(value) {
		if unicode.IsLetter(char) || unicode.IsDigit(char) {
			normalized.WriteRune(char)
		}
	}
	return normalized.String()
}

func instrumentSearchError(
	kind provider.ErrorKind,
	statusCode int,
	message string,
	err error,
) error {
	return &provider.Error{
		Provider:   providerName,
		Operation:  "search_instrument",
		Kind:       kind,
		StatusCode: statusCode,
		Message:    message,
		Err:        err,
	}
}
