package analysis

import (
	"strings"
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

func TestPriceRiskMetricsReturnEvidenceBackedWarnings(t *testing.T) {
	lastPrice := 80.0
	return20D := -20.0
	volatility20D := 55.0
	drawdown6M := -30.0
	volumeRatio20D := 3.0
	snapshot := models.PriceSnapshot{
		YahooTicker:                "AAPL",
		LastPrice:                  &lastPrice,
		ReturnPct20D:               &return20D,
		AnnualizedVolatilityPct20D: &volatility20D,
		MaxDrawdownPct6M:           &drawdown6M,
		VolumeRatio20D:             &volumeRatio20D,
	}
	signals, err := EvaluatePriceSnapshotWithConfig(
		snapshot,
		DefaultPriceSignalConfig(),
	)
	if err != nil {
		t.Fatalf("evaluate price signals: %v", err)
	}
	for _, ruleID := range []string{
		"price.return_20d_down",
		"price.volatility_20d_high",
		"price.drawdown_6m_high",
		"price.volume_ratio_20d_high",
	} {
		signal := signalByRuleID(t, signals, ruleID)
		if signal.Level != "warning" ||
			len(signal.Evidence) != 1 ||
			signal.Evidence[0].Metric == "" ||
			signal.Evidence[0].Threshold == "" ||
			signal.Evidence[0].Formula == "" {
			t.Fatalf("signal lacks calculation evidence: %#v", signal)
		}
	}
}

func TestPriceSignalConfigChangesThresholds(t *testing.T) {
	lastPrice := 100.0
	changePct := 6.0
	snapshot := models.PriceSnapshot{
		YahooTicker: "AAPL",
		LastPrice:   &lastPrice,
		ChangePct1D: &changePct,
	}
	config := DefaultPriceSignalConfig()
	config.DailyMoveWarningPct = 10
	signals, err := EvaluatePriceSnapshotWithConfig(snapshot, config)
	if err != nil {
		t.Fatalf("evaluate price signals: %v", err)
	}
	for _, signal := range signals {
		if signal.RuleID == "price.daily_move" {
			t.Fatalf("custom threshold was ignored: %#v", signal)
		}
	}
}

func TestPriceSignalConfigRejectsInvalidThreshold(t *testing.T) {
	config := DefaultPriceSignalConfig()
	config.VolumeRatioWarning = 0
	_, err := EvaluatePriceSnapshotWithConfig(models.PriceSnapshot{}, config)
	if err == nil || !strings.Contains(err.Error(), "volume ratio") {
		t.Fatalf("unexpected threshold error: %v", err)
	}
}

func signalByRuleID(
	t *testing.T,
	signals []Signal,
	ruleID string,
) Signal {
	t.Helper()
	for _, signal := range signals {
		if signal.RuleID == ruleID {
			return signal
		}
	}
	t.Fatalf("signal %s not found: %#v", ruleID, signals)
	return Signal{}
}
