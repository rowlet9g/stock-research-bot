package analysis

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/decimal"
	"github.com/rowlet9g/stock-research-bot/internal/models"
)

func TestReconcilePositionLedgerDoesNotTreatNetChangeAsCurrentPosition(
	t *testing.T,
) {
	report, err := ReconcilePositionLedger(
		[]models.PortfolioRecord{
			positionReconciliationRecord(nil),
		},
		time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("reconcile positions: %v", err)
	}
	if report.Status != models.DataStatusPartial ||
		report.Summary.MissingStoredPosition != 1 ||
		len(report.Items) != 1 {
		t.Fatalf("unexpected reconciliation report: %#v", report)
	}
	item := report.Items[0]
	if item.Status != PositionReconciliationPositionMissing ||
		item.BuyQuantity != "9" ||
		item.SellQuantity != "4" ||
		item.NetQuantityChange != "5" ||
		item.StoredPositionUnits != nil ||
		item.ImpliedOpeningUnits != nil {
		t.Fatalf("net change was treated as a current position: %#v", item)
	}
}

func TestReconcilePositionLedgerCalculatesImpliedOpeningBalance(t *testing.T) {
	position := &models.Position{
		QuantityUnits: 7 * decimal.Scale,
		Currency:      "KRW",
		AsOf:          time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC),
	}
	report, err := ReconcilePositionLedger(
		[]models.PortfolioRecord{
			positionReconciliationRecord(position),
		},
		time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("reconcile positions: %v", err)
	}
	item := report.Items[0]
	if report.Status != models.DataStatusAvailable ||
		item.Status != PositionReconciliationOpeningImplied ||
		item.StoredPosition != "7" ||
		item.ImpliedOpening != "2" {
		t.Fatalf("unexpected implied opening balance: %#v", item)
	}
}

func TestReconcilePositionLedgerMarksUnsupportedAction(t *testing.T) {
	record := positionReconciliationRecord(nil)
	record.Trades = append(record.Trades, models.Trade{
		ID:            10,
		Action:        "TRANSFER",
		QuantityUnits: decimal.Scale,
	})
	report, err := ReconcilePositionLedger(
		[]models.PortfolioRecord{record},
		time.Now(),
	)
	if err != nil {
		t.Fatalf("reconcile positions: %v", err)
	}
	if report.Status != models.DataStatusPartial ||
		report.Items[0].Status != PositionReconciliationUnsupportedTrade ||
		report.Items[0].UnsupportedTradeCount != 1 {
		t.Fatalf("unsupported action was accepted: %#v", report)
	}
}

func TestReconcilePositionLedgerRejectsQuantityOverflow(t *testing.T) {
	record := positionReconciliationRecord(nil)
	record.Trades = []models.Trade{
		{ID: 1, Action: "BUY", QuantityUnits: math.MaxInt64},
		{ID: 2, Action: "BUY", QuantityUnits: 1},
	}
	_, err := ReconcilePositionLedger(
		[]models.PortfolioRecord{record},
		time.Now(),
	)
	if err == nil || !strings.Contains(err.Error(), "exceeds int64") {
		t.Fatalf("unexpected quantity overflow error: %v", err)
	}
}

func positionReconciliationRecord(
	position *models.Position,
) models.PortfolioRecord {
	return models.PortfolioRecord{
		Instrument: models.Instrument{
			ID:       1,
			Name:     "삼성전자",
			Ticker:   "005930",
			Market:   "KOSPI",
			Currency: "KRW",
		},
		Position: position,
		Trades: []models.Trade{
			{
				ID:            1,
				Action:        "BUY",
				QuantityUnits: 2 * decimal.Scale,
				TradeDate:     time.Date(2026, 2, 2, 0, 0, 0, 0, time.UTC),
				Source:        "mirae-ledger",
			},
			{
				ID:            2,
				Action:        "BUY",
				QuantityUnits: 3 * decimal.Scale,
				TradeDate:     time.Date(2026, 3, 3, 0, 0, 0, 0, time.UTC),
				Source:        "mirae-ledger",
			},
			{
				ID:            3,
				Action:        "BUY",
				QuantityUnits: 4 * decimal.Scale,
				TradeDate:     time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC),
				Source:        "mirae-ledger",
			},
			{
				ID:            4,
				Action:        "SELL",
				QuantityUnits: 4 * decimal.Scale,
				TradeDate:     time.Date(2026, 3, 5, 0, 0, 0, 0, time.UTC),
				Source:        "mirae-ledger",
			},
		},
	}
}
