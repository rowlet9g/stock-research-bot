package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/analysis"
	"github.com/rowlet9g/stock-research-bot/internal/emaildelivery"
	"github.com/rowlet9g/stock-research-bot/internal/models"
	"github.com/rowlet9g/stock-research-bot/internal/prompt"
	sqlitestore "github.com/rowlet9g/stock-research-bot/internal/storage/sqlite"
)

func TestPortfolioResponseEmailPreviewLinksStoredAnalysisRun(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "forgetmenot.db")
	run := seedPortfolioResponseAnalysisRun(t, ctx, databasePath)
	promptHash := sha256.Sum256([]byte("portfolio prompt"))
	responsePath := filepath.Join(t.TempDir(), "response.md")
	if err := os.WriteFile(
		responsePath,
		[]byte(
			"# 핵심 요약\r\n\r\n집중 위험을 먼저 검토한다.\r\n\r\n"+
				strings.Repeat(
					"통화별 집중도와 투자 가설을 사실과 해석으로 나누어 검토한다. ",
					8,
				),
		),
		0o600,
	); err != nil {
		t.Fatalf("write response file: %v", err)
	}

	originalNow := portfolioResponseEmailNow
	portfolioResponseEmailNow = func() time.Time {
		return time.Date(2026, 7, 30, 1, 0, 0, 0, time.UTC)
	}
	t.Cleanup(func() { portfolioResponseEmailNow = originalNow })

	output := runCommand(
		t,
		"portfolio-response-email",
		"-db", databasePath,
		"-env", filepath.Join(t.TempDir(), "missing.env"),
		"-run-id", "1",
		"-file", responsePath,
		"-output", "json",
	)
	var result portfolioResponseEmailCommandResult
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("decode portfolio response preview: %v\n%s", err, output)
	}
	if result.Report.AnalysisRunID != run.ID ||
		result.Report.InputSHA256 != run.InputSHA256 ||
		result.Report.AnalysisOutputSHA256 != run.OutputSHA256 ||
		result.Report.PromptSHA256 != hex.EncodeToString(promptHash[:]) ||
		result.Report.ResponseSHA256 == "" ||
		result.Delivery.Requested ||
		result.Delivery.Sent ||
		!strings.Contains(result.Body, "집중 위험을 먼저 검토한다.") {
		t.Fatalf("unexpected portfolio response preview: %#v", result)
	}
}

func TestPortfolioResponseEmailSendsThroughConfiguredSMTP(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "forgetmenot.db")
	run := seedPortfolioResponseAnalysisRun(t, ctx, databasePath)
	responsePath := filepath.Join(t.TempDir(), "response.txt")
	if err := os.WriteFile(
		responsePath,
		[]byte(strings.Repeat(
			"포트폴리오 상세 분석에서 집중도와 보유 가설을 함께 검토한다. ",
			10,
		)),
		0o600,
	); err != nil {
		t.Fatalf("write response file: %v", err)
	}
	configureDailyEmailEnvironment(t)

	originalNow := portfolioResponseEmailNow
	originalSender := newPortfolioResponseEmailSender
	generatedAt := time.Date(2026, 7, 30, 1, 0, 0, 0, time.UTC)
	portfolioResponseEmailNow = func() time.Time { return generatedAt }
	fake := &recordingDailyEmailSender{}
	newPortfolioResponseEmailSender = func(
		_ emaildelivery.SMTPConfig,
	) (dailyEmailSender, error) {
		return fake, nil
	}
	t.Cleanup(func() {
		portfolioResponseEmailNow = originalNow
		newPortfolioResponseEmailSender = originalSender
	})

	output := runCommand(
		t,
		"portfolio-response-email",
		"-db", databasePath,
		"-env", filepath.Join(t.TempDir(), "missing.env"),
		"-run-id", "1",
		"-file", responsePath,
		"-send",
		"-output", "json",
	)
	var result portfolioResponseEmailCommandResult
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("decode portfolio response send: %v\n%s", err, output)
	}
	if result.Report.AnalysisRunID != run.ID ||
		!result.Delivery.Requested ||
		!result.Delivery.Sent ||
		result.Delivery.RecipientCount != 1 ||
		fake.Calls != 1 ||
		!strings.Contains(fake.Content, "Content-Type: text/plain") {
		t.Fatalf(
			"unexpected portfolio response delivery: result=%#v fake=%#v",
			result,
			fake,
		)
	}
}

func TestReadPortfolioResponseFileRejectsInvalidContent(t *testing.T) {
	emptyPath := filepath.Join(t.TempDir(), "empty.md")
	if err := os.WriteFile(emptyPath, []byte(" \r\n"), 0o600); err != nil {
		t.Fatalf("write empty response: %v", err)
	}
	if _, err := readPortfolioResponseFile(emptyPath); err == nil ||
		!strings.Contains(err.Error(), "content is required") {
		t.Fatalf("empty response was accepted: %v", err)
	}
	if _, err := readPortfolioResponseFile(
		filepath.Join(t.TempDir(), "missing.md"),
	); err == nil || !strings.Contains(err.Error(), "read portfolio response") {
		t.Fatalf("missing response was accepted: %v", err)
	}
	commandPath := filepath.Join(t.TempDir(), "command.md")
	if err := os.WriteFile(
		commandPath,
		[]byte(
			"Get-Clipboard -Raw |\n"+
				"Set-Content -LiteralPath data/reports/response.md",
		),
		0o600,
	); err != nil {
		t.Fatalf("write command response: %v", err)
	}
	if _, err := readPortfolioResponseFile(commandPath); err == nil ||
		!strings.Contains(err.Error(), "clipboard capture") {
		t.Fatalf("clipboard command response was accepted: %v", err)
	}
	largePath := filepath.Join(t.TempDir(), "large.md")
	if err := os.WriteFile(
		largePath,
		bytes.Repeat([]byte("a"), maxPortfolioResponseBytes+1),
		0o600,
	); err != nil {
		t.Fatalf("write large response: %v", err)
	}
	if _, err := readPortfolioResponseFile(largePath); err == nil ||
		!strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized response was accepted: %v", err)
	}
}

func TestDecodePortfolioBriefAnalysisRunRejectsPromptHashMismatch(
	t *testing.T,
) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "forgetmenot.db")
	run := seedPortfolioResponseAnalysisRun(t, ctx, databasePath)
	var result portfolioBriefCommandResult
	if err := json.Unmarshal(run.Payload, &result); err != nil {
		t.Fatalf("decode response seed payload: %v", err)
	}
	result.Brief.PromptSHA256 = strings.Repeat("f", 64)
	payload, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("encode tampered response seed payload: %v", err)
	}
	outputHash := sha256.Sum256(payload)
	run.Payload = payload
	run.OutputSHA256 = hex.EncodeToString(outputHash[:])

	if _, err := decodePortfolioBriefAnalysisRun(run); err == nil ||
		!strings.Contains(err.Error(), "prompt hash") {
		t.Fatalf("prompt hash mismatch was accepted: %v", err)
	}
}

func TestSamePortfolioResponseAndPromptNormalizesLineEndings(t *testing.T) {
	if !samePortfolioResponseAndPrompt(
		"first\r\nsecond\r\n",
		"first\nsecond",
	) {
		t.Fatal("equivalent prompt and response were not detected")
	}
	if samePortfolioResponseAndPrompt(
		"first\nanalysis",
		"first\nprompt",
	) {
		t.Fatal("different response was treated as the prompt")
	}
}

func seedPortfolioResponseAnalysisRun(
	t *testing.T,
	ctx context.Context,
	databasePath string,
) models.AnalysisRun {
	t.Helper()
	store, err := sqlitestore.Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("open response seed store: %v", err)
	}
	defer store.Close()

	generatedAt := time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC)
	valuation := analysis.PortfolioValuationReport{
		Version:     analysis.PortfolioValuationVersion,
		Status:      models.DataStatusAvailable,
		GeneratedAt: generatedAt,
		Config:      analysis.DefaultPortfolioValuationConfig(),
		Currencies:  []analysis.CurrencyValuation{},
		Positions:   []analysis.PositionValuation{},
		Issues:      []analysis.PortfolioValuationIssue{},
	}
	inputHash, err := analysis.PortfolioValuationSHA256(valuation)
	if err != nil {
		t.Fatalf("hash response seed valuation: %v", err)
	}
	promptText := "portfolio prompt"
	promptHash := sha256.Sum256([]byte(promptText))
	result := portfolioBriefCommandResult{
		Valuation: valuation,
		Scenarios: analysis.PortfolioScenarioReport{},
		Brief: prompt.PortfolioResearchBrief{
			Version:         prompt.PortfolioResearchBriefVersion,
			GeneratedAt:     generatedAt,
			ValuationSHA256: inputHash,
			PromptSHA256:    hex.EncodeToString(promptHash[:]),
			Prompt:          promptText,
		},
	}
	payload, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("encode response seed run: %v", err)
	}
	run, _, err := store.SaveAnalysisRun(
		ctx,
		sqlitestore.AnalysisRunInput{
			Kind:        portfolioBriefAnalysisKind,
			Status:      models.DataStatusAvailable,
			InputSHA256: inputHash,
			RuleVersion: result.Brief.Version,
			Payload:     payload,
			GeneratedAt: generatedAt,
		},
	)
	if err != nil {
		t.Fatalf("save response seed run: %v", err)
	}
	return run
}
