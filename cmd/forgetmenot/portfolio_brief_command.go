package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/analysis"
	"github.com/rowlet9g/stock-research-bot/internal/models"
	"github.com/rowlet9g/stock-research-bot/internal/prompt"
	sqlitestore "github.com/rowlet9g/stock-research-bot/internal/storage/sqlite"
)

var portfolioBriefNow = time.Now

type portfolioBriefCommandResult struct {
	Valuation    analysis.PortfolioValuationReport `json:"valuation"`
	Scenarios    analysis.PortfolioScenarioReport  `json:"scenarios"`
	Brief        prompt.PortfolioResearchBrief     `json:"brief"`
	AnalysisRun  *models.AnalysisRun               `json:"analysis_run,omitempty"`
	AlreadySaved bool                              `json:"already_saved,omitempty"`
}

func runPortfolioBrief(
	args []string,
	stdout io.Writer,
	stderr io.Writer,
) int {
	flags := flag.NewFlagSet("portfolio-brief", flag.ContinueOnError)
	flags.SetOutput(stderr)
	databasePath := flags.String("db", defaultDatabasePath, "SQLite database path")
	ticker := flags.String("ticker", "", "optional stored instrument ticker")
	workers := flags.Int("workers", 4, "concurrent price requests from 1 to 16")
	downsideBPS := flags.Int64("downside-bps", -2000, "downside price return in basis points")
	upsideBPS := flags.Int64("upside-bps", 2000, "upside price return in basis points")
	question := flags.String("question", "", "portfolio research question")
	saveRun := flags.Bool("save", false, "save the analysis run to SQLite")
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

	generatedAt := portfolioBriefNow()
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
	scenarioConfig := analysis.DefaultPortfolioScenarioConfig()
	scenarioConfig.DownsideReturnBPS = *downsideBPS
	scenarioConfig.UpsideReturnBPS = *upsideBPS
	scenarios, err := analysis.StressPortfolio(
		valuation,
		generatedAt,
		scenarioConfig,
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
	brief, err := prompt.BuildPortfolioResearchBrief(
		prompt.PortfolioResearchBriefInput{
			Valuation:    valuation,
			Scenarios:    scenarios,
			UserQuestion: *question,
		},
	)
	if err != nil {
		return writeRuntimeFailure(
			*outputFormat,
			stdout,
			stderr,
			"portfolio_prompt",
			err,
		)
	}
	result := portfolioBriefCommandResult{
		Valuation: valuation,
		Scenarios: scenarios,
		Brief:     brief,
	}
	if *saveRun {
		payload, err := json.Marshal(result)
		if err != nil {
			return writeRuntimeFailure(
				*outputFormat,
				stdout,
				stderr,
				"analysis_run",
				fmt.Errorf("encode portfolio brief run: %w", err),
			)
		}
		run, duplicate, err := store.SaveAnalysisRun(
			ctx,
			sqlitestore.AnalysisRunInput{
				Kind:        portfolioBriefAnalysisKind,
				Status:      scenarios.Status,
				InputSHA256: brief.ValuationSHA256,
				RuleVersion: brief.Version,
				Payload:     payload,
				GeneratedAt: brief.GeneratedAt,
			},
		)
		if err != nil {
			return writeRuntimeFailure(
				*outputFormat,
				stdout,
				stderr,
				"analysis_run",
				err,
			)
		}
		result.AnalysisRun = &run
		result.AlreadySaved = duplicate
	}
	if *outputFormat == "json" {
		if err := writeJSON(stdout, result); err != nil {
			return writeRuntimeFailure("text", stdout, stderr, "output", err)
		}
		return 0
	}
	if _, err := fmt.Fprint(stdout, brief.Prompt); err != nil {
		return writeRuntimeFailure("text", stdout, stderr, "output", err)
	}
	return 0
}
