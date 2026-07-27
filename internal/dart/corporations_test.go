package dart

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/models"
	"github.com/rowlet9g/stock-research-bot/internal/provider"
)

func TestCorporationCodesParsesArchiveAndSourceMetadata(t *testing.T) {
	fixture, err := os.ReadFile("testdata/corp_code.xml")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	archive := corporationArchive(t, fixture)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/corpCode.xml" {
			t.Errorf("unexpected request path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("crtfc_key"); got != "test-secret" {
			t.Errorf("unexpected API key: %q", got)
		}
		w.Header().Set("Content-Type", "application/x-msdownload")
		_, _ = w.Write(archive)
	}))
	defer server.Close()

	fetchedAt := time.Date(2026, 7, 27, 9, 0, 0, 0, time.UTC)
	client := &Client{
		apiKey:     "test-secret",
		baseURL:    server.URL,
		httpClient: server.Client(),
		now:        func() time.Time { return fetchedAt },
	}
	result, err := client.CorporationCodes(context.Background())
	if err != nil {
		t.Fatalf("fetch corporation codes: %v", err)
	}

	if requests != 1 {
		t.Fatalf("expected one request, got %d", requests)
	}
	if result.Status != models.DataStatusAvailable {
		t.Fatalf("expected available status, got %q", result.Status)
	}
	if len(result.Corporations) != 3 {
		t.Fatalf("expected three corporations, got %d", len(result.Corporations))
	}
	if result.Corporations[0].CorpCode != "00126380" ||
		result.Corporations[0].StockCode != "005930" {
		t.Fatalf("unexpected listed corporation: %#v", result.Corporations[0])
	}
	if result.Corporations[1].StockCode != "0068Y0" {
		t.Fatalf("expected alphanumeric stock code: %#v", result.Corporations[1])
	}
	if result.Corporations[2].StockCode != "" {
		t.Fatalf("expected unlisted corporation: %#v", result.Corporations[2])
	}
	if strings.Contains(result.Source.SourceURL, "test-secret") {
		t.Fatalf("source URL leaks API key: %s", result.Source.SourceURL)
	}
	if !result.Source.FetchedAt.Equal(fetchedAt) {
		t.Fatalf("unexpected fetched time: %s", result.Source.FetchedAt)
	}
	wantObservedAt := time.Date(2026, 4, 6, 0, 0, 0, 0, time.UTC)
	if result.Source.ObservedAt == nil || !result.Source.ObservedAt.Equal(wantObservedAt) {
		t.Fatalf("expected observed time %s, got %v", wantObservedAt, result.Source.ObservedAt)
	}
}

func TestCorporationCodesClassifiesOpenDARTErrorWithoutLeakingKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(
			`<?xml version="1.0" encoding="UTF-8"?><result><status>010</status><message>등록되지 않은 키입니다.</message></result>`,
		))
	}))
	defer server.Close()

	client := &Client{
		apiKey:     "test-secret",
		baseURL:    server.URL,
		httpClient: server.Client(),
		now:        time.Now,
	}
	_, err := client.CorporationCodes(context.Background())
	if err == nil {
		t.Fatal("expected OpenDART error")
	}
	if got := provider.KindOf(err); got != provider.ErrorKindInvalidRequest {
		t.Fatalf("expected invalid request, got %q", got)
	}
	if strings.Contains(err.Error(), "test-secret") {
		t.Fatalf("error leaks API key: %v", err)
	}
}

func TestCorporationCodesRetriesTransientHTTPFailure(t *testing.T) {
	fixture, err := os.ReadFile("testdata/corp_code.xml")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	archive := corporationArchive(t, fixture)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			http.Error(w, "temporary failure", http.StatusBadGateway)
			return
		}
		_, _ = w.Write(archive)
	}))
	defer server.Close()

	client := &Client{
		apiKey:      "test-secret",
		baseURL:     server.URL,
		httpClient:  server.Client(),
		now:         time.Now,
		maxAttempts: 2,
	}
	if _, err := client.CorporationCodes(context.Background()); err != nil {
		t.Fatalf("fetch corporation codes after retry: %v", err)
	}
	if requests != 2 {
		t.Fatalf("expected two requests, got %d", requests)
	}
}

func TestCorporationCodesRejectsMalformedCorporation(t *testing.T) {
	archive := corporationArchive(t, []byte(`
<result>
  <list>
    <corp_code>invalid</corp_code>
    <corp_name>Bad Corp</corp_name>
    <corp_eng_name>Bad Corp</corp_eng_name>
    <stock_code>005930</stock_code>
    <modify_date>20260727</modify_date>
  </list>
</result>`))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(archive)
	}))
	defer server.Close()

	client := &Client{
		apiKey:     "test-secret",
		baseURL:    server.URL,
		httpClient: server.Client(),
		now:        time.Now,
	}
	_, err := client.CorporationCodes(context.Background())
	if err == nil {
		t.Fatal("expected malformed corporation error")
	}
	if got := provider.KindOf(err); got != provider.ErrorKindBadResponse {
		t.Fatalf("expected bad response, got %q", got)
	}
}

func TestOpenDARTHTTPClientIncludesRSAKeyExchangeFallback(t *testing.T) {
	client := newOpenDARTHTTPClient()
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("unexpected transport type %T", client.Transport)
	}
	if transport.TLSClientConfig == nil {
		t.Fatal("expected OpenDART TLS configuration")
	}
	found := false
	for _, suite := range transport.TLSClientConfig.CipherSuites {
		if suite == tls.TLS_RSA_WITH_AES_128_GCM_SHA256 {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected RSA AES-GCM fallback cipher suite")
	}
}

func corporationArchive(t *testing.T, content []byte) []byte {
	t.Helper()
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	file, err := writer.Create("CORPCODE.xml")
	if err != nil {
		t.Fatalf("create fixture ZIP entry: %v", err)
	}
	if _, err := file.Write(content); err != nil {
		t.Fatalf("write fixture ZIP entry: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close fixture ZIP: %v", err)
	}
	return output.Bytes()
}
