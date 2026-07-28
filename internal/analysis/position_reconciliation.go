package analysis

import (
	"fmt"
	"math/big"
	"sort"
	"strings"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/decimal"
	"github.com/rowlet9g/stock-research-bot/internal/models"
)

const PositionReconciliationVersion = "position-reconciliation/v1"

type PositionReconciliationStatus string

const (
	PositionReconciliationNoActivity       PositionReconciliationStatus = "no_activity"
	PositionReconciliationNoTradeActivity  PositionReconciliationStatus = "no_trade_activity"
	PositionReconciliationPositionMissing  PositionReconciliationStatus = "position_missing"
	PositionReconciliationOpeningImplied   PositionReconciliationStatus = "opening_balance_implied"
	PositionReconciliationUnsupportedTrade PositionReconciliationStatus = "unsupported_trade"
)

type PositionReconciliationIssue struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

type PositionReconciliationItem struct {
	Instrument             models.Instrument             `json:"instrument"`
	Status                 PositionReconciliationStatus  `json:"status"`
	TradeCount             int                           `json:"trade_count"`
	BuyTradeCount          int                           `json:"buy_trade_count"`
	SellTradeCount         int                           `json:"sell_trade_count"`
	UnsupportedTradeCount  int                           `json:"unsupported_trade_count"`
	BuyQuantityUnits       int64                         `json:"buy_quantity_units"`
	BuyQuantity            string                        `json:"buy_quantity"`
	SellQuantityUnits      int64                         `json:"sell_quantity_units"`
	SellQuantity           string                        `json:"sell_quantity"`
	NetQuantityChangeUnits int64                         `json:"net_quantity_change_units"`
	NetQuantityChange      string                        `json:"net_quantity_change"`
	StoredPositionUnits    *int64                        `json:"stored_position_units,omitempty"`
	StoredPosition         string                        `json:"stored_position,omitempty"`
	ImpliedOpeningUnits    *int64                        `json:"implied_opening_units,omitempty"`
	ImpliedOpening         string                        `json:"implied_opening,omitempty"`
	FirstTradeAt           *time.Time                    `json:"first_trade_at,omitempty"`
	LastTradeAt            *time.Time                    `json:"last_trade_at,omitempty"`
	Sources                []string                      `json:"sources"`
	Issues                 []PositionReconciliationIssue `json:"issues"`
}

type PositionReconciliationSummary struct {
	Instruments           int `json:"instruments"`
	WithTrades            int `json:"with_trades"`
	WithStoredPosition    int `json:"with_stored_position"`
	MissingStoredPosition int `json:"missing_stored_position"`
	UnsupportedTradeItems int `json:"unsupported_trade_items"`
}

type PositionReconciliationReport struct {
	Status      models.DataStatus             `json:"status"`
	Version     string                        `json:"version"`
	GeneratedAt time.Time                     `json:"generated_at"`
	Summary     PositionReconciliationSummary `json:"summary"`
	Items       []PositionReconciliationItem  `json:"items"`
}

func ReconcilePositionLedger(
	records []models.PortfolioRecord,
	generatedAt time.Time,
) (PositionReconciliationReport, error) {
	if generatedAt.IsZero() {
		return PositionReconciliationReport{}, fmt.Errorf(
			"position reconciliation generation time is required",
		)
	}
	ordered := append([]models.PortfolioRecord(nil), records...)
	sort.SliceStable(ordered, func(left int, right int) bool {
		return strings.ToUpper(ordered[left].Instrument.Ticker) <
			strings.ToUpper(ordered[right].Instrument.Ticker)
	})

	report := PositionReconciliationReport{
		Status:      models.DataStatusAvailable,
		Version:     PositionReconciliationVersion,
		GeneratedAt: generatedAt.UTC(),
		Summary: PositionReconciliationSummary{
			Instruments: len(ordered),
		},
		Items: make([]PositionReconciliationItem, 0, len(ordered)),
	}
	seenTickers := make(map[string]struct{}, len(ordered))
	for _, record := range ordered {
		tickerKey := strings.ToUpper(strings.TrimSpace(record.Instrument.Ticker))
		if tickerKey == "" {
			return PositionReconciliationReport{}, fmt.Errorf(
				"position reconciliation instrument ticker is required",
			)
		}
		if _, exists := seenTickers[tickerKey]; exists {
			return PositionReconciliationReport{}, fmt.Errorf(
				"duplicate position reconciliation ticker %q",
				record.Instrument.Ticker,
			)
		}
		seenTickers[tickerKey] = struct{}{}

		item, err := reconcilePositionRecord(record)
		if err != nil {
			return PositionReconciliationReport{}, fmt.Errorf(
				"reconcile position %s: %w",
				record.Instrument.Ticker,
				err,
			)
		}
		if item.TradeCount > 0 {
			report.Summary.WithTrades++
		}
		if item.StoredPositionUnits != nil {
			report.Summary.WithStoredPosition++
		}
		if item.Status == PositionReconciliationPositionMissing {
			report.Summary.MissingStoredPosition++
			report.Status = models.DataStatusPartial
		}
		if item.UnsupportedTradeCount > 0 {
			report.Summary.UnsupportedTradeItems++
			report.Status = models.DataStatusPartial
		}
		report.Items = append(report.Items, item)
	}
	return report, nil
}

func reconcilePositionRecord(
	record models.PortfolioRecord,
) (PositionReconciliationItem, error) {
	item := PositionReconciliationItem{
		Instrument: record.Instrument,
		TradeCount: len(record.Trades),
		Sources:    []string{},
		Issues:     []PositionReconciliationIssue{},
	}
	sourceSet := map[string]struct{}{}
	var buyQuantity int64
	var sellQuantity int64
	for _, trade := range record.Trades {
		if trade.QuantityUnits < 0 {
			return PositionReconciliationItem{}, fmt.Errorf(
				"trade %d quantity must not be negative",
				trade.ID,
			)
		}
		action := strings.ToUpper(strings.TrimSpace(trade.Action))
		var err error
		switch action {
		case "BUY":
			item.BuyTradeCount++
			buyQuantity, err = addExactInt64(
				buyQuantity,
				trade.QuantityUnits,
			)
		case "SELL":
			item.SellTradeCount++
			sellQuantity, err = addExactInt64(
				sellQuantity,
				trade.QuantityUnits,
			)
		default:
			item.UnsupportedTradeCount++
			item.Issues = append(item.Issues, PositionReconciliationIssue{
				Kind: "unsupported_action",
				Message: fmt.Sprintf(
					"trade %d has unsupported action %q",
					trade.ID,
					trade.Action,
				),
			})
		}
		if err != nil {
			return PositionReconciliationItem{}, err
		}
		if !trade.TradeDate.IsZero() {
			tradeAt := trade.TradeDate.UTC()
			if item.FirstTradeAt == nil || tradeAt.Before(*item.FirstTradeAt) {
				item.FirstTradeAt = positionTimePointer(tradeAt)
			}
			if item.LastTradeAt == nil || tradeAt.After(*item.LastTradeAt) {
				item.LastTradeAt = positionTimePointer(tradeAt)
			}
		}
		source := strings.TrimSpace(trade.Source)
		if source != "" {
			sourceSet[source] = struct{}{}
		}
	}
	item.BuyQuantityUnits = buyQuantity
	item.BuyQuantity = decimal.Format(buyQuantity)
	item.SellQuantityUnits = sellQuantity
	item.SellQuantity = decimal.Format(sellQuantity)
	netChange, err := subtractExactInt64(buyQuantity, sellQuantity)
	if err != nil {
		return PositionReconciliationItem{}, err
	}
	item.NetQuantityChangeUnits = netChange
	item.NetQuantityChange = decimal.Format(netChange)
	for source := range sourceSet {
		item.Sources = append(item.Sources, source)
	}
	sort.Strings(item.Sources)

	switch {
	case item.UnsupportedTradeCount > 0:
		item.Status = PositionReconciliationUnsupportedTrade
	case len(record.Trades) == 0 && record.Position == nil:
		item.Status = PositionReconciliationNoActivity
	case len(record.Trades) == 0:
		item.Status = PositionReconciliationNoTradeActivity
	case record.Position == nil:
		item.Status = PositionReconciliationPositionMissing
		item.Issues = append(item.Issues, PositionReconciliationIssue{
			Kind:    "opening_balance_unknown",
			Message: "current position and opening balance are both unknown; net trade change is not a current holding quantity",
		})
	default:
		item.Status = PositionReconciliationOpeningImplied
	}
	if record.Position != nil {
		storedUnits := record.Position.QuantityUnits
		item.StoredPositionUnits = &storedUnits
		item.StoredPosition = decimal.Format(storedUnits)
	}
	if record.Position != nil &&
		len(record.Trades) > 0 &&
		item.UnsupportedTradeCount == 0 {
		storedUnits := record.Position.QuantityUnits
		impliedOpening, err := subtractExactInt64(storedUnits, netChange)
		if err != nil {
			return PositionReconciliationItem{}, err
		}
		item.ImpliedOpeningUnits = &impliedOpening
		item.ImpliedOpening = decimal.Format(impliedOpening)
		item.Issues = append(item.Issues, PositionReconciliationIssue{
			Kind:    "opening_balance_implied",
			Message: "implied opening quantity equals stored current quantity minus net trade change; it is not broker-verified",
		})
	}
	return item, nil
}

func addExactInt64(left int64, right int64) (int64, error) {
	result := new(big.Int).Add(big.NewInt(left), big.NewInt(right))
	if !result.IsInt64() {
		return 0, fmt.Errorf("quantity addition exceeds int64 range")
	}
	return result.Int64(), nil
}

func subtractExactInt64(left int64, right int64) (int64, error) {
	result := new(big.Int).Sub(big.NewInt(left), big.NewInt(right))
	if !result.IsInt64() {
		return 0, fmt.Errorf("quantity subtraction exceeds int64 range")
	}
	return result.Int64(), nil
}

func positionTimePointer(value time.Time) *time.Time {
	return &value
}
