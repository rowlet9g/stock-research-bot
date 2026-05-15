from stock_research_bot.analysis.rules import evaluate_price_snapshot
from stock_research_bot.models import PriceSnapshot


def test_missing_price_returns_error_signal() -> None:
    snapshot = PriceSnapshot("005930.KS", None, None, None, None, None)

    signals = evaluate_price_snapshot(snapshot)

    assert len(signals) == 1
    assert signals[0].level == "error"


def test_large_daily_move_returns_warning() -> None:
    snapshot = PriceSnapshot("AAPL", 100.0, -6.0, 101.0, 102.0, 1000)

    signals = evaluate_price_snapshot(snapshot)

    assert any(signal.level == "warning" for signal in signals)
