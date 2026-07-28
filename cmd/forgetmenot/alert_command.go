package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/alerting"
	"github.com/rowlet9g/stock-research-bot/internal/analysis"
	"github.com/rowlet9g/stock-research-bot/internal/models"
	sqlitestore "github.com/rowlet9g/stock-research-bot/internal/storage/sqlite"
)

const portfolioBriefAnalysisKind = "portfolio_brief"

var alertEvaluateNow = time.Now

type alertEvaluateCommandResult struct {
	SourceRunID       int64                              `json:"source_run_id"`
	SourceInputSHA256 string                             `json:"source_input_sha256"`
	Report            alerting.PortfolioCandidateReport  `json:"report"`
	Observations      []sqlitestore.AlertCandidateResult `json:"observations"`
	NewAlerts         int                                `json:"new_alerts"`
	NewObservations   int                                `json:"new_observations"`
}

type alertListCommandResult struct {
	Status models.AlertStatus `json:"status,omitempty"`
	Alerts []models.Alert     `json:"alerts"`
}

func runAlertEvaluate(
	args []string,
	stdout io.Writer,
	stderr io.Writer,
) int {
	flags := flag.NewFlagSet("alert-evaluate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	databasePath := flags.String("db", defaultDatabasePath, "SQLite database path")
	runID := flags.Int64("run-id", 0, "stored portfolio brief analysis run ID")
	outputFormat := flags.String("output", "text", "output format: text or json")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if exitCode := validateCommandOutput(outputFormat, stdout, stderr); exitCode != 0 {
		return exitCode
	}
	if *runID <= 0 {
		return commandInputError(
			*outputFormat,
			stdout,
			stderr,
			"run-id must be greater than zero",
		)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := sqlitestore.Open(ctx, *databasePath)
	if err != nil {
		return writeRuntimeFailure(*outputFormat, stdout, stderr, "storage", err)
	}
	defer store.Close()

	sourceRun, err := store.AnalysisRun(ctx, *runID)
	if err != nil {
		return writeRuntimeFailure(
			*outputFormat,
			stdout,
			stderr,
			"analysis_run",
			err,
		)
	}
	briefResult, err := decodePortfolioBriefAnalysisRun(sourceRun)
	if err != nil {
		return writeRuntimeFailure(
			*outputFormat,
			stdout,
			stderr,
			"analysis_run",
			err,
		)
	}
	report, err := alerting.EvaluatePortfolioCandidates(
		briefResult.Valuation,
		alertEvaluateNow(),
		alerting.DefaultPortfolioCandidateConfig(),
	)
	if err != nil {
		return writeRuntimeFailure(
			*outputFormat,
			stdout,
			stderr,
			"alert_evaluation",
			err,
		)
	}
	if report.InputValuationSHA256 != sourceRun.InputSHA256 {
		return writeRuntimeFailure(
			*outputFormat,
			stdout,
			stderr,
			"analysis_run",
			fmt.Errorf(
				"analysis run %d valuation hash does not match stored input hash",
				sourceRun.ID,
			),
		)
	}

	result := alertEvaluateCommandResult{
		SourceRunID:       sourceRun.ID,
		SourceInputSHA256: sourceRun.InputSHA256,
		Report:            report,
		Observations:      []sqlitestore.AlertCandidateResult{},
	}
	for _, candidate := range report.Candidates {
		payload, err := json.Marshal(candidate)
		if err != nil {
			return writeRuntimeFailure(
				*outputFormat,
				stdout,
				stderr,
				"alert_storage",
				fmt.Errorf("encode alert candidate: %w", err),
			)
		}
		observation, err := store.ObserveAlertCandidate(
			ctx,
			sqlitestore.AlertCandidateInput{
				Fingerprint: candidate.Fingerprint,
				Kind:        candidate.Kind,
				Severity:    candidate.Severity,
				Title:       candidate.Title,
				Fact:        candidate.Fact,
				SourceRunID: sourceRun.ID,
				Payload:     payload,
				DetectedAt:  report.EvaluatedAt,
			},
		)
		if err != nil {
			return writeRuntimeFailure(
				*outputFormat,
				stdout,
				stderr,
				"alert_storage",
				err,
			)
		}
		result.Observations = append(result.Observations, observation)
		if observation.AlertCreated {
			result.NewAlerts++
		}
		if observation.ObservationCreated {
			result.NewObservations++
		}
	}
	if *outputFormat == "json" {
		if err := writeJSON(stdout, result); err != nil {
			return writeRuntimeFailure("text", stdout, stderr, "output", err)
		}
		return 0
	}
	writeAlertEvaluationText(stdout, result)
	return 0
}

func runAlertList(
	args []string,
	stdout io.Writer,
	stderr io.Writer,
) int {
	flags := flag.NewFlagSet("alert-list", flag.ContinueOnError)
	flags.SetOutput(stderr)
	databasePath := flags.String("db", defaultDatabasePath, "SQLite database path")
	statusValue := flags.String("status", "", "optional alert status")
	limit := flags.Int("limit", 20, "maximum alerts from 1 to 500")
	includePayload := flags.Bool("include-payload", false, "include alert evidence JSON")
	outputFormat := flags.String("output", "text", "output format: text or json")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if exitCode := validateCommandOutput(outputFormat, stdout, stderr); exitCode != 0 {
		return exitCode
	}
	status := models.AlertStatus(
		strings.ToLower(strings.TrimSpace(*statusValue)),
	)
	if !validAlertListStatus(status) {
		return commandInputError(
			*outputFormat,
			stdout,
			stderr,
			`status must be empty, "pending", "sent", "failed", or "acknowledged"`,
		)
	}
	if *limit <= 0 || *limit > 500 {
		return commandInputError(
			*outputFormat,
			stdout,
			stderr,
			"limit must be between 1 and 500",
		)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := sqlitestore.Open(ctx, *databasePath)
	if err != nil {
		return writeRuntimeFailure(*outputFormat, stdout, stderr, "storage", err)
	}
	defer store.Close()
	alerts, err := store.ListAlerts(ctx, status, *limit)
	if err != nil {
		return writeRuntimeFailure(*outputFormat, stdout, stderr, "alerts", err)
	}
	if !*includePayload {
		for index := range alerts {
			alerts[index].Payload = nil
		}
	}
	result := alertListCommandResult{
		Status: status,
		Alerts: alerts,
	}
	if *outputFormat == "json" {
		if err := writeJSON(stdout, result); err != nil {
			return writeRuntimeFailure("text", stdout, stderr, "output", err)
		}
		return 0
	}
	writeAlertListText(stdout, result, *includePayload)
	return 0
}

func decodePortfolioBriefAnalysisRun(
	run models.AnalysisRun,
) (portfolioBriefCommandResult, error) {
	if run.Kind != portfolioBriefAnalysisKind {
		return portfolioBriefCommandResult{}, fmt.Errorf(
			"analysis run %d kind must be %q, got %q",
			run.ID,
			portfolioBriefAnalysisKind,
			run.Kind,
		)
	}
	outputHash := sha256.Sum256(run.Payload)
	if hex.EncodeToString(outputHash[:]) != run.OutputSHA256 {
		return portfolioBriefCommandResult{}, fmt.Errorf(
			"analysis run %d payload hash does not match stored output hash",
			run.ID,
		)
	}
	var result portfolioBriefCommandResult
	if err := json.Unmarshal(run.Payload, &result); err != nil {
		return portfolioBriefCommandResult{}, fmt.Errorf(
			"decode portfolio brief analysis run %d: %w",
			run.ID,
			err,
		)
	}
	valuationHash, err := analysis.PortfolioValuationSHA256(result.Valuation)
	if err != nil {
		return portfolioBriefCommandResult{}, fmt.Errorf(
			"hash portfolio brief valuation: %w",
			err,
		)
	}
	switch {
	case valuationHash != run.InputSHA256:
		return portfolioBriefCommandResult{}, fmt.Errorf(
			"analysis run %d valuation hash does not match stored input hash",
			run.ID,
		)
	case result.Brief.ValuationSHA256 != run.InputSHA256:
		return portfolioBriefCommandResult{}, fmt.Errorf(
			"analysis run %d brief hash does not match stored input hash",
			run.ID,
		)
	case result.Brief.Version != run.RuleVersion:
		return portfolioBriefCommandResult{}, fmt.Errorf(
			"analysis run %d brief version does not match stored rule version",
			run.ID,
		)
	}
	return result, nil
}

func validAlertListStatus(status models.AlertStatus) bool {
	switch status {
	case "",
		models.AlertStatusPending,
		models.AlertStatusSent,
		models.AlertStatusFailed,
		models.AlertStatusAcknowledged:
		return true
	default:
		return false
	}
}

func writeAlertEvaluationText(
	output io.Writer,
	result alertEvaluateCommandResult,
) {
	fmt.Fprintf(
		output,
		"Alert evaluation: source_run_id=%d candidates=%d new_alerts=%d new_observations=%d\n",
		result.SourceRunID,
		len(result.Report.Candidates),
		result.NewAlerts,
		result.NewObservations,
	)
	for _, observation := range result.Observations {
		alert := observation.Alert
		fmt.Fprintf(
			output,
			"- id=%d severity=%s status=%s occurrences=%d title=%s\n",
			alert.ID,
			alert.Severity,
			alert.Status,
			alert.OccurrenceCount,
			alert.Title,
		)
	}
}

func writeAlertListText(
	output io.Writer,
	result alertListCommandResult,
	includePayload bool,
) {
	status := string(result.Status)
	if status == "" {
		status = "all"
	}
	fmt.Fprintf(
		output,
		"Alerts: status=%s count=%d\n",
		status,
		len(result.Alerts),
	)
	for _, alert := range result.Alerts {
		fmt.Fprintf(
			output,
			"- id=%d severity=%s status=%s occurrences=%d last_detected_at=%s title=%s\n",
			alert.ID,
			alert.Severity,
			alert.Status,
			alert.OccurrenceCount,
			alert.LastDetectedAt.Format(time.RFC3339Nano),
			alert.Title,
		)
		if includePayload {
			fmt.Fprintf(output, "  payload=%s\n", alert.Payload)
		}
	}
}
