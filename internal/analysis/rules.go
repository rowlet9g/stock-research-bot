package analysis

import (
	"fmt"
	"math"
	"strconv"

	"github.com/rowlet9g/stock-research-bot/internal/models"
)

const PriceSignalRuleVersion = "price-signals/v2"

type PriceSignalConfig struct {
	DailyMoveWarningPct       float64 `json:"daily_move_warning_pct"`
	Return20DWarningMagnitude float64 `json:"return_20d_warning_magnitude"`
	Volatility20DWarningPct   float64 `json:"volatility_20d_warning_pct"`
	MaxDrawdownWarningPct     float64 `json:"max_drawdown_warning_pct"`
	VolumeRatioWarning        float64 `json:"volume_ratio_warning"`
}

func DefaultPriceSignalConfig() PriceSignalConfig {
	return PriceSignalConfig{
		DailyMoveWarningPct:       5,
		Return20DWarningMagnitude: 15,
		Volatility20DWarningPct:   40,
		MaxDrawdownWarningPct:     20,
		VolumeRatioWarning:        2,
	}
}

type SignalEvidence struct {
	Metric     string `json:"metric"`
	Value      string `json:"value"`
	Unit       string `json:"unit,omitempty"`
	Comparison string `json:"comparison,omitempty"`
	Threshold  string `json:"threshold,omitempty"`
	Formula    string `json:"formula,omitempty"`
}

type Signal struct {
	RuleID   string           `json:"rule_id"`
	Level    string           `json:"level"`
	Title    string           `json:"title"`
	Detail   string           `json:"detail"`
	Evidence []SignalEvidence `json:"evidence"`
}

func EvaluatePriceSnapshot(snapshot models.PriceSnapshot) []Signal {
	signals, err := EvaluatePriceSnapshotWithConfig(
		snapshot,
		DefaultPriceSignalConfig(),
	)
	if err != nil {
		return []Signal{
			{
				RuleID:   "price.config_invalid",
				Level:    "error",
				Title:    "가격 규칙 설정 오류",
				Detail:   err.Error(),
				Evidence: []SignalEvidence{},
			},
		}
	}
	return signals
}

func EvaluatePriceSnapshotWithConfig(
	snapshot models.PriceSnapshot,
	config PriceSignalConfig,
) ([]Signal, error) {
	if err := validatePriceSignalConfig(config); err != nil {
		return nil, err
	}
	if snapshot.LastPrice == nil {
		return []Signal{
			{
				RuleID:   "price.data_missing",
				Level:    "error",
				Title:    "가격 데이터 없음",
				Detail:   fmt.Sprintf("%s의 Yahoo Finance 가격 데이터를 가져오지 못했습니다.", snapshot.YahooTicker),
				Evidence: []SignalEvidence{},
			},
		}, nil
	}

	signals := []Signal{}

	if snapshot.ChangePct1D != nil &&
		math.Abs(*snapshot.ChangePct1D) >= config.DailyMoveWarningPct {
		direction := "상승"
		if *snapshot.ChangePct1D < 0 {
			direction = "하락"
		}
		signals = append(signals, Signal{
			RuleID: "price.daily_move",
			Level:  "warning",
			Title:  fmt.Sprintf("1일 %s 폭 확대", direction),
			Detail: fmt.Sprintf("전일 대비 %.2f%% 변동했습니다.", *snapshot.ChangePct1D),
			Evidence: []SignalEvidence{
				priceMetricEvidence(
					"change_pct_1d",
					*snapshot.ChangePct1D,
					"%",
					"absolute_greater_than_or_equal",
					config.DailyMoveWarningPct,
					"((latest_close / previous_close) - 1) * 100",
				),
			},
		})
	}

	if snapshot.ReturnPct20D != nil {
		switch {
		case *snapshot.ReturnPct20D <= -config.Return20DWarningMagnitude:
			signals = append(signals, Signal{
				RuleID: "price.return_20d_down",
				Level:  "warning",
				Title:  "20일 하락 모멘텀",
				Detail: fmt.Sprintf(
					"20거래일 전 종가 대비 %.2f%% 변동했습니다.",
					*snapshot.ReturnPct20D,
				),
				Evidence: []SignalEvidence{
					priceMetricEvidence(
						"return_pct_20d",
						*snapshot.ReturnPct20D,
						"%",
						"less_than_or_equal",
						-config.Return20DWarningMagnitude,
						"((latest_close / close_20_sessions_ago) - 1) * 100",
					),
				},
			})
		case *snapshot.ReturnPct20D >= config.Return20DWarningMagnitude:
			signals = append(signals, Signal{
				RuleID: "price.return_20d_up",
				Level:  "info",
				Title:  "20일 상승 모멘텀 후보",
				Detail: fmt.Sprintf(
					"20거래일 전 종가 대비 %.2f%% 변동했습니다.",
					*snapshot.ReturnPct20D,
				),
				Evidence: []SignalEvidence{
					priceMetricEvidence(
						"return_pct_20d",
						*snapshot.ReturnPct20D,
						"%",
						"greater_than_or_equal",
						config.Return20DWarningMagnitude,
						"((latest_close / close_20_sessions_ago) - 1) * 100",
					),
				},
			})
		}
	}

	if snapshot.AnnualizedVolatilityPct20D != nil &&
		*snapshot.AnnualizedVolatilityPct20D >=
			config.Volatility20DWarningPct {
		signals = append(signals, Signal{
			RuleID: "price.volatility_20d_high",
			Level:  "warning",
			Title:  "20일 변동성 확대",
			Detail: fmt.Sprintf(
				"20거래일 로그수익률의 연율화 변동성이 %.2f%%입니다.",
				*snapshot.AnnualizedVolatilityPct20D,
			),
			Evidence: []SignalEvidence{
				priceMetricEvidence(
					"annualized_volatility_pct_20d",
					*snapshot.AnnualizedVolatilityPct20D,
					"%",
					"greater_than_or_equal",
					config.Volatility20DWarningPct,
					"sample_stddev(log_daily_returns_20d) * sqrt(252) * 100",
				),
			},
		})
	}

	if snapshot.MaxDrawdownPct6M != nil &&
		*snapshot.MaxDrawdownPct6M <= -config.MaxDrawdownWarningPct {
		signals = append(signals, Signal{
			RuleID: "price.drawdown_6m_high",
			Level:  "warning",
			Title:  "6개월 최대 낙폭 확대",
			Detail: fmt.Sprintf(
				"6개월 관측 구간의 최대 낙폭이 %.2f%%입니다.",
				*snapshot.MaxDrawdownPct6M,
			),
			Evidence: []SignalEvidence{
				priceMetricEvidence(
					"max_drawdown_pct_6m",
					*snapshot.MaxDrawdownPct6M,
					"%",
					"less_than_or_equal",
					-config.MaxDrawdownWarningPct,
					"min((close / running_peak_close) - 1) * 100",
				),
			},
		})
	}

	if snapshot.VolumeRatio20D != nil &&
		*snapshot.VolumeRatio20D >= config.VolumeRatioWarning {
		signals = append(signals, Signal{
			RuleID: "price.volume_ratio_20d_high",
			Level:  "warning",
			Title:  "거래량 급증",
			Detail: fmt.Sprintf(
				"최근 거래량이 직전 20거래일 평균의 %.2f배입니다.",
				*snapshot.VolumeRatio20D,
			),
			Evidence: []SignalEvidence{
				priceMetricEvidence(
					"volume_ratio_20d",
					*snapshot.VolumeRatio20D,
					"x",
					"greater_than_or_equal",
					config.VolumeRatioWarning,
					"latest_volume / average(previous_20_session_volumes)",
				),
			},
		})
	}

	if snapshot.MA20 != nil && snapshot.MA60 != nil {
		last := *snapshot.LastPrice
		ma20 := *snapshot.MA20
		ma60 := *snapshot.MA60

		if last < ma20 && ma20 < ma60 {
			signals = append(signals, Signal{
				RuleID:   "price.trend_bearish",
				Level:    "warning",
				Title:    "단기 약세 구조",
				Detail:   "현재가가 20일선과 60일선 아래에 있습니다.",
				Evidence: movingAverageEvidence(last, ma20, ma60),
			})
		} else if last > ma20 && ma20 > ma60 {
			signals = append(signals, Signal{
				RuleID:   "price.trend_bullish",
				Level:    "info",
				Title:    "상승 추세 후보",
				Detail:   "현재가가 20일선과 60일선 위에 있습니다.",
				Evidence: movingAverageEvidence(last, ma20, ma60),
			})
		}
	}

	return signals, nil
}

func validatePriceSignalConfig(config PriceSignalConfig) error {
	values := map[string]float64{
		"daily move warning":          config.DailyMoveWarningPct,
		"20-day return warning":       config.Return20DWarningMagnitude,
		"20-day volatility warning":   config.Volatility20DWarningPct,
		"maximum drawdown warning":    config.MaxDrawdownWarningPct,
		"20-day volume ratio warning": config.VolumeRatioWarning,
	}
	for label, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 {
			return fmt.Errorf("%s threshold must be finite and greater than zero", label)
		}
	}
	return nil
}

func priceMetricEvidence(
	metric string,
	value float64,
	unit string,
	comparison string,
	threshold float64,
	formula string,
) SignalEvidence {
	return SignalEvidence{
		Metric:     metric,
		Value:      formatPriceMetric(value),
		Unit:       unit,
		Comparison: comparison,
		Threshold:  formatPriceMetric(threshold),
		Formula:    formula,
	}
}

func movingAverageEvidence(
	last float64,
	ma20 float64,
	ma60 float64,
) []SignalEvidence {
	return []SignalEvidence{
		{
			Metric:  "last_price",
			Value:   formatPriceMetric(last),
			Formula: "latest valid close",
		},
		{
			Metric:  "ma20",
			Value:   formatPriceMetric(ma20),
			Formula: "average(latest 20 valid closes)",
		},
		{
			Metric:  "ma60",
			Value:   formatPriceMetric(ma60),
			Formula: "average(latest 60 valid closes)",
		},
	}
}

func formatPriceMetric(value float64) string {
	return strconv.FormatFloat(value, 'f', 4, 64)
}
