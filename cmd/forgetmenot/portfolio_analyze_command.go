package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/analysis"
	"github.com/rowlet9g/stock-research-bot/internal/models"
	"github.com/rowlet9g/stock-research-bot/internal/provider"
	sqlitestore "github.com/rowlet9g/stock-research-bot/internal/storage/sqlite"
	"github.com/rowlet9g/stock-research-bot/internal/yahoo"
)

var portfolioAnalyzeNow = time.Now

var portfolioAnalyzePrice = yahoo.BuildPriceSnapshot

func runPortfolioAnalyze(
	args []string,
	stdout io.Writer,
	stderr io.Writer,
) int {
	flags := flag.NewFlagSet("portfolio-analyze", flag.ContinueOnError)
	flags.SetOutput(stderr)
	databasePath := flags.String("db", defaultDatabasePath, "SQLite database path")
	ticker := flags.String("ticker", "", "optional stored instrument ticker")
	workers := flags.Int("workers", 4, "concurrent price requests from 1 to 16")
	watchBPS := flags.Int64("watch-bps", 2500, "concentration watch threshold in basis points")
	highBPS := flags.Int64("high-bps", 4000, "high concentration threshold in basis points")
	outputFormat := flags.String("output", "text", "output format: text or json")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if exitCode := validateCommandOutput(outputFormat, stdout, stderr); exitCode != 0 {
		return exitCode
	}
	*ticker = strings.TrimSpace(*ticker)
	if *workers < 1 || *workers > 16 {
		return commandInputError(
			*outputFormat,
			stdout,
			stderr,
			"workers must be between 1 and 16",
		)
	}
	if *watchBPS <= 0 ||
		*highBPS <= *watchBPS ||
		*highBPS > 10000 {
		return commandInputError(
			*outputFormat,
			stdout,
			stderr,
			"concentration thresholds must satisfy 0 < watch-bps < high-bps <= 10000",
		)
	}

	config := analysis.PortfolioValuationConfig{
		WatchThresholdBPS: *watchBPS,
		HighThresholdBPS:  *highBPS,
		WeightBasis:       "gross_market_value_within_currency",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	store, err := sqlitestore.Open(ctx, *databasePath)
	if err != nil {
		return writeRuntimeFailure(*outputFormat, stdout, stderr, "storage", err)
	}
	defer store.Close()

	report, err := buildPortfolioValuation(
		ctx,
		store,
		*ticker,
		*workers,
		portfolioAnalyzeNow(),
		config,
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
	if *outputFormat == "json" {
		if err := writeJSON(stdout, report); err != nil {
			return writeRuntimeFailure("text", stdout, stderr, "output", err)
		}
		return 0
	}
	writePortfolioValuationText(stdout, report)
	return 0
}

func buildPortfolioValuation(
	ctx context.Context,
	store *sqlitestore.Store,
	ticker string,
	workers int,
	generatedAt time.Time,
	config analysis.PortfolioValuationConfig,
) (analysis.PortfolioValuationReport, error) {
	var records []models.PortfolioRecord
	var err error
	if ticker == "" {
		records, err = store.ListPortfolios(ctx)
	} else {
		var record models.PortfolioRecord
		record, err = store.Portfolio(ctx, ticker)
		records = []models.PortfolioRecord{record}
	}
	if err != nil {
		return analysis.PortfolioValuationReport{}, fmt.Errorf(
			"load portfolios: %w",
			err,
		)
	}

	inputs := make([]analysis.PortfolioValuationInput, len(records))
	for index, record := range records {
		inputs[index] = analysis.PortfolioValuationInput{
			Portfolio: record,
			Price: models.PriceSnapshot{
				YahooTicker: record.Instrument.YahooTicker,
				Status:      models.DataStatusNotRequested,
			},
			Issues: []analysis.PortfolioValuationIssue{},
		}
	}
	fetchPortfolioPrices(ctx, inputs, workers)
	report, err := analysis.ValuePortfolio(inputs, generatedAt, config)
	if err != nil {
		return analysis.PortfolioValuationReport{}, fmt.Errorf(
			"calculate portfolio valuation: %w",
			err,
		)
	}
	return report, nil
}

func fetchPortfolioPrices(
	ctx context.Context,
	inputs []analysis.PortfolioValuationInput,
	workers int,
) {
	jobs := make(chan int)
	var waitGroup sync.WaitGroup
	for range workers {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			for index := range jobs {
				fetchPortfolioPrice(ctx, &inputs[index])
			}
		}()
	}
	for index := range inputs {
		position := inputs[index].Portfolio.Position
		if position == nil || position.QuantityUnits == 0 {
			continue
		}
		jobs <- index
	}
	close(jobs)
	waitGroup.Wait()
}

func fetchPortfolioPrice(
	ctx context.Context,
	input *analysis.PortfolioValuationInput,
) {
	yahooTicker := strings.TrimSpace(input.Portfolio.Instrument.YahooTicker)
	if yahooTicker == "" {
		input.Price.Status = models.DataStatusUnavailable
		input.Issues = append(input.Issues, analysis.PortfolioValuationIssue{
			Kind:    "yahoo_ticker_missing",
			Message: "Yahoo ticker is not stored for this instrument",
		})
		return
	}

	requestContext, cancel := context.WithTimeout(ctx, providerRequestTimeout)
	price, err := portfolioAnalyzePrice(requestContext, yahooTicker)
	cancel()
	if err != nil {
		input.Price.Status = models.DataStatusUnavailable
		input.Issues = append(input.Issues, analysis.PortfolioValuationIssue{
			Kind:    string(provider.KindOf(err)),
			Message: err.Error(),
		})
		return
	}
	input.Price = price
	switch price.Status {
	case models.DataStatusEmpty:
		input.Issues = append(input.Issues, analysis.PortfolioValuationIssue{
			Kind:    string(provider.ErrorKindNoData),
			Message: "price provider returned no data",
		})
	case models.DataStatusUnavailable:
		input.Issues = append(input.Issues, analysis.PortfolioValuationIssue{
			Kind:    string(provider.ErrorKindUnavailable),
			Message: "price provider is unavailable",
		})
	case models.DataStatusPartial:
		if len(price.Warnings) == 0 {
			input.Issues = append(input.Issues, analysis.PortfolioValuationIssue{
				Kind:    "partial_data",
				Message: "price provider returned partial data",
			})
		}
	}
	for _, warning := range price.Warnings {
		input.Issues = append(input.Issues, analysis.PortfolioValuationIssue{
			Kind:    "partial_data",
			Message: warning,
		})
	}
}

func writePortfolioValuationText(
	output io.Writer,
	report analysis.PortfolioValuationReport,
) {
	fmt.Fprintf(
		output,
		"Portfolio valuation: status=%s version=%s instruments=%d positions=%d valued=%d unvalued=%d missing=%d currencies=%d\n",
		report.Status,
		report.Version,
		report.Summary.InputInstruments,
		report.Summary.StoredPositions,
		report.Summary.ValuedPositions,
		report.Summary.UnvaluedPositions,
		report.Summary.MissingPositions,
		report.Summary.CurrencyGroups,
	)
	fmt.Fprintf(
		output,
		"Generated at: %s\n",
		report.GeneratedAt.Format(time.RFC3339Nano),
	)
	fmt.Fprintf(
		output,
		"Cross-currency aggregation: %s\n",
		report.CrossCurrencyAggregation,
	)
	for _, group := range report.Currencies {
		fmt.Fprintf(
			output,
			"Currency %s: positions=%d valued=%d net=%s gross=%s cost=%s unrealized_pl=%s cost_complete=%t\n",
			group.Currency,
			group.Positions,
			group.ValuedPositions,
			group.NetMarketValue,
			group.GrossMarketValue,
			group.CostBasis,
			group.UnrealizedPL,
			group.CostBasisComplete,
		)
	}
	for _, item := range report.Positions {
		fmt.Fprintf(
			output,
			"- %s %s: status=%s quantity=%s currency=%s last=%s market_value=%s gross=%s weight=%s concentration=%s unrealized_pl=%s\n",
			item.Instrument.Ticker,
			item.Instrument.Name,
			item.Status,
			valueOrNA(item.Quantity),
			valueOrNA(item.Currency),
			valueOrNA(item.LastPrice),
			valueOrNA(item.MarketValue),
			valueOrNA(item.GrossMarketValue),
			valueOrNA(item.WeightPct),
			item.Concentration,
			valueOrNA(item.UnrealizedPL),
		)
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
