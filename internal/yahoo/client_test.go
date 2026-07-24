package yahoo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/models"
	"github.com/rowlet9g/stock-research-bot/internal/provider"
)

func TestBuildPriceSnapshotKeepsCloseAndVolumeOnSameTimestamp(t *testing.T) {
	fixture, err := os.ReadFile("testdata/chart_partial.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v8/finance/chart/AAPL" {
			t.Errorf("unexpected request path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("range"); got != "6mo" {
			t.Errorf("unexpected range: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture)
	}))
	defer server.Close()

	fetchedAt := time.Date(2026, 7, 24, 10, 30, 0, 0, time.UTC)
	client := &Client{
		baseURL:    server.URL + "/v8/finance/chart",
		httpClient: server.Client(),
		now:        func() time.Time { return fetchedAt },
	}

	snapshot, err := client.BuildPriceSnapshot(context.Background(), "AAPL")
	if err != nil {
		t.Fatalf("build snapshot: %v", err)
	}

	if snapshot.Status != models.DataStatusPartial {
		t.Fatalf("expected partial status, got %q", snapshot.Status)
	}
	if snapshot.LastPrice == nil || *snapshot.LastPrice != 101.0 {
		t.Fatalf("expected last valid close 101, got %v", snapshot.LastPrice)
	}
	if snapshot.Volume == nil || *snapshot.Volume != 2000 {
		t.Fatalf("expected volume 2000 from the same bar, got %v", snapshot.Volume)
	}
	wantObservedAt := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	if snapshot.LatestBar == nil || !snapshot.LatestBar.Timestamp.Equal(wantObservedAt) {
		t.Fatalf("expected latest valid bar at %s, got %#v", wantObservedAt, snapshot.LatestBar)
	}
	if snapshot.Source.ObservedAt == nil || !snapshot.Source.ObservedAt.Equal(wantObservedAt) {
		t.Fatalf("expected observed_at %s, got %v", wantObservedAt, snapshot.Source.ObservedAt)
	}
	if !snapshot.Source.FetchedAt.Equal(fetchedAt) {
		t.Fatalf("expected fetched_at %s, got %s", fetchedAt, snapshot.Source.FetchedAt)
	}
	if snapshot.Currency != "USD" {
		t.Fatalf("expected USD, got %q", snapshot.Currency)
	}
}

func TestBuildPriceSnapshotReturnsEmptyStatusForNoResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"chart":{"result":[],"error":null}}`))
	}))
	defer server.Close()

	client := &Client{
		baseURL:    server.URL,
		httpClient: server.Client(),
		now:        time.Now,
	}
	snapshot, err := client.BuildPriceSnapshot(context.Background(), "EMPTY")
	if err != nil {
		t.Fatalf("build empty snapshot: %v", err)
	}
	if snapshot.Status != models.DataStatusEmpty {
		t.Fatalf("expected empty status, got %q", snapshot.Status)
	}
}

func TestBuildPriceSnapshotClassifiesServerFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := &Client{
		baseURL:    server.URL,
		httpClient: server.Client(),
		now:        time.Now,
	}
	_, err := client.BuildPriceSnapshot(context.Background(), "AAPL")
	if err == nil {
		t.Fatal("expected provider error")
	}
	if got := provider.KindOf(err); got != provider.ErrorKindUnavailable {
		t.Fatalf("expected unavailable error, got %q", got)
	}
}

func TestBuildPriceSnapshotClassifiesCancelledContext(t *testing.T) {
	contextWithCancel, cancel := context.WithCancel(context.Background())
	cancel()

	client := &Client{
		baseURL: "http://127.0.0.1",
		httpClient: &http.Client{
			Timeout: time.Second,
		},
		now: time.Now,
	}
	snapshot, err := client.BuildPriceSnapshot(contextWithCancel, "AAPL")
	if err == nil {
		t.Fatal("expected provider error")
	}
	if got := provider.KindOf(err); got != provider.ErrorKindUnavailable {
		t.Fatalf("expected unavailable error, got %q", got)
	}
	if snapshot.Status != models.DataStatusUnavailable {
		t.Fatalf("expected unavailable snapshot, got %q", snapshot.Status)
	}
}
