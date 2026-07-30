package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/codexcli"
	"github.com/rowlet9g/stock-research-bot/internal/decimal"
	"github.com/rowlet9g/stock-research-bot/internal/emaildelivery"
	"github.com/rowlet9g/stock-research-bot/internal/models"
	"github.com/rowlet9g/stock-research-bot/internal/reporting"
	sqlitestore "github.com/rowlet9g/stock-research-bot/internal/storage/sqlite"
)

type recordingPortfolioCodexAnalyzer struct {
	Calls    int
	Prompt   string
	Response string
	Err      error
}

func (a *recordingPortfolioCodexAnalyzer) Analyze(
	_ context.Context,
	prompt string,
) (string, error) {
	a.Calls++
	a.Prompt = prompt
	return a.Response, a.Err
}

func TestPortfolioCodexEmailBuildsAndStoresAnalysisPreview(
	t *testing.T,
) {
	databasePath, generatedAt := seedPortfolioCodexCommandDatabase(t)
	responsePath := filepath.Join(t.TempDir(), "response.md")
	fake := &recordingPortfolioCodexAnalyzer{
		Response: portfolioCodexTestResponse(),
	}
	originalAnalyzer := newPortfolioCodexAnalyzer
	originalNow := portfolioCodexEmailNow
	originalPrice := portfolioAnalyzePrice
	var analyzerConfig codexcli.Config
	newPortfolioCodexAnalyzer = func(
		config codexcli.Config,
	) (portfolioCodexAnalyzer, error) {
		analyzerConfig = config
		return fake, nil
	}
	portfolioCodexEmailNow = func() time.Time { return generatedAt }
	portfolioAnalyzePrice = portfolioCodexTestPrice(generatedAt)
	t.Cleanup(func() {
		newPortfolioCodexAnalyzer = originalAnalyzer
		portfolioCodexEmailNow = originalNow
		portfolioAnalyzePrice = originalPrice
	})

	output := runCommand(
		t,
		"portfolio-codex-email",
		"-db", databasePath,
		"-env", filepath.Join(t.TempDir(), "missing.env"),
		"-question", "포트폴리오의 핵심 위험과 대응 조건을 검토해.",
		"-codex-reasoning", "low",
		"-response-file", responsePath,
		"-output", "json",
	)
	var result portfolioCodexEmailCommandResult
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("decode portfolio Codex preview: %v\n%s", err, output)
	}
	storedResponse, err := os.ReadFile(responsePath)
	if err != nil {
		t.Fatalf("read generated Codex response: %v", err)
	}
	if fake.Calls != 1 ||
		!strings.Contains(fake.Prompt, "셸 명령, 파일 읽기·쓰기") ||
		!strings.Contains(fake.Prompt, "결론 전체를 유보하지 말고") ||
		!strings.Contains(fake.Prompt, "40% 기준 재배분액") ||
		!strings.Contains(fake.Prompt, "Markdown 제목") ||
		!strings.Contains(fake.Prompt, "USD는 소수점 둘째 자리") ||
		!strings.Contains(fake.Prompt, "포트폴리오의 핵심 위험") ||
		!strings.Contains(fake.Prompt, "평가금액=400") ||
		analyzerConfig.ReasoningEffort != "low" ||
		analyzerConfig.WorkDir == "" ||
		result.AnalysisRunID <= 0 ||
		result.Report.ResponseOrigin !=
			reporting.PortfolioResponseOriginCodexChatGPT ||
		result.Report.Response != fake.Response ||
		result.Delivery.Requested ||
		result.Delivery.Sent ||
		strings.TrimSpace(string(storedResponse)) != fake.Response ||
		!strings.Contains(result.Body, "즉시 행동안") ||
		strings.Contains(result.Body, "SHA-256") {
		t.Fatalf(
			"unexpected portfolio Codex preview: result=%#v fake=%#v config=%#v",
			result,
			fake,
			analyzerConfig,
		)
	}

	ctx := context.Background()
	store, err := sqlitestore.Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("open generated analysis store: %v", err)
	}
	defer store.Close()
	run, err := store.AnalysisRun(ctx, result.AnalysisRunID)
	if err != nil {
		t.Fatalf("load generated analysis run: %v", err)
	}
	if run.Kind != portfolioBriefAnalysisKind ||
		run.InputSHA256 != result.Report.InputSHA256 ||
		run.OutputSHA256 != result.Report.AnalysisOutputSHA256 {
		t.Fatalf(
			"generated report was not linked to its analysis run: run=%#v report=%#v",
			run,
			result.Report,
		)
	}
}

func TestPortfolioCodexEmailSendsGeneratedAnalysis(
	t *testing.T,
) {
	databasePath, generatedAt := seedPortfolioCodexCommandDatabase(t)
	configureDailyEmailEnvironment(t)
	fakeAnalyzer := &recordingPortfolioCodexAnalyzer{
		Response: portfolioCodexTestResponse(),
	}
	fakeSender := &recordingDailyEmailSender{}
	originalAnalyzer := newPortfolioCodexAnalyzer
	originalSender := newPortfolioCodexEmailSender
	originalNow := portfolioCodexEmailNow
	originalPrice := portfolioAnalyzePrice
	newPortfolioCodexAnalyzer = func(
		_ codexcli.Config,
	) (portfolioCodexAnalyzer, error) {
		return fakeAnalyzer, nil
	}
	newPortfolioCodexEmailSender = func(
		_ emaildelivery.SMTPConfig,
	) (dailyEmailSender, error) {
		return fakeSender, nil
	}
	portfolioCodexEmailNow = func() time.Time { return generatedAt }
	portfolioAnalyzePrice = portfolioCodexTestPrice(generatedAt)
	t.Cleanup(func() {
		newPortfolioCodexAnalyzer = originalAnalyzer
		newPortfolioCodexEmailSender = originalSender
		portfolioCodexEmailNow = originalNow
		portfolioAnalyzePrice = originalPrice
	})

	output := runCommand(
		t,
		"portfolio-codex-email",
		"-db", databasePath,
		"-env", filepath.Join(t.TempDir(), "missing.env"),
		"-response-file", filepath.Join(t.TempDir(), "response.md"),
		"-send",
		"-output", "json",
	)
	var result portfolioCodexEmailCommandResult
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("decode portfolio Codex delivery: %v\n%s", err, output)
	}
	if fakeAnalyzer.Calls != 1 ||
		fakeSender.Calls != 1 ||
		!result.Delivery.Requested ||
		!result.Delivery.Sent ||
		result.Delivery.RecipientCount != 1 ||
		!strings.Contains(
			fakeSender.Content,
			"Content-Type: text/plain; charset=UTF-8",
		) {
		t.Fatalf(
			"unexpected portfolio Codex delivery: result=%#v analyzer=%#v sender=%#v",
			result,
			fakeAnalyzer,
			fakeSender,
		)
	}
}

func TestPortfolioCodexEmailRejectsPromptEcho(t *testing.T) {
	portfolioPrompt := strings.Repeat("포트폴리오 입력 ", 30)
	analysisPrompt := buildPortfolioCodexAnalysisPrompt(portfolioPrompt)
	if _, err := validateGeneratedPortfolioResponse(
		analysisPrompt,
		analysisPrompt,
		portfolioPrompt,
	); err == nil || !strings.Contains(err.Error(), "input prompt") {
		t.Fatalf("Codex prompt echo was accepted: %v", err)
	}
}

func TestPortfolioCodexEmailReportsAnalyzerFailure(t *testing.T) {
	databasePath, generatedAt := seedPortfolioCodexCommandDatabase(t)
	fake := &recordingPortfolioCodexAnalyzer{
		Err: context.DeadlineExceeded,
	}
	originalAnalyzer := newPortfolioCodexAnalyzer
	originalNow := portfolioCodexEmailNow
	originalPrice := portfolioAnalyzePrice
	newPortfolioCodexAnalyzer = func(
		_ codexcli.Config,
	) (portfolioCodexAnalyzer, error) {
		return fake, nil
	}
	portfolioCodexEmailNow = func() time.Time { return generatedAt }
	portfolioAnalyzePrice = portfolioCodexTestPrice(generatedAt)
	t.Cleanup(func() {
		newPortfolioCodexAnalyzer = originalAnalyzer
		portfolioCodexEmailNow = originalNow
		portfolioAnalyzePrice = originalPrice
	})

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run(
		[]string{
			"portfolio-codex-email",
			"-db", databasePath,
			"-env", filepath.Join(t.TempDir(), "missing.env"),
			"-response-file", filepath.Join(t.TempDir(), "response.md"),
		},
		&stdout,
		&stderr,
	)
	if exitCode == 0 ||
		!strings.Contains(stderr.String(), "context deadline exceeded") ||
		fake.Calls != 1 {
		t.Fatalf(
			"unexpected analyzer failure: exit=%d stdout=%q stderr=%q fake=%#v",
			exitCode,
			stdout.String(),
			stderr.String(),
			fake,
		)
	}
}

func seedPortfolioCodexCommandDatabase(
	t *testing.T,
) (string, time.Time) {
	t.Helper()
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "forgetmenot.db")
	store, err := sqlitestore.Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("open portfolio Codex store: %v", err)
	}
	defer store.Close()
	if _, err := store.SyncInstruments(ctx, []models.WatchlistItem{
		{
			Name:        "Apple",
			Ticker:      "AAPL",
			YahooTicker: "AAPL",
			Market:      "NASDAQ",
			Currency:    "USD",
		},
	}); err != nil {
		t.Fatalf("seed portfolio Codex instrument: %v", err)
	}
	generatedAt := time.Date(2026, 7, 30, 1, 0, 0, 0, time.UTC)
	if _, err := store.UpsertPosition(
		ctx,
		"AAPL",
		2*decimal.Scale,
		150*decimal.Scale,
		"USD",
		generatedAt.Add(-time.Hour),
	); err != nil {
		t.Fatalf("seed portfolio Codex position: %v", err)
	}
	return databasePath, generatedAt
}

func portfolioCodexTestPrice(
	generatedAt time.Time,
) func(context.Context, string) (models.PriceSnapshot, error) {
	return func(
		_ context.Context,
		yahooTicker string,
	) (models.PriceSnapshot, error) {
		lastPrice := 200.0
		observedAt := generatedAt.Add(-time.Hour)
		return models.PriceSnapshot{
			YahooTicker: yahooTicker,
			Currency:    "USD",
			Status:      models.DataStatusAvailable,
			LastPrice:   &lastPrice,
			Source: models.SourceMetadata{
				Provider:   "fixture",
				SourceURL:  "https://example.test/" + yahooTicker,
				ObservedAt: &observedAt,
				FetchedAt:  generatedAt,
			},
		}, nil
	}
}

func portfolioCodexTestResponse() string {
	return strings.TrimSpace(
		`1. 핵심 결론

현재 포트폴리오는 단일 종목 비중이 높아 가격 변동의 영향을 크게 받는다.
추가 매수는 보류하고 40% 집중도 기준까지 비중을 낮추는 안을 먼저 검토하는 편이 합리적이다.

2. 즉시 행동안

고집중 종목은 제공된 재배분 참고액을 기준으로 두세 차례에 나누어 조정하는 안을 우선한다.

3. 활성 종목 판단

Apple: 가격 타이밍 결과는 유리하지만 집중도가 높으므로 추가 매수는 보류하고 비중 축소를 검토한다.

4. 추가 매수와 신규 편입

기존 고집중 종목의 추가 매수는 보류하는 편이 합리적이다.

5. 다음 확인사항

실적, 공시, 산업 지표와 투자 가설을 갱신하면 조정 강도를 다시 판단할 수 있다.`,
	)
}
