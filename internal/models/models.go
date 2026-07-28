package models

import (
	"encoding/json"
	"time"
)

type DataStatus string

const (
	DataStatusAvailable    DataStatus = "available"
	DataStatusPartial      DataStatus = "partial"
	DataStatusEmpty        DataStatus = "empty"
	DataStatusUnavailable  DataStatus = "unavailable"
	DataStatusNotRequested DataStatus = "not_requested"
)

type InstrumentType string

const (
	InstrumentTypeUnknown        InstrumentType = "unknown"
	InstrumentTypeCommonStock    InstrumentType = "common_stock"
	InstrumentTypePreferredStock InstrumentType = "preferred_stock"
	InstrumentTypeETF            InstrumentType = "etf"
	InstrumentTypeETN            InstrumentType = "etn"
	InstrumentTypeOtherEquity    InstrumentType = "other_equity"
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
	YahooTicker                string         `json:"yahoo_ticker"`
	Currency                   string         `json:"currency,omitempty"`
	Status                     DataStatus     `json:"status"`
	MetricVersion              string         `json:"metric_version,omitempty"`
	LatestBar                  *PriceBar      `json:"latest_bar,omitempty"`
	LastPrice                  *float64       `json:"last_price,omitempty"`
	ChangePct1D                *float64       `json:"change_pct_1d,omitempty"`
	ReturnPct20D               *float64       `json:"return_pct_20d,omitempty"`
	ReturnPct60D               *float64       `json:"return_pct_60d,omitempty"`
	AnnualizedVolatilityPct20D *float64       `json:"annualized_volatility_pct_20d,omitempty"`
	AnnualizedVolatilityPct60D *float64       `json:"annualized_volatility_pct_60d,omitempty"`
	MaxDrawdownPct6M           *float64       `json:"max_drawdown_pct_6m,omitempty"`
	MA20                       *float64       `json:"ma20,omitempty"`
	MA60                       *float64       `json:"ma60,omitempty"`
	Volume                     *int64         `json:"volume,omitempty"`
	PreviousAverageVolume20D   *float64       `json:"previous_average_volume_20d,omitempty"`
	VolumeRatio20D             *float64       `json:"volume_ratio_20d,omitempty"`
	Source                     SourceMetadata `json:"source"`
	Warnings                   []string       `json:"warnings,omitempty"`
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
	ID              int64          `json:"id"`
	Name            string         `json:"name"`
	Ticker          string         `json:"ticker"`
	YahooTicker     string         `json:"yahoo_ticker"`
	DARTCorpCode    string         `json:"dart_corp_code,omitempty"`
	KRXStandardCode string         `json:"krx_standard_code,omitempty"`
	InstrumentType  InstrumentType `json:"instrument_type"`
	KRXVerifiedAt   *time.Time     `json:"krx_verified_at,omitempty"`
	Market          string         `json:"market"`
	Currency        string         `json:"currency"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

type DARTCorporation struct {
	CorpCode    string         `json:"corp_code"`
	Name        string         `json:"name"`
	EnglishName string         `json:"english_name,omitempty"`
	StockCode   string         `json:"stock_code,omitempty"`
	ModifiedAt  time.Time      `json:"modified_at"`
	Source      SourceMetadata `json:"source"`
}

type DARTDisclosure struct {
	CorpClass   string         `json:"corp_class"`
	CorpCode    string         `json:"corp_code"`
	CorpName    string         `json:"corp_name"`
	StockCode   string         `json:"stock_code,omitempty"`
	ReportName  string         `json:"report_name"`
	ReceiptNo   string         `json:"receipt_no"`
	ReceiptDate time.Time      `json:"receipt_date"`
	Submitter   string         `json:"submitter"`
	Remark      string         `json:"remark,omitempty"`
	ViewerURL   string         `json:"viewer_url"`
	Source      SourceMetadata `json:"source"`
}

type DARTDocumentEntry struct {
	Index             int    `json:"index"`
	Name              string `json:"name"`
	SHA256            string `json:"sha256"`
	CompressedBytes   int64  `json:"compressed_bytes"`
	UncompressedBytes int64  `json:"uncompressed_bytes"`
	CRC32             uint32 `json:"crc32"`
}

type DARTDocumentArchive struct {
	ID            int64               `json:"id,omitempty"`
	ReceiptNo     string              `json:"receipt_no"`
	SHA256        string              `json:"sha256"`
	ContentSHA256 string              `json:"content_sha256"`
	SizeBytes     int64               `json:"size_bytes"`
	RelativePath  string              `json:"relative_path"`
	IsCurrent     bool                `json:"is_current"`
	Entries       []DARTDocumentEntry `json:"entries"`
	Source        SourceMetadata      `json:"source"`
	CreatedAt     time.Time           `json:"created_at,omitempty"`
	UpdatedAt     time.Time           `json:"updated_at,omitempty"`
}

type DARTFinancialAccount struct {
	Index                   int    `json:"index"`
	StatementKind           string `json:"statement_kind"`
	StatementName           string `json:"statement_name"`
	AccountID               string `json:"account_id,omitempty"`
	AccountName             string `json:"account_name"`
	AccountDetail           string `json:"account_detail,omitempty"`
	CurrentTermName         string `json:"current_term_name"`
	CurrentAmount           string `json:"current_amount,omitempty"`
	CurrentAddAmount        string `json:"current_add_amount,omitempty"`
	PreviousTermName        string `json:"previous_term_name,omitempty"`
	PreviousAmount          string `json:"previous_amount,omitempty"`
	PreviousInterimTermName string `json:"previous_interim_term_name,omitempty"`
	PreviousInterimAmount   string `json:"previous_interim_amount,omitempty"`
	PreviousAddAmount       string `json:"previous_add_amount,omitempty"`
	BeforePreviousTermName  string `json:"before_previous_term_name,omitempty"`
	BeforePreviousAmount    string `json:"before_previous_amount,omitempty"`
	Order                   int    `json:"order"`
	Currency                string `json:"currency"`
}

type DARTFinancialStatement struct {
	ID            int64                  `json:"id,omitempty"`
	CorpCode      string                 `json:"corp_code"`
	BusinessYear  int                    `json:"business_year"`
	ReportCode    string                 `json:"report_code"`
	FSKind        string                 `json:"fs_kind"`
	ReceiptNo     string                 `json:"receipt_no"`
	ContentSHA256 string                 `json:"content_sha256"`
	IsCurrent     bool                   `json:"is_current"`
	Accounts      []DARTFinancialAccount `json:"accounts"`
	Source        SourceMetadata         `json:"source"`
	CreatedAt     time.Time              `json:"created_at,omitempty"`
	UpdatedAt     time.Time              `json:"updated_at,omitempty"`
}

type KRXInstrument struct {
	StandardCode    string         `json:"standard_code,omitempty"`
	ShortCode       string         `json:"short_code"`
	Name            string         `json:"name"`
	AbbreviatedName string         `json:"abbreviated_name,omitempty"`
	EnglishName     string         `json:"english_name,omitempty"`
	Market          string         `json:"market"`
	SecurityGroup   string         `json:"security_group,omitempty"`
	Section         string         `json:"section,omitempty"`
	ShareType       string         `json:"share_type,omitempty"`
	InstrumentType  InstrumentType `json:"instrument_type"`
	ListingDate     *time.Time     `json:"listing_date,omitempty"`
	Dataset         string         `json:"dataset"`
	Source          SourceMetadata `json:"source"`
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

type AnalysisRun struct {
	ID             int64           `json:"id"`
	Kind           string          `json:"kind"`
	Status         DataStatus      `json:"status"`
	InputSHA256    string          `json:"input_sha256"`
	OutputSHA256   string          `json:"output_sha256"`
	RuleVersion    string          `json:"rule_version"`
	IdempotencyKey string          `json:"idempotency_key"`
	Payload        json.RawMessage `json:"payload"`
	GeneratedAt    time.Time       `json:"generated_at"`
	CreatedAt      time.Time       `json:"created_at"`
}
