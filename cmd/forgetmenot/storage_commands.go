package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/decimal"
	"github.com/rowlet9g/stock-research-bot/internal/mirae"
	"github.com/rowlet9g/stock-research-bot/internal/models"
	sqlitestore "github.com/rowlet9g/stock-research-bot/internal/storage/sqlite"
	"github.com/rowlet9g/stock-research-bot/internal/watchlist"
	"github.com/rowlet9g/stock-research-bot/internal/yahoo"
)

const defaultDatabasePath = "data/forgetmenot.db"

func runDBInit(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("db-init", flag.ContinueOnError)
	flags.SetOutput(stderr)
	databasePath := flags.String("db", defaultDatabasePath, "SQLite database path")
	outputFormat := flags.String("output", "text", "output format: text or json")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if exitCode := validateCommandOutput(outputFormat, stdout, stderr); exitCode != 0 {
		return exitCode
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	store, err := sqlitestore.Open(ctx, *databasePath)
	if err != nil {
		return writeRuntimeFailure(*outputFormat, stdout, stderr, "storage", err)
	}
	defer store.Close()

	result := struct {
		Database string `json:"database"`
		Status   string `json:"status"`
	}{Database: *databasePath, Status: "ready"}
	return writeCommandResult(
		*outputFormat,
		stdout,
		fmt.Sprintf("Database ready: %s\n", *databasePath),
		result,
	)
}

func runWatchlistSync(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("watchlist-sync", flag.ContinueOnError)
	flags.SetOutput(stderr)
	databasePath := flags.String("db", defaultDatabasePath, "SQLite database path")
	watchlistPath := flags.String("watchlist", "data/watchlist.example.csv", "watchlist CSV path")
	outputFormat := flags.String("output", "text", "output format: text or json")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if exitCode := validateCommandOutput(outputFormat, stdout, stderr); exitCode != 0 {
		return exitCode
	}

	items, err := watchlist.Load(*watchlistPath)
	if err != nil {
		return writeFailure(*outputFormat, stdout, stderr, outputIssue{
			Scope:   "watchlist",
			Kind:    "invalid_input",
			Message: err.Error(),
		})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	store, err := sqlitestore.Open(ctx, *databasePath)
	if err != nil {
		return writeRuntimeFailure(*outputFormat, stdout, stderr, "storage", err)
	}
	defer store.Close()

	count, err := store.SyncInstruments(ctx, items)
	if err != nil {
		return writeRuntimeFailure(*outputFormat, stdout, stderr, "storage", err)
	}
	result := struct {
		Database string `json:"database"`
		Source   string `json:"source"`
		Synced   int    `json:"synced"`
	}{Database: *databasePath, Source: *watchlistPath, Synced: count}
	return writeCommandResult(
		*outputFormat,
		stdout,
		fmt.Sprintf("Synced %d instruments into %s\n", count, *databasePath),
		result,
	)
}

func runWatchlistList(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("watchlist-list", flag.ContinueOnError)
	flags.SetOutput(stderr)
	databasePath := flags.String("db", defaultDatabasePath, "SQLite database path")
	outputFormat := flags.String("output", "text", "output format: text or json")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if exitCode := validateCommandOutput(outputFormat, stdout, stderr); exitCode != 0 {
		return exitCode
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	store, err := sqlitestore.Open(ctx, *databasePath)
	if err != nil {
		return writeRuntimeFailure(*outputFormat, stdout, stderr, "storage", err)
	}
	defer store.Close()

	instruments, err := store.ListInstruments(ctx)
	if err != nil {
		return writeRuntimeFailure(*outputFormat, stdout, stderr, "storage", err)
	}
	if *outputFormat == "json" {
		if err := writeJSON(stdout, instruments); err != nil {
			return writeRuntimeFailure("text", stdout, stderr, "output", err)
		}
		return 0
	}
	if len(instruments) == 0 {
		fmt.Fprintln(stdout, "No instruments")
		return 0
	}
	for _, instrument := range instruments {
		fmt.Fprintf(
			stdout,
			"%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			instrument.Ticker,
			instrument.Name,
			instrument.Market,
			instrument.Currency,
			instrument.YahooTicker,
			instrument.InstrumentType,
			valueOrNA(instrument.KRXStandardCode),
		)
	}
	return 0
}

func runWatchlistDelete(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("watchlist-delete", flag.ContinueOnError)
	flags.SetOutput(stderr)
	databasePath := flags.String("db", defaultDatabasePath, "SQLite database path")
	ticker := flags.String("ticker", "", "ticker to delete")
	outputFormat := flags.String("output", "text", "output format: text or json")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if exitCode := validateCommandOutput(outputFormat, stdout, stderr); exitCode != 0 {
		return exitCode
	}
	if strings.TrimSpace(*ticker) == "" {
		return writeFailure(*outputFormat, stdout, stderr, outputIssue{
			Scope: "input", Kind: "invalid_input", Message: "ticker is required",
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	store, err := sqlitestore.Open(ctx, *databasePath)
	if err != nil {
		return writeRuntimeFailure(*outputFormat, stdout, stderr, "storage", err)
	}
	defer store.Close()

	deleted, err := store.DeleteInstrument(ctx, *ticker)
	if err != nil {
		return writeRuntimeFailure(*outputFormat, stdout, stderr, "storage", err)
	}
	result := struct {
		Ticker  string `json:"ticker"`
		Deleted bool   `json:"deleted"`
	}{Ticker: *ticker, Deleted: deleted}
	return writeCommandResult(
		*outputFormat,
		stdout,
		fmt.Sprintf("Deleted %s: %t\n", *ticker, deleted),
		result,
	)
}

func runTradesImport(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("trades-import", flag.ContinueOnError)
	flags.SetOutput(stderr)
	databasePath := flags.String("db", defaultDatabasePath, "SQLite database path")
	filePath := flags.String("file", "", "normalized trade CSV path")
	source := flags.String("source", "mirae-normalized", "trade source identifier")
	outputFormat := flags.String("output", "text", "output format: text or json")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if exitCode := validateCommandOutput(outputFormat, stdout, stderr); exitCode != 0 {
		return exitCode
	}
	if strings.TrimSpace(*filePath) == "" {
		return writeFailure(*outputFormat, stdout, stderr, outputIssue{
			Scope: "input", Kind: "invalid_input", Message: "trade CSV file is required",
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := sqlitestore.Open(ctx, *databasePath)
	if err != nil {
		return writeRuntimeFailure(*outputFormat, stdout, stderr, "storage", err)
	}
	defer store.Close()

	result, err := store.ImportTradeCSV(ctx, *filePath, *source)
	if err != nil {
		return writeRuntimeFailure(*outputFormat, stdout, stderr, "trade_import", err)
	}
	text := fmt.Sprintf(
		"Trade import: seen=%d inserted=%d duplicated=%d already_imported=%t\n",
		result.RowsSeen,
		result.RowsInserted,
		result.RowsDuplicated,
		result.AlreadyImported,
	)
	return writeCommandResult(*outputFormat, stdout, text, result)
}

type miraeInstrumentResolver interface {
	ResolveInstrument(
		context.Context,
		string,
		string,
	) (yahoo.InstrumentResolution, error)
}

type miraeImportResult struct {
	Database       string                        `json:"database"`
	File           string                        `json:"file"`
	LedgerRows     int                           `json:"ledger_rows"`
	TradeRows      int                           `json:"trade_rows"`
	SkippedRows    int                           `json:"skipped_rows"`
	ResolvedCache  int                           `json:"resolved_cache"`
	ResolvedOnline int                           `json:"resolved_online"`
	Warnings       []string                      `json:"warnings"`
	Import         sqlitestore.TradeImportResult `json:"import"`
}

func runMiraeImport(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("mirae-import", flag.ContinueOnError)
	flags.SetOutput(stderr)
	databasePath := flags.String("db", defaultDatabasePath, "SQLite database path")
	filePath := flags.String("file", "", "Mirae Asset transaction XLSX path")
	aliasPath := flags.String(
		"aliases",
		"data/cache/mirae_instrument_aliases.csv",
		"verified instrument alias CSV path",
	)
	source := flags.String("source", "mirae-ledger", "trade source identifier")
	outputFormat := flags.String("output", "text", "output format: text or json")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if exitCode := validateCommandOutput(outputFormat, stdout, stderr); exitCode != 0 {
		return exitCode
	}
	if strings.TrimSpace(*filePath) == "" {
		return writeFailure(*outputFormat, stdout, stderr, outputIssue{
			Scope: "input", Kind: "invalid_input", Message: "Mirae Asset XLSX file is required",
		})
	}

	ledger, err := mirae.LoadXLSX(*filePath)
	if err != nil {
		return writeFailure(*outputFormat, stdout, stderr, outputIssue{
			Scope: "mirae", Kind: "invalid_input", Message: err.Error(),
		})
	}
	aliases, aliasesFound, err := mirae.LoadInstrumentAliases(*aliasPath)
	if err != nil {
		return writeFailure(*outputFormat, stdout, stderr, outputIssue{
			Scope: "mirae", Kind: "invalid_input", Message: err.Error(),
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	store, err := sqlitestore.Open(ctx, *databasePath)
	if err != nil {
		return writeRuntimeFailure(*outputFormat, stdout, stderr, "storage", err)
	}
	defer store.Close()

	rows, resolvedCache, resolvedOnline, resolutionWarnings, err := resolveMiraeTradeRows(
		ctx,
		store,
		yahoo.NewClient(),
		ledger.Trades,
		aliases,
	)
	if err != nil {
		return writeRuntimeFailure(*outputFormat, stdout, stderr, "instrument_resolution", err)
	}
	importResult, err := store.ImportTradeRows(ctx, *source, ledger.FileSHA256, rows)
	if err != nil {
		return writeRuntimeFailure(*outputFormat, stdout, stderr, "trade_import", err)
	}
	result := miraeImportResult{
		Database:       *databasePath,
		File:           *filePath,
		LedgerRows:     ledger.RowsSeen,
		TradeRows:      len(ledger.Trades),
		SkippedRows:    ledger.RowsSkipped,
		ResolvedCache:  resolvedCache,
		ResolvedOnline: resolvedOnline,
		Warnings:       append(append([]string{}, ledger.Warnings...), resolutionWarnings...),
		Import:         importResult,
	}
	text := fmt.Sprintf(
		"Mirae import: ledger=%d trades=%d skipped=%d resolved_cache=%d resolved_online=%d inserted=%d duplicated=%d already_imported=%t\n",
		result.LedgerRows,
		result.TradeRows,
		result.SkippedRows,
		result.ResolvedCache,
		result.ResolvedOnline,
		result.Import.RowsInserted,
		result.Import.RowsDuplicated,
		result.Import.AlreadyImported,
	)
	if !aliasesFound {
		result.Warnings = append(
			result.Warnings,
			fmt.Sprintf("instrument alias cache %q was not found", *aliasPath),
		)
	}
	return writeCommandResult(*outputFormat, stdout, text, result)
}

func resolveMiraeTradeRows(
	ctx context.Context,
	store *sqlitestore.Store,
	resolver miraeInstrumentResolver,
	trades []mirae.Trade,
	aliases []mirae.InstrumentAlias,
) ([]sqlitestore.TradeImportRow, int, int, []string, error) {
	instruments, err := store.ListInstruments(ctx)
	if err != nil {
		return nil, 0, 0, nil, err
	}
	stored := map[string][]models.Instrument{}
	for _, instrument := range instruments {
		key := miraeInstrumentKey(instrument.Name, instrument.Currency)
		stored[key] = append(stored[key], instrument)
	}
	aliasIndex := map[string]mirae.InstrumentAlias{}
	for _, alias := range aliases {
		key := miraeInstrumentKey(alias.SourceName, alias.Instrument.Currency)
		if _, exists := aliasIndex[key]; exists {
			return nil, 0, 0, nil, fmt.Errorf(
				"multiple aliases match %q",
				alias.SourceName,
			)
		}
		aliasIndex[key] = alias
	}

	tickers := map[string]string{}
	newItems := []models.WatchlistItem{}
	warnings := []string{}
	resolvedCache := 0
	resolvedOnline := 0
	for _, trade := range trades {
		key := miraeInstrumentKey(trade.InstrumentName, trade.Currency)
		if _, exists := tickers[key]; exists {
			continue
		}
		matches := stored[key]
		if len(matches) > 1 {
			return nil, 0, 0, nil, fmt.Errorf(
				"multiple stored instruments match %q",
				trade.InstrumentName,
			)
		}
		if len(matches) == 1 {
			tickers[key] = matches[0].Ticker
			continue
		}
		if alias, exists := aliasIndex[key]; exists {
			tickers[key] = alias.Instrument.Ticker
			newItems = append(newItems, alias.Instrument)
			resolvedCache++
			continue
		}

		resolution, err := resolver.ResolveInstrument(
			ctx,
			trade.InstrumentName,
			trade.Currency,
		)
		if err != nil {
			return nil, 0, 0, nil, fmt.Errorf(
				"resolve instrument %q: %w",
				trade.InstrumentName,
				err,
			)
		}
		tickers[key] = resolution.Instrument.Ticker
		newItems = append(newItems, resolution.Instrument)
		resolvedOnline++
		if resolution.MatchKind == "provider_rank" {
			warnings = append(warnings, fmt.Sprintf(
				"instrument %q resolved by Yahoo provider ranking to %s",
				trade.InstrumentName,
				resolution.Instrument.YahooTicker,
			))
		}
	}
	if len(newItems) > 0 {
		if _, err := store.SyncInstruments(ctx, newItems); err != nil {
			return nil, 0, 0, nil, fmt.Errorf("store resolved instruments: %w", err)
		}
	}

	rows := make([]sqlitestore.TradeImportRow, 0, len(trades))
	for _, trade := range trades {
		key := miraeInstrumentKey(trade.InstrumentName, trade.Currency)
		rows = append(rows, sqlitestore.TradeImportRow{
			RowNumber: trade.RowNumber,
			Ticker:    tickers[key],
			Input: sqlitestore.TradeInput{
				ExternalID:    trade.ExternalID,
				TradeDate:     trade.TradeDate,
				Action:        trade.Action,
				QuantityUnits: trade.QuantityUnits,
				PriceUnits:    trade.PriceUnits,
				FeesUnits:     trade.FeesUnits,
				TaxesUnits:    trade.TaxesUnits,
				PriceSource:   trade.PriceSource,
				TaxesKnown:    trade.TaxesKnown,
				TimePrecision: trade.TimePrecision,
				Currency:      trade.Currency,
			},
		})
	}
	return rows, resolvedCache, resolvedOnline, warnings, nil
}

func miraeInstrumentKey(name string, currency string) string {
	return strings.ToUpper(strings.TrimSpace(currency)) +
		"\x1f" +
		mirae.NormalizeInstrumentName(name)
}

func runPositionSet(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("position-set", flag.ContinueOnError)
	flags.SetOutput(stderr)
	databasePath := flags.String("db", defaultDatabasePath, "SQLite database path")
	ticker := flags.String("ticker", "", "instrument ticker")
	quantity := flags.String("quantity", "", "position quantity")
	averageCost := flags.String("average-cost", "", "average unit cost")
	currency := flags.String("currency", "", "position currency")
	asOfText := flags.String("as-of", "", "position date in YYYY-MM-DD or RFC3339")
	outputFormat := flags.String("output", "text", "output format: text or json")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if exitCode := validateCommandOutput(outputFormat, stdout, stderr); exitCode != 0 {
		return exitCode
	}
	if strings.TrimSpace(*ticker) == "" {
		return commandInputError(*outputFormat, stdout, stderr, "ticker is required")
	}
	quantityUnits, err := decimal.Parse(*quantity)
	if err != nil {
		return commandInputError(*outputFormat, stdout, stderr, fmt.Sprintf("invalid quantity: %v", err))
	}
	averageCostUnits, err := decimal.Parse(*averageCost)
	if err != nil {
		return commandInputError(*outputFormat, stdout, stderr, fmt.Sprintf("invalid average cost: %v", err))
	}
	asOf, err := parseCommandTime(*asOfText)
	if err != nil {
		return commandInputError(*outputFormat, stdout, stderr, err.Error())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	store, err := sqlitestore.Open(ctx, *databasePath)
	if err != nil {
		return writeRuntimeFailure(*outputFormat, stdout, stderr, "storage", err)
	}
	defer store.Close()

	position, err := store.UpsertPosition(
		ctx,
		*ticker,
		quantityUnits,
		averageCostUnits,
		*currency,
		asOf,
	)
	if err != nil {
		return writeRuntimeFailure(*outputFormat, stdout, stderr, "storage", err)
	}
	view := newPositionView(position)
	return writeCommandResult(
		*outputFormat,
		stdout,
		fmt.Sprintf(
			"Position saved: %s quantity=%s average_cost=%s %s\n",
			*ticker,
			view.Quantity,
			view.AverageCost,
			view.Currency,
		),
		view,
	)
}

func runThesisSet(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("thesis-set", flag.ContinueOnError)
	flags.SetOutput(stderr)
	databasePath := flags.String("db", defaultDatabasePath, "SQLite database path")
	ticker := flags.String("ticker", "", "instrument ticker")
	summary := flags.String("summary", "", "investment thesis")
	invalidation := flags.String("invalidation", "", "thesis invalidation condition")
	horizon := flags.String("horizon", "", "expected holding period")
	metricsText := flags.String("metrics", "", "comma-separated check metrics")
	outputFormat := flags.String("output", "text", "output format: text or json")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if exitCode := validateCommandOutput(outputFormat, stdout, stderr); exitCode != 0 {
		return exitCode
	}
	if strings.TrimSpace(*ticker) == "" {
		return commandInputError(*outputFormat, stdout, stderr, "ticker is required")
	}
	if strings.TrimSpace(*summary) == "" {
		return commandInputError(*outputFormat, stdout, stderr, "thesis summary is required")
	}

	metrics := []string{}
	if strings.TrimSpace(*metricsText) != "" {
		metrics = strings.Split(*metricsText, ",")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	store, err := sqlitestore.Open(ctx, *databasePath)
	if err != nil {
		return writeRuntimeFailure(*outputFormat, stdout, stderr, "storage", err)
	}
	defer store.Close()

	thesis, err := store.UpsertThesis(
		ctx,
		*ticker,
		*summary,
		*invalidation,
		*horizon,
		metrics,
	)
	if err != nil {
		return writeRuntimeFailure(*outputFormat, stdout, stderr, "storage", err)
	}
	return writeCommandResult(
		*outputFormat,
		stdout,
		fmt.Sprintf("Thesis saved: %s\n", *ticker),
		thesis,
	)
}

func runPortfolioShow(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("portfolio-show", flag.ContinueOnError)
	flags.SetOutput(stderr)
	databasePath := flags.String("db", defaultDatabasePath, "SQLite database path")
	ticker := flags.String("ticker", "", "instrument ticker")
	outputFormat := flags.String("output", "text", "output format: text or json")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if exitCode := validateCommandOutput(outputFormat, stdout, stderr); exitCode != 0 {
		return exitCode
	}
	if strings.TrimSpace(*ticker) == "" {
		return commandInputError(*outputFormat, stdout, stderr, "ticker is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	store, err := sqlitestore.Open(ctx, *databasePath)
	if err != nil {
		return writeRuntimeFailure(*outputFormat, stdout, stderr, "storage", err)
	}
	defer store.Close()

	portfolio, err := store.Portfolio(ctx, *ticker)
	if err != nil {
		return writeRuntimeFailure(*outputFormat, stdout, stderr, "storage", err)
	}
	view := newPortfolioView(portfolio)
	if *outputFormat == "json" {
		if err := writeJSON(stdout, view); err != nil {
			return writeRuntimeFailure("text", stdout, stderr, "output", err)
		}
		return 0
	}

	fmt.Fprintf(
		stdout,
		"%s (%s / %s)\n",
		view.Instrument.Name,
		view.Instrument.Ticker,
		view.Instrument.Market,
	)
	if view.Position == nil {
		fmt.Fprintln(stdout, "Position: N/A")
	} else {
		fmt.Fprintf(
			stdout,
			"Position: %s @ %s %s (as of %s)\n",
			view.Position.Quantity,
			view.Position.AverageCost,
			view.Position.Currency,
			view.Position.AsOf.Format(time.RFC3339),
		)
	}
	fmt.Fprintf(stdout, "Trades: %d\n", len(view.Trades))
	if view.Thesis == nil {
		fmt.Fprintln(stdout, "Thesis: N/A")
	} else {
		fmt.Fprintf(stdout, "Thesis: %s\n", view.Thesis.Summary)
		fmt.Fprintf(stdout, "Invalidation: %s\n", valueOrNA(view.Thesis.InvalidationCondition))
		fmt.Fprintf(stdout, "Horizon: %s\n", valueOrNA(view.Thesis.ExpectedHoldingPeriod))
		fmt.Fprintf(stdout, "Check metrics: %s\n", strings.Join(view.Thesis.CheckMetrics, ", "))
	}
	return 0
}

type positionView struct {
	Quantity    string    `json:"quantity"`
	AverageCost string    `json:"average_cost"`
	Currency    string    `json:"currency"`
	AsOf        time.Time `json:"as_of"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type tradeView struct {
	ID            int64     `json:"id"`
	ExternalID    string    `json:"external_id,omitempty"`
	TradeDate     time.Time `json:"trade_date"`
	Action        string    `json:"action"`
	Quantity      string    `json:"quantity"`
	Price         string    `json:"price"`
	Fees          string    `json:"fees"`
	Taxes         string    `json:"taxes"`
	PriceSource   string    `json:"price_source"`
	TaxesKnown    bool      `json:"taxes_known"`
	TimePrecision string    `json:"time_precision"`
	Currency      string    `json:"currency"`
	Source        string    `json:"source"`
}

type portfolioView struct {
	Instrument models.Instrument `json:"instrument"`
	Position   *positionView     `json:"position,omitempty"`
	Trades     []tradeView       `json:"trades"`
	Thesis     *models.Thesis    `json:"thesis,omitempty"`
}

func newPositionView(position models.Position) positionView {
	return positionView{
		Quantity:    decimal.Format(position.QuantityUnits),
		AverageCost: decimal.Format(position.AverageCostUnits),
		Currency:    position.Currency,
		AsOf:        position.AsOf,
		UpdatedAt:   position.UpdatedAt,
	}
}

func newPortfolioView(record models.PortfolioRecord) portfolioView {
	view := portfolioView{
		Instrument: record.Instrument,
		Trades:     make([]tradeView, 0, len(record.Trades)),
		Thesis:     record.Thesis,
	}
	if record.Position != nil {
		position := newPositionView(*record.Position)
		view.Position = &position
	}
	for _, trade := range record.Trades {
		view.Trades = append(view.Trades, tradeView{
			ID:            trade.ID,
			ExternalID:    trade.ExternalID,
			TradeDate:     trade.TradeDate,
			Action:        trade.Action,
			Quantity:      decimal.Format(trade.QuantityUnits),
			Price:         decimal.Format(trade.PriceUnits),
			Fees:          decimal.Format(trade.FeesUnits),
			Taxes:         decimal.Format(trade.TaxesUnits),
			PriceSource:   trade.PriceSource,
			TaxesKnown:    trade.TaxesKnown,
			TimePrecision: trade.TimePrecision,
			Currency:      trade.Currency,
			Source:        trade.Source,
		})
	}
	return view
}

func validateCommandOutput(outputFormat *string, stdout io.Writer, stderr io.Writer) int {
	*outputFormat = strings.ToLower(strings.TrimSpace(*outputFormat))
	if *outputFormat == "text" || *outputFormat == "json" {
		return 0
	}
	return writeFailure(*outputFormat, stdout, stderr, outputIssue{
		Scope: "input", Kind: "invalid_input", Message: `output must be "text" or "json"`,
	})
}

func commandInputError(outputFormat string, stdout io.Writer, stderr io.Writer, message string) int {
	return writeFailure(outputFormat, stdout, stderr, outputIssue{
		Scope: "input", Kind: "invalid_input", Message: message,
	})
}

func writeRuntimeFailure(
	outputFormat string,
	stdout io.Writer,
	stderr io.Writer,
	scope string,
	err error,
) int {
	issue := outputIssue{Scope: scope, Kind: "operation_failed", Message: err.Error()}
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

func writeCommandResult(outputFormat string, output io.Writer, text string, value any) int {
	if outputFormat == "json" {
		if err := writeJSON(output, value); err != nil {
			return 1
		}
		return 0
	}
	fmt.Fprint(output, text)
	return 0
}

func parseCommandTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	for _, format := range []string{"2006-01-02", time.RFC3339} {
		if parsed, err := time.Parse(format, value); err == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("as-of must be YYYY-MM-DD or RFC3339")
}

func writeCommandHelp(output io.Writer) {
	fmt.Fprintln(output, "Commands:")
	fmt.Fprintln(output, "  db-init          Create or migrate the SQLite database")
	fmt.Fprintln(output, "  watchlist-sync   Upsert instruments from the watchlist CSV")
	fmt.Fprintln(output, "  watchlist-list   List stored instruments")
	fmt.Fprintln(output, "  watchlist-delete Delete an instrument without trade history")
	fmt.Fprintln(output, "  trades-import    Import the normalized trade CSV")
	fmt.Fprintln(output, "  mirae-import     Import the Mirae Asset transaction XLSX")
	fmt.Fprintln(output, "  dart-corp-sync   Sync OpenDART corporation codes and map instruments")
	fmt.Fprintln(output, "  krx-instrument-sync Sync KRX instrument identifiers and classifications")
	fmt.Fprintln(output, "  position-set     Store the current position snapshot")
	fmt.Fprintln(output, "  thesis-set       Store the current investment thesis")
	fmt.Fprintln(output, "  portfolio-show   Show instrument, position, trades, and thesis")
	fmt.Fprintln(output, "")
	fmt.Fprintln(output, "Without a command, the existing market analysis flow runs.")
}
