package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/analysis"
	"github.com/rowlet9g/stock-research-bot/internal/dart"
	sqlitestore "github.com/rowlet9g/stock-research-bot/internal/storage/sqlite"
)

type dartFinancialMetricsResult struct {
	Ticker string `json:"ticker,omitempty"`
	analysis.FinancialMetricReport
}

func runDARTFinancialMetrics(
	args []string,
	stdout io.Writer,
	stderr io.Writer,
) int {
	flags := flag.NewFlagSet("dart-financial-metrics", flag.ContinueOnError)
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
				"financial_metric_instrument",
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

	statement, err := store.CurrentDARTFinancialStatement(
		ctx,
		*corpCode,
		*businessYear,
		*reportCode,
		*fsKind,
	)
	if err != nil {
		return writeRuntimeFailure(
			*outputFormat,
			stdout,
			stderr,
			"dart_financial_metrics",
			err,
		)
	}
	result := dartFinancialMetricsResult{
		Ticker:                *ticker,
		FinancialMetricReport: analysis.BuildFinancialMetricReport(statement),
	}
	if *outputFormat == "json" {
		if err := writeJSON(stdout, result); err != nil {
			return writeRuntimeFailure("text", stdout, stderr, "output", err)
		}
		return 0
	}

	fmt.Fprintf(
		stdout,
		"OpenDART financial metrics: status=%s corp_code=%s year=%d report=%s fs_div=%s receipt=%s content_sha256=%s\n",
		result.Status,
		result.CorpCode,
		result.BusinessYear,
		result.ReportCode,
		result.FSKind,
		result.ReceiptNo,
		result.ContentSHA256,
	)
	fmt.Fprintln(stdout, "Metrics:")
	for _, metric := range result.Metrics {
		fmt.Fprintf(
			stdout,
			"- %s: status=%s current=%s previous=%s currency=%s period_basis=%s account_id=%s\n",
			metric.Label,
			metric.Status,
			valueOrNA(metric.CurrentAmount),
			valueOrNA(metric.PreviousAmount),
			valueOrNA(metric.Currency),
			valueOrNA(metric.PeriodBasis),
			valueOrNA(metric.AccountID),
		)
		if metric.Message != "" {
			fmt.Fprintf(stdout, "  %s\n", metric.Message)
		}
	}
	fmt.Fprintln(stdout, "Ratios:")
	for _, ratio := range result.Ratios {
		fmt.Fprintf(
			stdout,
			"- %s: status=%s value=%s\n",
			ratio.Label,
			ratio.Status,
			financialRatioDisplayValue(ratio),
		)
		if ratio.Message != "" {
			fmt.Fprintf(stdout, "  %s\n", ratio.Message)
		}
	}
	return 0
}

func financialRatioDisplayValue(ratio analysis.FinancialRatio) string {
	if ratio.Value == "" {
		return "N/A"
	}
	return ratio.Value + ratio.Unit
}
