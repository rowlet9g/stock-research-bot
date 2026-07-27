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

const (
	dartDisclosureSyncTimeout = 5 * time.Minute
	maxDisclosureLookbackDays = 3650
)

var dartDisclosureSeoulLocation = time.FixedZone("Asia/Seoul", 9*60*60)

type dartDisclosureClient interface {
	DisclosureHistory(
		context.Context,
		string,
		time.Time,
		time.Time,
		int,
	) (dart.DisclosureResult, error)
}

var newDARTDisclosureClient = func(apiKey string) dartDisclosureClient {
	return dart.NewClient(apiKey)
}

var dartDisclosureNow = time.Now

type dartDisclosureCorporationResult struct {
	Ticker       string                `json:"ticker"`
	Name         string                `json:"name"`
	CorpCode     string                `json:"corp_code"`
	Status       models.DataStatus     `json:"status"`
	TotalCount   int                   `json:"total_count"`
	PagesFetched int                   `json:"pages_fetched"`
	Disclosures  int                   `json:"disclosures"`
	Inserted     int                   `json:"inserted"`
	Updated      int                   `json:"updated"`
	Source       models.SourceMetadata `json:"source"`
	Warnings     []string              `json:"warnings,omitempty"`
}

type dartDisclosureSyncCommandResult struct {
	Database     string                            `json:"database"`
	Status       models.DataStatus                 `json:"status"`
	BeginDate    string                            `json:"begin_date"`
	EndDate      string                            `json:"end_date"`
	Corporations []dartDisclosureCorporationResult `json:"corporations"`
	Disclosures  int                               `json:"disclosures"`
	Inserted     int                               `json:"inserted"`
	Updated      int                               `json:"updated"`
	Issues       []outputIssue                     `json:"issues,omitempty"`
}

type dartDisclosureListResult struct {
	Ticker      string                  `json:"ticker,omitempty"`
	CorpCode    string                  `json:"corp_code"`
	Disclosures []models.DARTDisclosure `json:"disclosures"`
}

func runDARTDisclosureSync(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("dart-disclosure-sync", flag.ContinueOnError)
	flags.SetOutput(stderr)
	databasePath := flags.String("db", defaultDatabasePath, "SQLite database path")
	ticker := flags.String("ticker", "", "optional stored instrument ticker")
	days := flags.Int("days", 30, "disclosure lookback days")
	pageSize := flags.Int("page-size", 100, "OpenDART results per page")
	outputFormat := flags.String("output", "text", "output format: text or json")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if exitCode := validateCommandOutput(outputFormat, stdout, stderr); exitCode != 0 {
		return exitCode
	}
	switch {
	case *days <= 0 || *days > maxDisclosureLookbackDays:
		return commandInputError(
			*outputFormat,
			stdout,
			stderr,
			fmt.Sprintf(
				"days must be between 1 and %d",
				maxDisclosureLookbackDays,
			),
		)
	case *pageSize <= 0 || *pageSize > 100:
		return commandInputError(
			*outputFormat,
			stdout,
			stderr,
			"page-size must be between 1 and 100",
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

	ctx, cancel := context.WithTimeout(
		context.Background(),
		dartDisclosureSyncTimeout,
	)
	defer cancel()
	store, err := sqlitestore.Open(ctx, *databasePath)
	if err != nil {
		return writeRuntimeFailure(*outputFormat, stdout, stderr, "storage", err)
	}
	defer store.Close()

	instruments, err := disclosureSyncInstruments(ctx, store, *ticker)
	if err != nil {
		return writeRuntimeFailure(
			*outputFormat,
			stdout,
			stderr,
			"disclosure_instruments",
			err,
		)
	}
	if len(instruments) == 0 {
		return commandInputError(
			*outputFormat,
			stdout,
			stderr,
			"no stored instruments have an OpenDART corporation code",
		)
	}

	endDate := disclosureDateOnly(
		dartDisclosureNow().In(dartDisclosureSeoulLocation),
	)
	beginDate := endDate.AddDate(0, 0, -*days)
	result := dartDisclosureSyncCommandResult{
		Database:     *databasePath,
		Status:       models.DataStatusUnavailable,
		BeginDate:    beginDate.Format("2006-01-02"),
		EndDate:      endDate.Format("2006-01-02"),
		Corporations: make([]dartDisclosureCorporationResult, 0, len(instruments)),
		Issues:       []outputIssue{},
	}

	client := newDARTDisclosureClient(settings.OpenDARTAPIKey)
	successfulCorporations := 0
	for _, instrument := range instruments {
		disclosureResult, fetchErr := client.DisclosureHistory(
			ctx,
			instrument.DARTCorpCode,
			beginDate,
			endDate,
			*pageSize,
		)
		view := dartDisclosureCorporationResult{
			Ticker:       instrument.Ticker,
			Name:         instrument.Name,
			CorpCode:     instrument.DARTCorpCode,
			Status:       disclosureResult.Status,
			TotalCount:   disclosureResult.TotalCount,
			PagesFetched: disclosureResult.PagesFetched,
			Disclosures:  len(disclosureResult.Disclosures),
			Source:       disclosureResult.Source,
			Warnings:     disclosureResult.Warnings,
		}
		if fetchErr != nil {
			view.Status = models.DataStatusUnavailable
			result.Issues = append(result.Issues, outputIssue{
				Scope:   "dart_disclosures_" + instrument.Ticker,
				Kind:    string(provider.KindOf(fetchErr)),
				Message: fetchErr.Error(),
			})
			result.Corporations = append(result.Corporations, view)
			continue
		}

		syncResult, syncErr := store.SyncDARTDisclosures(
			ctx,
			instrument.DARTCorpCode,
			disclosureResult.Disclosures,
		)
		if syncErr != nil {
			view.Status = models.DataStatusUnavailable
			result.Issues = append(result.Issues, outputIssue{
				Scope:   "dart_disclosures_" + instrument.Ticker,
				Kind:    "operation_failed",
				Message: syncErr.Error(),
			})
			result.Corporations = append(result.Corporations, view)
			continue
		}

		successfulCorporations++
		view.Inserted = syncResult.Inserted
		view.Updated = syncResult.Updated
		result.Disclosures += syncResult.DisclosuresSeen
		result.Inserted += syncResult.Inserted
		result.Updated += syncResult.Updated
		for _, warning := range disclosureResult.Warnings {
			result.Issues = append(result.Issues, outputIssue{
				Scope:   "dart_disclosures_" + instrument.Ticker,
				Kind:    "partial_data",
				Message: warning,
			})
		}
		result.Corporations = append(result.Corporations, view)
	}

	switch {
	case successfulCorporations == len(instruments) && len(result.Issues) == 0:
		result.Status = models.DataStatusAvailable
	case successfulCorporations > 0:
		result.Status = models.DataStatusPartial
	}
	return writeDARTDisclosureSyncResult(
		*outputFormat,
		stdout,
		stderr,
		result,
		successfulCorporations > 0,
	)
}

func disclosureSyncInstruments(
	ctx context.Context,
	store *sqlitestore.Store,
	ticker string,
) ([]models.Instrument, error) {
	ticker = strings.TrimSpace(ticker)
	if ticker != "" {
		instrument, err := store.Instrument(ctx, ticker)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(instrument.DARTCorpCode) == "" {
			return nil, fmt.Errorf(
				"instrument %q has no OpenDART corporation code",
				ticker,
			)
		}
		return []models.Instrument{instrument}, nil
	}

	instruments, err := store.ListInstruments(ctx)
	if err != nil {
		return nil, err
	}
	selected := make([]models.Instrument, 0, len(instruments))
	seenCorporations := map[string]struct{}{}
	for _, instrument := range instruments {
		corpCode := strings.TrimSpace(instrument.DARTCorpCode)
		if corpCode == "" {
			continue
		}
		if _, exists := seenCorporations[corpCode]; exists {
			continue
		}
		seenCorporations[corpCode] = struct{}{}
		selected = append(selected, instrument)
	}
	return selected, nil
}

func runDARTDisclosureList(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("dart-disclosure-list", flag.ContinueOnError)
	flags.SetOutput(stderr)
	databasePath := flags.String("db", defaultDatabasePath, "SQLite database path")
	ticker := flags.String("ticker", "", "stored instrument ticker")
	corpCode := flags.String("corp-code", "", "OpenDART corporation code")
	limit := flags.Int("limit", 20, "maximum disclosures to return")
	outputFormat := flags.String("output", "text", "output format: text or json")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if exitCode := validateCommandOutput(outputFormat, stdout, stderr); exitCode != 0 {
		return exitCode
	}

	*ticker = strings.TrimSpace(*ticker)
	*corpCode = strings.TrimSpace(*corpCode)
	switch {
	case (*ticker == "" && *corpCode == "") ||
		(*ticker != "" && *corpCode != ""):
		return commandInputError(
			*outputFormat,
			stdout,
			stderr,
			"provide exactly one of ticker or corp-code",
		)
	case *limit <= 0 || *limit > 1000:
		return commandInputError(
			*outputFormat,
			stdout,
			stderr,
			"limit must be between 1 and 1000",
		)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	store, err := sqlitestore.Open(ctx, *databasePath)
	if err != nil {
		return writeRuntimeFailure(*outputFormat, stdout, stderr, "storage", err)
	}
	defer store.Close()

	if *ticker != "" {
		instrument, err := store.Instrument(ctx, *ticker)
		if err != nil {
			return writeRuntimeFailure(
				*outputFormat,
				stdout,
				stderr,
				"disclosure_instrument",
				err,
			)
		}
		if strings.TrimSpace(instrument.DARTCorpCode) == "" {
			return commandInputError(
				*outputFormat,
				stdout,
				stderr,
				fmt.Sprintf(
					"instrument %q has no OpenDART corporation code",
					*ticker,
				),
			)
		}
		*corpCode = instrument.DARTCorpCode
	}

	disclosures, err := store.ListDARTDisclosures(ctx, *corpCode, *limit)
	if err != nil {
		return writeRuntimeFailure(
			*outputFormat,
			stdout,
			stderr,
			"dart_disclosure_storage",
			err,
		)
	}
	result := dartDisclosureListResult{
		Ticker:      *ticker,
		CorpCode:    *corpCode,
		Disclosures: disclosures,
	}
	if *outputFormat == "json" {
		if err := writeJSON(stdout, result); err != nil {
			return writeRuntimeFailure("text", stdout, stderr, "output", err)
		}
		return 0
	}

	fmt.Fprintf(
		stdout,
		"OpenDART disclosures: corp_code=%s count=%d\n",
		result.CorpCode,
		len(result.Disclosures),
	)
	for _, disclosure := range result.Disclosures {
		fmt.Fprintf(
			stdout,
			"- %s %s (%s)\n  %s\n",
			disclosure.ReceiptDate.Format("2006-01-02"),
			disclosure.ReportName,
			disclosure.ReceiptNo,
			disclosure.ViewerURL,
		)
	}
	return 0
}

func disclosureDateOnly(value time.Time) time.Time {
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

func writeDARTDisclosureSyncResult(
	outputFormat string,
	stdout io.Writer,
	stderr io.Writer,
	result dartDisclosureSyncCommandResult,
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
			"OpenDART disclosure sync: status=%s range=%s..%s disclosures=%d inserted=%d updated=%d\n",
			result.Status,
			result.BeginDate,
			result.EndDate,
			result.Disclosures,
			result.Inserted,
			result.Updated,
		)
		for _, corporation := range result.Corporations {
			fmt.Fprintf(
				stdout,
				"- %s (%s): status=%s disclosures=%d inserted=%d updated=%d\n",
				corporation.Name,
				corporation.Ticker,
				corporation.Status,
				corporation.Disclosures,
				corporation.Inserted,
				corporation.Updated,
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
