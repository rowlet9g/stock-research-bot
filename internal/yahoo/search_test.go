package yahoo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestResolveInstrumentFiltersByCurrencyAndUsesProviderRanking(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("q"); got != "삼성전자보통주" {
			t.Errorf("unexpected search query: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"quotes": [
				{
					"exchange": "KSC",
					"shortname": "SamsungElec",
					"longname": "Samsung Electronics Co., Ltd.",
					"quoteType": "EQUITY",
					"symbol": "005930.KS",
					"exchDisp": "KSE"
				},
				{
					"exchange": "NMS",
					"shortname": "Samsung Electronics ADR",
					"quoteType": "EQUITY",
					"symbol": "SSNLF",
					"exchDisp": "NASDAQ"
				}
			]
		}`))
	}))
	defer server.Close()

	client := &Client{
		baseURL:    server.URL,
		searchURL:  server.URL,
		httpClient: server.Client(),
		now:        time.Now,
	}
	resolution, err := client.ResolveInstrument(
		context.Background(),
		"삼성전자보통주",
		"KRW",
	)
	if err != nil {
		t.Fatalf("resolve instrument: %v", err)
	}
	if resolution.Instrument.Ticker != "005930" {
		t.Fatalf("unexpected ticker: %q", resolution.Instrument.Ticker)
	}
	if resolution.Instrument.YahooTicker != "005930.KS" {
		t.Fatalf("unexpected Yahoo ticker: %q", resolution.Instrument.YahooTicker)
	}
	if resolution.Instrument.Market != "KOSPI" {
		t.Fatalf("unexpected market: %q", resolution.Instrument.Market)
	}
	if resolution.MatchKind != "provider_rank" {
		t.Fatalf("unexpected match kind: %q", resolution.MatchKind)
	}
}

func TestResolveInstrumentPrefersExactNameMatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"quotes": [
				{
					"exchange": "NMS",
					"shortname": "Invesco QQQ Trust",
					"quoteType": "ETF",
					"symbol": "QQQ",
					"exchDisp": "NASDAQ"
				},
				{
					"exchange": "NMS",
					"shortname": "Invesco NASDAQ 100 ETF",
					"quoteType": "ETF",
					"symbol": "QQQM",
					"exchDisp": "NASDAQ"
				}
			]
		}`))
	}))
	defer server.Close()

	client := &Client{
		searchURL:  server.URL,
		httpClient: server.Client(),
		now:        time.Now,
	}
	resolution, err := client.ResolveInstrument(
		context.Background(),
		"INVESCO NASDAQ 100",
		"USD",
	)
	if err != nil {
		t.Fatalf("resolve instrument: %v", err)
	}
	if resolution.Instrument.Ticker != "QQQM" {
		t.Fatalf("expected QQQM, got %q", resolution.Instrument.Ticker)
	}
	if resolution.MatchKind != "partial_name" {
		t.Fatalf("unexpected match kind: %q", resolution.MatchKind)
	}
}

func TestResolveInstrumentRejectsMissingMarketCandidate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"quotes": [
				{
					"exchange": "NMS",
					"shortname": "Example",
					"quoteType": "EQUITY",
					"symbol": "EXAMPLE"
				}
			]
		}`))
	}))
	defer server.Close()

	client := &Client{
		searchURL:  server.URL,
		httpClient: server.Client(),
		now:        time.Now,
	}
	if _, err := client.ResolveInstrument(context.Background(), "예시", "KRW"); err == nil {
		t.Fatal("expected KRW resolution error")
	}
}
