package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/codexcli"
	"github.com/rowlet9g/stock-research-bot/internal/config"
	"github.com/rowlet9g/stock-research-bot/internal/emaildelivery"
	"github.com/rowlet9g/stock-research-bot/internal/reporting"
	sqlitestore "github.com/rowlet9g/stock-research-bot/internal/storage/sqlite"
)

const defaultPortfolioCodexResponseFile = "data/reports/portfolio-response-latest.md"

var portfolioCodexEmailNow = time.Now

type portfolioCodexAnalyzer interface {
	Analyze(context.Context, string) (string, error)
}

var newPortfolioCodexAnalyzer = func(
	codexConfig codexcli.Config,
) (portfolioCodexAnalyzer, error) {
	return codexcli.New(codexConfig)
}

var newPortfolioCodexEmailSender = func(
	smtpConfig emaildelivery.SMTPConfig,
) (dailyEmailSender, error) {
	return emaildelivery.NewSMTPSender(smtpConfig)
}

type portfolioCodexEmailCommandResult struct {
	AnalysisRunID int64                             `json:"analysis_run_id"`
	AlreadySaved  bool                              `json:"already_saved"`
	ResponseFile  string                            `json:"response_file"`
	Report        reporting.PortfolioResponseReport `json:"report"`
	Subject       string                            `json:"subject"`
	Body          string                            `json:"body"`
	Delivery      dailyEmailDeliveryResult          `json:"delivery"`
}

func runPortfolioCodexEmail(
	args []string,
	stdout io.Writer,
	stderr io.Writer,
) int {
	flags := flag.NewFlagSet(
		"portfolio-codex-email",
		flag.ContinueOnError,
	)
	flags.SetOutput(stderr)
	databasePath := flags.String(
		"db",
		defaultDatabasePath,
		"SQLite database path",
	)
	envPath := flags.String("env", ".env", "environment file path")
	ticker := flags.String("ticker", "", "optional stored instrument ticker")
	workers := flags.Int(
		"workers",
		4,
		"concurrent price requests from 1 to 16",
	)
	downsideBPS := flags.Int64(
		"downside-bps",
		-2000,
		"downside price return in basis points",
	)
	upsideBPS := flags.Int64(
		"upside-bps",
		2000,
		"upside price return in basis points",
	)
	question := flags.String(
		"question",
		"",
		"inline portfolio research question; overrides -question-file",
	)
	questionFile := flags.String(
		"question-file",
		defaultPortfolioQuestionFile,
		"portfolio research question file; empty uses the built-in request",
	)
	codexPath := flags.String(
		"codex-path",
		"",
		"optional Codex CLI executable path",
	)
	codexTimeout := flags.Duration(
		"codex-timeout",
		10*time.Minute,
		"maximum Codex analysis duration",
	)
	codexReasoning := flags.String(
		"codex-reasoning",
		"medium",
		"Codex reasoning effort: minimal, low, medium, or high",
	)
	responsePath := flags.String(
		"response-file",
		defaultPortfolioCodexResponseFile,
		"generated Codex response file",
	)
	send := flags.Bool(
		"send",
		false,
		"send the generated response through configured SMTP",
	)
	outputFormat := flags.String(
		"output",
		"text",
		"output format: text or json",
	)
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if exitCode := validateCommandOutput(
		outputFormat,
		stdout,
		stderr,
	); exitCode != 0 {
		return exitCode
	}
	*ticker = strings.TrimSpace(*ticker)
	if err := validatePortfolioScenarioFlags(
		*workers,
		*downsideBPS,
		*upsideBPS,
	); err != nil {
		return commandInputError(
			*outputFormat,
			stdout,
			stderr,
			err.Error(),
		)
	}
	if *codexTimeout < 30*time.Second ||
		*codexTimeout > 30*time.Minute {
		return commandInputError(
			*outputFormat,
			stdout,
			stderr,
			"codex-timeout must be between 30s and 30m",
		)
	}
	if strings.TrimSpace(*responsePath) == "" {
		return commandInputError(
			*outputFormat,
			stdout,
			stderr,
			"response-file is required",
		)
	}
	resolvedQuestion, err := resolvePortfolioQuestion(
		*question,
		*questionFile,
	)
	if err != nil {
		return commandInputError(
			*outputFormat,
			stdout,
			stderr,
			err.Error(),
		)
	}

	settings := config.Load(*envPath)
	location, err := portfolioEmailLocation(settings.EmailReportTimeZone)
	if err != nil {
		return commandInputError(
			*outputFormat,
			stdout,
			stderr,
			err.Error(),
		)
	}
	var deliveryConfig emaildelivery.DeliveryConfig
	if *send {
		deliveryConfig, err = parseDailyEmailConfig(settings)
		if err != nil {
			return commandInputError(
				*outputFormat,
				stdout,
				stderr,
				err.Error(),
			)
		}
	}
	generatedAt := portfolioCodexEmailNow()

	buildContext, cancelBuild := context.WithTimeout(
		context.Background(),
		2*time.Minute,
	)
	defer cancelBuild()
	store, err := sqlitestore.Open(buildContext, *databasePath)
	if err != nil {
		return writeRuntimeFailure(
			*outputFormat,
			stdout,
			stderr,
			"storage",
			err,
		)
	}
	defer store.Close()
	briefResult, err := buildPortfolioBriefResult(
		buildContext,
		store,
		portfolioBriefBuildRequest{
			Ticker:      *ticker,
			Workers:     *workers,
			DownsideBPS: *downsideBPS,
			UpsideBPS:   *upsideBPS,
			Question:    resolvedQuestion,
			GeneratedAt: generatedAt,
		},
	)
	if err != nil {
		scope, cause := portfolioBriefFailure(err)
		return writeRuntimeFailure(
			*outputFormat,
			stdout,
			stderr,
			scope,
			cause,
		)
	}
	briefResult, err = savePortfolioBriefResult(
		buildContext,
		store,
		briefResult,
	)
	if err != nil {
		scope, cause := portfolioBriefFailure(err)
		return writeRuntimeFailure(
			*outputFormat,
			stdout,
			stderr,
			scope,
			cause,
		)
	}
	if briefResult.AnalysisRun == nil {
		return writeRuntimeFailure(
			*outputFormat,
			stdout,
			stderr,
			"analysis_run",
			fmt.Errorf("saved portfolio analysis run is missing"),
		)
	}

	isolatedDirectory, err := os.MkdirTemp(
		"",
		"forgetmenot-codex-*",
	)
	if err != nil {
		return writeRuntimeFailure(
			*outputFormat,
			stdout,
			stderr,
			"codex_setup",
			fmt.Errorf("create isolated Codex working directory: %w", err),
		)
	}
	defer os.RemoveAll(isolatedDirectory)
	analyzer, err := newPortfolioCodexAnalyzer(codexcli.Config{
		Executable:      *codexPath,
		WorkDir:         isolatedDirectory,
		ReasoningEffort: *codexReasoning,
	})
	if err != nil {
		return commandInputError(
			*outputFormat,
			stdout,
			stderr,
			err.Error(),
		)
	}
	analysisPrompt := buildPortfolioCodexAnalysisPrompt(
		briefResult.Brief.Prompt,
	)
	analysisContext, cancelAnalysis := context.WithTimeout(
		context.Background(),
		*codexTimeout,
	)
	response, err := analyzer.Analyze(
		analysisContext,
		analysisPrompt,
	)
	cancelAnalysis()
	if err != nil {
		return writeRuntimeFailure(
			*outputFormat,
			stdout,
			stderr,
			"codex_analysis",
			err,
		)
	}
	response, err = validateGeneratedPortfolioResponse(
		response,
		analysisPrompt,
		briefResult.Brief.Prompt,
	)
	if err != nil {
		return writeRuntimeFailure(
			*outputFormat,
			stdout,
			stderr,
			"codex_response",
			err,
		)
	}
	writtenResponsePath, err := writePortfolioCodexResponseFile(
		*responsePath,
		response,
	)
	if err != nil {
		return writeRuntimeFailure(
			*outputFormat,
			stdout,
			stderr,
			"codex_response_file",
			err,
		)
	}

	run := briefResult.AnalysisRun
	report, err := reporting.BuildPortfolioResponseReport(
		reporting.PortfolioResponseReportInput{
			AnalysisRunID:          run.ID,
			AnalysisRunKind:        run.Kind,
			AnalysisRunGeneratedAt: run.GeneratedAt,
			InputSHA256:            run.InputSHA256,
			AnalysisOutputSHA256:   run.OutputSHA256,
			PromptSHA256:           briefResult.Brief.PromptSHA256,
			ResponseOrigin:         reporting.PortfolioResponseOriginCodexChatGPT,
			Response:               response,
		},
		portfolioCodexEmailNow(),
		location,
	)
	if err != nil {
		return writeRuntimeFailure(
			*outputFormat,
			stdout,
			stderr,
			"portfolio_response_report",
			err,
		)
	}
	subject := report.Subject(settings.EmailSubjectPrefix)
	body, err := report.TextBody(location)
	if err != nil {
		return writeRuntimeFailure(
			*outputFormat,
			stdout,
			stderr,
			"portfolio_response_report",
			err,
		)
	}
	result := portfolioCodexEmailCommandResult{
		AnalysisRunID: run.ID,
		AlreadySaved:  briefResult.AlreadySaved,
		ResponseFile:  writtenResponsePath,
		Report:        report,
		Subject:       subject,
		Body:          body,
		Delivery: dailyEmailDeliveryResult{
			Requested: *send,
		},
	}
	if *send {
		message, err := emaildelivery.NewMessage(
			deliveryConfig.From,
			deliveryConfig.To,
			subject,
			body,
			report.GeneratedAt,
		)
		if err != nil {
			return writeRuntimeFailure(
				*outputFormat,
				stdout,
				stderr,
				"email_message",
				err,
			)
		}
		sender, err := newPortfolioCodexEmailSender(
			deliveryConfig.SMTP,
		)
		if err != nil {
			return commandInputError(
				*outputFormat,
				stdout,
				stderr,
				err.Error(),
			)
		}
		emailContext, cancelEmail := context.WithTimeout(
			context.Background(),
			time.Minute,
		)
		err = sender.Send(emailContext, message)
		cancelEmail()
		if err != nil {
			return writeRuntimeFailure(
				*outputFormat,
				stdout,
				stderr,
				"email_delivery",
				err,
			)
		}
		sentAt := report.GeneratedAt.UTC()
		result.Delivery.Sent = true
		result.Delivery.RecipientCount = len(deliveryConfig.To)
		result.Delivery.SentAt = &sentAt
	}
	return writePortfolioCodexEmailResult(
		*outputFormat,
		stdout,
		stderr,
		result,
	)
}

func portfolioEmailLocation(
	timeZone string,
) (*time.Location, error) {
	timeZone = strings.TrimSpace(timeZone)
	if timeZone == "" {
		timeZone = "Asia/Seoul"
	}
	location, err := time.LoadLocation(timeZone)
	if err != nil {
		return nil, fmt.Errorf(
			"EMAIL_REPORT_TIMEZONE %q is invalid: %w",
			timeZone,
			err,
		)
	}
	return location, nil
}

func buildPortfolioCodexAnalysisPrompt(
	portfolioPrompt string,
) string {
	return strings.TrimSpace(
		`아래의 포트폴리오 리서치 요청에만 답해.

실행 제약:
- 셸 명령, 파일 읽기·쓰기, 웹 검색, 외부 도구를 사용하지 마.
- 제공된 입력에 적힌 사실만 사용하고, 누락된 값은 추정하지 마.
- 코드나 저장소를 분석하거나 수정하지 마.
- 최종 포트폴리오 분석 보고서 본문만 한국어로 출력해.
- 독자에게 직접 말하는 반말 대신 간결한 서술형 문체(-다/-이다)를 사용해.
- Markdown 제목, 굵게 표시, 표, 코드 표시를 사용하지 말고 일반 텍스트로 작성해.
- 별도의 데이터 범위, 가격 출처, 강점, 일반론 절을 만들지 마.
- 자료가 부족해도 결론 전체를 유보하지 말고 현재 수치에 근거한 기본 조치를 제시해.
- 각 활성 종목에 유지, 추가매수 보류, 비중 축소 검토 중 하나의 기본 조치를 제시해.
- high 종목은 제공된 40% 기준 재배분액을 1차 위험관리안으로 구체적으로 인용해.
- 확정적인 주문 지시가 아니라 조건과 수치를 붙인 직접적인 권고형 문장으로 작성해.
- 사람이 읽는 금액은 KRW는 정수, USD는 소수점 둘째 자리까지만 반올림해.
- 전체 분량은 약 2,500~4,000자로 제한해.

[포트폴리오 리서치 요청]
` + strings.TrimSpace(portfolioPrompt),
	)
}

func validateGeneratedPortfolioResponse(
	response string,
	analysisPrompt string,
	portfolioPrompt string,
) (string, error) {
	if len(response) > maxPortfolioResponseBytes {
		return "", fmt.Errorf(
			"Codex response exceeds %d bytes",
			maxPortfolioResponseBytes,
		)
	}
	validated, err := reporting.ValidatePortfolioResponse(response)
	if err != nil {
		return "", err
	}
	if samePortfolioResponseAndPrompt(validated, analysisPrompt) ||
		samePortfolioResponseAndPrompt(validated, portfolioPrompt) {
		return "", fmt.Errorf(
			"Codex response contains the input prompt instead of an analysis",
		)
	}
	return validated, nil
}

func writePortfolioCodexResponseFile(
	path string,
	response string,
) (string, error) {
	absolutePath, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return "", fmt.Errorf(
			"resolve Codex response file %q: %w",
			path,
			err,
		)
	}
	if err := os.MkdirAll(filepath.Dir(absolutePath), 0o700); err != nil {
		return "", fmt.Errorf(
			"create Codex response directory %q: %w",
			filepath.Dir(absolutePath),
			err,
		)
	}
	if err := os.WriteFile(
		absolutePath,
		[]byte(strings.TrimSpace(response)+"\n"),
		0o600,
	); err != nil {
		return "", fmt.Errorf(
			"write Codex response file %q: %w",
			absolutePath,
			err,
		)
	}
	return absolutePath, nil
}

func writePortfolioCodexEmailResult(
	outputFormat string,
	stdout io.Writer,
	stderr io.Writer,
	result portfolioCodexEmailCommandResult,
) int {
	if outputFormat == "json" {
		if err := writeJSON(stdout, result); err != nil {
			return writeRuntimeFailure(
				"text",
				stdout,
				stderr,
				"output",
				err,
			)
		}
		return 0
	}
	fmt.Fprint(stdout, result.Body)
	fmt.Fprintf(
		stdout,
		"\nCodex 응답 저장: %s\n",
		result.ResponseFile,
	)
	if result.Delivery.Sent {
		fmt.Fprintf(
			stdout,
			"이메일 전송 완료: 수신자 %d명 / 분석 실행 ID %d\n",
			result.Delivery.RecipientCount,
			result.AnalysisRunID,
		)
	} else {
		fmt.Fprintln(
			stdout,
			"미리보기 전용: 이메일을 전송하지 않았습니다.",
		)
	}
	return 0
}
