package models

import "time"

type DataStatus string

const (
	DataStatusAvailable    DataStatus = "available"
	DataStatusPartial      DataStatus = "partial"
	DataStatusEmpty        DataStatus = "empty"
	DataStatusUnavailable  DataStatus = "unavailable"
	DataStatusNotRequested DataStatus = "not_requested"
)

type SourceMetadata struct {
	Provider   string     `json:"provider"`
	SourceURL  string     `json:"source_url"`
	ObservedAt *time.Time `json:"observed_at,omitempty"`
	FetchedAt  time.Time  `json:"fetched_at"`
}

type WatchlistItem struct {
	Name         string `json:"name"`
	Ticker       string `json:"ticker"`
	YahooTicker  string `json:"yahoo_ticker"`
	DARTCorpCode string `json:"dart_corp_code,omitempty"`
	Market       string `json:"market"`
	Currency     string `json:"currency"`
}

type PriceBar struct {
	Timestamp time.Time `json:"timestamp"`
	Close     *float64  `json:"close,omitempty"`
	Volume    *int64    `json:"volume,omitempty"`
}

type PriceSnapshot struct {
	YahooTicker string         `json:"yahoo_ticker"`
	Currency    string         `json:"currency,omitempty"`
	Status      DataStatus     `json:"status"`
	LatestBar   *PriceBar      `json:"latest_bar,omitempty"`
	LastPrice   *float64       `json:"last_price,omitempty"`
	ChangePct1D *float64       `json:"change_pct_1d,omitempty"`
	MA20        *float64       `json:"ma20,omitempty"`
	MA60        *float64       `json:"ma60,omitempty"`
	Volume      *int64         `json:"volume,omitempty"`
	Source      SourceMetadata `json:"source"`
	Warnings    []string       `json:"warnings,omitempty"`
}

type TradeJournalEntry struct {
	TradeDate             time.Time `json:"trade_date"`
	Ticker                string    `json:"ticker"`
	Action                string    `json:"action"`
	Price                 float64   `json:"price"`
	Quantity              float64   `json:"quantity"`
	Thesis                string    `json:"thesis"`
	InvalidationCondition string    `json:"invalidation_condition"`
	ExpectedHoldingPeriod string    `json:"expected_holding_period"`
}

type Instrument struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	Ticker       string    `json:"ticker"`
	YahooTicker  string    `json:"yahoo_ticker"`
	DARTCorpCode string    `json:"dart_corp_code,omitempty"`
	Market       string    `json:"market"`
	Currency     string    `json:"currency"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Trade struct {
	ID             int64     `json:"id"`
	InstrumentID   int64     `json:"instrument_id"`
	ExternalID     string    `json:"external_id,omitempty"`
	TradeDate      time.Time `json:"trade_date"`
	Action         string    `json:"action"`
	QuantityUnits  int64     `json:"quantity_units"`
	PriceUnits     int64     `json:"price_units"`
	FeesUnits      int64     `json:"fees_units"`
	TaxesUnits     int64     `json:"taxes_units"`
	PriceSource    string    `json:"price_source"`
	TaxesKnown     bool      `json:"taxes_known"`
	TimePrecision  string    `json:"time_precision"`
	Currency       string    `json:"currency"`
	Source         string    `json:"source"`
	IdempotencyKey string    `json:"idempotency_key"`
	CreatedAt      time.Time `json:"created_at"`
}

type Position struct {
	InstrumentID     int64     `json:"instrument_id"`
	QuantityUnits    int64     `json:"quantity_units"`
	AverageCostUnits int64     `json:"average_cost_units"`
	Currency         string    `json:"currency"`
	AsOf             time.Time `json:"as_of"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type Thesis struct {
	InstrumentID          int64     `json:"instrument_id"`
	Summary               string    `json:"summary"`
	InvalidationCondition string    `json:"invalidation_condition"`
	ExpectedHoldingPeriod string    `json:"expected_holding_period"`
	CheckMetrics          []string  `json:"check_metrics"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

type PortfolioRecord struct {
	Instrument Instrument `json:"instrument"`
	Position   *Position  `json:"position,omitempty"`
	Trades     []Trade    `json:"trades"`
	Thesis     *Thesis    `json:"thesis,omitempty"`
}
