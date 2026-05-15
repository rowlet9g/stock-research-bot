from __future__ import annotations

from typing import Iterable

import pandas as pd
import yfinance as yf

from stock_research_bot.models import PriceSnapshot


def fetch_price_history(yahoo_ticker: str, period: str = "6mo") -> pd.DataFrame:
    frame = yf.download(
        yahoo_ticker,
        period=period,
        progress=False,
        auto_adjust=False,
        threads=False,
    )
    if frame.empty:
        return frame
    frame = frame.reset_index()
    frame.columns = [str(column).lower().replace(" ", "_") for column in frame.columns]
    return frame


def build_price_snapshot(yahoo_ticker: str, period: str = "6mo") -> PriceSnapshot:
    history = fetch_price_history(yahoo_ticker, period=period)
    if history.empty or "close" not in history:
        return PriceSnapshot(yahoo_ticker, None, None, None, None, None)

    close = history["close"].dropna()
    if close.empty:
        return PriceSnapshot(yahoo_ticker, None, None, None, None, None)

    last_price = float(close.iloc[-1])
    previous_price = float(close.iloc[-2]) if len(close) >= 2 else None
    change_pct_1d = (
        ((last_price / previous_price) - 1.0) * 100.0
        if previous_price and previous_price != 0
        else None
    )

    volume = None
    if "volume" in history and not history["volume"].dropna().empty:
        volume = int(history["volume"].dropna().iloc[-1])

    return PriceSnapshot(
        yahoo_ticker=yahoo_ticker,
        last_price=last_price,
        change_pct_1d=change_pct_1d,
        ma20=float(close.tail(20).mean()) if len(close) >= 20 else None,
        ma60=float(close.tail(60).mean()) if len(close) >= 60 else None,
        volume=volume,
    )


def build_price_snapshots(yahoo_tickers: Iterable[str]) -> list[PriceSnapshot]:
    return [build_price_snapshot(ticker) for ticker in yahoo_tickers if ticker]
