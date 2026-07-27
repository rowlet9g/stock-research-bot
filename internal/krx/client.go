package krx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/models"
	"github.com/rowlet9g/stock-research-bot/internal/provider"
)

const (
	defaultBaseURL     = "https://data-dbg.krx.co.kr/svc/apis"
	providerName       = "krx"
	maxResponseBytes   = 32 << 20
	defaultHTTPTimeout = 30 * time.Second
	defaultMaxAttempts = 3
	defaultRetryDelay  = 200 * time.Millisecond
)

type Dataset string

const (
	DatasetKOSPI  Dataset = "kospi"
	DatasetKOSDAQ Dataset = "kosdaq"
	DatasetKONEX  Dataset = "konex"
	DatasetETF    Dataset = "etf"
	DatasetETN    Dataset = "etn"
)

type DatasetResult struct {
	Dataset     Dataset                `json:"dataset"`
	Status      models.DataStatus      `json:"status"`
	Instruments []models.KRXInstrument `json:"instruments"`
	Source      models.SourceMetadata  `json:"source"`
}

type Client struct {
	apiKey      string
	baseURL     string
	httpClient  *http.Client
	now         func() time.Time
	maxAttempts int
	retryDelay  time.Duration
}

type datasetDefinition struct {
	path   string
	market string
	kind   models.InstrumentType
}

var datasetDefinitions = map[Dataset]datasetDefinition{
	DatasetKOSPI: {
		path:   "/sto/stk_isu_base_info",
		market: "KOSPI",
	},
	DatasetKOSDAQ: {
		path:   "/sto/ksq_isu_base_info",
		market: "KOSDAQ",
	},
	DatasetKONEX: {
		path:   "/sto/knx_isu_base_info",
		market: "KONEX",
	},
	DatasetETF: {
		path:   "/etp/etf_bydd_trd",
		market: "KRX",
		kind:   models.InstrumentTypeETF,
	},
	DatasetETN: {
		path:   "/etp/etn_bydd_trd",
		market: "KRX",
		kind:   models.InstrumentTypeETN,
	},
}

func InstrumentDatasets() []Dataset {
	return []Dataset{
		DatasetKOSPI,
		DatasetKOSDAQ,
		DatasetKONEX,
		DatasetETF,
		DatasetETN,
	}
}

func NewClient(apiKey string) *Client {
	return &Client{
		apiKey:      strings.TrimSpace(apiKey),
		baseURL:     defaultBaseURL,
		httpClient:  newHTTPClient(),
		now:         time.Now,
		maxAttempts: defaultMaxAttempts,
		retryDelay:  defaultRetryDelay,
	}
}

func newHTTPClient() *http.Client {
	return &http.Client{
		Transport: http.DefaultTransport.(*http.Transport).Clone(),
		Timeout:   defaultHTTPTimeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func (c *Client) Instruments(
	ctx context.Context,
	dataset Dataset,
	asOf time.Time,
) (DatasetResult, error) {
	definition, ok := datasetDefinitions[dataset]
	if !ok {
		return DatasetResult{}, requestError(
			"fetch_instruments",
			provider.ErrorKindInvalidRequest,
			fmt.Sprintf("unsupported KRX dataset %q", dataset),
			nil,
		)
	}

	sourceURL := strings.TrimRight(c.baseURL, "/") + definition.path
	observedAt := dateOnlyUTC(asOf)
	result := DatasetResult{
		Dataset: dataset,
		Status:  models.DataStatusUnavailable,
		Source: models.SourceMetadata{
			Provider:   providerName,
			SourceURL:  sourceURL + "?basDd=" + observedAt.Format("20060102"),
			ObservedAt: &observedAt,
			FetchedAt:  c.now().UTC(),
		},
		Instruments: []models.KRXInstrument{},
	}
	if c.apiKey == "" {
		return result, requestError(
			"fetch_instruments",
			provider.ErrorKindInvalidRequest,
			"API key is required",
			nil,
		)
	}
	if asOf.IsZero() {
		return result, requestError(
			"fetch_instruments",
			provider.ErrorKindInvalidRequest,
			"as-of date is required",
			nil,
		)
	}

	response, err := c.fetch(ctx, result.Source.SourceURL)
	if err != nil {
		return result, err
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		kind := provider.ErrorKindBadResponse
		switch {
		case response.StatusCode == http.StatusUnauthorized ||
			response.StatusCode == http.StatusForbidden:
			kind = provider.ErrorKindInvalidRequest
		case response.StatusCode == http.StatusTooManyRequests ||
			response.StatusCode >= 500:
			kind = provider.ErrorKindUnavailable
		}
		return result, &provider.Error{
			Provider:   providerName,
			Operation:  "fetch_instruments",
			Kind:       kind,
			StatusCode: response.StatusCode,
			Message:    fmt.Sprintf("unexpected HTTP status for %s", dataset),
		}
	}

	content, err := readLimited(response.Body, maxResponseBytes)
	if err != nil {
		return result, requestError(
			"read_instruments",
			provider.ErrorKindBadResponse,
			fmt.Sprintf("KRX %s response exceeds the size limit or could not be read", dataset),
			err,
		)
	}

	instruments, err := decodeDataset(dataset, definition, content, result.Source)
	if err != nil {
		return result, requestError(
			"decode_instruments",
			provider.ErrorKindBadResponse,
			fmt.Sprintf("invalid KRX %s response: %v", dataset, err),
			err,
		)
	}
	if len(instruments) == 0 {
		result.Status = models.DataStatusEmpty
		return result, nil
	}
	result.Status = models.DataStatusAvailable
	result.Instruments = instruments
	return result, nil
}

func (c *Client) fetch(ctx context.Context, requestURL string) (*http.Response, error) {
	attempts := c.maxAttempts
	if attempts <= 0 {
		attempts = 1
	}

	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
		if err != nil {
			return nil, requestError(
				"build_instrument_request",
				provider.ErrorKindInvalidRequest,
				"could not build KRX instrument request",
				err,
			)
		}
		request.Header.Set("AUTH_KEY", c.apiKey)
		request.Header.Set("User-Agent", "forgetmenot-research-bot/0.1")

		response, err := c.httpClient.Do(request)
		if err == nil && !retryableStatus(response.StatusCode) {
			return response, nil
		}
		if err != nil {
			lastErr = err
		}
		if attempt == attempts {
			if response != nil {
				return response, nil
			}
			break
		}
		if response != nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
			response.Body.Close()
		}
		if err := waitForRetry(ctx, c.retryDelay*time.Duration(attempt)); err != nil {
			lastErr = err
			break
		}
	}
	return nil, requestError(
		"fetch_instruments",
		provider.ErrorKindUnavailable,
		"request failed: "+safeTransportError(lastErr),
		lastErr,
	)
}

func decodeDataset(
	dataset Dataset,
	definition datasetDefinition,
	content []byte,
	source models.SourceMetadata,
) ([]models.KRXInstrument, error) {
	switch dataset {
	case DatasetKOSPI, DatasetKOSDAQ, DatasetKONEX:
		var payload stockResponse
		if err := json.Unmarshal(content, &payload); err != nil {
			return nil, fmt.Errorf("decode stock JSON: %w", err)
		}
		return normalizeStocks(dataset, definition, payload.Rows, source)
	case DatasetETF, DatasetETN:
		var payload productResponse
		if err := json.Unmarshal(content, &payload); err != nil {
			return nil, fmt.Errorf("decode product JSON: %w", err)
		}
		return normalizeProducts(dataset, definition, payload.Rows, source)
	default:
		return nil, fmt.Errorf("unsupported dataset %q", dataset)
	}
}

func normalizeStocks(
	dataset Dataset,
	definition datasetDefinition,
	rows []stockDTO,
	source models.SourceMetadata,
) ([]models.KRXInstrument, error) {
	instruments := make([]models.KRXInstrument, 0, len(rows))
	seenShortCodes := make(map[string]struct{}, len(rows))
	seenStandardCodes := make(map[string]struct{}, len(rows))
	for index, row := range rows {
		shortCode := strings.ToUpper(strings.TrimSpace(row.ShortCode))
		standardCode := strings.ToUpper(strings.TrimSpace(row.StandardCode))
		name := strings.TrimSpace(row.Name)
		market := strings.ToUpper(strings.TrimSpace(row.Market))
		if !fixedUpperAlphanumeric(shortCode, 6) {
			return nil, fmt.Errorf("row %d has invalid short code %q", index+1, shortCode)
		}
		if !fixedUpperAlphanumeric(standardCode, 12) {
			return nil, fmt.Errorf("row %d has invalid standard code %q", index+1, standardCode)
		}
		if name == "" {
			return nil, fmt.Errorf("row %d has no name", index+1)
		}
		if market != definition.market {
			return nil, fmt.Errorf(
				"row %d has market %q, expected %q",
				index+1,
				market,
				definition.market,
			)
		}
		listingDate, err := time.Parse("20060102", strings.TrimSpace(row.ListingDate))
		if err != nil {
			return nil, fmt.Errorf("row %d has invalid listing date %q", index+1, row.ListingDate)
		}
		if _, exists := seenShortCodes[shortCode]; exists {
			return nil, fmt.Errorf("row %d duplicates short code %q", index+1, shortCode)
		}
		if _, exists := seenStandardCodes[standardCode]; exists {
			return nil, fmt.Errorf("row %d duplicates standard code %q", index+1, standardCode)
		}
		seenShortCodes[shortCode] = struct{}{}
		seenStandardCodes[standardCode] = struct{}{}
		listingDate = listingDate.UTC()
		instruments = append(instruments, models.KRXInstrument{
			StandardCode:    standardCode,
			ShortCode:       shortCode,
			Name:            name,
			AbbreviatedName: strings.TrimSpace(row.AbbreviatedName),
			EnglishName:     strings.TrimSpace(row.EnglishName),
			Market:          market,
			SecurityGroup:   strings.TrimSpace(row.SecurityGroup),
			Section:         strings.TrimSpace(row.Section),
			ShareType:       strings.TrimSpace(row.ShareType),
			InstrumentType:  stockInstrumentType(row.ShareType),
			ListingDate:     &listingDate,
			Dataset:         string(dataset),
			Source:          source,
		})
	}
	return instruments, nil
}

func normalizeProducts(
	dataset Dataset,
	definition datasetDefinition,
	rows []productDTO,
	source models.SourceMetadata,
) ([]models.KRXInstrument, error) {
	instruments := make([]models.KRXInstrument, 0, len(rows))
	seenShortCodes := make(map[string]struct{}, len(rows))
	for index, row := range rows {
		shortCode := strings.ToUpper(strings.TrimSpace(row.ShortCode))
		name := strings.TrimSpace(row.Name)
		if !fixedUpperAlphanumeric(shortCode, 6) {
			return nil, fmt.Errorf("row %d has invalid short code %q", index+1, shortCode)
		}
		if name == "" {
			return nil, fmt.Errorf("row %d has no name", index+1)
		}
		rowDate, err := time.Parse("20060102", strings.TrimSpace(row.BaseDate))
		if err != nil {
			return nil, fmt.Errorf("row %d has invalid base date %q", index+1, row.BaseDate)
		}
		if source.ObservedAt == nil || !rowDate.UTC().Equal(source.ObservedAt.UTC()) {
			return nil, fmt.Errorf(
				"row %d base date %q does not match requested date",
				index+1,
				row.BaseDate,
			)
		}
		if _, exists := seenShortCodes[shortCode]; exists {
			return nil, fmt.Errorf("row %d duplicates short code %q", index+1, shortCode)
		}
		seenShortCodes[shortCode] = struct{}{}
		instruments = append(instruments, models.KRXInstrument{
			ShortCode:       shortCode,
			Name:            name,
			AbbreviatedName: name,
			Market:          definition.market,
			InstrumentType:  definition.kind,
			Dataset:         string(dataset),
			Source:          source,
		})
	}
	return instruments, nil
}

func stockInstrumentType(shareType string) models.InstrumentType {
	shareType = strings.TrimSpace(shareType)
	switch {
	case strings.Contains(shareType, "우선"):
		return models.InstrumentTypePreferredStock
	case strings.Contains(shareType, "보통"):
		return models.InstrumentTypeCommonStock
	default:
		return models.InstrumentTypeOtherEquity
	}
}

func dateOnlyUTC(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}

func retryableStatus(statusCode int) bool {
	return statusCode == http.StatusTooManyRequests || statusCode >= 500
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func readLimited(input io.Reader, limit int64) ([]byte, error) {
	content, err := io.ReadAll(io.LimitReader(input, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > limit {
		return nil, fmt.Errorf("content exceeds %d bytes", limit)
	}
	return content, nil
}

func safeTransportError(err error) string {
	if err == nil {
		return "unknown transport error"
	}
	var urlError *url.Error
	if errors.As(err, &urlError) && urlError.Err != nil {
		err = urlError.Err
	}
	return err.Error()
}

func requestError(
	operation string,
	kind provider.ErrorKind,
	message string,
	err error,
) error {
	return &provider.Error{
		Provider:  providerName,
		Operation: operation,
		Kind:      kind,
		Message:   message,
		Err:       err,
	}
}

func fixedUpperAlphanumeric(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') &&
			(character < 'A' || character > 'Z') {
			return false
		}
	}
	return true
}

type stockResponse struct {
	Rows []stockDTO `json:"OutBlock_1"`
}

type stockDTO struct {
	StandardCode    string `json:"ISU_CD"`
	ShortCode       string `json:"ISU_SRT_CD"`
	Name            string `json:"ISU_NM"`
	AbbreviatedName string `json:"ISU_ABBRV"`
	EnglishName     string `json:"ISU_ENG_NM"`
	ListingDate     string `json:"LIST_DD"`
	Market          string `json:"MKT_TP_NM"`
	SecurityGroup   string `json:"SECUGRP_NM"`
	Section         string `json:"SECT_TP_NM"`
	ShareType       string `json:"KIND_STKCERT_TP_NM"`
}

type productResponse struct {
	Rows []productDTO `json:"OutBlock_1"`
}

type productDTO struct {
	BaseDate  string `json:"BAS_DD"`
	ShortCode string `json:"ISU_CD"`
	Name      string `json:"ISU_NM"`
}
