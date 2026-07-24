package analysis

import (
	"fmt"
	"math"

	"github.com/rowlet9g/stock-research-bot/internal/models"
)

type Signal struct {
	Level  string `json:"level"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

func EvaluatePriceSnapshot(snapshot models.PriceSnapshot) []Signal {
	if snapshot.LastPrice == nil {
		return []Signal{
			{
				Level:  "error",
				Title:  "가격 데이터 없음",
				Detail: fmt.Sprintf("%s의 Yahoo Finance 가격 데이터를 가져오지 못했습니다.", snapshot.YahooTicker),
			},
		}
	}

	var signals []Signal

	if snapshot.ChangePct1D != nil && math.Abs(*snapshot.ChangePct1D) >= 5 {
		direction := "상승"
		if *snapshot.ChangePct1D < 0 {
			direction = "하락"
		}
		signals = append(signals, Signal{
			Level:  "warning",
			Title:  fmt.Sprintf("1일 %s 폭 확대", direction),
			Detail: fmt.Sprintf("전일 대비 %.2f%% 변동했습니다.", *snapshot.ChangePct1D),
		})
	}

	if snapshot.MA20 != nil && snapshot.MA60 != nil {
		last := *snapshot.LastPrice
		ma20 := *snapshot.MA20
		ma60 := *snapshot.MA60

		if last < ma20 && ma20 < ma60 {
			signals = append(signals, Signal{
				Level:  "warning",
				Title:  "단기 약세 구조",
				Detail: "현재가가 20일선과 60일선 아래에 있습니다.",
			})
		} else if last > ma20 && ma20 > ma60 {
			signals = append(signals, Signal{
				Level:  "info",
				Title:  "상승 추세 후보",
				Detail: "현재가가 20일선과 60일선 위에 있습니다.",
			})
		}
	}

	return signals
}
