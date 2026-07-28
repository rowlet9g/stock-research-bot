package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/analysis"
	"github.com/rowlet9g/stock-research-bot/internal/models"
	sqlitestore "github.com/rowlet9g/stock-research-bot/internal/storage/sqlite"
)

func runPositionReconcile(
	args []string,
	stdout io.Writer,
	stderr io.Writer,
) int {
	flags := flag.NewFlagSet("position-reconcile", flag.ContinueOnError)
	flags.SetOutput(stderr)
	databasePath := flags.String("db", defaultDatabasePath, "SQLite database path")
	ticker := flags.String("ticker", "", "optional stored instrument ticker")
	outputFormat := flags.String("output", "text", "output format: text or json")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if exitCode := validateCommandOutput(outputFormat, stdout, stderr); exitCode != 0 {
		return exitCode
	}
	*ticker = strings.TrimSpace(*ticker)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := sqlitestore.Open(ctx, *databasePath)
	if err != nil {
		return writeRuntimeFailure(*outputFormat, stdout, stderr, "storage", err)
	}
	defer store.Close()

	var records []models.PortfolioRecord
	if *ticker == "" {
		records, err = store.ListPortfolios(ctx)
	} else {
		var record models.PortfolioRecord
		record, err = store.Portfolio(ctx, *ticker)
		records = []models.PortfolioRecord{record}
	}
	if err != nil {
		return writeRuntimeFailure(
			*outputFormat,
			stdout,
			stderr,
			"portfolios",
			err,
		)
	}
	report, err := analysis.ReconcilePositionLedger(
		records,
		analysisSnapshotNow(),
	)
	if err != nil {
		return writeRuntimeFailure(
			*outputFormat,
			stdout,
			stderr,
			"position_reconciliation",
			err,
		)
	}
	if *outputFormat == "json" {
		if err := writeJSON(stdout, report); err != nil {
			return writeRuntimeFailure("text", stdout, stderr, "output", err)
		}
		return 0
	}
	writePositionReconciliationText(stdout, report)
	return 0
}

func writePositionReconciliationText(
	output io.Writer,
	report analysis.PositionReconciliationReport,
) {
	fmt.Fprintf(
		output,
		"Position reconciliation: status=%s version=%s instruments=%d with_trades=%d with_position=%d missing_position=%d unsupported=%d\n",
		report.Status,
		report.Version,
		report.Summary.Instruments,
		report.Summary.WithTrades,
		report.Summary.WithStoredPosition,
		report.Summary.MissingStoredPosition,
		report.Summary.UnsupportedTradeItems,
	)
	fmt.Fprintf(
		output,
		"Generated at: %s\n",
		report.GeneratedAt.Format(time.RFC3339Nano),
	)
	for _, item := range report.Items {
		fmt.Fprintf(
			output,
			"- %s %s: status=%s trades=%d buy=%s sell=%s net_change=%s",
			item.Instrument.Ticker,
			item.Instrument.Name,
			item.Status,
			item.TradeCount,
			item.BuyQuantity,
			item.SellQuantity,
			item.NetQuantityChange,
		)
		if item.StoredPositionUnits != nil {
			fmt.Fprintf(output, " stored_position=%s", item.StoredPosition)
		}
		if item.ImpliedOpeningUnits != nil {
			fmt.Fprintf(output, " implied_opening=%s", item.ImpliedOpening)
		}
		fmt.Fprintln(output)
		for _, issue := range item.Issues {
			fmt.Fprintf(
				output,
				"  - kind=%s message=%s\n",
				issue.Kind,
				issue.Message,
			)
		}
	}
}
