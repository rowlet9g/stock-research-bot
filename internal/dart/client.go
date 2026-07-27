package dart

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/models"
	"github.com/rowlet9g/stock-research-bot/internal/provider"
)

const (
	defaultBaseURL             = "https://opendart.fss.or.kr/api"
	providerName               = "opendart"
	maxDisclosureResponseBytes = 8 << 20
	maxDisclosurePages         = 1000
)

type Disclosure = models.DARTDisclosure

type DisclosureResult struct {
	Status       models.DataStatus     `json:"status"`
	Disclosures  []Disclosure          `json:"disclosures"`
	TotalCount   int                   `json:"total_count"`
	PagesFetched int                   `json:"pages_fetched"`
	BeginDate    string                `json:"begin_date"`
	EndDate      string                `json:"end_date"`
	Source       models.SourceMetadata `json:"source"`
	Warnings     []string              `json:"warnings,omitempty"`
}

type Client struct {
	apiKey      string
	baseURL     string
	httpClient  *http.Client
	now         func() time.Time
	maxAttempts int
	retryDelay  time.Duration
}

func NewClient(apiKey string) *Client {
	return &Client{
		apiKey:      strings.TrimSpace(apiKey),
		baseURL:     defaultBaseURL,
		httpClient:  newOpenDARTHTTPClient(),
		now:         time.Now,
		maxAttempts: 3,
		retryDelay:  200 * time.Millisecond,
	}
}

func newOpenDARTHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{
		MinVersion: tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
			// OpenDART currently requires an RSA key-exchange fallback with Go clients.
			tls.TLS_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
		},
	}
	return &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func NotRequestedResult() DisclosureResult {
	return DisclosureResult{
		Status: models.DataStatusNotRequested,
		Source: models.SourceMetadata{
			Provider:  providerName,
			SourceURL: defaultBaseURL + "/list.json",
			FetchedAt: time.Now().UTC(),
		},
		Disclosures: []Disclosure{},
	}
}

func (c *Client) RecentDisclosures(ctx context.Context, corpCode string, days int, pageCount int) (DisclosureResult, error) {
	if days <= 0 {
		return DisclosureResult{}, invalidRequestError("days must be greater than zero")
	}
	endDate := c.now().UTC()
	beginDate := endDate.AddDate(0, 0, -days)
	return c.searchDisclosures(
		ctx,
		corpCode,
		beginDate,
		endDate,
		pageCount,
		1,
	)
}

func (c *Client) DisclosureHistory(
	ctx context.Context,
	corpCode string,
	beginDate time.Time,
	endDate time.Time,
	pageCount int,
) (DisclosureResult, error) {
	return c.searchDisclosures(
		ctx,
		corpCode,
		beginDate,
		endDate,
		pageCount,
		maxDisclosurePages,
	)
}

func invalidRequestError(message string) error {
	return &provider.Error{
		Provider:  providerName,
		Operation: "fetch_disclosures",
		Kind:      provider.ErrorKindInvalidRequest,
		Message:   message,
	}
}

func (c *Client) searchDisclosures(
	ctx context.Context,
	corpCode string,
	beginDate time.Time,
	endDate time.Time,
	pageCount int,
	pageLimit int,
) (DisclosureResult, error) {
	corpCode = strings.TrimSpace(corpCode)
	beginDate = dateOnlyUTC(beginDate)
	endDate = dateOnlyUTC(endDate)
	sourceURL := strings.TrimRight(c.baseURL, "/") + "/list.json"
	safeParams := url.Values{}
	safeParams.Set("bgn_de", beginDate.Format("20060102"))
	safeParams.Set("corp_code", corpCode)
	safeParams.Set("end_de", endDate.Format("20060102"))
	safeParams.Set("page_count", strconv.Itoa(pageCount))
	safeParams.Set("sort", "date")
	safeParams.Set("sort_mth", "desc")
	safeSourceURL := sourceURL + "?" + safeParams.Encode()
	result := DisclosureResult{
		Status: models.DataStatusUnavailable,
		Source: models.SourceMetadata{
			Provider:  providerName,
			SourceURL: safeSourceURL,
			FetchedAt: c.now().UTC(),
		},
		Disclosures: []Disclosure{},
		BeginDate:   beginDate.Format("2006-01-02"),
		EndDate:     endDate.Format("2006-01-02"),
	}

	switch {
	case c.apiKey == "":
		return result, invalidRequestError("API key is required")
	case !isFixedDigits(corpCode, 8):
		return result, invalidRequestError("corporation code must be 8 digits")
	case beginDate.IsZero() || endDate.IsZero():
		return result, invalidRequestError("begin and end dates are required")
	case beginDate.After(endDate):
		return result, invalidRequestError("begin date must not be after end date")
	case pageCount <= 0 || pageCount > 100:
		return result, invalidRequestError("page count must be between 1 and 100")
	case pageLimit <= 0:
		return result, invalidRequestError("page limit must be greater than zero")
	}

	seenReceiptNumbers := map[string]struct{}{}
	var latestReceiptDate time.Time
	for pageNumber := 1; pageNumber <= pageLimit; pageNumber++ {
		params := cloneValues(safeParams)
		params.Set("crtfc_key", c.apiKey)
		params.Set("page_no", strconv.Itoa(pageNumber))
		payload, err := c.fetchDisclosurePage(
			ctx,
			sourceURL+"?"+params.Encode(),
		)
		if err != nil {
			return result, err
		}
		if payload.Status == "013" {
			result.Status = models.DataStatusEmpty
			return result, nil
		}
		if payload.Status != "000" {
			return result, dartAPIError(
				"fetch_disclosures",
				payload.Status,
				payload.Message,
			)
		}
		if payload.PageNo != pageNumber ||
			payload.PageCount <= 0 ||
			payload.TotalPage <= 0 ||
			payload.TotalCount < 0 {
			return result, dartRequestError(
				"decode_disclosures",
				provider.ErrorKindBadResponse,
				fmt.Sprintf(
					"invalid OpenDART pagination on page %d",
					pageNumber,
				),
			)
		}
		if pageNumber == 1 {
			result.TotalCount = payload.TotalCount
			if payload.TotalPage > maxDisclosurePages {
				return result, dartRequestError(
					"fetch_disclosures",
					provider.ErrorKindBadResponse,
					fmt.Sprintf(
						"OpenDART disclosure result exceeds %d pages",
						maxDisclosurePages,
					),
				)
			}
		}
		result.PagesFetched++
		for rowIndex, item := range payload.List {
			disclosure, err := normalizeDisclosure(item, result.Source)
			if err != nil {
				return result, dartRequestError(
					"decode_disclosures",
					provider.ErrorKindBadResponse,
					fmt.Sprintf(
						"invalid disclosure on page %d row %d: %v",
						pageNumber,
						rowIndex+1,
						err,
					),
				)
			}
			if _, exists := seenReceiptNumbers[disclosure.ReceiptNo]; exists {
				return result, dartRequestError(
					"decode_disclosures",
					provider.ErrorKindBadResponse,
					fmt.Sprintf(
						"duplicate OpenDART receipt number %q",
						disclosure.ReceiptNo,
					),
				)
			}
			seenReceiptNumbers[disclosure.ReceiptNo] = struct{}{}
			if disclosure.ReceiptDate.After(latestReceiptDate) {
				latestReceiptDate = disclosure.ReceiptDate
			}
			result.Disclosures = append(result.Disclosures, disclosure)
		}
		if pageNumber >= payload.TotalPage {
			break
		}
	}

	if len(result.Disclosures) == 0 {
		result.Status = models.DataStatusEmpty
		return result, nil
	}
	result.Status = models.DataStatusAvailable
	if !latestReceiptDate.IsZero() {
		observedAt := latestReceiptDate.UTC()
		result.Source.ObservedAt = &observedAt
	}
	return result, nil
}

func (c *Client) fetchDisclosurePage(
	ctx context.Context,
	requestURL string,
) (listResponse, error) {
	attempts := c.maxAttempts
	if attempts <= 0 {
		attempts = 1
	}

	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
		if err != nil {
			return listResponse{}, &provider.Error{
				Provider:  providerName,
				Operation: "build_disclosure_request",
				Kind:      provider.ErrorKindInvalidRequest,
				Message:   "could not build disclosure request",
				Err:       err,
			}
		}
		request.Header.Set("User-Agent", "forgetmenot-research-bot/0.1")
		response, err := c.httpClient.Do(request)
		if err != nil {
			lastErr = err
		} else if !retryableHTTPStatus(response.StatusCode) {
			payload, decodeErr := decodeDisclosureResponse(response)
			if decodeErr != nil {
				return listResponse{}, decodeErr
			}
			return payload, nil
		}

		if attempt == attempts {
			if response != nil {
				payload, decodeErr := decodeDisclosureResponse(response)
				if decodeErr != nil {
					return listResponse{}, decodeErr
				}
				return payload, nil
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
	return listResponse{}, &provider.Error{
		Provider:  providerName,
		Operation: "fetch_disclosures",
		Kind:      provider.ErrorKindUnavailable,
		Message:   "request failed: " + safeTransportError(lastErr),
		Err:       lastErr,
	}
}

func decodeDisclosureResponse(response *http.Response) (listResponse, error) {
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		kind := provider.ErrorKindBadResponse
		if response.StatusCode == http.StatusTooManyRequests ||
			response.StatusCode >= 500 {
			kind = provider.ErrorKindUnavailable
		}
		return listResponse{}, &provider.Error{
			Provider:   providerName,
			Operation:  "fetch_disclosures",
			Kind:       kind,
			StatusCode: response.StatusCode,
			Message:    "unexpected HTTP status",
		}
	}
	content, err := readLimited(response.Body, maxDisclosureResponseBytes)
	if err != nil {
		return listResponse{}, &provider.Error{
			Provider:  providerName,
			Operation: "read_disclosures",
			Kind:      provider.ErrorKindBadResponse,
			Message:   "disclosure response exceeds the size limit or could not be read",
			Err:       err,
		}
	}
	var payload listResponse
	if err := json.Unmarshal(content, &payload); err != nil {
		return listResponse{}, &provider.Error{
			Provider:  providerName,
			Operation: "decode_disclosures",
			Kind:      provider.ErrorKindBadResponse,
			Message:   "invalid JSON response",
			Err:       err,
		}
	}
	return payload, nil
}

func normalizeDisclosure(
	item disclosureDTO,
	source models.SourceMetadata,
) (models.DARTDisclosure, error) {
	corpClass := strings.ToUpper(strings.TrimSpace(item.CorpClass))
	corpCode := strings.TrimSpace(item.CorpCode)
	stockCode := strings.ToUpper(strings.TrimSpace(item.StockCode))
	reportName := strings.TrimSpace(item.ReportName)
	receiptNo := strings.TrimSpace(item.ReceiptNo)
	receiptDate, err := time.Parse("20060102", strings.TrimSpace(item.ReceiptDate))
	switch {
	case corpClass != "Y" && corpClass != "K" &&
		corpClass != "N" && corpClass != "E":
		return models.DARTDisclosure{}, fmt.Errorf(
			"invalid corporation class %q",
			corpClass,
		)
	case !isFixedDigits(corpCode, 8):
		return models.DARTDisclosure{}, fmt.Errorf(
			"invalid corporation code %q",
			corpCode,
		)
	case strings.TrimSpace(item.CorpName) == "":
		return models.DARTDisclosure{}, fmt.Errorf("corporation name is empty")
	case stockCode != "" && !isFixedUpperAlphanumeric(stockCode, 6):
		return models.DARTDisclosure{}, fmt.Errorf(
			"invalid stock code %q",
			stockCode,
		)
	case reportName == "":
		return models.DARTDisclosure{}, fmt.Errorf("report name is empty")
	case !isFixedDigits(receiptNo, 14):
		return models.DARTDisclosure{}, fmt.Errorf(
			"invalid receipt number %q",
			receiptNo,
		)
	case err != nil:
		return models.DARTDisclosure{}, fmt.Errorf(
			"invalid receipt date %q",
			item.ReceiptDate,
		)
	case strings.TrimSpace(item.Submitter) == "":
		return models.DARTDisclosure{}, fmt.Errorf("submitter is empty")
	}

	receiptDate = receiptDate.UTC()
	rowSource := source
	rowSource.ObservedAt = &receiptDate
	return models.DARTDisclosure{
		CorpClass:   corpClass,
		CorpCode:    corpCode,
		CorpName:    strings.TrimSpace(item.CorpName),
		StockCode:   stockCode,
		ReportName:  reportName,
		ReceiptNo:   receiptNo,
		ReceiptDate: receiptDate,
		Submitter:   strings.TrimSpace(item.Submitter),
		Remark:      strings.TrimSpace(item.Remark),
		ViewerURL: "https://dart.fss.or.kr/dsaf001/main.do?rcpNo=" +
			receiptNo,
		Source: rowSource,
	}, nil
}

func cloneValues(input url.Values) url.Values {
	output := make(url.Values, len(input))
	for key, values := range input {
		output[key] = append([]string(nil), values...)
	}
	return output
}

func dateOnlyUTC(value time.Time) time.Time {
	if value.IsZero() {
		return time.Time{}
	}
	return time.Date(
		value.Year(),
		value.Month(),
		value.Day(),
		0,
		0,
		0,
		0,
		time.UTC,
	)
}

type listResponse struct {
	Status     string          `json:"status"`
	Message    string          `json:"message"`
	PageNo     int             `json:"page_no"`
	PageCount  int             `json:"page_count"`
	TotalCount int             `json:"total_count"`
	TotalPage  int             `json:"total_page"`
	List       []disclosureDTO `json:"list"`
}

type disclosureDTO struct {
	CorpClass   string `json:"corp_cls"`
	CorpCode    string `json:"corp_code"`
	CorpName    string `json:"corp_name"`
	StockCode   string `json:"stock_code"`
	ReportName  string `json:"report_nm"`
	ReceiptNo   string `json:"rcept_no"`
	ReceiptDate string `json:"rcept_dt"`
	Submitter   string `json:"flr_nm"`
	Remark      string `json:"rm"`
}
