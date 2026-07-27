package dart

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/models"
	"github.com/rowlet9g/stock-research-bot/internal/provider"
)

func TestFullFinancialStatementReturnsNormalizedAccounts(t *testing.T) {
	fixture, err := os.ReadFile("testdata/financial_statement.json")
	if err != nil {
		t.Fatalf("read financial statement fixture: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(
		response http.ResponseWriter,
		request *http.Request,
	) {
		if request.URL.Path != "/fnlttSinglAcntAll.json" {
			t.Errorf("unexpected request path: %s", request.URL.Path)
		}
		query := request.URL.Query()
		if query.Get("crtfc_key") != "test-secret" ||
			query.Get("corp_code") != "00126380" ||
			query.Get("bsns_year") != "2025" ||
			query.Get("reprt_code") != DARTReportCodeAnnual ||
			query.Get("fs_div") != DARTFinancialStatementConsolidated {
			t.Errorf("unexpected request query: %s", request.URL.RawQuery)
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write(fixture)
	}))
	defer server.Close()

	fetchedAt := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	client := &Client{
		apiKey:      "test-secret",
		baseURL:     server.URL,
		httpClient:  server.Client(),
		now:         func() time.Time { return fetchedAt },
		maxAttempts: 1,
	}
	result, err := client.FullFinancialStatement(
		context.Background(),
		"00126380",
		2025,
		DARTReportCodeAnnual,
		DARTFinancialStatementConsolidated,
	)
	if err != nil {
		t.Fatalf("fetch full financial statement: %v", err)
	}
	statement := result.Statement
	if result.Status != models.DataStatusAvailable ||
		statement.ReceiptNo != "20260310000777" ||
		len(statement.Accounts) != 2 ||
		len(statement.ContentSHA256) != 64 {
		t.Fatalf("unexpected financial statement: %#v", result)
	}
	if statement.Accounts[0].CurrentAmount != "500000000000000" ||
		statement.Accounts[1].CurrentAmount != "-300000000000" ||
		statement.Accounts[1].CurrentAddAmount != "300000000000000" {
		t.Fatalf("amounts were not normalized: %#v", statement.Accounts)
	}
	if statement.Source.ObservedAt == nil ||
		statement.Source.ObservedAt.Format("2006-01-02") != "2026-03-10" {
		t.Fatalf("unexpected observed time: %#v", statement.Source)
	}
	if strings.Contains(statement.Source.SourceURL, "test-secret") {
		t.Fatalf("source URL leaked API key: %q", statement.Source.SourceURL)
	}
}

func TestFullFinancialStatementClassifiesNoData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		response http.ResponseWriter,
		_ *http.Request,
	) {
		_, _ = response.Write([]byte(
			`{"status":"013","message":"조회된 데이타가 없습니다.","list":[]}`,
		))
	}))
	defer server.Close()

	client := &Client{
		apiKey:      "test-secret",
		baseURL:     server.URL,
		httpClient:  server.Client(),
		now:         time.Now,
		maxAttempts: 1,
	}
	result, err := client.FullFinancialStatement(
		context.Background(),
		"00126380",
		2025,
		DARTReportCodeAnnual,
		DARTFinancialStatementConsolidated,
	)
	if err != nil {
		t.Fatalf("fetch empty financial statement: %v", err)
	}
	if result.Status != models.DataStatusEmpty ||
		len(result.Statement.Accounts) != 0 {
		t.Fatalf("unexpected empty financial statement: %#v", result)
	}
}

func TestFullFinancialStatementRejectsInvalidAmount(t *testing.T) {
	fixture, err := os.ReadFile("testdata/financial_statement.json")
	if err != nil {
		t.Fatalf("read financial statement fixture: %v", err)
	}
	var payload financialStatementResponse
	if err := json.Unmarshal(fixture, &payload); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	payload.List[0].CurrentAmount = "12,34"
	invalid, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encode invalid fixture: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(
		response http.ResponseWriter,
		_ *http.Request,
	) {
		_, _ = response.Write(invalid)
	}))
	defer server.Close()
	client := &Client{
		apiKey:      "test-secret",
		baseURL:     server.URL,
		httpClient:  server.Client(),
		now:         time.Now,
		maxAttempts: 1,
	}
	_, err = client.FullFinancialStatement(
		context.Background(),
		"00126380",
		2025,
		DARTReportCodeAnnual,
		DARTFinancialStatementConsolidated,
	)
	if provider.KindOf(err) != provider.ErrorKindBadResponse {
		t.Fatalf("expected bad response for invalid amount, got %v", err)
	}
}

func TestFinancialStatementContentHashIgnoresResponseOrder(t *testing.T) {
	fixture, err := os.ReadFile("testdata/financial_statement.json")
	if err != nil {
		t.Fatalf("read financial statement fixture: %v", err)
	}
	var payload financialStatementResponse
	if err := json.Unmarshal(fixture, &payload); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	source := models.SourceMetadata{
		Provider:  "opendart",
		SourceURL: "https://opendart.fss.or.kr/api/fnlttSinglAcntAll.json",
		FetchedAt: time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC),
	}
	first, err := normalizeFinancialStatement(
		payload.List,
		"00126380",
		2025,
		DARTReportCodeAnnual,
		DARTFinancialStatementConsolidated,
		source,
	)
	if err != nil {
		t.Fatalf("normalize first statement: %v", err)
	}
	payload.List[0], payload.List[1] = payload.List[1], payload.List[0]
	second, err := normalizeFinancialStatement(
		payload.List,
		"00126380",
		2025,
		DARTReportCodeAnnual,
		DARTFinancialStatementConsolidated,
		source,
	)
	if err != nil {
		t.Fatalf("normalize reordered statement: %v", err)
	}
	if first.ContentSHA256 != second.ContentSHA256 {
		t.Fatalf(
			"response order changed content hash: first=%s second=%s",
			first.ContentSHA256,
			second.ContentSHA256,
		)
	}
}

func TestNormalizeFinancialAmount(t *testing.T) {
	tests := map[string]string{
		"":          "",
		"-":         "",
		"0":         "0",
		"000":       "0",
		"1,234":     "1234",
		"-1,234":    "-1234",
		"(1,234)":   "-1234",
		"+1,234":    "1234",
		"(000,000)": "0",
	}
	for input, expected := range tests {
		actual, err := normalizeFinancialAmount(input)
		if err != nil {
			t.Fatalf("normalize amount %q: %v", input, err)
		}
		if actual != expected {
			t.Fatalf(
				"normalize amount %q: got %q, want %q",
				input,
				actual,
				expected,
			)
		}
	}
}
