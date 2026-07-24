package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestRunWritesStructuredJSONForInputError(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := run([]string{
		"-output", "json",
		"-watchlist", "does-not-exist.csv",
	}, &stdout, &stderr)

	if exitCode != 2 {
		t.Fatalf("expected exit code 2, got %d", exitCode)
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}

	var payload struct {
		Error outputIssue `json:"error"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode JSON output: %v\n%s", err, stdout.String())
	}
	if payload.Error.Scope != "watchlist" || payload.Error.Kind != "invalid_input" {
		t.Fatalf("unexpected error payload: %#v", payload.Error)
	}
}

func TestRunRejectsUnsupportedOutputFormat(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := run([]string{"-output", "yaml"}, &stdout, &stderr)

	if exitCode != 2 {
		t.Fatalf("expected exit code 2, got %d", exitCode)
	}
	if !strings.Contains(stderr.String(), `output must be "text" or "json"`) {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
}
