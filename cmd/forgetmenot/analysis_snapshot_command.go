package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/analysis"
	"github.com/rowlet9g/stock-research-bot/internal/models"
	"github.com/rowlet9g/stock-research-bot/internal/provider"
	sqlitestore "github.com/rowlet9g/stock-research-bot/internal/storage/sqlite"
	"github.com/rowlet9g/stock-research-bot/internal/yahoo"
)

var analysisSnapshotNow = time.Now

var analysisSnapshotPrice = yahoo.BuildPriceSnapshot

func runAnalysisSnapshot(
	args []string,
	stdout io.Writer,
	stderr io.Writer,
) int {
	flags := flag.NewFlagSet("analysis-snapshot", flag.ContinueOnError)
	flags.SetOutput(stderr)
	databasePath := flags.String("db", defaultDatabasePath, "SQLite database path")
	ticker := flags.String("ticker", "", "stored instrument ticker")
	disclosureLimit := flags.Int(
		"disclosure-limit",
		20,
		"maximum stored disclosures",
	)
	fsKind := flags.String("fs-div", "CFS", "CFS or OFS")
	outputFormat := flags.String("output", "text", "output format: text or json")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if exitCode := validateCommandOutput(outputFormat, stdout, stderr); exitCode != 0 {
		return exitCode
	}

	*ticker = strings.TrimSpace(*ticker)
	*fsKind = strings.ToUpper(strings.TrimSpace(*fsKind))
	switch {
	case *ticker == "":
		return commandInputError(
			*outputFormat,
			stdout,
			stderr,
			"ticker is required",
		)
	case *disclosureLimit <= 0 || *disclosureLimit > 100:
		return commandInputError(
			*outputFormat,
			stdout,
			stderr,
			"disclosure-limit must be between 1 and 100",
		)
	case *fsKind != "CFS" && *fsKind != "OFS":
		return commandInputError(
			*outputFormat,
			stdout,
			stderr,
			"fs-div must be CFS or OFS",
		)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := sqlitestore.Open(ctx, *databasePath)
	if err != nil {
		return writeRuntimeFailure(*outputFormat, stdout, stderr, "storage", err)
	}
	defer store.Close()

	portfolio, err := store.Portfolio(ctx, *ticker)
	if err != nil {
		return writeRuntimeFailure(
			*outputFormat,
			stdout,
			stderr,
			"portfolio",
			err,
		)
	}
	issues := []analysis.AnalysisInputIssue{}

	priceContext, cancelPrice := context.WithTimeout(
		context.Background(),
		providerRequestTimeout,
	)
	price, priceErr := analysisSnapshotPrice(
		priceContext,
		portfolio.Instrument.YahooTicker,
	)
	cancelPrice()
	if priceErr != nil {
		issues = append(issues, analysis.AnalysisInputIssue{
			Scope:   "price",
			Kind:    string(provider.KindOf(priceErr)),
			Message: priceErr.Error(),
		})
	} else {
		switch price.Status {
		case models.DataStatusEmpty:
			issues = append(issues, analysis.AnalysisInputIssue{
				Scope:   "price",
				Kind:    string(provider.ErrorKindNoData),
				Message: "price provider returned no data",
			})
		case models.DataStatusUnavailable:
			issues = append(issues, analysis.AnalysisInputIssue{
				Scope:   "price",
				Kind:    string(provider.ErrorKindUnavailable),
				Message: "price provider is unavailable",
			})
		case models.DataStatusPartial:
			if len(price.Warnings) == 0 {
				issues = append(issues, analysis.AnalysisInputIssue{
					Scope:   "price",
					Kind:    "partial_data",
					Message: "price provider returned partial data",
				})
			}
		}
	}
	for _, warning := range price.Warnings {
		issues = append(issues, analysis.AnalysisInputIssue{
			Scope:   "price",
			Kind:    "partial_data",
			Message: warning,
		})
	}

	disclosures := analysis.AnalysisDisclosureInput{
		Status:      models.DataStatusNotRequested,
		Disclosures: []models.DARTDisclosure{},
	}
	financials := analysis.FinancialMetricReport{
		Status:  models.DataStatusNotRequested,
		Metrics: []analysis.FinancialMetric{},
		Ratios:  []analysis.FinancialRatio{},
	}
	corpCode := strings.TrimSpace(portfolio.Instrument.DARTCorpCode)
	if corpCode != "" {
		storedDisclosures, disclosureErr := store.ListDARTDisclosures(
			ctx,
			corpCode,
			*disclosureLimit,
		)
		switch {
		case disclosureErr != nil:
			disclosures.Status = models.DataStatusUnavailable
			issues = append(issues, analysis.AnalysisInputIssue{
				Scope:   "disclosures",
				Kind:    "operation_failed",
				Message: disclosureErr.Error(),
			})
		case len(storedDisclosures) == 0:
			disclosures.Status = models.DataStatusEmpty
			issues = append(issues, analysis.AnalysisInputIssue{
				Scope:   "disclosures",
				Kind:    string(provider.ErrorKindNoData),
				Message: "no stored OpenDART disclosures were found",
			})
		default:
			disclosures.Status = models.DataStatusAvailable
			disclosures.Disclosures = storedDisclosures
		}

		statement, financialErr := store.LatestCurrentDARTFinancialStatement(
			ctx,
			corpCode,
			*fsKind,
		)
		switch {
		case errors.Is(financialErr, sqlitestore.ErrNotFound):
			financials.Status = models.DataStatusEmpty
			issues = append(issues, analysis.AnalysisInputIssue{
				Scope:   "financials",
				Kind:    string(provider.ErrorKindNoData),
				Message: financialErr.Error(),
			})
		case financialErr != nil:
			financials.Status = models.DataStatusUnavailable
			issues = append(issues, analysis.AnalysisInputIssue{
				Scope:   "financials",
				Kind:    "operation_failed",
				Message: financialErr.Error(),
			})
		default:
			financials = analysis.BuildFinancialMetricReport(statement)
			if financials.Status != models.DataStatusAvailable {
				issues = append(issues, analysis.AnalysisInputIssue{
					Scope:   "financials",
					Kind:    "partial_data",
					Message: "one or more financial metrics or ratios are unavailable",
				})
			}
		}
	}

	snapshot, err := analysis.BuildAnalysisInputSnapshot(
		analysisSnapshotNow(),
		analysis.AnalysisInput{
			Portfolio:   portfolio,
			Price:       price,
			Signals:     analysis.EvaluatePriceSnapshot(price),
			Disclosures: disclosures,
			Financials:  financials,
			Issues:      issues,
		},
	)
	if err != nil {
		return writeRuntimeFailure(
			*outputFormat,
			stdout,
			stderr,
			"analysis_snapshot",
			err,
		)
	}
	if *outputFormat == "json" {
		if err := writeJSON(stdout, snapshot); err != nil {
			return writeRuntimeFailure("text", stdout, stderr, "output", err)
		}
		return 0
	}
	writeAnalysisSnapshotText(stdout, snapshot)
	return 0
}

func writeAnalysisSnapshotText(
	output io.Writer,
	snapshot analysis.AnalysisInputSnapshot,
) {
	fmt.Fprintf(
		output,
		"Analysis input snapshot: status=%s ticker=%s schema=%s input_sha256=%s\n",
		snapshot.Status,
		snapshot.Portfolio.Instrument.Ticker,
		snapshot.SchemaVersion,
		snapshot.InputSHA256,
	)
	fmt.Fprintf(
		output,
		"Generated at: %s\n",
		snapshot.GeneratedAt.Format(time.RFC3339Nano),
	)
	fmt.Fprintf(
		output,
		"Rules: price=%s financials=%s\n",
		snapshot.RuleVersions.PriceSignals,
		snapshot.RuleVersions.FinancialMetrics,
	)
	fmt.Fprintf(
		output,
		"Portfolio: position=%t trades=%d thesis=%t\n",
		snapshot.Portfolio.Position != nil,
		len(snapshot.Portfolio.Trades),
		snapshot.Portfolio.Thesis != nil,
	)
	fmt.Fprintf(
		output,
		"Price: status=%s yahoo_ticker=%s last=%s observed_at=%s\n",
		snapshot.Price.Status,
		snapshot.Price.YahooTicker,
		formatOptionalFloat(snapshot.Price.LastPrice),
		formatObservedAt(snapshot.Price.Source.ObservedAt),
	)
	fmt.Fprintf(
		output,
		"Disclosures: status=%s count=%d\n",
		snapshot.Disclosures.Status,
		len(snapshot.Disclosures.Disclosures),
	)
	fmt.Fprintf(
		output,
		"Financials: status=%s year=%d report=%s fs_div=%s receipt=%s\n",
		snapshot.Financials.Status,
		snapshot.Financials.BusinessYear,
		valueOrNA(snapshot.Financials.ReportCode),
		valueOrNA(snapshot.Financials.FSKind),
		valueOrNA(snapshot.Financials.ReceiptNo),
	)
	if len(snapshot.Issues) == 0 {
		return
	}
	fmt.Fprintln(output, "Issues:")
	for _, issue := range snapshot.Issues {
		fmt.Fprintf(
			output,
			"- scope=%s kind=%s message=%s\n",
			issue.Scope,
			issue.Kind,
			issue.Message,
		)
	}
}

func formatOptionalFloat(value *float64) string {
	if value == nil {
		return "N/A"
	}
	return fmt.Sprintf("%.4f", *value)
}
