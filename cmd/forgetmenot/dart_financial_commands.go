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

const dartFinancialSyncTimeout = 5 * time.Minute

type dartFinancialClient interface {
	FullFinancialStatement(
		context.Context,
		string,
		int,
		string,
		string,
	) (dart.FinancialStatementResult, error)
}

var newDARTFinancialClient = func(apiKey string) dartFinancialClient {
	return dart.NewClient(apiKey)
}

var dartFinancialNow = time.Now

type dartFinancialSyncItem struct {
	Ticker         string                `json:"ticker,omitempty"`
	Name           string                `json:"name,omitempty"`
	CorpCode       string                `json:"corp_code"`
	FSKind         string                `json:"fs_kind"`
	Status         models.DataStatus     `json:"status"`
	ReceiptNo      string                `json:"receipt_no,omitempty"`
	ContentSHA256  string                `json:"content_sha256,omitempty"`
	Accounts       int                   `json:"accounts"`
	Inserted       bool                  `json:"inserted"`
	VersionChanged bool                  `json:"version_changed"`
	Source         models.SourceMetadata `json:"source"`
}

type dartFinancialSyncCommandResult struct {
	Database        string                  `json:"database"`
	BusinessYear    int                     `json:"business_year"`
	ReportCode      string                  `json:"report_code"`
	Status          models.DataStatus       `json:"status"`
	Queries         int                     `json:"queries"`
	Statements      int                     `json:"statements"`
	Accounts        int                     `json:"accounts"`
	Inserted        int                     `json:"inserted"`
	VersionsChanged int                     `json:"versions_changed"`
	Items           []dartFinancialSyncItem `json:"items"`
	Issues          []outputIssue           `json:"issues,omitempty"`
}

type dartFinancialStatementView struct {
	models.DARTFinancialStatement
	AccountsTotal int `json:"accounts_total"`
}

type dartFinancialListResult struct {
	Ticker       string                       `json:"ticker,omitempty"`
	CorpCode     string                       `json:"corp_code"`
	BusinessYear int                          `json:"business_year"`
	ReportCode   string                       `json:"report_code"`
	FSKind       string                       `json:"fs_kind"`
	Versions     []dartFinancialStatementView `json:"versions"`
}

func runDARTFinancialSync(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("dart-financial-sync", flag.ContinueOnError)
	flags.SetOutput(stderr)
	databasePath := flags.String("db", defaultDatabasePath, "SQLite database path")
	ticker := flags.String("ticker", "", "optional stored instrument ticker")
	corpCode := flags.String("corp-code", "", "optional OpenDART corporation code")
	businessYear := flags.Int("year", 0, "business year, 2015 or later")
	reportCode := flags.String(
		"report-code",
		dart.DARTReportCodeAnnual,
		"11011 annual, 11012 half-year, 11013 Q1, 11014 Q3",
	)
	fsKind := flags.String("fs-div", "CFS", "CFS, OFS, or both")
	outputFormat := flags.String("output", "text", "output format: text or json")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if exitCode := validateCommandOutput(outputFormat, stdout, stderr); exitCode != 0 {
		return exitCode
	}

	*ticker = strings.TrimSpace(*ticker)
	*corpCode = strings.TrimSpace(*corpCode)
	*reportCode = strings.TrimSpace(*reportCode)
	*fsKind = strings.ToUpper(strings.TrimSpace(*fsKind))
	switch {
	case *ticker != "" && *corpCode != "":
		return commandInputError(
			*outputFormat,
			stdout,
			stderr,
			"ticker and corp-code cannot be used together",
		)
	case *businessYear < 2015 ||
		*businessYear > dartFinancialNow().UTC().Year():
		return commandInputError(
			*outputFormat,
			stdout,
			stderr,
			fmt.Sprintf(
				"year must be between 2015 and %d",
				dartFinancialNow().UTC().Year(),
			),
		)
	case !validFinancialReportCode(*reportCode):
		return commandInputError(
			*outputFormat,
			stdout,
			stderr,
			"report-code must be 11011, 11012, 11013, or 11014",
		)
	case *fsKind != "CFS" && *fsKind != "OFS" && *fsKind != "BOTH":
		return commandInputError(
			*outputFormat,
			stdout,
			stderr,
			"fs-div must be CFS, OFS, or both",
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
		dartFinancialSyncTimeout,
	)
	defer cancel()
	store, err := sqlitestore.Open(ctx, *databasePath)
	if err != nil {
		return writeRuntimeFailure(*outputFormat, stdout, stderr, "storage", err)
	}
	defer store.Close()
	instruments, err := financialSyncInstruments(
		ctx,
		store,
		*ticker,
		*corpCode,
	)
	if err != nil {
		return writeRuntimeFailure(
			*outputFormat,
			stdout,
			stderr,
			"financial_instruments",
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

	fsKinds := []string{*fsKind}
	if *fsKind == "BOTH" {
		fsKinds = []string{
			dart.DARTFinancialStatementConsolidated,
			dart.DARTFinancialStatementSeparate,
		}
	}
	result := dartFinancialSyncCommandResult{
		Database:     *databasePath,
		BusinessYear: *businessYear,
		ReportCode:   *reportCode,
		Status:       models.DataStatusUnavailable,
		Queries:      len(instruments) * len(fsKinds),
		Items:        make([]dartFinancialSyncItem, 0, len(instruments)*len(fsKinds)),
		Issues:       []outputIssue{},
	}

	client := newDARTFinancialClient(settings.OpenDARTAPIKey)
	completedQueries := 0
	for _, instrument := range instruments {
		for _, queryFSKind := range fsKinds {
			financialResult, fetchErr := client.FullFinancialStatement(
				ctx,
				instrument.DARTCorpCode,
				*businessYear,
				*reportCode,
				queryFSKind,
			)
			item := dartFinancialSyncItem{
				Ticker:   instrument.Ticker,
				Name:     instrument.Name,
				CorpCode: instrument.DARTCorpCode,
				FSKind:   queryFSKind,
				Status:   financialResult.Status,
				Source:   financialResult.Statement.Source,
			}
			if fetchErr != nil {
				item.Status = models.DataStatusUnavailable
				result.Issues = append(result.Issues, outputIssue{
					Scope: "dart_financial_" +
						instrument.DARTCorpCode + "_" + queryFSKind,
					Kind:    string(provider.KindOf(fetchErr)),
					Message: fetchErr.Error(),
				})
				result.Items = append(result.Items, item)
				continue
			}
			completedQueries++
			if financialResult.Status == models.DataStatusEmpty {
				result.Items = append(result.Items, item)
				continue
			}

			syncResult, syncErr := store.SyncDARTFinancialStatement(
				ctx,
				financialResult.Statement,
			)
			if syncErr != nil {
				completedQueries--
				item.Status = models.DataStatusUnavailable
				result.Issues = append(result.Issues, outputIssue{
					Scope: "dart_financial_storage_" +
						instrument.DARTCorpCode + "_" + queryFSKind,
					Kind:    "operation_failed",
					Message: syncErr.Error(),
				})
				result.Items = append(result.Items, item)
				continue
			}

			item.ReceiptNo = financialResult.Statement.ReceiptNo
			item.ContentSHA256 = financialResult.Statement.ContentSHA256
			item.Accounts = len(financialResult.Statement.Accounts)
			item.Inserted = syncResult.Inserted
			item.VersionChanged = syncResult.VersionChanged
			result.Statements++
			result.Accounts += item.Accounts
			if item.Inserted {
				result.Inserted++
			}
			if item.VersionChanged {
				result.VersionsChanged++
			}
			result.Items = append(result.Items, item)
		}
	}

	switch {
	case completedQueries == result.Queries && result.Statements == 0:
		result.Status = models.DataStatusEmpty
	case completedQueries == result.Queries:
		result.Status = models.DataStatusAvailable
	case completedQueries > 0:
		result.Status = models.DataStatusPartial
	}
	return writeDARTFinancialSyncResult(
		*outputFormat,
		stdout,
		stderr,
		result,
		completedQueries > 0,
	)
}

func financialSyncInstruments(
	ctx context.Context,
	store *sqlitestore.Store,
	ticker string,
	corpCode string,
) ([]models.Instrument, error) {
	if corpCode != "" {
		if len(corpCode) != 8 {
			return nil, fmt.Errorf(
				"invalid OpenDART corporation code %q",
				corpCode,
			)
		}
		for _, character := range corpCode {
			if character < '0' || character > '9' {
				return nil, fmt.Errorf(
					"invalid OpenDART corporation code %q",
					corpCode,
				)
			}
		}
		return []models.Instrument{{DARTCorpCode: corpCode}}, nil
	}
	return disclosureSyncInstruments(ctx, store, ticker)
}

func runDARTFinancialList(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("dart-financial-list", flag.ContinueOnError)
	flags.SetOutput(stderr)
	databasePath := flags.String("db", defaultDatabasePath, "SQLite database path")
	ticker := flags.String("ticker", "", "stored instrument ticker")
	corpCode := flags.String("corp-code", "", "OpenDART corporation code")
	businessYear := flags.Int("year", 0, "business year")
	reportCode := flags.String(
		"report-code",
		dart.DARTReportCodeAnnual,
		"OpenDART report code",
	)
	fsKind := flags.String("fs-div", "CFS", "CFS or OFS")
	versionLimit := flags.Int("versions", 1, "maximum versions to return")
	accountLimit := flags.Int("account-limit", 1000, "maximum accounts per version")
	outputFormat := flags.String("output", "text", "output format: text or json")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if exitCode := validateCommandOutput(outputFormat, stdout, stderr); exitCode != 0 {
		return exitCode
	}

	*ticker = strings.TrimSpace(*ticker)
	*corpCode = strings.TrimSpace(*corpCode)
	*reportCode = strings.TrimSpace(*reportCode)
	*fsKind = strings.ToUpper(strings.TrimSpace(*fsKind))
	switch {
	case (*ticker == "" && *corpCode == "") ||
		(*ticker != "" && *corpCode != ""):
		return commandInputError(
			*outputFormat,
			stdout,
			stderr,
			"provide exactly one of ticker or corp-code",
		)
	case *businessYear < 2015 || *businessYear > 9999:
		return commandInputError(
			*outputFormat,
			stdout,
			stderr,
			"year must be between 2015 and 9999",
		)
	case !validFinancialReportCode(*reportCode):
		return commandInputError(
			*outputFormat,
			stdout,
			stderr,
			"report-code must be 11011, 11012, 11013, or 11014",
		)
	case *fsKind != "CFS" && *fsKind != "OFS":
		return commandInputError(
			*outputFormat,
			stdout,
			stderr,
			"fs-div must be CFS or OFS",
		)
	case *versionLimit <= 0 || *versionLimit > 100:
		return commandInputError(
			*outputFormat,
			stdout,
			stderr,
			"versions must be between 1 and 100",
		)
	case *accountLimit <= 0 || *accountLimit > 10000:
		return commandInputError(
			*outputFormat,
			stdout,
			stderr,
			"account-limit must be between 1 and 10000",
		)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
				"financial_instrument",
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

	statements, err := store.ListDARTFinancialStatementVersions(
		ctx,
		*corpCode,
		*businessYear,
		*reportCode,
		*fsKind,
		*versionLimit,
	)
	if err != nil {
		return writeRuntimeFailure(
			*outputFormat,
			stdout,
			stderr,
			"dart_financial_storage",
			err,
		)
	}
	result := dartFinancialListResult{
		Ticker:       *ticker,
		CorpCode:     *corpCode,
		BusinessYear: *businessYear,
		ReportCode:   *reportCode,
		FSKind:       *fsKind,
		Versions:     make([]dartFinancialStatementView, 0, len(statements)),
	}
	for _, statement := range statements {
		total := len(statement.Accounts)
		if len(statement.Accounts) > *accountLimit {
			statement.Accounts = statement.Accounts[:*accountLimit]
		}
		result.Versions = append(result.Versions, dartFinancialStatementView{
			DARTFinancialStatement: statement,
			AccountsTotal:          total,
		})
	}
	if *outputFormat == "json" {
		if err := writeJSON(stdout, result); err != nil {
			return writeRuntimeFailure("text", stdout, stderr, "output", err)
		}
		return 0
	}

	fmt.Fprintf(
		stdout,
		"OpenDART financial statements: corp_code=%s year=%d report=%s fs_div=%s versions=%d\n",
		result.CorpCode,
		result.BusinessYear,
		result.ReportCode,
		result.FSKind,
		len(result.Versions),
	)
	for _, version := range result.Versions {
		fmt.Fprintf(
			stdout,
			"- current=%t receipt=%s content_sha256=%s accounts=%d/%d\n",
			version.IsCurrent,
			version.ReceiptNo,
			version.ContentSHA256,
			len(version.Accounts),
			version.AccountsTotal,
		)
		for _, account := range version.Accounts {
			fmt.Fprintf(
				stdout,
				"  [%s/%d] %s: current=%s cumulative=%s %s\n",
				account.StatementKind,
				account.Order,
				account.AccountName,
				valueOrNA(account.CurrentAmount),
				valueOrNA(account.CurrentAddAmount),
				account.Currency,
			)
		}
	}
	return 0
}

func validFinancialReportCode(value string) bool {
	switch value {
	case dart.DARTReportCodeAnnual,
		dart.DARTReportCodeHalfYear,
		dart.DARTReportCodeFirstQuarter,
		dart.DARTReportCodeThirdQuarter:
		return true
	default:
		return false
	}
}

func writeDARTFinancialSyncResult(
	outputFormat string,
	stdout io.Writer,
	stderr io.Writer,
	result dartFinancialSyncCommandResult,
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
			"OpenDART financial sync: status=%s year=%d report=%s queries=%d statements=%d accounts=%d inserted=%d changed=%d\n",
			result.Status,
			result.BusinessYear,
			result.ReportCode,
			result.Queries,
			result.Statements,
			result.Accounts,
			result.Inserted,
			result.VersionsChanged,
		)
		for _, item := range result.Items {
			label := item.CorpCode
			if item.Ticker != "" {
				label = item.Ticker + "/" + item.CorpCode
			}
			fmt.Fprintf(
				stdout,
				"- %s %s: status=%s receipt=%s accounts=%d inserted=%t changed=%t\n",
				label,
				item.FSKind,
				item.Status,
				valueOrNA(item.ReceiptNo),
				item.Accounts,
				item.Inserted,
				item.VersionChanged,
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
