package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/dart"
	"github.com/rowlet9g/stock-research-bot/internal/models"
	sqlitestore "github.com/rowlet9g/stock-research-bot/internal/storage/sqlite"
)

func TestDARTDocumentSyncStoresFileMetadataAndSkipsCurrentVersion(t *testing.T) {
	ctx := context.Background()
	tempDirectory := t.TempDir()
	databasePath := filepath.Join(tempDirectory, "forgetmenot.db")
	documentRoot := filepath.Join(tempDirectory, "documents")
	store, err := sqlitestore.Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if _, err := store.SyncInstruments(ctx, []models.WatchlistItem{
		{
			Name:         "삼성전자",
			Ticker:       "005930",
			YahooTicker:  "005930.KS",
			DARTCorpCode: "00126380",
			Market:       "KOSPI",
			Currency:     "KRW",
		},
	}); err != nil {
		t.Fatalf("seed instrument: %v", err)
	}
	fetchedAt := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	disclosure := commandDARTDisclosure(fetchedAt)
	if _, err := store.SyncDARTDisclosures(
		ctx,
		"00126380",
		[]models.DARTDisclosure{disclosure},
	); err != nil {
		t.Fatalf("seed disclosure: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close seed store: %v", err)
	}

	content := []byte("validated document ZIP")
	sum := sha256.Sum256(content)
	hash := hex.EncodeToString(sum[:])
	contentHash := "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	entryHash := "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	client := &stubDARTDocumentClient{
		result: dart.DocumentResult{
			Status: models.DataStatusAvailable,
			Archive: models.DARTDocumentArchive{
				ReceiptNo:     disclosure.ReceiptNo,
				SHA256:        hash,
				ContentSHA256: contentHash,
				SizeBytes:     int64(len(content)),
				IsCurrent:     true,
				Entries: []models.DARTDocumentEntry{
					{
						Index:             0,
						Name:              disclosure.ReceiptNo + ".xml",
						SHA256:            entryHash,
						CompressedBytes:   10,
						UncompressedBytes: 20,
						CRC32:             123,
					},
				},
				Source: models.SourceMetadata{
					Provider: "opendart",
					SourceURL: "https://opendart.fss.or.kr/api/document.xml?rcept_no=" +
						disclosure.ReceiptNo,
					FetchedAt: fetchedAt,
				},
			},
			Content: content,
		},
	}
	originalFactory := newDARTDocumentClient
	newDARTDocumentClient = func(apiKey string) dartDocumentClient {
		if apiKey != "test-secret" {
			t.Fatalf("unexpected OpenDART API key: %q", apiKey)
		}
		return client
	}
	t.Cleanup(func() {
		newDARTDocumentClient = originalFactory
	})
	t.Setenv("OPENDART_API_KEY", "test-secret")

	output := runCommand(
		t,
		"dart-document-sync",
		"-db", databasePath,
		"-root", documentRoot,
		"-ticker", "005930",
		"-limit", "1",
		"-output", "json",
	)
	var syncResult dartDocumentSyncCommandResult
	if err := json.Unmarshal(output, &syncResult); err != nil {
		t.Fatalf("decode document sync result: %v\n%s", err, output)
	}
	if syncResult.Status != models.DataStatusAvailable ||
		syncResult.Selected != 1 ||
		syncResult.Saved != 1 ||
		len(syncResult.Documents) != 1 ||
		!syncResult.Documents[0].ArchiveInserted {
		t.Fatalf("unexpected document sync result: %#v", syncResult)
	}
	storedPath := filepath.Join(
		documentRoot,
		filepath.FromSlash(syncResult.Documents[0].RelativePath),
	)
	storedContent, err := os.ReadFile(storedPath)
	if err != nil {
		t.Fatalf("read stored document: %v", err)
	}
	if string(storedContent) != string(content) {
		t.Fatalf("stored document content mismatch")
	}

	repeatOutput := runCommand(
		t,
		"dart-document-sync",
		"-db", databasePath,
		"-root", documentRoot,
		"-ticker", "005930",
		"-limit", "1",
		"-output", "json",
	)
	var repeatResult dartDocumentSyncCommandResult
	if err := json.Unmarshal(repeatOutput, &repeatResult); err != nil {
		t.Fatalf("decode repeated document sync: %v\n%s", err, repeatOutput)
	}
	if repeatResult.Selected != 0 || repeatResult.Saved != 0 || client.calls != 1 {
		t.Fatalf("current document was downloaded again: %#v", repeatResult)
	}

	equivalentContent := []byte("different ZIP container bytes")
	equivalentSum := sha256.Sum256(equivalentContent)
	equivalentHash := hex.EncodeToString(equivalentSum[:])
	client.result.Content = equivalentContent
	client.result.Archive.SHA256 = equivalentHash
	client.result.Archive.SizeBytes = int64(len(equivalentContent))
	client.result.Archive.Entries[0].CompressedBytes = 11

	forceOutput := runCommand(
		t,
		"dart-document-sync",
		"-db", databasePath,
		"-root", documentRoot,
		"-receipt-no", disclosure.ReceiptNo,
		"-force",
		"-output", "json",
	)
	var forceResult dartDocumentSyncCommandResult
	if err := json.Unmarshal(forceOutput, &forceResult); err != nil {
		t.Fatalf("decode forced document sync: %v\n%s", err, forceOutput)
	}
	if forceResult.Saved != 1 ||
		len(forceResult.Documents) != 1 ||
		!forceResult.Documents[0].FileReused ||
		forceResult.Documents[0].ArchiveInserted ||
		forceResult.Documents[0].VersionChanged ||
		forceResult.Documents[0].SHA256 != hash ||
		forceResult.Documents[0].ContentSHA256 != contentHash ||
		client.calls != 2 {
		t.Fatalf("equivalent ZIP was not reused: %#v", forceResult)
	}
	reusedContent, err := os.ReadFile(storedPath)
	if err != nil {
		t.Fatalf("read reused document: %v", err)
	}
	if string(reusedContent) != string(content) {
		t.Fatalf("equivalent download replaced the verified stored archive")
	}

	listOutput := runCommand(
		t,
		"dart-document-list",
		"-db", databasePath,
		"-receipt-no", disclosure.ReceiptNo,
		"-output", "json",
	)
	var listResult dartDocumentListResult
	if err := json.Unmarshal(listOutput, &listResult); err != nil {
		t.Fatalf("decode document list: %v\n%s", err, listOutput)
	}
	if len(listResult.Archives) != 1 ||
		listResult.Archives[0].SHA256 != hash ||
		listResult.Archives[0].ContentSHA256 != contentHash ||
		!listResult.Archives[0].IsCurrent ||
		len(listResult.Archives[0].Entries) != 1 ||
		listResult.Archives[0].Entries[0].SHA256 != entryHash {
		t.Fatalf("unexpected document list: %#v", listResult)
	}
}

type stubDARTDocumentClient struct {
	result dart.DocumentResult
	err    error
	calls  int
}

func (c *stubDARTDocumentClient) DisclosureDocument(
	context.Context,
	string,
) (dart.DocumentResult, error) {
	c.calls++
	return c.result, c.err
}
