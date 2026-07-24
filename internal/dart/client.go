package dart

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/models"
	"github.com/rowlet9g/stock-research-bot/internal/provider"
)

const (
	defaultBaseURL = "https://opendart.fss.or.kr/api"
	providerName   = "opendart"
)

type Disclosure struct {
	CorpCode    string `json:"corp_code"`
	CorpName    string `json:"corp_name"`
	ReportName  string `json:"report_name"`
	ReceiptNo   string `json:"receipt_no"`
	ReceiptDate string `json:"receipt_date"`
	Submitter   string `json:"submitter"`
}

type DisclosureResult struct {
	Status      models.DataStatus     `json:"status"`
	Disclosures []Disclosure          `json:"disclosures"`
	Source      models.SourceMetadata `json:"source"`
	Warnings    []string              `json:"warnings,omitempty"`
}

type Client struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
	now        func() time.Time
}

func NewClient(apiKey string) *Client {
	return &Client{
		apiKey:  strings.TrimSpace(apiKey),
		baseURL: defaultBaseURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		now: time.Now,
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
	sourceURL := strings.TrimRight(c.baseURL, "/") + "/list.json"
	result := DisclosureResult{
		Status: models.DataStatusUnavailable,
		Source: models.SourceMetadata{
			Provider:  providerName,
			SourceURL: sourceURL,
			FetchedAt: c.now().UTC(),
		},
		Disclosures: []Disclosure{},
	}

	corpCode = strings.TrimSpace(corpCode)
	switch {
	case c.apiKey == "":
		return result, invalidRequestError("API key is required")
	case corpCode == "":
		return result, invalidRequestError("corporation code is required")
	case days <= 0:
		return result, invalidRequestError("days must be greater than zero")
	case pageCount <= 0 || pageCount > 100:
		return result, invalidRequestError("page count must be between 1 and 100")
	}

	endDate := c.now()
	beginDate := endDate.AddDate(0, 0, -days)

	params := url.Values{}
	params.Set("crtfc_key", c.apiKey)
	params.Set("bgn_de", beginDate.Format("20060102"))
	params.Set("end_de", endDate.Format("20060102"))
	params.Set("page_count", strconv.Itoa(pageCount))
	params.Set("corp_code", corpCode)

	requestURL := sourceURL + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return result, &provider.Error{
			Provider:  providerName,
			Operation: "build_disclosure_request",
			Kind:      provider.ErrorKindInvalidRequest,
			Message:   "could not build disclosure request",
			Err:       err,
		}
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return result, &provider.Error{
			Provider:  providerName,
			Operation: "fetch_disclosures",
			Kind:      provider.ErrorKindUnavailable,
			Message:   "request failed",
			Err:       err,
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		kind := provider.ErrorKindBadResponse
		if resp.StatusCode >= 500 {
			kind = provider.ErrorKindUnavailable
		}
		return result, &provider.Error{
			Provider:   providerName,
			Operation:  "fetch_disclosures",
			Kind:       kind,
			StatusCode: resp.StatusCode,
			Message:    "unexpected HTTP status",
		}
	}

	var payload listResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return result, &provider.Error{
			Provider:  providerName,
			Operation: "decode_disclosures",
			Kind:      provider.ErrorKindBadResponse,
			Message:   "invalid JSON response",
			Err:       err,
		}
	}

	if payload.Status == "013" {
		result.Status = models.DataStatusEmpty
		return result, nil
	}
	if payload.Status != "000" {
		return result, &provider.Error{
			Provider:  providerName,
			Operation: "fetch_disclosures",
			Kind:      provider.ErrorKindBadResponse,
			Message:   fmt.Sprintf("OpenDART error %s: %s", payload.Status, payload.Message),
		}
	}

	result.Status = models.DataStatusAvailable
	result.Disclosures = make([]Disclosure, 0, len(payload.List))
	var latestReceiptDate time.Time
	for i, item := range payload.List {
		if item.ReceiptNo == "" || item.ReceiptDate == "" || item.ReportName == "" {
			result.Status = models.DataStatusPartial
			result.Warnings = append(result.Warnings, fmt.Sprintf("disclosure %d is missing a required field", i+1))
		}
		if receiptDate, err := time.Parse("20060102", item.ReceiptDate); err == nil {
			if receiptDate.After(latestReceiptDate) {
				latestReceiptDate = receiptDate
			}
		} else if item.ReceiptDate != "" {
			result.Status = models.DataStatusPartial
			result.Warnings = append(result.Warnings, fmt.Sprintf("disclosure %d has an invalid receipt date", i+1))
		}
		result.Disclosures = append(result.Disclosures, Disclosure{
			CorpCode:    item.CorpCode,
			CorpName:    item.CorpName,
			ReportName:  item.ReportName,
			ReceiptNo:   item.ReceiptNo,
			ReceiptDate: item.ReceiptDate,
			Submitter:   item.Submitter,
		})
	}
	if len(result.Disclosures) == 0 {
		result.Status = models.DataStatusEmpty
		return result, nil
	}

	if !latestReceiptDate.IsZero() {
		observedAt := latestReceiptDate.UTC()
		result.Source.ObservedAt = &observedAt
	}
	return result, nil
}

func invalidRequestError(message string) error {
	return &provider.Error{
		Provider:  providerName,
		Operation: "fetch_disclosures",
		Kind:      provider.ErrorKindInvalidRequest,
		Message:   message,
	}
}

type listResponse struct {
	Status  string          `json:"status"`
	Message string          `json:"message"`
	List    []disclosureDTO `json:"list"`
}

type disclosureDTO struct {
	CorpCode    string `json:"corp_code"`
	CorpName    string `json:"corp_name"`
	ReportName  string `json:"report_nm"`
	ReceiptNo   string `json:"rcept_no"`
	ReceiptDate string `json:"rcept_dt"`
	Submitter   string `json:"flr_nm"`
}
