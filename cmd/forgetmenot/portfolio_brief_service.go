package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/analysis"
	"github.com/rowlet9g/stock-research-bot/internal/investmentprofile"
	"github.com/rowlet9g/stock-research-bot/internal/prompt"
	sqlitestore "github.com/rowlet9g/stock-research-bot/internal/storage/sqlite"
)

type portfolioBriefBuildRequest struct {
	Ticker      string
	Workers     int
	DownsideBPS int64
	UpsideBPS   int64
	Question    string
	GeneratedAt time.Time
	Profile     *investmentprofile.Profile
}

type portfolioBriefStepError struct {
	Scope string
	Err   error
}

func (e *portfolioBriefStepError) Error() string {
	return e.Scope + ": " + e.Err.Error()
}

func (e *portfolioBriefStepError) Unwrap() error {
	return e.Err
}

func buildPortfolioBriefResult(
	ctx context.Context,
	store *sqlitestore.Store,
	request portfolioBriefBuildRequest,
) (portfolioBriefCommandResult, error) {
	valuation, err := buildPortfolioValuation(
		ctx,
		store,
		request.Ticker,
		request.Workers,
		request.GeneratedAt,
		analysis.DefaultPortfolioValuationConfig(),
	)
	if err != nil {
		return portfolioBriefCommandResult{}, &portfolioBriefStepError{
			Scope: "portfolio_valuation",
			Err:   err,
		}
	}
	scenarioConfig := analysis.DefaultPortfolioScenarioConfig()
	scenarioConfig.DownsideReturnBPS = request.DownsideBPS
	scenarioConfig.UpsideReturnBPS = request.UpsideBPS
	scenarios, err := analysis.StressPortfolio(
		valuation,
		request.GeneratedAt,
		scenarioConfig,
	)
	if err != nil {
		return portfolioBriefCommandResult{}, &portfolioBriefStepError{
			Scope: "portfolio_scenarios",
			Err:   err,
		}
	}
	brief, err := prompt.BuildPortfolioResearchBrief(
		prompt.PortfolioResearchBriefInput{
			Valuation:    valuation,
			Scenarios:    scenarios,
			UserQuestion: request.Question,
			Profile:      request.Profile,
		},
	)
	if err != nil {
		return portfolioBriefCommandResult{}, &portfolioBriefStepError{
			Scope: "portfolio_prompt",
			Err:   err,
		}
	}
	return portfolioBriefCommandResult{
		Valuation: valuation,
		Scenarios: scenarios,
		Brief:     brief,
	}, nil
}

func savePortfolioBriefResult(
	ctx context.Context,
	store *sqlitestore.Store,
	result portfolioBriefCommandResult,
) (portfolioBriefCommandResult, error) {
	payloadResult := result
	payloadResult.AnalysisRun = nil
	payloadResult.AlreadySaved = false
	payload, err := json.Marshal(payloadResult)
	if err != nil {
		return portfolioBriefCommandResult{}, &portfolioBriefStepError{
			Scope: "analysis_run",
			Err:   fmt.Errorf("encode portfolio brief run: %w", err),
		}
	}
	run, duplicate, err := store.SaveAnalysisRun(
		ctx,
		sqlitestore.AnalysisRunInput{
			Kind:        portfolioBriefAnalysisKind,
			Status:      result.Scenarios.Status,
			InputSHA256: result.Brief.ValuationSHA256,
			RuleVersion: result.Brief.Version,
			Payload:     payload,
			GeneratedAt: result.Brief.GeneratedAt,
		},
	)
	if err != nil {
		return portfolioBriefCommandResult{}, &portfolioBriefStepError{
			Scope: "analysis_run",
			Err:   err,
		}
	}
	result.AnalysisRun = &run
	result.AlreadySaved = duplicate
	return result, nil
}

func portfolioBriefFailure(err error) (string, error) {
	var stepError *portfolioBriefStepError
	if errors.As(err, &stepError) {
		return stepError.Scope, stepError.Err
	}
	return "portfolio_brief", err
}
