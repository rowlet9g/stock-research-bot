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
	profilePath := filepath.Join(t.TempDir(), "investment-profile.json")
	if err := os.WriteFile(
		profilePath,
		[]byte(testInvestmentProfileJSON()),
		0o600,
	); err != nil {
		t.Fatalf("write investment profile: %v", err)
	}
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
		"-profile", profilePath,
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
		!strings.Contains(fake.Prompt, "셸 명령과 로컬 파일 읽기·쓰기") ||
		!strings.Contains(fake.Prompt, "실시간 웹 검색을 반드시") ||
		!strings.Contains(fake.Prompt, "partial_sell") ||
		!strings.Contains(fake.Prompt, "평단 하락 자체가 아니라") ||
		!strings.Contains(fake.Prompt, "실제 편입 후보 2~4개") ||
		!strings.Contains(fake.Prompt, "40% 기준 재배분액") ||
		!strings.Contains(fake.Prompt, "보호 수량이 있는 종목") ||
		!strings.Contains(fake.Prompt, "목표 배분: category=core") ||
		!strings.Contains(fake.Prompt, "증액조건=실적 확인 후 증액") ||
		!strings.Contains(fake.Prompt, "research_conclusions") ||
		!strings.Contains(fake.Prompt, "포트폴리오의 핵심 위험") ||
		!strings.Contains(fake.Prompt, "평가금액=400") ||
		analyzerConfig.ReasoningEffort != "low" ||
		!analyzerConfig.LiveWebSearch ||
		len(analyzerConfig.OutputSchema) == 0 ||
		analyzerConfig.WorkDir == "" ||
		result.AnalysisRunID <= 0 ||
		result.Report.ResponseOrigin !=
			reporting.PortfolioResponseOriginCodexWebResearch ||
		len(result.Advice.Holdings) != 1 ||
		result.Advice.Holdings[0].Action != "partial_sell" ||
		!strings.Contains(result.Report.Response, "AAPL: 부분 매도") ||
		result.Delivery.Requested ||
		result.Delivery.Sent ||
		strings.TrimSpace(string(storedResponse)) != result.Report.Response ||
		!strings.Contains(result.Body, "포트폴리오 조정안") ||
		!strings.Contains(result.Body, "SPYM") ||
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
		`{
  "version": "portfolio-advice/v1",
  "as_of": "2026-07-30T01:00:00Z",
  "executive_summary": [
    "AAPL 단일 종목 집중을 줄이는 것이 가장 먼저 필요한 조정이다.",
    "보호수량 1주는 유지하고 초과 1주만 부분 매도하는 편이 적절하다.",
    "신규 자금은 코어와 방어 자산을 함께 보완하는 편이 낫다."
  ],
  "portfolio_actions": [
    "AAPL 1주를 부분 매도해 단일 종목 집중도를 낮춘다.",
    "SPYM은 월 신규자금의 코어 몫으로 적립식 매수한다.",
    "SGOV는 방어 몫으로 나누어 매수한다."
  ],
  "holdings": [
    {
      "ticker": "AAPL",
      "action": "partial_sell",
      "conviction": "medium",
      "quantity_change": "-1",
      "target_adjustment": "보호수량 1주를 남기고 초과 1주를 부분 매도한다.",
      "thesis_status": "mixed",
      "increase_condition_status": "partially_met",
      "rationale": "서비스 매출 성장은 이어졌지만 현재 포트폴리오가 AAPL 한 종목에 집중되어 있어 추가 매수보다 부분 매도가 낫다.",
      "averaging_down_assessment": "현재 가격은 평단보다 높고 집중도도 높아 평단을 낮추기 위한 추가 매수는 타당하지 않다.",
      "counterargument": "서비스 성장과 현금흐름이 예상보다 강하면 한 주를 유지하는 장기 가치는 남아 있다.",
      "action_trigger": "현재 구성에서 초과 1주를 두 차례 이내로 매도한다.",
      "evidence": [
        {
          "claim": "공식 분기 자료에서 서비스 매출 추이를 확인했다.",
          "source_kind": "primary",
          "source_name": "Apple Investor Relations",
          "source_date": "2026-07-30",
          "url": "https://www.apple.com/newsroom/"
        },
        {
          "claim": "시장 가격 추세와 최근 실적 기대를 교차 확인했다.",
          "source_kind": "secondary",
          "source_name": "Reuters",
          "source_date": "2026-07-30",
          "url": "https://www.reuters.com/technology/"
        }
      ]
    }
  ],
  "candidates": [
    {
      "ticker": "SPYM",
      "name": "SPDR Portfolio S&P 500 ETF",
      "allocation_category": "core",
      "action": "accumulate",
      "proposed_role": "미국 대형주 분산 코어",
      "entry_plan": "신규자금의 코어 배분을 월 단위로 적립한다.",
      "rationale": "단일 종목 의존도를 낮추면서 광범위한 대형주 노출을 확보한다.",
      "risks": "미국 대형주 시장 전체 하락과 환율 변동에 노출된다.",
      "evidence": [
        {
          "claim": "운용사 공식 페이지에서 지수와 비용 구조를 확인했다.",
          "source_kind": "primary",
          "source_name": "State Street Global Advisors",
          "source_date": "2026-07-30",
          "url": "https://www.ssga.com/us/en/intermediary/etfs"
        },
        {
          "claim": "ETF 시장 정보에서 거래 특성을 교차 확인했다.",
          "source_kind": "secondary",
          "source_name": "Morningstar",
          "source_date": "2026-07-30",
          "url": "https://www.morningstar.com/etfs"
        }
      ]
    },
    {
      "ticker": "SGOV",
      "name": "iShares 0-3 Month Treasury Bond ETF",
      "allocation_category": "defensive",
      "action": "accumulate",
      "proposed_role": "단기 미국 국채 방어 자산",
      "entry_plan": "방어 목표 비중을 향해 신규자금을 세 차례로 나누어 적립한다.",
      "rationale": "주식 집중도를 낮추고 짧은 듀레이션의 국채 노출을 더한다.",
      "risks": "정책금리 하락 시 분배수익률이 낮아질 수 있다.",
      "evidence": [
        {
          "claim": "운용사 공식 페이지에서 만기 범위와 비용을 확인했다.",
          "source_kind": "primary",
          "source_name": "iShares",
          "source_date": "2026-07-30",
          "url": "https://www.ishares.com/us/products/314116/"
        },
        {
          "claim": "미국 단기 국채 금리 환경을 교차 확인했다.",
          "source_kind": "secondary",
          "source_name": "Federal Reserve Bank of St. Louis",
          "source_date": "2026-07-30",
          "url": "https://fred.stlouisfed.org/"
        }
      ]
    }
  ],
  "research_conclusions": [
    "AAPL의 사업 가설은 일부 유지되지만 단일 종목 집중 위험이 더 큰 제약이다.",
    "SPYM은 개별 종목 위험을 낮추는 코어 역할에 적합하다.",
    "SGOV는 짧은 듀레이션으로 방어 자산의 빈자리를 보완한다."
  ],
  "limitations": [
    "세금과 실제 주문 수수료는 계좌별 정보가 없어 반영하지 못했다."
  ]
}`,
	)
}
