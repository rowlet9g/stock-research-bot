package models

import "time"

type WatchlistItem struct {
	Name         string
	Ticker       string
	YahooTicker  string
	DARTCorpCode string
	Market       string
	Currency     string
}

type PriceSnapshot struct {
	YahooTicker string
	LastPrice   *float64
	ChangePct1D *float64
	MA20        *float64
	MA60        *float64
	Volume      *int64
}

type TradeJournalEntry struct {
	TradeDate             time.Time
	Ticker                string
	Action                string
	Price                 float64
	Quantity              float64
	Thesis                string
	InvalidationCondition string
	ExpectedHoldingPeriod string
}
