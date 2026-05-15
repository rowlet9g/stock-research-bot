from __future__ import annotations

from stock_research_bot.analysis.rules import Signal
from stock_research_bot.data.dart import Disclosure
from stock_research_bot.models import PriceSnapshot


SYSTEM_PROMPT = """You are an investment research assistant for education.
Do not provide guaranteed returns or definitive buy/sell orders.
Separate facts, interpretation, risks, and questions.
When data is insufficient, say so directly.
Write in Korean."""


def build_stock_brief_prompt(
    name: str,
    snapshot: PriceSnapshot,
    signals: list[Signal],
    disclosures: list[Disclosure],
    user_thesis: str | None = None,
) -> str:
    signal_lines = "\n".join(
        f"- [{signal.level}] {signal.title}: {signal.detail}" for signal in signals
    )
    disclosure_lines = "\n".join(
        f"- {item.receipt_date} {item.corp_name}: {item.report_name} ({item.receipt_no})"
        for item in disclosures[:10]
    )

    return f"""다음 종목에 대한 투자 공부용 브리핑을 작성하라.

종목명: {name}
Yahoo ticker: {snapshot.yahoo_ticker}
현재가: {snapshot.last_price}
1일 변동률(%): {snapshot.change_pct_1d}
20일 이동평균: {snapshot.ma20}
60일 이동평균: {snapshot.ma60}
거래량: {snapshot.volume}

가격 신호:
{signal_lines or "- 특이 신호 없음"}

최근 DART 공시:
{disclosure_lines or "- 제공된 공시 없음"}

사용자 투자 가설:
{user_thesis or "아직 기록되지 않음"}

출력 형식:
1. 확인된 사실
2. 가능한 해석
3. 주요 리스크
4. 사용자가 추가로 확인해야 할 질문
5. 관심종목 유지/주의/재검토 중 하나로 분류
"""
