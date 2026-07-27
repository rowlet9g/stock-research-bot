package krx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/models"
	"github.com/rowlet9g/stock-research-bot/internal/provider"
)

func TestClientNormalizesOfficialInstrumentDatasets(t *testing.T) {
	stockFixture := readFixture(t, "testdata/stock_base_info.json")
	etfFixture := readFixture(t, "testdata/etf_bydd_trd.json")
	etnFixture := readFixture(t, "testdata/etn_bydd_trd.json")
	var requestCount int32

	server := httptest.NewServer(http.HandlerFunc(func(
		response http.ResponseWriter,
		request *http.Request,
	) {
		atomic.AddInt32(&requestCount, 1)
		if request.Header.Get("AUTH_KEY") != "test-secret" {
			t.Errorf("unexpected AUTH_KEY header: %q", request.Header.Get("AUTH_KEY"))
		}
		if request.URL.Query().Get("basDd") != "20200414" {
			t.Errorf("unexpected basDd query: %q", request.URL.RawQuery)
		}
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/sto/stk_isu_base_info":
			_, _ = response.Write(stockFixture)
		case "/etp/etf_bydd_trd":
			_, _ = response.Write(etfFixture)
		case "/etp/etn_bydd_trd":
			_, _ = response.Write(etnFixture)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	client := testClient(server, "test-secret")
	asOf := time.Date(2020, 4, 14, 9, 0, 0, 0, time.FixedZone("KST", 9*60*60))

	stockResult, err := client.Instruments(context.Background(), DatasetKOSPI, asOf)
	if err != nil {
		t.Fatalf("fetch KOSPI instruments: %v", err)
	}
	if stockResult.Status != models.DataStatusAvailable ||
		len(stockResult.Instruments) != 3 {
		t.Fatalf("unexpected KOSPI result: %#v", stockResult)
	}
	if stockResult.Instruments[0].InstrumentType != models.InstrumentTypeCommonStock {
		t.Fatalf("expected common stock: %#v", stockResult.Instruments[0])
	}
	if stockResult.Instruments[1].InstrumentType != models.InstrumentTypePreferredStock {
		t.Fatalf("expected preferred stock: %#v", stockResult.Instruments[1])
	}
	if stockResult.Instruments[2].StandardCode != "KYG5307W1015" {
		t.Fatalf("expected alphanumeric standard code: %#v", stockResult.Instruments[2])
	}
	if strings.Contains(stockResult.Source.SourceURL, "test-secret") {
		t.Fatalf("source URL leaked API key: %q", stockResult.Source.SourceURL)
	}
	if stockResult.Source.ObservedAt == nil ||
		stockResult.Source.ObservedAt.Format("2006-01-02") != "2020-04-14" {
		t.Fatalf("unexpected observed date: %#v", stockResult.Source.ObservedAt)
	}

	etfResult, err := client.Instruments(context.Background(), DatasetETF, asOf)
	if err != nil {
		t.Fatalf("fetch ETF instruments: %v", err)
	}
	if len(etfResult.Instruments) != 1 ||
		etfResult.Instruments[0].InstrumentType != models.InstrumentTypeETF ||
		etfResult.Instruments[0].StandardCode != "" {
		t.Fatalf("unexpected ETF result: %#v", etfResult)
	}

	etnResult, err := client.Instruments(context.Background(), DatasetETN, asOf)
	if err != nil {
		t.Fatalf("fetch ETN instruments: %v", err)
	}
	if len(etnResult.Instruments) != 1 ||
		etnResult.Instruments[0].InstrumentType != models.InstrumentTypeETN {
		t.Fatalf("unexpected ETN result: %#v", etnResult)
	}
	if got := atomic.LoadInt32(&requestCount); got != 3 {
		t.Fatalf("expected 3 requests, got %d", got)
	}
}

func TestClientClassifiesAuthorizationFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		response http.ResponseWriter,
		_ *http.Request,
	) {
		http.Error(response, "unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()

	client := testClient(server, "invalid-secret")
	_, err := client.Instruments(
		context.Background(),
		DatasetKOSPI,
		time.Date(2020, 4, 14, 0, 0, 0, 0, time.UTC),
	)
	if provider.KindOf(err) != provider.ErrorKindInvalidRequest {
		t.Fatalf("expected invalid request error, got %v", err)
	}
}

func TestClientRetriesTemporaryServerFailure(t *testing.T) {
	fixture := readFixture(t, "testdata/stock_base_info.json")
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(
		response http.ResponseWriter,
		_ *http.Request,
	) {
		if atomic.AddInt32(&attempts, 1) == 1 {
			http.Error(response, "temporary", http.StatusBadGateway)
			return
		}
		_, _ = response.Write(fixture)
	}))
	defer server.Close()

	client := testClient(server, "test-secret")
	client.maxAttempts = 2
	client.retryDelay = 0
	result, err := client.Instruments(
		context.Background(),
		DatasetKOSPI,
		time.Date(2020, 4, 14, 0, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("fetch after retry: %v", err)
	}
	if result.Status != models.DataStatusAvailable ||
		atomic.LoadInt32(&attempts) != 2 {
		t.Fatalf("unexpected retry result: result=%#v attempts=%d", result, attempts)
	}
}

func TestClientRejectsMalformedDataset(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		response http.ResponseWriter,
		_ *http.Request,
	) {
		_, _ = response.Write([]byte(`{
			"OutBlock_1": [{
				"ISU_CD": "KR7000010001",
				"ISU_SRT_CD": "TOO-LONG",
				"ISU_NM": "잘못된 종목",
				"LIST_DD": "20200414",
				"MKT_TP_NM": "KOSPI",
				"KIND_STKCERT_TP_NM": "보통주"
			}]
		}`))
	}))
	defer server.Close()

	client := testClient(server, "test-secret")
	_, err := client.Instruments(
		context.Background(),
		DatasetKOSPI,
		time.Date(2020, 4, 14, 0, 0, 0, 0, time.UTC),
	)
	if provider.KindOf(err) != provider.ErrorKindBadResponse {
		t.Fatalf("expected bad response error, got %v", err)
	}
}

func testClient(server *httptest.Server, apiKey string) *Client {
	client := NewClient(apiKey)
	client.baseURL = server.URL
	client.httpClient = server.Client()
	client.maxAttempts = 1
	client.now = func() time.Time {
		return time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	}
	return client
}

func readFixture(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %q: %v", path, err)
	}
	return content
}
