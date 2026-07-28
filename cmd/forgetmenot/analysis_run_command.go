package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/models"
	sqlitestore "github.com/rowlet9g/stock-research-bot/internal/storage/sqlite"
)

type analysisRunListResult struct {
	Kind string               `json:"kind"`
	Runs []models.AnalysisRun `json:"runs"`
}

func runAnalysisRunList(
	args []string,
	stdout io.Writer,
	stderr io.Writer,
) int {
	flags := flag.NewFlagSet("analysis-run-list", flag.ContinueOnError)
	flags.SetOutput(stderr)
	databasePath := flags.String("db", defaultDatabasePath, "SQLite database path")
	kind := flags.String("kind", "", "analysis run kind")
	limit := flags.Int("limit", 20, "maximum runs from 1 to 500")
	includePayload := flags.Bool("include-payload", false, "include stored JSON payload")
	outputFormat := flags.String("output", "text", "output format: text or json")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if exitCode := validateCommandOutput(outputFormat, stdout, stderr); exitCode != 0 {
		return exitCode
	}
	*kind = strings.TrimSpace(*kind)
	switch {
	case *kind == "":
		return commandInputError(
			*outputFormat,
			stdout,
			stderr,
			"kind is required",
		)
	case *limit <= 0 || *limit > 500:
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
	runs, err := store.ListAnalysisRuns(ctx, *kind, *limit)
	if err != nil {
		return writeRuntimeFailure(
			*outputFormat,
			stdout,
			stderr,
			"analysis_runs",
			err,
		)
	}
	if !*includePayload {
		for index := range runs {
			runs[index].Payload = nil
		}
	}
	result := analysisRunListResult{
		Kind: *kind,
		Runs: runs,
	}
	if *outputFormat == "json" {
		if err := writeJSON(stdout, result); err != nil {
			return writeRuntimeFailure("text", stdout, stderr, "output", err)
		}
		return 0
	}
	fmt.Fprintf(
		stdout,
		"Analysis runs: kind=%s count=%d\n",
		result.Kind,
		len(result.Runs),
	)
	for _, run := range result.Runs {
		fmt.Fprintf(
			stdout,
			"- id=%d status=%s generated_at=%s rules=%s input_sha256=%s output_sha256=%s\n",
			run.ID,
			run.Status,
			run.GeneratedAt.Format(time.RFC3339Nano),
			run.RuleVersion,
			run.InputSHA256,
			run.OutputSHA256,
		)
		if *includePayload {
			fmt.Fprintf(stdout, "  payload=%s\n", run.Payload)
		}
	}
	return 0
}
