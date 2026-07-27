package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/models"
)

func TestSyncDARTCorporationsMapsInstrumentsAndPreservesMapping(t *testing.T) {
	ctx := context.Background()
	fixedNow := time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC)
	store, err := openWithClock(
		ctx,
		filepath.Join(t.TempDir(), "forgetmenot.db"),
		func() time.Time { return fixedNow },
	)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	item := models.WatchlistItem{
		Name:        "삼성전자",
		Ticker:      "005930",
		YahooTicker: "005930.KS",
		Market:      "KOSPI",
		Currency:    "KRW",
	}
	alphanumericItem := models.WatchlistItem{
		Name:        "영숫자종목",
		Ticker:      "0068y0",
		YahooTicker: "0068Y0.KS",
		Market:      "KOSPI",
		Currency:    "KRW",
	}
	if _, err := store.SyncInstruments(
		ctx,
		[]models.WatchlistItem{item, alphanumericItem},
	); err != nil {
		t.Fatalf("sync instruments: %v", err)
	}

	corporations := []models.DARTCorporation{
		dartCorporation("00126380", "삼성전자", "005930", fixedNow),
		dartCorporation("01234567", "영숫자종목", "0068Y0", fixedNow),
	}
	result, err := store.SyncDARTCorporations(ctx, corporations)
	if err != nil {
		t.Fatalf("sync corporations: %v", err)
	}
	if result.CorporationsSeen != 2 ||
		result.Listed != 2 ||
		result.Inactive != 0 ||
		result.InstrumentsMapped != 2 {
		t.Fatalf("unexpected sync result: %#v", result)
	}

	instrument, err := store.Instrument(ctx, "005930")
	if err != nil {
		t.Fatalf("query mapped instrument: %v", err)
	}
	if instrument.DARTCorpCode != "00126380" {
		t.Fatalf("expected mapped corporation code, got %#v", instrument)
	}
	alphanumericInstrument, err := store.Instrument(ctx, "0068y0")
	if err != nil {
		t.Fatalf("query alphanumeric instrument: %v", err)
	}
	if alphanumericInstrument.DARTCorpCode != "01234567" {
		t.Fatalf(
			"expected case-insensitive alphanumeric mapping, got %#v",
			alphanumericInstrument,
		)
	}

	if _, err := store.UpsertInstrument(ctx, item); err != nil {
		t.Fatalf("repeat instrument upsert: %v", err)
	}
	instrument, err = store.Instrument(ctx, "005930")
	if err != nil {
		t.Fatalf("query preserved instrument: %v", err)
	}
	if instrument.DARTCorpCode != "00126380" {
		t.Fatalf("blank input erased mapped corporation code: %#v", instrument)
	}

	secondResult, err := store.SyncDARTCorporations(ctx, corporations[:1])
	if err != nil {
		t.Fatalf("repeat corporation sync: %v", err)
	}
	if secondResult.Inactive != 1 || secondResult.InstrumentsMapped != 0 {
		t.Fatalf("unexpected repeat sync result: %#v", secondResult)
	}
}

func TestSyncDARTCorporationsRejectsIncompleteSnapshotWithoutChangingData(t *testing.T) {
	ctx := context.Background()
	fixedNow := time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC)
	store, err := openWithClock(
		ctx,
		filepath.Join(t.TempDir(), "forgetmenot.db"),
		func() time.Time { return fixedNow },
	)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	corporation := dartCorporation("00126380", "삼성전자", "005930", fixedNow)
	if _, err := store.SyncDARTCorporations(ctx, []models.DARTCorporation{corporation}); err != nil {
		t.Fatalf("seed corporation: %v", err)
	}
	if _, err := store.SyncDARTCorporations(ctx, nil); err == nil {
		t.Fatal("expected empty snapshot error")
	}

	var current int
	if err := store.db.QueryRowContext(
		ctx,
		`SELECT is_current FROM dart_corporations WHERE corp_code = ?`,
		corporation.CorpCode,
	).Scan(&current); err != nil {
		t.Fatalf("query corporation state: %v", err)
	}
	if current != 1 {
		t.Fatalf("expected existing corporation to remain current, got %d", current)
	}
}

func dartCorporation(
	corpCode string,
	name string,
	stockCode string,
	fetchedAt time.Time,
) models.DARTCorporation {
	modifiedAt := time.Date(2026, 4, 6, 0, 0, 0, 0, time.UTC)
	return models.DARTCorporation{
		CorpCode:   corpCode,
		Name:       name,
		StockCode:  stockCode,
		ModifiedAt: modifiedAt,
		Source: models.SourceMetadata{
			Provider:   "opendart",
			SourceURL:  "https://opendart.fss.or.kr/api/corpCode.xml",
			ObservedAt: &modifiedAt,
			FetchedAt:  fetchedAt,
		},
	}
}
