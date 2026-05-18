package analysis

import (
	"testing"

	"github.com/rowlet9g/stock-research-bot/internal/models"
)

func TestMissingPriceReturnsErrorSignal(t *testing.T) {
	snapshot := models.PriceSnapshot{YahooTicker: "005930.KS"}

	signals := EvaluatePriceSnapshot(snapshot)

	if len(signals) != 1 {
		t.Fatalf("expected 1 signal, got %d", len(signals))
	}
	if signals[0].Level != "error" {
		t.Fatalf("expected error signal, got %q", signals[0].Level)
	}
}

func TestLargeDailyMoveReturnsWarning(t *testing.T) {
	lastPrice := 100.0
	changePct := -6.0
	ma20 := 101.0
	ma60 := 102.0
	volume := int64(1000)
	snapshot := models.PriceSnapshot{
		YahooTicker: "AAPL",
		LastPrice:   &lastPrice,
		ChangePct1D: &changePct,
		MA20:        &ma20,
		MA60:        &ma60,
		Volume:      &volume,
	}

	signals := EvaluatePriceSnapshot(snapshot)

	for _, signal := range signals {
		if signal.Level == "warning" {
			return
		}
	}
	t.Fatalf("expected at least one warning signal, got %#v", signals)
}
