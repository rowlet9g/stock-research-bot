from __future__ import annotations

from dataclasses import dataclass
from datetime import date


@dataclass(frozen=True)
class WatchlistItem:
    name: str
    ticker: str
    yahoo_ticker: str
    dart_corp_code: str | None
    market: str
    currency: str


@dataclass(frozen=True)
class PriceSnapshot:
    yahoo_ticker: str
    last_price: float | None
    change_pct_1d: float | None
    ma20: float | None
    ma60: float | None
    volume: int | None


@dataclass(frozen=True)
class TradeJournalEntry:
    trade_date: date
    ticker: str
    action: str
    price: float
    quantity: float
    thesis: str
    invalidation_condition: str
    expected_holding_period: str
