package codexcli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const defaultReasoningEffort = "medium"

type Config struct {
	Executable      string
	WorkDir         string
	ReasoningEffort string
	LiveWebSearch   bool
	OutputSchema    []byte
}

type Runner struct {
	executable      string
	workDir         string
	reasoningEffort string
	liveWebSearch   bool
	outputSchema    string
	environment     []string
}

func New(config Config) (*Runner, error) {
	executable, err := discoverExecutable(config.Executable)
	if err != nil {
		return nil, err
	}
	workDir := strings.TrimSpace(config.WorkDir)
	if workDir == "" {
		return nil, fmt.Errorf("Codex CLI working directory is required")
	}
	workDir, err = filepath.Abs(workDir)
	if err != nil {
		return nil, fmt.Errorf(
			"resolve Codex CLI working directory %q: %w",
			config.WorkDir,
			err,
		)
	}
	info, err := os.Stat(workDir)
	if err != nil {
		return nil, fmt.Errorf(
			"inspect Codex CLI working directory %q: %w",
			workDir,
			err,
		)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf(
			"Codex CLI working directory %q is not a directory",
			workDir,
		)
	}
	reasoningEffort := strings.ToLower(
		strings.TrimSpace(config.ReasoningEffort),
	)
	if reasoningEffort == "" {
		reasoningEffort = defaultReasoningEffort
	}
	switch reasoningEffort {
	case "minimal", "low", "medium", "high":
	default:
		return nil, fmt.Errorf(
			"Codex CLI reasoning effort %q is invalid",
			reasoningEffort,
		)
	}
	outputSchema := ""
	if len(config.OutputSchema) > 0 {
		if !json.Valid(config.OutputSchema) {
			return nil, fmt.Errorf("Codex CLI output schema is not valid JSON")
		}
		outputSchema = filepath.Join(workDir, "codex-output-schema.json")
		if err := os.WriteFile(outputSchema, config.OutputSchema, 0o600); err != nil {
			return nil, fmt.Errorf(
				"write Codex CLI output schema %q: %w",
				outputSchema,
				err,
			)
		}
	}
	return &Runner{
		executable:      executable,
		workDir:         workDir,
		reasoningEffort: reasoningEffort,
		liveWebSearch:   config.LiveWebSearch,
		outputSchema:    outputSchema,
		environment:     environmentWithoutAPIKeys(os.Environ()),
	}, nil
}

func (r *Runner) CheckChatGPTAuth(ctx context.Context) error {
	command := exec.CommandContext(
		ctx,
		r.executable,
		"login",
		"status",
	)
	command.Dir = r.workDir
	command.Env = r.environment
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf(
			"check Codex CLI ChatGPT authentication: %w",
			err,
		)
	}
	status := strings.TrimSpace(string(output))
	if !strings.Contains(
		strings.ToLower(status),
		"logged in using chatgpt",
	) {
		return fmt.Errorf(
			"Codex CLI must be logged in using ChatGPT; current status did not confirm ChatGPT authentication",
		)
	}
	return nil
}

func (r *Runner) Analyze(
	ctx context.Context,
	prompt string,
) (string, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return "", fmt.Errorf("Codex CLI analysis prompt is required")
	}
	if err := r.CheckChatGPTAuth(ctx); err != nil {
		return "", err
	}
	command := exec.CommandContext(
		ctx,
		r.executable,
		r.executionArguments()...,
	)
	command.Dir = r.workDir
	command.Env = r.environment
	command.Stdin = strings.NewReader(prompt)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("run Codex CLI analysis: %w", err)
	}
	response := strings.TrimSpace(stdout.String())
	if response == "" {
		return "", fmt.Errorf(
			"Codex CLI analysis returned an empty final response",
		)
	}
	return response, nil
}

func (r *Runner) executionArguments() []string {
	arguments := make([]string, 0, 20)
	if r.liveWebSearch {
		arguments = append(arguments, "--search")
	}
	arguments = append(arguments,
		"exec",
		"--ephemeral",
		"--ignore-user-config",
		"--sandbox",
		"read-only",
		"--skip-git-repo-check",
		"--color",
		"never",
		"--cd",
		r.workDir,
		"--config",
		fmt.Sprintf(
			"model_reasoning_effort=%q",
			r.reasoningEffort,
		),
	)
	if r.outputSchema != "" {
		arguments = append(
			arguments,
			"--output-schema",
			r.outputSchema,
		)
	}
	return append(arguments, "-")
}

func discoverExecutable(configured string) (string, error) {
	configured = strings.TrimSpace(configured)
	if configured != "" {
		return validateExecutable(configured)
	}
	if runtime.GOOS == "windows" {
		appData := strings.TrimSpace(os.Getenv("APPDATA"))
		if appData != "" {
			pattern := filepath.Join(
				appData,
				"npm",
				"node_modules",
				"@openai",
				"codex",
				"node_modules",
				"@openai",
				"codex-win32-*",
				"vendor",
				"*",
				"bin",
				"codex.exe",
			)
			matches, err := filepath.Glob(pattern)
			if err != nil {
				return "", fmt.Errorf(
					"search npm Codex CLI executable: %w",
					err,
				)
			}
			for _, match := range matches {
				if executable, err := validateExecutable(match); err == nil {
					return executable, nil
				}
			}
		}
	}
	executable, err := exec.LookPath("codex")
	if err != nil {
		return "", fmt.Errorf(
			"find Codex CLI executable: install @openai/codex or provide -codex-path",
		)
	}
	return validateExecutable(executable)
}

func validateExecutable(path string) (string, error) {
	path = strings.TrimSpace(path)
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf(
			"resolve Codex CLI executable %q: %w",
			path,
			err,
		)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return "", fmt.Errorf(
			"inspect Codex CLI executable %q: %w",
			absolute,
			err,
		)
	}
	if info.IsDir() {
		return "", fmt.Errorf(
			"Codex CLI executable %q is a directory",
			absolute,
		)
	}
	return absolute, nil
}

func environmentWithoutAPIKeys(environment []string) []string {
	filtered := make([]string, 0, len(environment))
	for _, value := range environment {
		key, _, found := strings.Cut(value, "=")
		if found {
			switch strings.ToUpper(strings.TrimSpace(key)) {
			case "OPENAI_API_KEY", "CODEX_API_KEY":
				continue
			}
		}
		filtered = append(filtered, value)
	}
	return filtered
}
