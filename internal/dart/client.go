package dart

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

const baseURL = "https://opendart.fss.or.kr/api"

type Disclosure struct {
	CorpCode    string
	CorpName    string
	ReportName  string
	ReceiptNo   string
	ReceiptDate string
	Submitter   string
}

type Client struct {
	apiKey     string
	httpClient *http.Client
}

func NewClient(apiKey string) *Client {
	return &Client{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (c *Client) RecentDisclosures(ctx context.Context, corpCode string, days int, pageCount int) ([]Disclosure, error) {
	endDate := time.Now()
	beginDate := endDate.AddDate(0, 0, -days)

	params := url.Values{}
	params.Set("crtfc_key", c.apiKey)
	params.Set("bgn_de", beginDate.Format("20060102"))
	params.Set("end_de", endDate.Format("20060102"))
	params.Set("page_count", fmt.Sprintf("%d", pageCount))
	if corpCode != "" {
		params.Set("corp_code", corpCode)
	}

	requestURL := fmt.Sprintf("%s/list.json?%s", baseURL, params.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("OpenDART HTTP status: %s", resp.Status)
	}

	var payload listResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}

	if payload.Status != "000" && payload.Status != "013" {
		return nil, fmt.Errorf("OpenDART error %s: %s", payload.Status, payload.Message)
	}

	disclosures := make([]Disclosure, 0, len(payload.List))
	for _, item := range payload.List {
		disclosures = append(disclosures, Disclosure{
			CorpCode:    item.CorpCode,
			CorpName:    item.CorpName,
			ReportName:  item.ReportName,
			ReceiptNo:   item.ReceiptNo,
			ReceiptDate: item.ReceiptDate,
			Submitter:   item.Submitter,
		})
	}
	return disclosures, nil
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
