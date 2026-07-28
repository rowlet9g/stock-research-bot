package yahoo

import (
	"context"
	"math"
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
	if snapshot.MetricVersion != PriceMetricVersion {
		t.Fatalf("unexpected price metric version: %q", snapshot.MetricVersion)
	}
}

func TestBuildPriceSnapshotCalculatesReturnsRiskAndVolume(t *testing.T) {
	bars := make([]models.PriceBar, 0, 61)
	for index := 0; index < 61; index++ {
		closeValue := 100 + float64(index)
		volume := int64(1000 + index)
		bars = append(bars, models.PriceBar{
			Timestamp: time.Date(
				2026,
				1,
				index+1,
				0,
				0,
				0,
				0,
				time.UTC,
			),
			Close:  &closeValue,
			Volume: &volume,
		})
	}
	snapshot := buildPriceSnapshot("TEST", priceHistory{
		Bars:     bars,
		Currency: "USD",
		Source: models.SourceMetadata{
			Provider:  providerName,
			SourceURL: "https://example.test/TEST",
			FetchedAt: time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC),
		},
	})
	assertFloatNear(t, snapshot.ReturnPct20D, 14.2857142857, 0.000001)
	assertFloatNear(t, snapshot.ReturnPct60D, 60, 0.000001)
	assertFloatNear(
		t,
		snapshot.PreviousAverageVolume20D,
		1049.5,
		0.000001,
	)
	assertFloatNear(
		t,
		snapshot.VolumeRatio20D,
		float64(1060)/1049.5,
		0.000001,
	)
	assertFloatNear(t, snapshot.MaxDrawdownPct6M, 0, 0.000001)
	if snapshot.AnnualizedVolatilityPct20D == nil ||
		*snapshot.AnnualizedVolatilityPct20D <= 0 ||
		snapshot.AnnualizedVolatilityPct60D == nil ||
		*snapshot.AnnualizedVolatilityPct60D <= 0 {
		t.Fatalf("volatility was not calculated: %#v", snapshot)
	}
}

func TestPriceRiskMetricsCalculatePeakToTroughDrawdown(t *testing.T) {
	values := []float64{100, 120, 90, 95, 130, 104}
	assertFloatNear(t, maxDrawdownPct(values), -25, 0.000001)
}

func TestBuildPriceBarsRejectsNonPositiveAndInfiniteCloses(t *testing.T) {
	zero := 0.0
	negative := -1.0
	infinite := math.Inf(1)
	volume := int64(1)
	bars, warnings := buildPriceBars(
		[]int64{1, 2, 3},
		[]*float64{&zero, &negative, &infinite},
		[]*int64{&volume, &volume, &volume},
	)
	if len(warnings) != 1 {
		t.Fatalf("expected invalid close warning, got %#v", warnings)
	}
	for _, bar := range bars {
		if bar.Close != nil {
			t.Fatalf("invalid close was accepted: %#v", bars)
		}
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

func assertFloatNear(
	t *testing.T,
	actual *float64,
	expected float64,
	tolerance float64,
) {
	t.Helper()
	if actual == nil || math.Abs(*actual-expected) > tolerance {
		t.Fatalf(
			"expected %.10f +/- %.10f, got %v",
			expected,
			tolerance,
			actual,
		)
	}
}
