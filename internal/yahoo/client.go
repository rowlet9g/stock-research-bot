package yahoo

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/models"
)

func EmptyPriceSnapshot(yahooTicker string) models.PriceSnapshot {
	return models.PriceSnapshot{YahooTicker: yahooTicker}
}

func BuildPriceSnapshot(ctx context.Context, yahooTicker string) (models.PriceSnapshot, error) {
	history, err := fetchPriceHistory(ctx, yahooTicker)
	if err != nil {
		return EmptyPriceSnapshot(yahooTicker), err
	}
	if len(history.Close) == 0 {
		return EmptyPriceSnapshot(yahooTicker), nil
	}

	lastPrice := history.Close[len(history.Close)-1]
	var previousPrice *float64
	if len(history.Close) >= 2 {
		previousPrice = &history.Close[len(history.Close)-2]
	}

	var changePct *float64
	if previousPrice != nil && *previousPrice != 0 {
		value := ((lastPrice / *previousPrice) - 1.0) * 100.0
		changePct = &value
	}

	var volume *int64
	if len(history.Volume) > 0 {
		value := history.Volume[len(history.Volume)-1]
		volume = &value
	}

	return models.PriceSnapshot{
		YahooTicker: yahooTicker,
		LastPrice:   &lastPrice,
		ChangePct1D: changePct,
		MA20:        movingAverage(history.Close, 20),
		MA60:        movingAverage(history.Close, 60),
		Volume:      volume,
	}, nil
}

func fetchPriceHistory(ctx context.Context, yahooTicker string) (priceHistory, error) {
	escapedTicker := url.PathEscape(yahooTicker)
	requestURL := fmt.Sprintf("https://query1.finance.yahoo.com/v8/finance/chart/%s?range=6mo&interval=1d", escapedTicker)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return priceHistory{}, err
	}
	req.Header.Set("User-Agent", "forgetmenot-research-bot/0.1")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return priceHistory{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return priceHistory{}, fmt.Errorf("Yahoo Finance HTTP status: %s", resp.Status)
	}

	var payload chartResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return priceHistory{}, err
	}
	if payload.Chart.Error != nil {
		return priceHistory{}, fmt.Errorf("Yahoo Finance error: %s", payload.Chart.Error.Description)
	}
	if len(payload.Chart.Result) == 0 || len(payload.Chart.Result[0].Indicators.Quote) == 0 {
		return priceHistory{}, nil
	}

	quote := payload.Chart.Result[0].Indicators.Quote[0]
	history := priceHistory{}
	for _, value := range quote.Close {
		if value != nil && !math.IsNaN(*value) {
			history.Close = append(history.Close, *value)
		}
	}
	for _, value := range quote.Volume {
		if value != nil {
			history.Volume = append(history.Volume, *value)
		}
	}
	return history, nil
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

type priceHistory struct {
	Close  []float64
	Volume []int64
}

type chartResponse struct {
	Chart struct {
		Result []struct {
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
