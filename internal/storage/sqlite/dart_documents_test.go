package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/models"
)

func TestSyncDARTDocumentArchiveVersionsAndPendingState(t *testing.T) {
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

	disclosure := dartDisclosure(
		"20260724000123",
		"2026-07-24",
		"주요사항보고서",
		fixedNow,
	)
	if _, err := store.SyncDARTDisclosures(
		ctx,
		"00126380",
		[]models.DARTDisclosure{disclosure},
	); err != nil {
		t.Fatalf("seed disclosure: %v", err)
	}
	pending, err := store.ListPendingDARTDocumentDisclosures(
		ctx,
		"00126380",
		10,
	)
	if err != nil {
		t.Fatalf("list pending documents: %v", err)
	}
	if len(pending) != 1 || pending[0].ReceiptNo != disclosure.ReceiptNo {
		t.Fatalf("unexpected pending documents: %#v", pending)
	}

	firstArchive := dartDocumentArchive(
		disclosure,
		strings.Repeat("a", 64),
		strings.Repeat("c", 64),
		fixedNow,
	)
	first, err := store.SyncDARTDocumentArchive(ctx, firstArchive)
	if err != nil {
		t.Fatalf("sync first document archive: %v", err)
	}
	if !first.Inserted || first.VersionChanged || first.ArchiveID == 0 {
		t.Fatalf("unexpected first archive result: %#v", first)
	}
	repeat, err := store.SyncDARTDocumentArchive(ctx, firstArchive)
	if err != nil {
		t.Fatalf("repeat document archive sync: %v", err)
	}
	if repeat.Inserted || repeat.VersionChanged || repeat.ArchiveID != first.ArchiveID {
		t.Fatalf("unexpected repeat archive result: %#v", repeat)
	}

	equivalentArchive := dartDocumentArchive(
		disclosure,
		strings.Repeat("b", 64),
		firstArchive.ContentSHA256,
		fixedNow.Add(30*time.Minute),
	)
	equivalent, err := store.SyncDARTDocumentArchive(ctx, equivalentArchive)
	if err != nil {
		t.Fatalf("sync equivalent document archive: %v", err)
	}
	if equivalent.Inserted ||
		equivalent.VersionChanged ||
		equivalent.ArchiveID != first.ArchiveID {
		t.Fatalf("equivalent ZIP became a new version: %#v", equivalent)
	}

	pending, err = store.ListPendingDARTDocumentDisclosures(ctx, "00126380", 10)
	if err != nil {
		t.Fatalf("list pending documents after sync: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("synchronized disclosure remains pending: %#v", pending)
	}

	secondArchive := dartDocumentArchive(
		disclosure,
		strings.Repeat("b", 64),
		strings.Repeat("d", 64),
		fixedNow.Add(time.Hour),
	)
	second, err := store.SyncDARTDocumentArchive(ctx, secondArchive)
	if err != nil {
		t.Fatalf("sync changed document archive: %v", err)
	}
	if !second.Inserted || !second.VersionChanged {
		t.Fatalf("unexpected changed archive result: %#v", second)
	}

	archives, err := store.ListDARTDocumentArchives(
		ctx,
		disclosure.ReceiptNo,
		10,
	)
	if err != nil {
		t.Fatalf("list document archives: %v", err)
	}
	if len(archives) != 2 ||
		!archives[0].IsCurrent ||
		archives[0].SHA256 != secondArchive.SHA256 ||
		archives[0].ContentSHA256 != secondArchive.ContentSHA256 ||
		archives[1].IsCurrent ||
		archives[1].SHA256 != firstArchive.SHA256 ||
		archives[1].ContentSHA256 != firstArchive.ContentSHA256 ||
		len(archives[0].Entries) != 1 {
		t.Fatalf("unexpected document versions: %#v", archives)
	}
	byContent, err := store.DARTDocumentArchiveByContentHash(
		ctx,
		disclosure.ReceiptNo,
		firstArchive.ContentSHA256,
	)
	if err != nil {
		t.Fatalf("query document by content hash: %v", err)
	}
	if byContent.ID != first.ArchiveID ||
		byContent.SHA256 != firstArchive.SHA256 ||
		len(byContent.Entries) != 1 ||
		byContent.Entries[0].SHA256 != firstArchive.Entries[0].SHA256 {
		t.Fatalf("unexpected archive by content hash: %#v", byContent)
	}
}

func dartDocumentArchive(
	disclosure models.DARTDisclosure,
	rawHash string,
	contentHash string,
	fetchedAt time.Time,
) models.DARTDocumentArchive {
	observedAt := disclosure.ReceiptDate
	return models.DARTDocumentArchive{
		ReceiptNo:     disclosure.ReceiptNo,
		SHA256:        rawHash,
		ContentSHA256: contentHash,
		SizeBytes:     123,
		RelativePath:  disclosure.ReceiptNo + "/" + rawHash + ".zip",
		IsCurrent:     true,
		Entries: []models.DARTDocumentEntry{
			{
				Index:             0,
				Name:              disclosure.ReceiptNo + ".xml",
				SHA256:            contentHash,
				CompressedBytes:   100,
				UncompressedBytes: 200,
				CRC32:             12345,
			},
		},
		Source: models.SourceMetadata{
			Provider: "opendart",
			SourceURL: "https://opendart.fss.or.kr/api/document.xml?rcept_no=" +
				disclosure.ReceiptNo,
			ObservedAt: &observedAt,
			FetchedAt:  fetchedAt,
		},
	}
}
