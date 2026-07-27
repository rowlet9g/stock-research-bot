package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/config"
	"github.com/rowlet9g/stock-research-bot/internal/dart"
	"github.com/rowlet9g/stock-research-bot/internal/models"
	"github.com/rowlet9g/stock-research-bot/internal/provider"
	filestore "github.com/rowlet9g/stock-research-bot/internal/storage/files"
	sqlitestore "github.com/rowlet9g/stock-research-bot/internal/storage/sqlite"
)

const (
	defaultDARTDocumentRoot = "data/raw/opendart/documents"
	dartDocumentSyncTimeout = 5 * time.Minute
	maxDARTDocumentsPerSync = 100
)

type dartDocumentClient interface {
	DisclosureDocument(context.Context, string) (dart.DocumentResult, error)
}

var newDARTDocumentClient = func(apiKey string) dartDocumentClient {
	return dart.NewClient(apiKey)
}

type dartDocumentSyncItem struct {
	ReceiptNo       string                `json:"receipt_no"`
	ReceiptDate     string                `json:"receipt_date"`
	ReportName      string                `json:"report_name"`
	Status          models.DataStatus     `json:"status"`
	SHA256          string                `json:"sha256,omitempty"`
	ContentSHA256   string                `json:"content_sha256,omitempty"`
	SizeBytes       int64                 `json:"size_bytes,omitempty"`
	Entries         int                   `json:"entries,omitempty"`
	RelativePath    string                `json:"relative_path,omitempty"`
	ArchiveInserted bool                  `json:"archive_inserted"`
	VersionChanged  bool                  `json:"version_changed"`
	FileReused      bool                  `json:"file_reused"`
	Source          models.SourceMetadata `json:"source"`
}

type dartDocumentSyncCommandResult struct {
	Database  string                 `json:"database"`
	Root      string                 `json:"root"`
	Status    models.DataStatus      `json:"status"`
	Selected  int                    `json:"selected"`
	Saved     int                    `json:"saved"`
	Skipped   int                    `json:"skipped"`
	Documents []dartDocumentSyncItem `json:"documents"`
	Issues    []outputIssue          `json:"issues,omitempty"`
}

type dartDocumentListResult struct {
	ReceiptNo string                       `json:"receipt_no"`
	Archives  []models.DARTDocumentArchive `json:"archives"`
}

func runDARTDocumentSync(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("dart-document-sync", flag.ContinueOnError)
	flags.SetOutput(stderr)
	databasePath := flags.String("db", defaultDatabasePath, "SQLite database path")
	root := flags.String("root", defaultDARTDocumentRoot, "document storage root")
	ticker := flags.String("ticker", "", "stored instrument ticker")
	receiptNo := flags.String("receipt-no", "", "OpenDART receipt number")
	limit := flags.Int("limit", 10, "maximum documents to download")
	force := flags.Bool("force", false, "download documents with a stored current version")
	outputFormat := flags.String("output", "text", "output format: text or json")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if exitCode := validateCommandOutput(outputFormat, stdout, stderr); exitCode != 0 {
		return exitCode
	}

	*ticker = strings.TrimSpace(*ticker)
	*receiptNo = strings.TrimSpace(*receiptNo)
	switch {
	case (*ticker == "" && *receiptNo == "") ||
		(*ticker != "" && *receiptNo != ""):
		return commandInputError(
			*outputFormat,
			stdout,
			stderr,
			"provide exactly one of ticker or receipt-no",
		)
	case *limit <= 0 || *limit > maxDARTDocumentsPerSync:
		return commandInputError(
			*outputFormat,
			stdout,
			stderr,
			fmt.Sprintf(
				"limit must be between 1 and %d",
				maxDARTDocumentsPerSync,
			),
		)
	}

	settings := config.Load(".env")
	if strings.TrimSpace(settings.OpenDARTAPIKey) == "" {
		return commandInputError(
			*outputFormat,
			stdout,
			stderr,
			"OPENDART_API_KEY is required",
		)
	}

	ctx, cancel := context.WithTimeout(context.Background(), dartDocumentSyncTimeout)
	defer cancel()
	store, err := sqlitestore.Open(ctx, *databasePath)
	if err != nil {
		return writeRuntimeFailure(*outputFormat, stdout, stderr, "storage", err)
	}
	defer store.Close()
	documentStore, err := filestore.NewDARTDocumentStore(*root)
	if err != nil {
		return writeRuntimeFailure(
			*outputFormat,
			stdout,
			stderr,
			"document_storage",
			err,
		)
	}

	disclosures, skipped, err := selectDARTDocumentDisclosures(
		ctx,
		store,
		*ticker,
		*receiptNo,
		*limit,
		*force,
	)
	if err != nil {
		return writeRuntimeFailure(
			*outputFormat,
			stdout,
			stderr,
			"document_disclosures",
			err,
		)
	}
	result := dartDocumentSyncCommandResult{
		Database:  *databasePath,
		Root:      *root,
		Status:    models.DataStatusAvailable,
		Selected:  len(disclosures),
		Skipped:   skipped,
		Documents: make([]dartDocumentSyncItem, 0, len(disclosures)),
		Issues:    []outputIssue{},
	}
	if len(disclosures) == 0 {
		return writeDARTDocumentSyncResult(
			*outputFormat,
			stdout,
			stderr,
			result,
			true,
		)
	}

	client := newDARTDocumentClient(settings.OpenDARTAPIKey)
	for _, disclosure := range disclosures {
		view := dartDocumentSyncItem{
			ReceiptNo:   disclosure.ReceiptNo,
			ReceiptDate: disclosure.ReceiptDate.Format("2006-01-02"),
			ReportName:  disclosure.ReportName,
			Status:      models.DataStatusUnavailable,
		}
		documentResult, fetchErr := client.DisclosureDocument(
			ctx,
			disclosure.ReceiptNo,
		)
		view.Source = documentResult.Archive.Source
		if fetchErr != nil {
			result.Issues = append(result.Issues, outputIssue{
				Scope:   "dart_document_" + disclosure.ReceiptNo,
				Kind:    string(provider.KindOf(fetchErr)),
				Message: fetchErr.Error(),
			})
			result.Documents = append(result.Documents, view)
			continue
		}

		observedAt := disclosure.ReceiptDate.UTC()
		documentResult.Archive.Source.ObservedAt = &observedAt
		existingArchive, lookupErr := store.DARTDocumentArchiveByContentHash(
			ctx,
			disclosure.ReceiptNo,
			documentResult.Archive.ContentSHA256,
		)
		var storedFile filestore.StoredDARTDocument
		switch {
		case lookupErr == nil:
			storedFile, err = documentStore.Verify(
				ctx,
				existingArchive.RelativePath,
				existingArchive.SHA256,
				existingArchive.SizeBytes,
			)
			if err == nil {
				freshSource := documentResult.Archive.Source
				documentResult.Archive = existingArchive
				documentResult.Archive.Source = freshSource
				documentResult.Archive.IsCurrent = true
			}
		case errors.Is(lookupErr, sqlitestore.ErrNotFound):
			storedFile, err = documentStore.Save(
				ctx,
				disclosure.ReceiptNo,
				documentResult.Archive.SHA256,
				documentResult.Content,
			)
		default:
			err = lookupErr
		}
		if err != nil {
			result.Issues = append(result.Issues, outputIssue{
				Scope:   "dart_document_file_" + disclosure.ReceiptNo,
				Kind:    "operation_failed",
				Message: err.Error(),
			})
			result.Documents = append(result.Documents, view)
			continue
		}

		documentResult.Archive.RelativePath = storedFile.RelativePath
		syncResult, syncErr := store.SyncDARTDocumentArchive(
			ctx,
			documentResult.Archive,
		)
		if syncErr != nil {
			result.Issues = append(result.Issues, outputIssue{
				Scope:   "dart_document_metadata_" + disclosure.ReceiptNo,
				Kind:    "operation_failed",
				Message: syncErr.Error(),
			})
			result.Documents = append(result.Documents, view)
			continue
		}

		result.Saved++
		view.Status = models.DataStatusAvailable
		view.SHA256 = documentResult.Archive.SHA256
		view.ContentSHA256 = documentResult.Archive.ContentSHA256
		view.SizeBytes = documentResult.Archive.SizeBytes
		view.Entries = len(documentResult.Archive.Entries)
		view.RelativePath = storedFile.RelativePath
		view.ArchiveInserted = syncResult.Inserted
		view.VersionChanged = syncResult.VersionChanged
		view.FileReused = storedFile.AlreadyPresent
		view.Source = documentResult.Archive.Source
		result.Documents = append(result.Documents, view)
	}

	switch {
	case result.Saved == result.Selected:
		result.Status = models.DataStatusAvailable
	case result.Saved > 0:
		result.Status = models.DataStatusPartial
	default:
		result.Status = models.DataStatusUnavailable
	}
	return writeDARTDocumentSyncResult(
		*outputFormat,
		stdout,
		stderr,
		result,
		result.Saved > 0,
	)
}

func selectDARTDocumentDisclosures(
	ctx context.Context,
	store *sqlitestore.Store,
	ticker string,
	receiptNo string,
	limit int,
	force bool,
) ([]models.DARTDisclosure, int, error) {
	if receiptNo != "" {
		disclosure, err := store.DARTDisclosureByReceipt(ctx, receiptNo)
		if err != nil {
			return nil, 0, err
		}
		if !force {
			archives, err := store.ListDARTDocumentArchives(ctx, receiptNo, 1)
			if err != nil {
				return nil, 0, err
			}
			if len(archives) > 0 && archives[0].IsCurrent {
				return []models.DARTDisclosure{}, 1, nil
			}
		}
		return []models.DARTDisclosure{disclosure}, 0, nil
	}

	instrument, err := store.Instrument(ctx, ticker)
	if err != nil {
		return nil, 0, err
	}
	if strings.TrimSpace(instrument.DARTCorpCode) == "" {
		return nil, 0, fmt.Errorf(
			"instrument %q has no OpenDART corporation code",
			ticker,
		)
	}
	if force {
		disclosures, err := store.ListDARTDisclosures(
			ctx,
			instrument.DARTCorpCode,
			limit,
		)
		return disclosures, 0, err
	}
	disclosures, err := store.ListPendingDARTDocumentDisclosures(
		ctx,
		instrument.DARTCorpCode,
		limit,
	)
	return disclosures, 0, err
}

func runDARTDocumentList(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("dart-document-list", flag.ContinueOnError)
	flags.SetOutput(stderr)
	databasePath := flags.String("db", defaultDatabasePath, "SQLite database path")
	receiptNo := flags.String("receipt-no", "", "OpenDART receipt number")
	limit := flags.Int("limit", 10, "maximum archive versions to return")
	outputFormat := flags.String("output", "text", "output format: text or json")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if exitCode := validateCommandOutput(outputFormat, stdout, stderr); exitCode != 0 {
		return exitCode
	}
	*receiptNo = strings.TrimSpace(*receiptNo)
	if *receiptNo == "" {
		return commandInputError(
			*outputFormat,
			stdout,
			stderr,
			"receipt-no is required",
		)
	}
	if *limit <= 0 || *limit > 100 {
		return commandInputError(
			*outputFormat,
			stdout,
			stderr,
			"limit must be between 1 and 100",
		)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	store, err := sqlitestore.Open(ctx, *databasePath)
	if err != nil {
		return writeRuntimeFailure(*outputFormat, stdout, stderr, "storage", err)
	}
	defer store.Close()
	archives, err := store.ListDARTDocumentArchives(ctx, *receiptNo, *limit)
	if err != nil {
		return writeRuntimeFailure(
			*outputFormat,
			stdout,
			stderr,
			"dart_document_storage",
			err,
		)
	}

	result := dartDocumentListResult{
		ReceiptNo: *receiptNo,
		Archives:  archives,
	}
	if *outputFormat == "json" {
		if err := writeJSON(stdout, result); err != nil {
			return writeRuntimeFailure("text", stdout, stderr, "output", err)
		}
		return 0
	}
	fmt.Fprintf(
		stdout,
		"OpenDART document archives: receipt_no=%s versions=%d\n",
		result.ReceiptNo,
		len(result.Archives),
	)
	for _, archive := range result.Archives {
		fmt.Fprintf(
			stdout,
			"- current=%t sha256=%s content_sha256=%s size=%d path=%s entries=%d\n",
			archive.IsCurrent,
			archive.SHA256,
			archive.ContentSHA256,
			archive.SizeBytes,
			archive.RelativePath,
			len(archive.Entries),
		)
	}
	return 0
}

func writeDARTDocumentSyncResult(
	outputFormat string,
	stdout io.Writer,
	stderr io.Writer,
	result dartDocumentSyncCommandResult,
	succeeded bool,
) int {
	if outputFormat == "json" {
		if err := writeJSON(stdout, result); err != nil {
			fmt.Fprintf(stderr, "write JSON output: %v\n", err)
			return 1
		}
	} else {
		fmt.Fprintf(
			stdout,
			"OpenDART document sync: status=%s selected=%d saved=%d skipped=%d root=%s\n",
			result.Status,
			result.Selected,
			result.Saved,
			result.Skipped,
			result.Root,
		)
		for _, document := range result.Documents {
			fmt.Fprintf(
				stdout,
				"- %s: status=%s size=%d entries=%d path=%s\n",
				document.ReceiptNo,
				document.Status,
				document.SizeBytes,
				document.Entries,
				valueOrNA(document.RelativePath),
			)
		}
		for _, issue := range result.Issues {
			fmt.Fprintf(
				stdout,
				"- issue [%s/%s] %s\n",
				issue.Scope,
				issue.Kind,
				issue.Message,
			)
		}
	}
	if !succeeded {
		return 1
	}
	return 0
}
