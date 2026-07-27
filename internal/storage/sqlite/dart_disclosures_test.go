package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/models"
)

func TestSyncDARTDisclosuresIsIdempotentAndOrdered(t *testing.T) {
	ctx := context.Background()
	fixedNow := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	store, err := openWithClock(
		ctx,
		filepath.Join(t.TempDir(), "forgetmenot.db"),
		func() time.Time { return fixedNow },
	)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	disclosures := []models.DARTDisclosure{
		dartDisclosure(
			"20260723000456",
			"2026-07-23",
			"기업설명회(IR)개최",
			fixedNow,
		),
		dartDisclosure(
			"20260724000123",
			"2026-07-24",
			"주요사항보고서",
			fixedNow,
		),
	}
	first, err := store.SyncDARTDisclosures(ctx, "00126380", disclosures)
	if err != nil {
		t.Fatalf("sync disclosures: %v", err)
	}
	if first.DisclosuresSeen != 2 || first.Inserted != 2 || first.Updated != 0 {
		t.Fatalf("unexpected first sync result: %#v", first)
	}

	second, err := store.SyncDARTDisclosures(ctx, "00126380", disclosures)
	if err != nil {
		t.Fatalf("repeat disclosure sync: %v", err)
	}
	if second.Inserted != 0 || second.Updated != 2 {
		t.Fatalf("unexpected repeat sync result: %#v", second)
	}

	stored, err := store.ListDARTDisclosures(ctx, "00126380", 10)
	if err != nil {
		t.Fatalf("list disclosures: %v", err)
	}
	if len(stored) != 2 {
		t.Fatalf("expected 2 stored disclosures, got %d", len(stored))
	}
	if stored[0].ReceiptNo != "20260724000123" ||
		stored[1].ReceiptNo != "20260723000456" {
		t.Fatalf("unexpected disclosure order: %#v", stored)
	}
	if stored[0].Source.Provider != "opendart" ||
		stored[0].ViewerURL == "" ||
		stored[0].ReceiptDate.Format("2006-01-02") != "2026-07-24" {
		t.Fatalf("unexpected stored disclosure: %#v", stored[0])
	}
}

func TestSyncDARTDisclosuresRejectsMixedCorporationsWithoutChangingData(t *testing.T) {
	ctx := context.Background()
	fixedNow := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	store, err := openWithClock(
		ctx,
		filepath.Join(t.TempDir(), "forgetmenot.db"),
		func() time.Time { return fixedNow },
	)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	valid := dartDisclosure(
		"20260724000123",
		"2026-07-24",
		"주요사항보고서",
		fixedNow,
	)
	invalid := dartDisclosure(
		"20260723000456",
		"2026-07-23",
		"기업설명회(IR)개최",
		fixedNow,
	)
	invalid.CorpCode = "00164742"
	if _, err := store.SyncDARTDisclosures(
		ctx,
		"00126380",
		[]models.DARTDisclosure{valid, invalid},
	); err == nil {
		t.Fatal("expected mixed corporation error")
	}

	stored, err := store.ListDARTDisclosures(ctx, "00126380", 10)
	if err != nil {
		t.Fatalf("list disclosures after rejection: %v", err)
	}
	if len(stored) != 0 {
		t.Fatalf("invalid batch changed stored disclosures: %#v", stored)
	}

	empty, err := store.SyncDARTDisclosures(ctx, "00126380", nil)
	if err != nil {
		t.Fatalf("sync empty disclosure result: %v", err)
	}
	if empty.DisclosuresSeen != 0 || empty.Inserted != 0 || empty.Updated != 0 {
		t.Fatalf("unexpected empty sync result: %#v", empty)
	}
}

func dartDisclosure(
	receiptNo string,
	receiptDateText string,
	reportName string,
	fetchedAt time.Time,
) models.DARTDisclosure {
	receiptDate, err := time.Parse("2006-01-02", receiptDateText)
	if err != nil {
		panic(err)
	}
	observedAt := receiptDate
	return models.DARTDisclosure{
		CorpClass:   "Y",
		CorpCode:    "00126380",
		CorpName:    "삼성전자",
		StockCode:   "005930",
		ReportName:  reportName,
		ReceiptNo:   receiptNo,
		ReceiptDate: receiptDate,
		Submitter:   "삼성전자",
		Remark:      "유",
		ViewerURL: "https://dart.fss.or.kr/dsaf001/main.do?rcpNo=" +
			receiptNo,
		Source: models.SourceMetadata{
			Provider:   "opendart",
			SourceURL:  "https://opendart.fss.or.kr/api/list.json?corp_code=00126380",
			ObservedAt: &observedAt,
			FetchedAt:  fetchedAt,
		},
	}
}
