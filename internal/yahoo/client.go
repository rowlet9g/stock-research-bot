package yahoo

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/models"
	"github.com/rowlet9g/stock-research-bot/internal/provider"
)

const (
	defaultBaseURL     = "https://query1.finance.yahoo.com/v8/finance/chart"
	providerName       = "yahoo_finance"
	PriceMetricVersion = "yahoo-price-metrics/v1"
)

type Client struct {
	baseURL    string
	searchURL  string
	httpClient *http.Client
	now        func() time.Time
}

func NewClient() *Client {
	return &Client{
		baseURL:   defaultBaseURL,
		searchURL: defaultSearchURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		now: time.Now,
	}
}

func EmptyPriceSnapshot(yahooTicker string) models.PriceSnapshot {
	return models.PriceSnapshot{
		YahooTicker:   yahooTicker,
		Status:        models.DataStatusUnavailable,
		MetricVersion: PriceMetricVersion,
		Source: models.SourceMetadata{
			Provider:  providerName,
			SourceURL: defaultBaseURL,
			FetchedAt: time.Now().UTC(),
		},
	}
}

func BuildPriceSnapshot(ctx context.Context, yahooTicker string) (models.PriceSnapshot, error) {
	return NewClient().BuildPriceSnapshot(ctx, yahooTicker)
}

func (c *Client) BuildPriceSnapshot(ctx context.Context, yahooTicker string) (models.PriceSnapshot, error) {
	yahooTicker = strings.TrimSpace(yahooTicker)
	if yahooTicker == "" {
		return EmptyPriceSnapshot(yahooTicker), &provider.Error{
			Provider:  providerName,
			Operation: "fetch_price_history",
			Kind:      provider.ErrorKindInvalidRequest,
			Message:   "ticker is required",
		}
	}

	history, err := c.fetchPriceHistory(ctx, yahooTicker)
	if err != nil {
		snapshot := EmptyPriceSnapshot(yahooTicker)
		snapshot.Source.FetchedAt = c.now().UTC()
		return snapshot, err
	}

	return buildPriceSnapshot(yahooTicker, history), nil
}

func (c *Client) fetchPriceHistory(ctx context.Context, yahooTicker string) (priceHistory, error) {
	escapedTicker := url.PathEscape(yahooTicker)
	sourceURL := fmt.Sprintf("%s/%s", strings.TrimRight(c.baseURL, "/"), escapedTicker)
	requestURL := sourceURL + "?range=6mo&interval=1d"
	fetchedAt := c.now().UTC()
	history := priceHistory{
		Source: models.SourceMetadata{
			Provider:  providerName,
			SourceURL: sourceURL,
			FetchedAt: fetchedAt,
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return history, &provider.Error{
			Provider:  providerName,
			Operation: "build_request",
			Kind:      provider.ErrorKindInvalidRequest,
			Message:   "could not build price request",
			Err:       err,
		}
	}
	req.Header.Set("User-Agent", "forgetmenot-research-bot/0.1")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return history, &provider.Error{
			Provider:  providerName,
			Operation: "fetch_price_history",
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
		return history, &provider.Error{
			Provider:   providerName,
			Operation:  "fetch_price_history",
			Kind:       kind,
			StatusCode: resp.StatusCode,
			Message:    "unexpected HTTP status",
		}
	}

	var payload chartResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return history, &provider.Error{
			Provider:  providerName,
			Operation: "decode_price_history",
			Kind:      provider.ErrorKindBadResponse,
			Message:   "invalid JSON response",
			Err:       err,
		}
	}
	if payload.Chart.Error != nil {
		return history, &provider.Error{
			Provider:  providerName,
			Operation: "fetch_price_history",
			Kind:      provider.ErrorKindBadResponse,
			Message:   payload.Chart.Error.Description,
		}
	}
	if len(payload.Chart.Result) == 0 || len(payload.Chart.Result[0].Indicators.Quote) == 0 {
		return history, nil
	}

	result := payload.Chart.Result[0]
	history.Currency = result.Meta.Currency
	history.Bars, history.Warnings = buildPriceBars(
		result.Timestamp,
		result.Indicators.Quote[0].Close,
		result.Indicators.Quote[0].Volume,
	)
	return history, nil
}

func buildPriceSnapshot(yahooTicker string, history priceHistory) models.PriceSnapshot {
	snapshot := models.PriceSnapshot{
		YahooTicker:   yahooTicker,
		Currency:      history.Currency,
		Status:        models.DataStatusAvailable,
		MetricVersion: PriceMetricVersion,
		Source:        history.Source,
		Warnings:      history.Warnings,
	}
	if len(history.Warnings) > 0 {
		snapshot.Status = models.DataStatusPartial
	}

	validBarIndexes := make([]int, 0, len(history.Bars))
	closes := make([]float64, 0, len(history.Bars))
	for i, bar := range history.Bars {
		if bar.Close == nil {
			continue
		}
		validBarIndexes = append(validBarIndexes, i)
		closes = append(closes, *bar.Close)
	}
	if len(validBarIndexes) == 0 {
		snapshot.Status = models.DataStatusEmpty
		return snapshot
	}

	latest := history.Bars[validBarIndexes[len(validBarIndexes)-1]]
	lastPrice := *latest.Close
	snapshot.LatestBar = &latest
	snapshot.LastPrice = &lastPrice
	snapshot.MA20 = movingAverage(closes, 20)
	snapshot.MA60 = movingAverage(closes, 60)
	snapshot.ReturnPct20D = periodReturnPct(closes, 20)
	snapshot.ReturnPct60D = periodReturnPct(closes, 60)
	snapshot.AnnualizedVolatilityPct20D = annualizedVolatilityPct(
		closes,
		20,
	)
	snapshot.AnnualizedVolatilityPct60D = annualizedVolatilityPct(
		closes,
		60,
	)
	snapshot.MaxDrawdownPct6M = maxDrawdownPct(closes)
	observedAt := latest.Timestamp
	snapshot.Source.ObservedAt = &observedAt

	if latest.Volume != nil {
		volume := *latest.Volume
		snapshot.Volume = &volume
		snapshot.PreviousAverageVolume20D = previousAverageVolume(
			history.Bars,
			validBarIndexes[len(validBarIndexes)-1],
			20,
		)
		if snapshot.PreviousAverageVolume20D != nil &&
			*snapshot.PreviousAverageVolume20D > 0 {
			ratio := float64(volume) / *snapshot.PreviousAverageVolume20D
			snapshot.VolumeRatio20D = &ratio
		}
	}
	if len(closes) >= 2 {
		previousPrice := closes[len(closes)-2]
		if previousPrice != 0 {
			changePct := ((lastPrice / previousPrice) - 1.0) * 100.0
			snapshot.ChangePct1D = &changePct
		}
	}

	return snapshot
}

func buildPriceBars(timestamps []int64, closes []*float64, volumes []*int64) ([]models.PriceBar, []string) {
	warnings := []string{}
	if len(timestamps) != len(closes) || len(timestamps) != len(volumes) {
		warnings = append(warnings, fmt.Sprintf(
			"timestamp/close/volume length mismatch: %d/%d/%d",
			len(timestamps),
			len(closes),
			len(volumes),
		))
	}

	bars := make([]models.PriceBar, 0, len(timestamps))
	hasMissingValue := false
	for i, timestamp := range timestamps {
		bar := models.PriceBar{Timestamp: time.Unix(timestamp, 0).UTC()}
		if i < len(closes) &&
			closes[i] != nil &&
			!math.IsNaN(*closes[i]) &&
			!math.IsInf(*closes[i], 0) &&
			*closes[i] > 0 {
			closeValue := *closes[i]
			bar.Close = &closeValue
		} else {
			hasMissingValue = true
		}
		if i < len(volumes) && volumes[i] != nil {
			volumeValue := *volumes[i]
			bar.Volume = &volumeValue
		} else {
			hasMissingValue = true
		}
		bars = append(bars, bar)
	}
	if hasMissingValue {
		warnings = append(warnings, "one or more price bars contain missing close or volume values")
	}
	return bars, warnings
}

func movingAverage(values []float64, window int) *float64 {
	if len(values) < window {
		return nil
	}
	start := len(values) - window
	sum := 0.0
	for _, value := range values[start:] {
		sum += value
	}
	average := sum / float64(window)
	return &average
}

func periodReturnPct(values []float64, periods int) *float64 {
	if periods <= 0 || len(values) < periods+1 {
		return nil
	}
	start := values[len(values)-periods-1]
	end := values[len(values)-1]
	if start <= 0 || end <= 0 {
		return nil
	}
	result := ((end / start) - 1) * 100
	return &result
}

func annualizedVolatilityPct(
	values []float64,
	periods int,
) *float64 {
	if periods < 2 || len(values) < periods+1 {
		return nil
	}
	window := values[len(values)-periods-1:]
	returns := make([]float64, 0, periods)
	for index := 1; index < len(window); index++ {
		if window[index-1] <= 0 || window[index] <= 0 {
			return nil
		}
		returns = append(
			returns,
			math.Log(window[index]/window[index-1]),
		)
	}
	mean := 0.0
	for _, value := range returns {
		mean += value
	}
	mean /= float64(len(returns))
	sumSquaredDeviation := 0.0
	for _, value := range returns {
		difference := value - mean
		sumSquaredDeviation += difference * difference
	}
	sampleVariance := sumSquaredDeviation / float64(len(returns)-1)
	result := math.Sqrt(sampleVariance) * math.Sqrt(252) * 100
	return &result
}

func maxDrawdownPct(values []float64) *float64 {
	if len(values) == 0 {
		return nil
	}
	peak := values[0]
	if peak <= 0 {
		return nil
	}
	worst := 0.0
	for _, value := range values[1:] {
		if value <= 0 {
			return nil
		}
		if value > peak {
			peak = value
			continue
		}
		drawdown := ((value / peak) - 1) * 100
		if drawdown < worst {
			worst = drawdown
		}
	}
	return &worst
}

func previousAverageVolume(
	bars []models.PriceBar,
	latestIndex int,
	window int,
) *float64 {
	if window <= 0 || latestIndex < window || latestIndex >= len(bars) {
		return nil
	}
	start := latestIndex - window
	total := float64(0)
	for _, bar := range bars[start:latestIndex] {
		if bar.Volume == nil || *bar.Volume < 0 {
			return nil
		}
		total += float64(*bar.Volume)
	}
	average := total / float64(window)
	return &average
}

type priceHistory struct {
	Bars     []models.PriceBar
	Currency string
	Source   models.SourceMetadata
	Warnings []string
}

type chartResponse struct {
	Chart struct {
		Result []struct {
			Meta struct {
				Currency string `json:"currency"`
			} `json:"meta"`
			Timestamp  []int64 `json:"timestamp"`
			Indicators struct {
				Quote []struct {
					Close  []*float64 `json:"close"`
					Volume []*int64   `json:"volume"`
				} `json:"quote"`
			} `json:"indicators"`
		} `json:"result"`
		Error *struct {
			Code        string `json:"code"`
			Description string `json:"description"`
		} `json:"error"`
	} `json:"chart"`
}
