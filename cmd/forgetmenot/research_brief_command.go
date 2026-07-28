package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/analysis"
	"github.com/rowlet9g/stock-research-bot/internal/prompt"
	sqlitestore "github.com/rowlet9g/stock-research-bot/internal/storage/sqlite"
)

type researchBriefCommandResult struct {
	Snapshot   analysis.AnalysisInputSnapshot `json:"snapshot"`
	Assessment analysis.RiskAssessment        `json:"assessment"`
	Prompt     string                         `json:"prompt"`
}

func runResearchBrief(
	args []string,
	stdout io.Writer,
	stderr io.Writer,
) int {
	flags := flag.NewFlagSet("research-brief", flag.ContinueOnError)
	flags.SetOutput(stderr)
	databasePath := flags.String("db", defaultDatabasePath, "SQLite database path")
	ticker := flags.String("ticker", "", "stored instrument ticker")
	disclosureLimit := flags.Int(
		"disclosure-limit",
		20,
		"maximum stored disclosures",
	)
	fsKind := flags.String("fs-div", "CFS", "CFS or OFS")
	question := flags.String("question", "", "research question")
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

	generatedAt := analysisSnapshotNow()
	snapshot, err := buildStoredAnalysisSnapshot(
		ctx,
		store,
		*ticker,
		*disclosureLimit,
		*fsKind,
		generatedAt,
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
		generatedAt,
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
	briefPrompt, err := prompt.BuildResearchBriefPrompt(prompt.ResearchBriefInput{
		Snapshot:     snapshot,
		Assessment:   assessment,
		UserQuestion: *question,
	})
	if err != nil {
		return writeRuntimeFailure(
			*outputFormat,
			stdout,
			stderr,
			"research_prompt",
			err,
		)
	}
	result := researchBriefCommandResult{
		Snapshot:   snapshot,
		Assessment: assessment,
		Prompt:     briefPrompt,
	}
	if *outputFormat == "json" {
		if err := writeJSON(stdout, result); err != nil {
			return writeRuntimeFailure("text", stdout, stderr, "output", err)
		}
		return 0
	}
	if _, err := fmt.Fprint(stdout, briefPrompt); err != nil {
		return writeRuntimeFailure("text", stdout, stderr, "output", err)
	}
	return 0
}
