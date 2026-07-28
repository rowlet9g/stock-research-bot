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

type riskAssessmentCommandResult struct {
	Snapshot   analysis.AnalysisInputSnapshot `json:"snapshot"`
	Assessment analysis.RiskAssessment        `json:"assessment"`
}

func runRiskAssessment(
	args []string,
	stdout io.Writer,
	stderr io.Writer,
) int {
	flags := flag.NewFlagSet("risk-assess", flag.ContinueOnError)
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

	evaluatedAt := analysisSnapshotNow()
	snapshot, err := buildStoredAnalysisSnapshot(
		ctx,
		store,
		*ticker,
		*disclosureLimit,
		*fsKind,
		evaluatedAt,
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
	assessment, err := analysis.EvaluateRiskSnapshot(
		snapshot,
		evaluatedAt,
		analysis.DefaultRiskRuleConfig(),
	)
	if err != nil {
		return writeRuntimeFailure(
			*outputFormat,
			stdout,
			stderr,
			"risk_assessment",
			err,
		)
	}
	result := riskAssessmentCommandResult{
		Snapshot:   snapshot,
		Assessment: assessment,
	}
	if *outputFormat == "json" {
		if err := writeJSON(stdout, result); err != nil {
			return writeRuntimeFailure("text", stdout, stderr, "output", err)
		}
		return 0
	}
	writeRiskAssessmentText(stdout, result)
	return 0
}

func writeRiskAssessmentText(
	output io.Writer,
	result riskAssessmentCommandResult,
) {
	assessment := result.Assessment
	fmt.Fprintf(
		output,
		"Risk assessment: status=%s ticker=%s rules=%s input_sha256=%s findings=%d\n",
		assessment.Status,
		result.Snapshot.Portfolio.Instrument.Ticker,
		assessment.RuleSetVersion,
		assessment.InputSHA256,
		len(assessment.Findings),
	)
	fmt.Fprintf(
		output,
		"Evaluated at: %s\n",
		assessment.EvaluatedAt.Format(time.RFC3339Nano),
	)
	if len(assessment.Findings) == 0 {
		fmt.Fprintln(output, "Findings: none")
	} else {
		fmt.Fprintln(output, "Findings:")
	}
	for _, finding := range assessment.Findings {
		fmt.Fprintf(
			output,
			"- [%s] [%s] %s (%s)\n",
			finding.Severity,
			finding.Category,
			finding.Title,
			finding.RuleID,
		)
		fmt.Fprintf(output, "  사실: %s\n", finding.Fact)
		fmt.Fprintf(
			output,
			"  가능한 해석: %s\n",
			finding.PossibleInterpretation,
		)
		for _, question := range finding.ValidationQuestions {
			fmt.Fprintf(output, "  확인 질문: %s\n", question)
		}
	}
	if len(assessment.InputIssues) == 0 {
		return
	}
	fmt.Fprintln(output, "Input issues:")
	for _, issue := range assessment.InputIssues {
		fmt.Fprintf(
			output,
			"- scope=%s kind=%s message=%s\n",
			issue.Scope,
			issue.Kind,
			issue.Message,
		)
	}
}
