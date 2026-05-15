from __future__ import annotations

from dataclasses import dataclass

from stock_research_bot.models import PriceSnapshot


@dataclass(frozen=True)
class Signal:
    level: str
    title: str
    detail: str


def evaluate_price_snapshot(snapshot: PriceSnapshot) -> list[Signal]:
    signals: list[Signal] = []

    if snapshot.last_price is None:
        return [
            Signal(
                level="error",
                title="가격 데이터 없음",
                detail=f"{snapshot.yahoo_ticker}의 Yahoo Finance 가격 데이터를 가져오지 못했습니다.",
            )
        ]

    if snapshot.change_pct_1d is not None and abs(snapshot.change_pct_1d) >= 5:
        direction = "상승" if snapshot.change_pct_1d > 0 else "하락"
        signals.append(
            Signal(
                level="warning",
                title=f"1일 {direction} 폭 확대",
                detail=f"전일 대비 {snapshot.change_pct_1d:.2f}% 변동했습니다.",
            )
        )

    if snapshot.ma20 and snapshot.ma60:
        if snapshot.last_price < snapshot.ma20 < snapshot.ma60:
            signals.append(
                Signal(
                    level="warning",
                    title="단기 약세 구조",
                    detail="현재가가 20일선과 60일선 아래에 있습니다.",
                )
            )
        elif snapshot.last_price > snapshot.ma20 > snapshot.ma60:
            signals.append(
                Signal(
                    level="info",
                    title="상승 추세 후보",
                    detail="현재가가 20일선과 60일선 위에 있습니다.",
                )
            )

    return signals
