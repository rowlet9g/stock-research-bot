package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/models"
)

func TestSyncDARTFinancialStatementPreservesVersions(t *testing.T) {
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

	firstStatement := dartFinancialStatement(
		"20260310000777",
		strings.Repeat("a", 64),
		"500000000000000",
		fixedNow,
	)
	first, err := store.SyncDARTFinancialStatement(ctx, firstStatement)
	if err != nil {
		t.Fatalf("sync first financial statement: %v", err)
	}
	if !first.Inserted ||
		first.VersionChanged ||
		first.StatementID == 0 ||
		first.Accounts != 1 {
		t.Fatalf("unexpected first financial sync: %#v", first)
	}

	repeat, err := store.SyncDARTFinancialStatement(ctx, firstStatement)
	if err != nil {
		t.Fatalf("repeat financial statement sync: %v", err)
	}
	if repeat.Inserted ||
		repeat.VersionChanged ||
		repeat.StatementID != first.StatementID {
		t.Fatalf("unexpected repeated financial sync: %#v", repeat)
	}

	secondStatement := dartFinancialStatement(
		"20260401000123",
		strings.Repeat("b", 64),
		"510000000000000",
		fixedNow.Add(time.Hour),
	)
	second, err := store.SyncDARTFinancialStatement(ctx, secondStatement)
	if err != nil {
		t.Fatalf("sync changed financial statement: %v", err)
	}
	if !second.Inserted || !second.VersionChanged {
		t.Fatalf("unexpected changed financial sync: %#v", second)
	}

	versions, err := store.ListDARTFinancialStatementVersions(
		ctx,
		"00126380",
		2025,
		"11011",
		"CFS",
		10,
	)
	if err != nil {
		t.Fatalf("list financial statement versions: %v", err)
	}
	if len(versions) != 2 ||
		!versions[0].IsCurrent ||
		versions[0].ContentSHA256 != secondStatement.ContentSHA256 ||
		versions[0].Accounts[0].CurrentAmount != "510000000000000" ||
		versions[1].IsCurrent ||
		versions[1].ContentSHA256 != firstStatement.ContentSHA256 {
		t.Fatalf("unexpected financial statement versions: %#v", versions)
	}

	current, err := store.CurrentDARTFinancialStatement(
		ctx,
		"00126380",
		2025,
		"11011",
		"CFS",
	)
	if err != nil {
		t.Fatalf("query current financial statement: %v", err)
	}
	if current.ID != second.StatementID ||
		current.ReceiptNo != secondStatement.ReceiptNo {
		t.Fatalf("unexpected current financial statement: %#v", current)
	}
}

func TestSyncDARTFinancialStatementRejectsInvalidAmount(t *testing.T) {
	ctx := context.Background()
	store, err := Open(
		ctx,
		filepath.Join(t.TempDir(), "forgetmenot.db"),
	)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	statement := dartFinancialStatement(
		"20260310000777",
		strings.Repeat("a", 64),
		"1,000",
		time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC),
	)
	if _, err := store.SyncDARTFinancialStatement(
		ctx,
		statement,
	); err == nil {
		t.Fatal("expected invalid normalized amount error")
	}
	versions, err := store.ListDARTFinancialStatementVersions(
		ctx,
		"00126380",
		2025,
		"11011",
		"CFS",
		10,
	)
	if err != nil {
		t.Fatalf("list financial statements after rejection: %v", err)
	}
	if len(versions) != 0 {
		t.Fatalf("invalid statement changed storage: %#v", versions)
	}
}

func TestLatestCurrentDARTFinancialStatementUsesLatestObservation(
	t *testing.T,
) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "forgetmenot.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	annual := dartFinancialStatement(
		"20260310000777",
		strings.Repeat("a", 64),
		"500000000000000",
		time.Date(2026, 3, 10, 1, 0, 0, 0, time.UTC),
	)
	if _, err := store.SyncDARTFinancialStatement(ctx, annual); err != nil {
		t.Fatalf("sync annual financial statement: %v", err)
	}

	thirdQuarter := dartFinancialStatement(
		"20251114002447",
		strings.Repeat("b", 64),
		"490000000000000",
		time.Date(2026, 7, 28, 1, 0, 0, 0, time.UTC),
	)
	thirdQuarter.ReportCode = "11014"
	thirdQuarter.Source.ObservedAt = timePointer(
		time.Date(2025, 11, 14, 0, 0, 0, 0, time.UTC),
	)
	if _, err := store.SyncDARTFinancialStatement(
		ctx,
		thirdQuarter,
	); err != nil {
		t.Fatalf("sync third-quarter financial statement: %v", err)
	}

	latest, err := store.LatestCurrentDARTFinancialStatement(
		ctx,
		"00126380",
		"CFS",
	)
	if err != nil {
		t.Fatalf("query latest financial statement: %v", err)
	}
	if latest.ReceiptNo != annual.ReceiptNo ||
		latest.ContentSHA256 != annual.ContentSHA256 ||
		len(latest.Accounts) != 1 {
		t.Fatalf("unexpected latest financial statement: %#v", latest)
	}

	_, err = store.LatestCurrentDARTFinancialStatement(
		ctx,
		"00126380",
		"OFS",
	)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected missing OFS statement, got %v", err)
	}
}

func dartFinancialStatement(
	receiptNo string,
	contentHash string,
	currentAmount string,
	fetchedAt time.Time,
) models.DARTFinancialStatement {
	observedAt := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
	return models.DARTFinancialStatement{
		CorpCode:      "00126380",
		BusinessYear:  2025,
		ReportCode:    "11011",
		FSKind:        "CFS",
		ReceiptNo:     receiptNo,
		ContentSHA256: contentHash,
		IsCurrent:     true,
		Accounts: []models.DARTFinancialAccount{
			{
				Index:            0,
				StatementKind:    "BS",
				StatementName:    "연결 재무상태표",
				AccountID:        "ifrs-full_Assets",
				AccountName:      "자산총계",
				CurrentTermName:  "제 57 기",
				CurrentAmount:    currentAmount,
				PreviousTermName: "제 56 기말",
				PreviousAmount:   "450000000000000",
				Order:            1,
				Currency:         "KRW",
			},
		},
		Source: models.SourceMetadata{
			Provider: "opendart",
			SourceURL: "https://opendart.fss.or.kr/api/fnlttSinglAcntAll.json?" +
				"bsns_year=2025&corp_code=00126380&fs_div=CFS&reprt_code=11011",
			ObservedAt: &observedAt,
			FetchedAt:  fetchedAt,
		},
	}
}

func timePointer(value time.Time) *time.Time {
	return &value
}
