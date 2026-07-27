package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/models"
)

func TestSyncKRXInstrumentsClassifiesAndCrossMapsIdentifiers(t *testing.T) {
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

	if _, err := store.SyncDARTCorporations(ctx, []models.DARTCorporation{
		dartCorporation("00123456", "NH프라임리츠", "338100", fixedNow),
	}); err != nil {
		t.Fatalf("seed DART corporation: %v", err)
	}
	if _, err := store.SyncInstruments(ctx, []models.WatchlistItem{
		{
			Name: "NH프라임리츠", Ticker: "338100", YahooTicker: "338100.KS",
			Market: "KOSPI", Currency: "KRW",
		},
		{
			Name: "검증우선주", Ticker: "000010", YahooTicker: "000010.KS",
			Market: "KOSPI", Currency: "KRW",
		},
		{
			Name: "ARIRANG 200선물레버리지", Ticker: "253150",
			YahooTicker: "253150.KS", Market: "KOSPI", Currency: "KRW",
		},
		{
			Name: "신한 FnGuide 5G 테마주 ETN", Ticker: "500052",
			YahooTicker: "500052.KS", Market: "KOSPI", Currency: "KRW",
		},
	}); err != nil {
		t.Fatalf("seed instruments: %v", err)
	}

	common := krxInstrument(
		"kospi",
		"338100",
		"KR7338100001",
		"NH프라임리츠보통주",
		models.InstrumentTypeCommonStock,
		fixedNow,
	)
	preferred := krxInstrument(
		"kospi",
		"000010",
		"KR7000010001",
		"검증우선주",
		models.InstrumentTypePreferredStock,
		fixedNow,
	)
	result, err := store.SyncKRXInstruments(
		ctx,
		"kospi",
		[]models.KRXInstrument{common, preferred},
	)
	if err != nil {
		t.Fatalf("sync KOSPI instruments: %v", err)
	}
	if result.InstrumentsSeen != 2 ||
		result.InstrumentsMapped != 2 ||
		result.InstrumentsUnmapped != 0 ||
		result.DARTMapped != 1 ||
		result.DARTConflicts != 0 {
		t.Fatalf("unexpected KOSPI sync result: %#v", result)
	}

	commonStored := mustInstrument(t, store, ctx, "338100")
	if commonStored.KRXStandardCode != "KR7338100001" ||
		commonStored.InstrumentType != models.InstrumentTypeCommonStock ||
		commonStored.DARTCorpCode != "00123456" ||
		commonStored.KRXVerifiedAt == nil {
		t.Fatalf("unexpected common stock mapping: %#v", commonStored)
	}
	preferredStored := mustInstrument(t, store, ctx, "000010")
	if preferredStored.InstrumentType != models.InstrumentTypePreferredStock ||
		preferredStored.DARTCorpCode != "" {
		t.Fatalf("unexpected preferred stock mapping: %#v", preferredStored)
	}

	etf := krxInstrument(
		"etf",
		"253150",
		"",
		"ARIRANG 200선물레버리지",
		models.InstrumentTypeETF,
		fixedNow,
	)
	etn := krxInstrument(
		"etn",
		"500052",
		"",
		"신한 FnGuide 5G 테마주 ETN",
		models.InstrumentTypeETN,
		fixedNow,
	)
	if _, err := store.SyncKRXInstruments(
		ctx,
		"etf",
		[]models.KRXInstrument{etf},
	); err != nil {
		t.Fatalf("sync ETF instruments: %v", err)
	}
	if _, err := store.SyncKRXInstruments(
		ctx,
		"etn",
		[]models.KRXInstrument{etn},
	); err != nil {
		t.Fatalf("sync ETN instruments: %v", err)
	}
	if stored := mustInstrument(t, store, ctx, "253150"); stored.InstrumentType != models.InstrumentTypeETF {
		t.Fatalf("unexpected ETF mapping: %#v", stored)
	}
	if stored := mustInstrument(t, store, ctx, "500052"); stored.InstrumentType != models.InstrumentTypeETN {
		t.Fatalf("unexpected ETN mapping: %#v", stored)
	}

	repeat, err := store.SyncKRXInstruments(
		ctx,
		"kospi",
		[]models.KRXInstrument{common, preferred},
	)
	if err != nil {
		t.Fatalf("repeat KOSPI sync: %v", err)
	}
	if repeat.InstrumentsMapped != 0 ||
		repeat.InstrumentsUnmapped != 0 ||
		repeat.DARTMapped != 0 {
		t.Fatalf("repeat sync was not idempotent: %#v", repeat)
	}
}

func TestSyncKRXInstrumentsClearsInactiveMapping(t *testing.T) {
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

	if _, err := store.SyncInstruments(ctx, []models.WatchlistItem{
		{
			Name: "NH프라임리츠", Ticker: "338100", YahooTicker: "338100.KS",
			Market: "KOSPI", Currency: "KRW",
		},
		{
			Name: "검증우선주", Ticker: "000010", YahooTicker: "000010.KS",
			Market: "KOSPI", Currency: "KRW",
		},
	}); err != nil {
		t.Fatalf("seed instruments: %v", err)
	}
	common := krxInstrument(
		"kospi",
		"338100",
		"KR7338100001",
		"NH프라임리츠보통주",
		models.InstrumentTypeCommonStock,
		fixedNow,
	)
	preferred := krxInstrument(
		"kospi",
		"000010",
		"KR7000010001",
		"검증우선주",
		models.InstrumentTypePreferredStock,
		fixedNow,
	)
	if _, err := store.SyncKRXInstruments(
		ctx,
		"kospi",
		[]models.KRXInstrument{common, preferred},
	); err != nil {
		t.Fatalf("initial sync: %v", err)
	}

	result, err := store.SyncKRXInstruments(
		ctx,
		"kospi",
		[]models.KRXInstrument{common},
	)
	if err != nil {
		t.Fatalf("sync reduced snapshot: %v", err)
	}
	if result.Inactive != 1 || result.InstrumentsUnmapped != 1 {
		t.Fatalf("unexpected inactive result: %#v", result)
	}
	stored := mustInstrument(t, store, ctx, "000010")
	if stored.InstrumentType != models.InstrumentTypeUnknown ||
		stored.KRXStandardCode != "" ||
		stored.KRXVerifiedAt != nil {
		t.Fatalf("inactive KRX mapping was preserved: %#v", stored)
	}
}

func TestDARTSyncDoesNotMapKRXPreferredStock(t *testing.T) {
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

	if _, err := store.SyncInstruments(ctx, []models.WatchlistItem{
		{
			Name: "검증우선주", Ticker: "000010", YahooTicker: "000010.KS",
			Market: "KOSPI", Currency: "KRW",
		},
	}); err != nil {
		t.Fatalf("seed preferred instrument: %v", err)
	}
	preferred := krxInstrument(
		"kospi",
		"000010",
		"KR7000010001",
		"검증우선주",
		models.InstrumentTypePreferredStock,
		fixedNow,
	)
	if _, err := store.SyncKRXInstruments(
		ctx,
		"kospi",
		[]models.KRXInstrument{preferred},
	); err != nil {
		t.Fatalf("sync KRX preferred instrument: %v", err)
	}

	result, err := store.SyncDARTCorporations(ctx, []models.DARTCorporation{
		dartCorporation("00999999", "검증회사", "000010", fixedNow),
	})
	if err != nil {
		t.Fatalf("sync DART corporations: %v", err)
	}
	if result.InstrumentsMapped != 0 {
		t.Fatalf("preferred stock received a DART mapping: %#v", result)
	}
	stored := mustInstrument(t, store, ctx, "000010")
	if stored.DARTCorpCode != "" {
		t.Fatalf("preferred stock has unexpected DART code: %#v", stored)
	}
}

func TestSyncKRXInstrumentsRejectsIncompleteSnapshotWithoutChangingData(t *testing.T) {
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

	instrument := krxInstrument(
		"kospi",
		"338100",
		"KR7338100001",
		"NH프라임리츠보통주",
		models.InstrumentTypeCommonStock,
		fixedNow,
	)
	if _, err := store.SyncKRXInstruments(
		ctx,
		"kospi",
		[]models.KRXInstrument{instrument},
	); err != nil {
		t.Fatalf("seed KRX instrument: %v", err)
	}
	if _, err := store.SyncKRXInstruments(ctx, "kospi", nil); err == nil {
		t.Fatal("expected empty snapshot error")
	}

	var current int
	if err := store.db.QueryRowContext(
		ctx,
		`SELECT is_current FROM krx_instruments WHERE short_code = ?`,
		instrument.ShortCode,
	).Scan(&current); err != nil {
		t.Fatalf("query KRX instrument state: %v", err)
	}
	if current != 1 {
		t.Fatalf("expected existing KRX instrument to remain current, got %d", current)
	}
}

func krxInstrument(
	dataset string,
	shortCode string,
	standardCode string,
	name string,
	instrumentType models.InstrumentType,
	fetchedAt time.Time,
) models.KRXInstrument {
	observedAt := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	listingDate := time.Date(2020, 4, 14, 0, 0, 0, 0, time.UTC)
	if instrumentType == models.InstrumentTypeETF ||
		instrumentType == models.InstrumentTypeETN {
		listingDate = time.Time{}
	}
	instrument := models.KRXInstrument{
		StandardCode:   standardCode,
		ShortCode:      shortCode,
		Name:           name,
		Market:         "KOSPI",
		InstrumentType: instrumentType,
		Dataset:        dataset,
		Source: models.SourceMetadata{
			Provider:   "krx",
			SourceURL:  "https://data-dbg.krx.co.kr/svc/apis/test?basDd=20260724",
			ObservedAt: &observedAt,
			FetchedAt:  fetchedAt,
		},
	}
	if !listingDate.IsZero() {
		instrument.ListingDate = &listingDate
	}
	if instrumentType == models.InstrumentTypeETF ||
		instrumentType == models.InstrumentTypeETN {
		instrument.Market = "KRX"
	}
	return instrument
}

func mustInstrument(
	t *testing.T,
	store *Store,
	ctx context.Context,
	ticker string,
) models.Instrument {
	t.Helper()
	instrument, err := store.Instrument(ctx, ticker)
	if err != nil {
		t.Fatalf("query instrument %q: %v", ticker, err)
	}
	return instrument
}
