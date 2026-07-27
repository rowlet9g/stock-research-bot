package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/config"
	"github.com/rowlet9g/stock-research-bot/internal/krx"
	"github.com/rowlet9g/stock-research-bot/internal/models"
	"github.com/rowlet9g/stock-research-bot/internal/provider"
	sqlitestore "github.com/rowlet9g/stock-research-bot/internal/storage/sqlite"
)

const (
	krxInstrumentSyncTimeout = 5 * time.Minute
	krxDateSearchLimit       = 10
)

var krxSeoulLocation = time.FixedZone("Asia/Seoul", 9*60*60)

type krxInstrumentClient interface {
	Instruments(
		context.Context,
		krx.Dataset,
		time.Time,
	) (krx.DatasetResult, error)
}

var newKRXInstrumentClient = func(apiKey string) krxInstrumentClient {
	return krx.NewClient(apiKey)
}

var krxNow = time.Now

type krxDatasetSyncResult struct {
	Dataset             krx.Dataset           `json:"dataset"`
	Status              models.DataStatus     `json:"status"`
	Instruments         int                   `json:"instruments"`
	Inactive            int                   `json:"inactive"`
	InstrumentsMapped   int                   `json:"instruments_mapped"`
	InstrumentsUnmapped int                   `json:"instruments_unmapped"`
	DARTMapped          int                   `json:"dart_mapped"`
	DARTConflicts       int                   `json:"dart_conflicts"`
	Source              models.SourceMetadata `json:"source"`
}

type krxInstrumentSyncResult struct {
	Database      string                 `json:"database"`
	Status        models.DataStatus      `json:"status"`
	AsOf          string                 `json:"as_of"`
	Datasets      []krxDatasetSyncResult `json:"datasets"`
	DARTConflicts int                    `json:"dart_conflicts"`
	Issues        []outputIssue          `json:"issues,omitempty"`
}

func runKRXInstrumentSync(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("krx-instrument-sync", flag.ContinueOnError)
	flags.SetOutput(stderr)
	databasePath := flags.String("db", defaultDatabasePath, "SQLite database path")
	dateText := flags.String("date", "", "KRX base date in YYYY-MM-DD")
	outputFormat := flags.String("output", "text", "output format: text or json")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if exitCode := validateCommandOutput(outputFormat, stdout, stderr); exitCode != 0 {
		return exitCode
	}

	settings := config.Load(".env")
	if strings.TrimSpace(settings.KRXAPIKey) == "" {
		return commandInputError(
			*outputFormat,
			stdout,
			stderr,
			"KRX_API_KEY is required",
		)
	}

	ctx, cancel := context.WithTimeout(context.Background(), krxInstrumentSyncTimeout)
	defer cancel()
	client := newKRXInstrumentClient(settings.KRXAPIKey)

	asOf, prefetchedKOSPI, err := selectKRXBaseDate(ctx, client, *dateText)
	if err != nil {
		return writeProviderFailure(
			*outputFormat,
			stdout,
			stderr,
			"krx_instruments",
			err,
		)
	}

	store, err := sqlitestore.Open(ctx, *databasePath)
	if err != nil {
		return writeRuntimeFailure(*outputFormat, stdout, stderr, "storage", err)
	}
	defer store.Close()

	result := krxInstrumentSyncResult{
		Database: *databasePath,
		Status:   models.DataStatusUnavailable,
		AsOf:     asOf.Format("2006-01-02"),
		Datasets: make([]krxDatasetSyncResult, 0, len(krx.InstrumentDatasets())),
		Issues:   []outputIssue{},
	}
	successfulDatasets := 0
	for _, dataset := range krx.InstrumentDatasets() {
		datasetResult := krx.DatasetResult{}
		var fetchErr error
		if dataset == krx.DatasetKOSPI && prefetchedKOSPI != nil {
			datasetResult = *prefetchedKOSPI
		} else {
			datasetResult, fetchErr = client.Instruments(ctx, dataset, asOf)
		}

		view := krxDatasetSyncResult{
			Dataset: dataset,
			Status:  datasetResult.Status,
			Source:  datasetResult.Source,
		}
		if fetchErr != nil {
			view.Status = models.DataStatusUnavailable
			result.Issues = append(result.Issues, outputIssue{
				Scope:   "krx_" + string(dataset),
				Kind:    string(provider.KindOf(fetchErr)),
				Message: fetchErr.Error(),
			})
			result.Datasets = append(result.Datasets, view)
			continue
		}
		if datasetResult.Status != models.DataStatusAvailable ||
			len(datasetResult.Instruments) == 0 {
			view.Status = models.DataStatusEmpty
			result.Issues = append(result.Issues, outputIssue{
				Scope:   "krx_" + string(dataset),
				Kind:    string(provider.ErrorKindNoData),
				Message: fmt.Sprintf("KRX %s returned no instruments", dataset),
			})
			result.Datasets = append(result.Datasets, view)
			continue
		}

		syncResult, syncErr := store.SyncKRXInstruments(
			ctx,
			string(dataset),
			datasetResult.Instruments,
		)
		if syncErr != nil {
			view.Status = models.DataStatusUnavailable
			result.Issues = append(result.Issues, outputIssue{
				Scope:   "krx_" + string(dataset),
				Kind:    "operation_failed",
				Message: syncErr.Error(),
			})
			result.Datasets = append(result.Datasets, view)
			continue
		}

		successfulDatasets++
		view.Instruments = syncResult.InstrumentsSeen
		view.Inactive = syncResult.Inactive
		view.InstrumentsMapped = syncResult.InstrumentsMapped
		view.InstrumentsUnmapped = syncResult.InstrumentsUnmapped
		view.DARTMapped = syncResult.DARTMapped
		view.DARTConflicts = syncResult.DARTConflicts
		if syncResult.DARTConflicts > result.DARTConflicts {
			result.DARTConflicts = syncResult.DARTConflicts
		}
		result.Datasets = append(result.Datasets, view)
	}

	switch {
	case successfulDatasets == len(krx.InstrumentDatasets()):
		result.Status = models.DataStatusAvailable
	case successfulDatasets > 0:
		result.Status = models.DataStatusPartial
	}
	return writeKRXInstrumentSyncResult(
		*outputFormat,
		stdout,
		stderr,
		result,
		successfulDatasets > 0,
	)
}

func selectKRXBaseDate(
	ctx context.Context,
	client krxInstrumentClient,
	dateText string,
) (time.Time, *krx.DatasetResult, error) {
	dateText = strings.TrimSpace(dateText)
	if dateText != "" {
		parsed, err := time.Parse("2006-01-02", dateText)
		if err != nil {
			return time.Time{}, nil, &provider.Error{
				Provider:  "krx",
				Operation: "select_base_date",
				Kind:      provider.ErrorKindInvalidRequest,
				Message:   "date must be YYYY-MM-DD",
				Err:       err,
			}
		}
		return parsed.UTC(), nil, nil
	}

	candidate := previousWeekday(
		krxNow().In(krxSeoulLocation).AddDate(0, 0, -1),
	)
	for attempt := 0; attempt < krxDateSearchLimit; attempt++ {
		result, err := client.Instruments(ctx, krx.DatasetKOSPI, candidate)
		if err != nil {
			return time.Time{}, nil, err
		}
		if result.Status == models.DataStatusAvailable &&
			len(result.Instruments) > 0 {
			candidate = dateOnly(candidate)
			return candidate, &result, nil
		}
		candidate = previousWeekday(candidate.AddDate(0, 0, -1))
	}
	return time.Time{}, nil, &provider.Error{
		Provider:  "krx",
		Operation: "select_base_date",
		Kind:      provider.ErrorKindNoData,
		Message: fmt.Sprintf(
			"no KOSPI instrument data found in the latest %d weekdays",
			krxDateSearchLimit,
		),
	}
}

func previousWeekday(value time.Time) time.Time {
	value = dateOnly(value)
	for value.Weekday() == time.Saturday || value.Weekday() == time.Sunday {
		value = value.AddDate(0, 0, -1)
	}
	return value
}

func dateOnly(value time.Time) time.Time {
	return time.Date(
		value.Year(),
		value.Month(),
		value.Day(),
		0,
		0,
		0,
		0,
		time.UTC,
	)
}

func writeKRXInstrumentSyncResult(
	outputFormat string,
	stdout io.Writer,
	stderr io.Writer,
	result krxInstrumentSyncResult,
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
			"KRX instrument sync: status=%s as_of=%s database=%s\n",
			result.Status,
			result.AsOf,
			result.Database,
		)
		for _, dataset := range result.Datasets {
			fmt.Fprintf(
				stdout,
				"- %s: status=%s instruments=%d inactive=%d mapped=%d unmapped=%d dart_mapped=%d\n",
				dataset.Dataset,
				dataset.Status,
				dataset.Instruments,
				dataset.Inactive,
				dataset.InstrumentsMapped,
				dataset.InstrumentsUnmapped,
				dataset.DARTMapped,
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
