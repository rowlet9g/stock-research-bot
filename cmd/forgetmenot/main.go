package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/analysis"
	"github.com/rowlet9g/stock-research-bot/internal/config"
	"github.com/rowlet9g/stock-research-bot/internal/dart"
	"github.com/rowlet9g/stock-research-bot/internal/models"
	"github.com/rowlet9g/stock-research-bot/internal/prompt"
	"github.com/rowlet9g/stock-research-bot/internal/provider"
	sqlitestore "github.com/rowlet9g/stock-research-bot/internal/storage/sqlite"
	"github.com/rowlet9g/stock-research-bot/internal/watchlist"
	"github.com/rowlet9g/stock-research-bot/internal/yahoo"
)

const providerRequestTimeout = 12 * time.Second

type outputIssue struct {
	Scope   string `json:"scope"`
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

type cliResult struct {
	Selected    models.WatchlistItem  `json:"selected"`
	Price       models.PriceSnapshot  `json:"price"`
	Signals     []analysis.Signal     `json:"signals"`
	Disclosures dart.DisclosureResult `json:"disclosures"`
	Issues      []outputIssue         `json:"issues"`
	Prompt      string                `json:"prompt"`
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "db-init":
			return runDBInit(args[1:], stdout, stderr)
		case "watchlist-sync":
			return runWatchlistSync(args[1:], stdout, stderr)
		case "watchlist-list":
			return runWatchlistList(args[1:], stdout, stderr)
		case "watchlist-delete":
			return runWatchlistDelete(args[1:], stdout, stderr)
		case "trades-import":
			return runTradesImport(args[1:], stdout, stderr)
		case "mirae-import":
			return runMiraeImport(args[1:], stdout, stderr)
		case "dart-corp-sync":
			return runDARTCorporationSync(args[1:], stdout, stderr)
		case "dart-disclosure-sync":
			return runDARTDisclosureSync(args[1:], stdout, stderr)
		case "dart-disclosure-list":
			return runDARTDisclosureList(args[1:], stdout, stderr)
		case "dart-document-sync":
			return runDARTDocumentSync(args[1:], stdout, stderr)
		case "dart-document-list":
			return runDARTDocumentList(args[1:], stdout, stderr)
		case "dart-financial-sync":
			return runDARTFinancialSync(args[1:], stdout, stderr)
		case "dart-financial-list":
			return runDARTFinancialList(args[1:], stdout, stderr)
		case "dart-financial-metrics":
			return runDARTFinancialMetrics(args[1:], stdout, stderr)
		case "analysis-snapshot":
			return runAnalysisSnapshot(args[1:], stdout, stderr)
		case "risk-assess":
			return runRiskAssessment(args[1:], stdout, stderr)
		case "research-brief":
			return runResearchBrief(args[1:], stdout, stderr)
		case "position-reconcile":
			return runPositionReconcile(args[1:], stdout, stderr)
		case "positions-import":
			return runPositionsImport(args[1:], stdout, stderr)
		case "portfolio-analyze":
			return runPortfolioAnalyze(args[1:], stdout, stderr)
		case "portfolio-scenarios":
			return runPortfolioScenarios(args[1:], stdout, stderr)
		case "portfolio-brief":
			return runPortfolioBrief(args[1:], stdout, stderr)
		case "analysis-run-list":
			return runAnalysisRunList(args[1:], stdout, stderr)
		case "alert-evaluate":
			return runAlertEvaluate(args[1:], stdout, stderr)
		case "alert-list":
			return runAlertList(args[1:], stdout, stderr)
		case "daily-email-report":
			return runDailyEmailReport(args[1:], stdout, stderr)
		case "portfolio-response-email":
			return runPortfolioResponseEmail(args[1:], stdout, stderr)
		case "portfolio-codex-email":
			return runPortfolioCodexEmail(args[1:], stdout, stderr)
		case "investment-profile-sync":
			return runInvestmentProfileSync(args[1:], stdout, stderr)
		case "krx-instrument-sync":
			return runKRXInstrumentSync(args[1:], stdout, stderr)
		case "position-set":
			return runPositionSet(args[1:], stdout, stderr)
		case "thesis-set":
			return runThesisSet(args[1:], stdout, stderr)
		case "portfolio-show":
			return runPortfolioShow(args[1:], stdout, stderr)
		case "help":
			writeCommandHelp(stdout)
			return 0
		default:
			fmt.Fprintf(stderr, "unknown command %q\n", args[0])
			writeCommandHelp(stderr)
			return 2
		}
	}
	return runAnalyze(args, stdout, stderr)
}

func runAnalyze(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("forgetmenot", flag.ContinueOnError)
	flags.SetOutput(stderr)
	watchlistPath := flags.String("watchlist", "data/watchlist.example.csv", "watchlist CSV path")
	name := flags.String("name", "", "stock name to analyze")
	ticker := flags.String("ticker", "", "ticker or Yahoo ticker to analyze")
	thesis := flags.String("thesis", "", "user investment thesis")
	days := flags.Int("days", 30, "DART disclosure lookback days")
	databasePath := flags.String("db", defaultDatabasePath, "SQLite database path")
	outputFormat := flags.String("output", "text", "output format: text or json")
	if err := flags.Parse(args); err != nil {
		return 2
	}

	*outputFormat = strings.ToLower(strings.TrimSpace(*outputFormat))
	if *outputFormat != "text" && *outputFormat != "json" {
		return writeFailure(*outputFormat, stdout, stderr, outputIssue{
			Scope:   "input",
			Kind:    "invalid_input",
			Message: `output must be "text" or "json"`,
		})
	}
	if *days <= 0 {
		return writeFailure(*outputFormat, stdout, stderr, outputIssue{
			Scope:   "input",
			Kind:    "invalid_input",
			Message: "days must be greater than zero",
		})
	}

	settings := config.Load(".env")
	items, err := watchlist.Load(*watchlistPath)
	if err != nil {
		return writeFailure(*outputFormat, stdout, stderr, outputIssue{
			Scope:   "watchlist",
			Kind:    "invalid_input",
			Message: err.Error(),
		})
	}
	selected, err := watchlist.Select(items, *name, *ticker)
	if err != nil {
		return writeFailure(*outputFormat, stdout, stderr, outputIssue{
			Scope:   "watchlist",
			Kind:    "invalid_input",
			Message: err.Error(),
		})
	}

	issues := []outputIssue{}
	identifierContext, cancelIdentifiers := context.WithTimeout(context.Background(), 10*time.Second)
	selected, err = enrichDARTCorporationCode(
		identifierContext,
		selected,
		*databasePath,
	)
	cancelIdentifiers()
	if err != nil {
		issues = append(issues, outputIssue{
			Scope:   "identifiers",
			Kind:    "operation_failed",
			Message: err.Error(),
		})
	}

	priceContext, cancelPrice := context.WithTimeout(context.Background(), providerRequestTimeout)
	snapshot, priceErr := yahoo.BuildPriceSnapshot(priceContext, selected.YahooTicker)
	cancelPrice()
	if priceErr != nil {
		issues = append(issues, outputIssue{
			Scope:   "price",
			Kind:    string(provider.KindOf(priceErr)),
			Message: priceErr.Error(),
		})
	} else if snapshot.Status == models.DataStatusEmpty {
		issues = append(issues, outputIssue{
			Scope:   "price",
			Kind:    string(provider.ErrorKindNoData),
			Message: "price provider returned no data",
		})
	}
	for _, warning := range snapshot.Warnings {
		issues = append(issues, outputIssue{
			Scope:   "price",
			Kind:    "partial_data",
			Message: warning,
		})
	}
	signals := analysis.EvaluatePriceSnapshot(snapshot)
	if signals == nil {
		signals = []analysis.Signal{}
	}

	disclosures := dart.NotRequestedResult()
	if settings.OpenDARTAPIKey != "" && strings.TrimSpace(selected.DARTCorpCode) != "" {
		dartContext, cancelDART := context.WithTimeout(context.Background(), providerRequestTimeout)
		disclosures, err = dart.NewClient(settings.OpenDARTAPIKey).RecentDisclosures(
			dartContext,
			selected.DARTCorpCode,
			*days,
			20,
		)
		cancelDART()
		if err != nil {
			issues = append(issues, outputIssue{
				Scope:   "disclosures",
				Kind:    string(provider.KindOf(err)),
				Message: err.Error(),
			})
		} else if disclosures.Status == models.DataStatusEmpty {
			issues = append(issues, outputIssue{
				Scope:   "disclosures",
				Kind:    string(provider.ErrorKindNoData),
				Message: "OpenDART returned no disclosures",
			})
		}
	} else {
		issues = append(issues, outputIssue{
			Scope:   "disclosures",
			Kind:    "not_requested",
			Message: "OpenDART API key or corporation code is not configured",
		})
	}
	for _, warning := range disclosures.Warnings {
		issues = append(issues, outputIssue{
			Scope:   "disclosures",
			Kind:    "partial_data",
			Message: warning,
		})
	}

	briefPrompt := prompt.BuildStockBriefPrompt(prompt.StockBriefInput{
		Name:        selected.Name,
		Snapshot:    snapshot,
		Signals:     signals,
		Disclosures: disclosures,
		UserThesis:  *thesis,
	})
	result := cliResult{
		Selected:    selected,
		Price:       snapshot,
		Signals:     signals,
		Disclosures: disclosures,
		Issues:      issues,
		Prompt:      briefPrompt,
	}

	if *outputFormat == "json" {
		if err := writeJSON(stdout, result); err != nil {
			fmt.Fprintf(stderr, "write JSON output: %v\n", err)
			return 1
		}
		return 0
	}
	writeText(stdout, result)
	return 0
}

func enrichDARTCorporationCode(
	ctx context.Context,
	item models.WatchlistItem,
	databasePath string,
) (models.WatchlistItem, error) {
	if strings.TrimSpace(item.DARTCorpCode) != "" ||
		strings.TrimSpace(databasePath) == "" {
		return item, nil
	}
	if _, err := os.Stat(databasePath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return item, nil
		}
		return item, fmt.Errorf("inspect identifier database %q: %w", databasePath, err)
	}

	store, err := sqlitestore.Open(ctx, databasePath)
	if err != nil {
		return item, fmt.Errorf("open identifier database: %w", err)
	}
	defer store.Close()

	instrument, err := store.Instrument(ctx, item.Ticker)
	if err != nil {
		if errors.Is(err, sqlitestore.ErrNotFound) {
			return item, nil
		}
		return item, fmt.Errorf("query stored instrument %q: %w", item.Ticker, err)
	}
	if strings.TrimSpace(instrument.DARTCorpCode) != "" {
		item.DARTCorpCode = instrument.DARTCorpCode
	}
	return item, nil
}

func writeFailure(outputFormat string, stdout io.Writer, stderr io.Writer, issue outputIssue) int {
	if outputFormat == "json" {
		if err := writeJSON(stdout, struct {
			Error outputIssue `json:"error"`
		}{Error: issue}); err != nil {
			fmt.Fprintf(stderr, "write JSON error: %v\n", err)
		}
	} else {
		fmt.Fprintln(stderr, issue.Message)
	}
	return 2
}

func writeJSON(output io.Writer, value any) error {
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}

func writeText(output io.Writer, result cliResult) {
	fmt.Fprintf(output, "Selected: %s (%s / %s)\n", result.Selected.Name, result.Selected.Ticker, result.Selected.YahooTicker)
	fmt.Fprintf(output, "Price status: %s\n", result.Price.Status)
	fmt.Fprintf(output, "Currency: %s\n", valueOrNA(result.Price.Currency))
	fmt.Fprintf(output, "Observed at: %s\n", formatObservedAt(result.Price.Source.ObservedAt))
	fmt.Fprintf(output, "Fetched at: %s\n", formatFetchedAt(result.Price.Source.FetchedAt))
	fmt.Fprintf(output, "Source: %s\n", valueOrNA(result.Price.Source.SourceURL))
	fmt.Fprintf(output, "Last price: %s\n", formatFloat(result.Price.LastPrice))
	fmt.Fprintf(output, "1D change: %s%%\n", formatFloat(result.Price.ChangePct1D))
	fmt.Fprintf(output, "20D return: %s%%\n", formatFloat(result.Price.ReturnPct20D))
	fmt.Fprintf(output, "60D return: %s%%\n", formatFloat(result.Price.ReturnPct60D))
	fmt.Fprintf(
		output,
		"20D annualized volatility: %s%%\n",
		formatFloat(result.Price.AnnualizedVolatilityPct20D),
	)
	fmt.Fprintf(
		output,
		"60D annualized volatility: %s%%\n",
		formatFloat(result.Price.AnnualizedVolatilityPct60D),
	)
	fmt.Fprintf(
		output,
		"6M max drawdown: %s%%\n",
		formatFloat(result.Price.MaxDrawdownPct6M),
	)
	fmt.Fprintf(output, "MA20: %s\n", formatFloat(result.Price.MA20))
	fmt.Fprintf(output, "MA60: %s\n", formatFloat(result.Price.MA60))
	fmt.Fprintf(output, "Volume: %s\n", formatInt(result.Price.Volume))
	fmt.Fprintf(
		output,
		"Previous 20D average volume: %s\n",
		formatFloat(result.Price.PreviousAverageVolume20D),
	)
	fmt.Fprintf(
		output,
		"20D volume ratio: %s\n\n",
		formatFloat(result.Price.VolumeRatio20D),
	)

	fmt.Fprintln(output, "Signals:")
	if len(result.Signals) == 0 {
		fmt.Fprintln(output, "- 특이 신호 없음")
	}
	for _, signal := range result.Signals {
		fmt.Fprintf(output, "- [%s] %s: %s\n", signal.Level, signal.Title, signal.Detail)
	}

	fmt.Fprintln(output, "\nRecent DART disclosures:")
	fmt.Fprintf(output, "Status: %s\n", result.Disclosures.Status)
	fmt.Fprintf(output, "Observed at: %s\n", formatObservedAt(result.Disclosures.Source.ObservedAt))
	fmt.Fprintf(output, "Fetched at: %s\n", formatFetchedAt(result.Disclosures.Source.FetchedAt))
	fmt.Fprintf(output, "Source: %s\n", valueOrNA(result.Disclosures.Source.SourceURL))
	if len(result.Disclosures.Disclosures) == 0 {
		fmt.Fprintln(output, "- 제공된 공시 없음")
	}
	for _, item := range result.Disclosures.Disclosures {
		fmt.Fprintf(
			output,
			"- %s %s: %s (%s)\n",
			item.ReceiptDate.Format("2006-01-02"),
			item.CorpName,
			item.ReportName,
			item.ReceiptNo,
		)
	}

	if len(result.Issues) > 0 {
		fmt.Fprintln(output, "\nData issues:")
		for _, issue := range result.Issues {
			fmt.Fprintf(output, "- [%s/%s] %s\n", issue.Scope, issue.Kind, issue.Message)
		}
	}

	fmt.Fprintln(output, "\n--- ChatGPT Prompt ---")
	fmt.Fprintln(output, result.Prompt)
}

func formatObservedAt(value *time.Time) string {
	if value == nil {
		return "N/A"
	}
	return value.UTC().Format(time.RFC3339)
}

func formatFetchedAt(value time.Time) string {
	if value.IsZero() {
		return "N/A"
	}
	return value.UTC().Format(time.RFC3339)
}

func valueOrNA(value string) string {
	if strings.TrimSpace(value) == "" {
		return "N/A"
	}
	return value
}

func formatFloat(value *float64) string {
	if value == nil {
		return "N/A"
	}
	return fmt.Sprintf("%.2f", *value)
}

func formatInt(value *int64) string {
	if value == nil {
		return "N/A"
	}
	return fmt.Sprintf("%d", *value)
}
