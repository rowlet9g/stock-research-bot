package codexcli

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestNewBuildsReadOnlyEphemeralArguments(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "codex-test")
	if err := os.WriteFile(executable, []byte("test"), 0o700); err != nil {
		t.Fatalf("write fake Codex executable: %v", err)
	}
	runner, err := New(Config{
		Executable:      executable,
		WorkDir:         t.TempDir(),
		ReasoningEffort: "low",
	})
	if err != nil {
		t.Fatalf("create Codex runner: %v", err)
	}
	arguments := runner.executionArguments()
	for _, expected := range []string{
		"exec",
		"--ephemeral",
		"--ignore-user-config",
		"read-only",
		"--skip-git-repo-check",
		"model_reasoning_effort=\"low\"",
		"-",
	} {
		if !contains(arguments, expected) {
			t.Fatalf(
				"Codex arguments missing %q: %#v",
				expected,
				arguments,
			)
		}
	}
}

func TestEnvironmentWithoutAPIKeys(t *testing.T) {
	input := []string{
		"PATH=test",
		"OPENAI_API_KEY=secret",
		"codex_api_key=secret-two",
		"CODEX_HOME=home",
	}
	got := environmentWithoutAPIKeys(input)
	want := []string{"PATH=test", "CODEX_HOME=home"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected filtered environment: %#v", got)
	}
}

func TestNewRejectsInvalidConfiguration(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "codex-test")
	if err := os.WriteFile(executable, []byte("test"), 0o700); err != nil {
		t.Fatalf("write fake Codex executable: %v", err)
	}
	if _, err := New(Config{
		Executable:      executable,
		WorkDir:         t.TempDir(),
		ReasoningEffort: "xhigh",
	}); err == nil || !strings.Contains(err.Error(), "reasoning effort") {
		t.Fatalf("invalid reasoning effort was accepted: %v", err)
	}
	if _, err := New(Config{
		Executable: filepath.Join(t.TempDir(), "missing"),
		WorkDir:    t.TempDir(),
	}); err == nil || !strings.Contains(err.Error(), "executable") {
		t.Fatalf("missing executable was accepted: %v", err)
	}
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
