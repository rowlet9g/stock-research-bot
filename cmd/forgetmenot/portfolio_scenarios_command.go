package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/analysis"
	sqlitestore "github.com/rowlet9g/stock-research-bot/internal/storage/sqlite"
)

var portfolioScenarioNow = time.Now

func runPortfolioScenarios(
	args []string,
	stdout io.Writer,
	stderr io.Writer,
) int {
	flags := flag.NewFlagSet("portfolio-scenarios", flag.ContinueOnError)
	flags.SetOutput(stderr)
	databasePath := flags.String("db", defaultDatabasePath, "SQLite database path")
	ticker := flags.String("ticker", "", "optional stored instrument ticker")
	workers := flags.Int("workers", 4, "concurrent price requests from 1 to 16")
	downsideBPS := flags.Int64("downside-bps", -2000, "downside price return in basis points")
	upsideBPS := flags.Int64("upside-bps", 2000, "upside price return in basis points")
	outputFormat := flags.String("output", "text", "output format: text or json")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if exitCode := validateCommandOutput(outputFormat, stdout, stderr); exitCode != 0 {
		return exitCode
	}
	*ticker = strings.TrimSpace(*ticker)
	if err := validatePortfolioScenarioFlags(
		*workers,
		*downsideBPS,
		*upsideBPS,
	); err != nil {
		return commandInputError(
			*outputFormat,
			stdout,
			stderr,
			err.Error(),
		)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	store, err := sqlitestore.Open(ctx, *databasePath)
	if err != nil {
		return writeRuntimeFailure(*outputFormat, stdout, stderr, "storage", err)
	}
	defer store.Close()

	generatedAt := portfolioScenarioNow()
	valuation, err := buildPortfolioValuation(
		ctx,
		store,
		*ticker,
		*workers,
		generatedAt,
		analysis.DefaultPortfolioValuationConfig(),
	)
	if err != nil {
		return writeRuntimeFailure(
			*outputFormat,
			stdout,
			stderr,
			"portfolio_valuation",
			err,
		)
	}
	config := analysis.DefaultPortfolioScenarioConfig()
	config.DownsideReturnBPS = *downsideBPS
	config.UpsideReturnBPS = *upsideBPS
	report, err := analysis.StressPortfolio(
		valuation,
		generatedAt,
		config,
	)
	if err != nil {
		return writeRuntimeFailure(
			*outputFormat,
			stdout,
			stderr,
			"portfolio_scenarios",
			err,
		)
	}
	if *outputFormat == "json" {
		if err := writeJSON(stdout, report); err != nil {
			return writeRuntimeFailure("text", stdout, stderr, "output", err)
		}
		return 0
	}
	writePortfolioScenariosText(stdout, report)
	return 0
}

func writePortfolioScenariosText(
	output io.Writer,
	report analysis.PortfolioScenarioReport,
) {
	fmt.Fprintf(
		output,
		"Portfolio scenarios: status=%s version=%s input_status=%s input_sha256=%s positions=%d included=%d excluded=%d currencies=%d\n",
		report.Status,
		report.Version,
		report.InputValuationStatus,
		report.InputValuationSHA256,
		report.Summary.InputPositions,
		report.Summary.IncludedPositions,
		report.Summary.ExcludedPositions,
		report.Summary.CurrencyGroups,
	)
	fmt.Fprintf(
		output,
		"Generated at: %s\n",
		report.GeneratedAt.Format(time.RFC3339Nano),
	)
	fmt.Fprintf(
		output,
		"Cross-currency aggregation: %s; probability=%s\n",
		report.CrossCurrencyAggregation,
		report.Config.ProbabilityPolicy,
	)
	for _, scenario := range report.Scenarios {
		fmt.Fprintf(
			output,
			"Scenario %s: return=%s%% probability=%s\n",
			scenario.ID,
			scenario.ReturnPct,
			scenario.ProbabilityStatus,
		)
		fmt.Fprintf(output, "  Assumption: %s\n", scenario.Assumption)
		fmt.Fprintf(output, "  Result: %s\n", scenario.MechanicalResult)
		for _, currency := range scenario.Currencies {
			fmt.Fprintf(
				output,
				"  - %s current=%s scenario=%s change=%s current_gross=%s scenario_gross=%s\n",
				currency.Currency,
				currency.CurrentNetValue,
				currency.ScenarioNetValue,
				currency.Change,
				currency.CurrentGrossExposure,
				currency.ScenarioGrossExposure,
			)
		}
	}
	if len(report.Issues) == 0 {
		return
	}
	fmt.Fprintln(output, "Issues:")
	for _, issue := range report.Issues {
		fmt.Fprintf(
			output,
			"- ticker=%s kind=%s message=%s\n",
			valueOrNA(issue.Ticker),
			issue.Kind,
			issue.Message,
		)
	}
}

func validatePortfolioScenarioFlags(
	workers int,
	downsideBPS int64,
	upsideBPS int64,
) error {
	switch {
	case workers < 1 || workers > 16:
		return fmt.Errorf("workers must be between 1 and 16")
	case downsideBPS < -10000 ||
		downsideBPS >= 0 ||
		upsideBPS <= 0 ||
		upsideBPS > 100000:
		return fmt.Errorf(
			"scenario returns must satisfy -10000 <= downside-bps < 0 < upside-bps <= 100000",
		)
	default:
		return nil
	}
}
