package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/config"
	"github.com/rowlet9g/stock-research-bot/internal/dart"
	"github.com/rowlet9g/stock-research-bot/internal/models"
	"github.com/rowlet9g/stock-research-bot/internal/provider"
	sqlitestore "github.com/rowlet9g/stock-research-bot/internal/storage/sqlite"
)

const dartCorporationSyncTimeout = 2 * time.Minute

type dartCorporationSyncResult struct {
	Database          string                `json:"database"`
	Status            models.DataStatus     `json:"status"`
	Corporations      int                   `json:"corporations"`
	Listed            int                   `json:"listed"`
	Inactive          int                   `json:"inactive"`
	InstrumentsMapped int                   `json:"instruments_mapped"`
	Source            models.SourceMetadata `json:"source"`
}

type dartCorporationClient interface {
	CorporationCodes(context.Context) (dart.CorporationCodeResult, error)
}

var newDARTCorporationClient = func(apiKey string) dartCorporationClient {
	return dart.NewClient(apiKey)
}

func runDARTCorporationSync(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("dart-corp-sync", flag.ContinueOnError)
	flags.SetOutput(stderr)
	databasePath := flags.String("db", defaultDatabasePath, "SQLite database path")
	outputFormat := flags.String("output", "text", "output format: text or json")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if exitCode := validateCommandOutput(outputFormat, stdout, stderr); exitCode != 0 {
		return exitCode
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

	ctx, cancel := context.WithTimeout(context.Background(), dartCorporationSyncTimeout)
	defer cancel()
	corporationResult, err := newDARTCorporationClient(
		settings.OpenDARTAPIKey,
	).CorporationCodes(ctx)
	if err != nil {
		return writeProviderFailure(
			*outputFormat,
			stdout,
			stderr,
			"dart_corporations",
			err,
		)
	}
	if corporationResult.Status != models.DataStatusAvailable ||
		len(corporationResult.Corporations) == 0 {
		return writeProviderFailure(
			*outputFormat,
			stdout,
			stderr,
			"dart_corporations",
			&provider.Error{
				Provider:  "opendart",
				Operation: "fetch_corporation_codes",
				Kind:      provider.ErrorKindNoData,
				Message:   "OpenDART returned an empty corporation list",
			},
		)
	}

	store, err := sqlitestore.Open(ctx, *databasePath)
	if err != nil {
		return writeRuntimeFailure(*outputFormat, stdout, stderr, "storage", err)
	}
	defer store.Close()

	syncResult, err := store.SyncDARTCorporations(ctx, corporationResult.Corporations)
	if err != nil {
		return writeRuntimeFailure(
			*outputFormat,
			stdout,
			stderr,
			"dart_corporation_storage",
			err,
		)
	}
	result := dartCorporationSyncResult{
		Database:          *databasePath,
		Status:            corporationResult.Status,
		Corporations:      syncResult.CorporationsSeen,
		Listed:            syncResult.Listed,
		Inactive:          syncResult.Inactive,
		InstrumentsMapped: syncResult.InstrumentsMapped,
		Source:            corporationResult.Source,
	}
	text := fmt.Sprintf(
		"OpenDART corporation sync: total=%d listed=%d inactive=%d instruments_mapped=%d database=%s\n",
		result.Corporations,
		result.Listed,
		result.Inactive,
		result.InstrumentsMapped,
		result.Database,
	)
	return writeCommandResult(*outputFormat, stdout, text, result)
}

func writeProviderFailure(
	outputFormat string,
	stdout io.Writer,
	stderr io.Writer,
	scope string,
	err error,
) int {
	issue := outputIssue{
		Scope:   scope,
		Kind:    string(provider.KindOf(err)),
		Message: err.Error(),
	}
	if outputFormat == "json" {
		if writeErr := writeJSON(stdout, struct {
			Error outputIssue `json:"error"`
		}{Error: issue}); writeErr != nil {
			fmt.Fprintf(stderr, "write JSON error: %v\n", writeErr)
		}
	} else {
		fmt.Fprintln(stderr, issue.Message)
	}
	return 1
}
