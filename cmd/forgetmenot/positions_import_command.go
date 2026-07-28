package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	sqlitestore "github.com/rowlet9g/stock-research-bot/internal/storage/sqlite"
)

func runPositionsImport(
	args []string,
	stdout io.Writer,
	stderr io.Writer,
) int {
	flags := flag.NewFlagSet("positions-import", flag.ContinueOnError)
	flags.SetOutput(stderr)
	databasePath := flags.String("db", defaultDatabasePath, "SQLite database path")
	filePath := flags.String("file", "", "current position CSV path")
	outputFormat := flags.String("output", "text", "output format: text or json")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if exitCode := validateCommandOutput(outputFormat, stdout, stderr); exitCode != 0 {
		return exitCode
	}
	*filePath = strings.TrimSpace(*filePath)
	if *filePath == "" {
		return commandInputError(
			*outputFormat,
			stdout,
			stderr,
			"file is required",
		)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := sqlitestore.Open(ctx, *databasePath)
	if err != nil {
		return writeRuntimeFailure(*outputFormat, stdout, stderr, "storage", err)
	}
	defer store.Close()
	result, err := store.ImportPositionCSV(ctx, *filePath)
	if err != nil {
		return writeRuntimeFailure(
			*outputFormat,
			stdout,
			stderr,
			"positions_import",
			err,
		)
	}
	return writeCommandResult(
		*outputFormat,
		stdout,
		fmt.Sprintf(
			"Positions imported: rows=%d changed=%d unchanged=%d file=%s\n",
			result.RowsSeen,
			result.RowsChanged,
			result.RowsUnchanged,
			result.File,
		),
		result,
	)
}
