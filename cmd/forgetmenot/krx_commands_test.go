package main

import (
	"context"
	"testing"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/krx"
	"github.com/rowlet9g/stock-research-bot/internal/models"
)

func TestSelectKRXBaseDateSkipsWeekendAndEmptyWeekday(t *testing.T) {
	originalNow := krxNow
	krxNow = func() time.Time {
		return time.Date(2026, 7, 27, 9, 0, 0, 0, krxSeoulLocation)
	}
	t.Cleanup(func() {
		krxNow = originalNow
	})

	client := &dateSelectionKRXClient{}
	asOf, result, err := selectKRXBaseDate(context.Background(), client, "")
	if err != nil {
		t.Fatalf("select KRX base date: %v", err)
	}
	if asOf.Format("2006-01-02") != "2026-07-23" {
		t.Fatalf("unexpected base date: %s", asOf.Format("2006-01-02"))
	}
	if result == nil || result.Status != models.DataStatusAvailable {
		t.Fatalf("unexpected prefetched result: %#v", result)
	}
	if len(client.requestedDates) != 2 ||
		client.requestedDates[0] != "2026-07-24" ||
		client.requestedDates[1] != "2026-07-23" {
		t.Fatalf("unexpected date search sequence: %#v", client.requestedDates)
	}
}

type dateSelectionKRXClient struct {
	requestedDates []string
}

func (c *dateSelectionKRXClient) Instruments(
	_ context.Context,
	dataset krx.Dataset,
	asOf time.Time,
) (krx.DatasetResult, error) {
	c.requestedDates = append(c.requestedDates, asOf.Format("2006-01-02"))
	result := krx.DatasetResult{
		Dataset:     dataset,
		Status:      models.DataStatusEmpty,
		Instruments: []models.KRXInstrument{},
	}
	if asOf.Format("2006-01-02") == "2026-07-23" {
		result.Status = models.DataStatusAvailable
		result.Instruments = []models.KRXInstrument{{ShortCode: "338100"}}
	}
	return result, nil
}
