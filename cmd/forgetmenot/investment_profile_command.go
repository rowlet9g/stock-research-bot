package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/investmentprofile"
	sqlitestore "github.com/rowlet9g/stock-research-bot/internal/storage/sqlite"
)

const defaultInvestmentProfilePath = "data/investment_profile.json"

type investmentProfileSyncCommandResult struct {
	ProfileVersion string                       `json:"profile_version"`
	ProfilePath    string                       `json:"profile_path"`
	Sync           sqlitestore.ThesisSyncResult `json:"sync"`
}

func runInvestmentProfileSync(
	args []string,
	stdout io.Writer,
	stderr io.Writer,
) int {
	flags := flag.NewFlagSet("investment-profile-sync", flag.ContinueOnError)
	flags.SetOutput(stderr)
	databasePath := flags.String("db", defaultDatabasePath, "SQLite database path")
	profilePath := flags.String(
		"file",
		defaultInvestmentProfilePath,
		"investment profile JSON path",
	)
	outputFormat := flags.String("output", "text", "output format: text or json")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if exitCode := validateCommandOutput(outputFormat, stdout, stderr); exitCode != 0 {
		return exitCode
	}

	profile, err := investmentprofile.Load(*profilePath)
	if err != nil {
		return commandInputError(*outputFormat, stdout, stderr, err.Error())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	store, err := sqlitestore.Open(ctx, *databasePath)
	if err != nil {
		return writeRuntimeFailure(*outputFormat, stdout, stderr, "storage", err)
	}
	defer store.Close()

	syncResult, err := syncInvestmentProfile(ctx, store, profile)
	if err != nil {
		return writeRuntimeFailure(
			*outputFormat,
			stdout,
			stderr,
			"investment_profile",
			err,
		)
	}
	result := investmentProfileSyncCommandResult{
		ProfileVersion: profile.Version,
		ProfilePath:    strings.TrimSpace(*profilePath),
		Sync:           syncResult,
	}
	return writeCommandResult(
		*outputFormat,
		stdout,
		fmt.Sprintf(
			"Investment profile synced: rows=%d changed=%d unchanged=%d\n",
			result.Sync.RowsSeen,
			result.Sync.RowsChanged,
			result.Sync.RowsUnchanged,
		),
		result,
	)
}

func syncInvestmentProfile(
	ctx context.Context,
	store *sqlitestore.Store,
	profile investmentprofile.Profile,
) (sqlitestore.ThesisSyncResult, error) {
	rows := make([]sqlitestore.ThesisSyncRow, 0, len(profile.Theses))
	for index, thesis := range profile.Theses {
		rows = append(rows, sqlitestore.ThesisSyncRow{
			RowNumber: index + 1,
			Ticker:    thesis.Ticker,
			Thesis: sqlitestore.ThesisInput{
				AllocationCategory:     thesis.AllocationCategory,
				ProtectedQuantityUnits: thesis.ProtectedQuantityUnits,
				Summary:                thesis.Summary,
				InvalidationCondition:  thesis.InvalidationCondition,
				IncreaseCondition:      thesis.IncreaseCondition,
				ExpectedHoldingPeriod:  thesis.ExpectedHoldingPeriod,
				CheckMetrics:           thesis.CheckMetrics,
			},
		})
	}
	return store.SyncTheses(ctx, rows)
}
