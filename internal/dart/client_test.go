package dart

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/models"
	"github.com/rowlet9g/stock-research-bot/internal/provider"
)

func TestRecentDisclosuresReturnsSourceMetadata(t *testing.T) {
	fixture, err := os.ReadFile("testdata/list.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/list.json" {
			t.Errorf("unexpected request path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("crtfc_key"); got != "test-secret" {
			t.Errorf("unexpected API key: %q", got)
		}
		if got := r.URL.Query().Get("corp_code"); got != "00126380" {
			t.Errorf("unexpected corporation code: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture)
	}))
	defer server.Close()

	fetchedAt := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	client := &Client{
		apiKey:     "test-secret",
		baseURL:    server.URL,
		httpClient: server.Client(),
		now:        func() time.Time { return fetchedAt },
	}
	result, err := client.RecentDisclosures(context.Background(), "00126380", 30, 20)
	if err != nil {
		t.Fatalf("fetch disclosures: %v", err)
	}

	if result.Status != models.DataStatusAvailable {
		t.Fatalf("expected available status, got %q", result.Status)
	}
	if len(result.Disclosures) != 2 {
		t.Fatalf("expected 2 disclosures, got %d", len(result.Disclosures))
	}
	if strings.Contains(result.Source.SourceURL, "test-secret") {
		t.Fatalf("source URL leaks API key: %s", result.Source.SourceURL)
	}
	if !result.Source.FetchedAt.Equal(fetchedAt) {
		t.Fatalf("expected fetched_at %s, got %s", fetchedAt, result.Source.FetchedAt)
	}
	wantObservedAt := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	if result.Source.ObservedAt == nil || !result.Source.ObservedAt.Equal(wantObservedAt) {
		t.Fatalf("expected observed_at %s, got %v", wantObservedAt, result.Source.ObservedAt)
	}
}

func TestRecentDisclosuresReturnsEmptyStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"013","message":"조회된 데이터가 없습니다."}`))
	}))
	defer server.Close()

	client := &Client{
		apiKey:     "test-secret",
		baseURL:    server.URL,
		httpClient: server.Client(),
		now:        time.Now,
	}
	result, err := client.RecentDisclosures(context.Background(), "00126380", 30, 20)
	if err != nil {
		t.Fatalf("fetch disclosures: %v", err)
	}
	if result.Status != models.DataStatusEmpty {
		t.Fatalf("expected empty status, got %q", result.Status)
	}
}

func TestRecentDisclosuresDoesNotExposeAPIKeyOnFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "failed", http.StatusBadGateway)
	}))
	defer server.Close()

	client := &Client{
		apiKey:     "test-secret",
		baseURL:    server.URL,
		httpClient: server.Client(),
		now:        time.Now,
	}
	_, err := client.RecentDisclosures(context.Background(), "00126380", 30, 20)
	if err == nil {
		t.Fatal("expected provider error")
	}
	if strings.Contains(err.Error(), "test-secret") {
		t.Fatalf("error leaks API key: %v", err)
	}
	if got := provider.KindOf(err); got != provider.ErrorKindUnavailable {
		t.Fatalf("expected unavailable error, got %q", got)
	}
}
