package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/config"
	"github.com/rowlet9g/stock-research-bot/internal/emaildelivery"
	"github.com/rowlet9g/stock-research-bot/internal/reporting"
	sqlitestore "github.com/rowlet9g/stock-research-bot/internal/storage/sqlite"
)

const maxPortfolioResponseBytes = 256 * 1024

var portfolioResponseEmailNow = time.Now

var newPortfolioResponseEmailSender = func(
	smtpConfig emaildelivery.SMTPConfig,
) (dailyEmailSender, error) {
	return emaildelivery.NewSMTPSender(smtpConfig)
}

type portfolioResponseEmailCommandResult struct {
	Report   reporting.PortfolioResponseReport `json:"report"`
	Subject  string                            `json:"subject"`
	Body     string                            `json:"body"`
	Delivery dailyEmailDeliveryResult          `json:"delivery"`
}

func runPortfolioResponseEmail(
	args []string,
	stdout io.Writer,
	stderr io.Writer,
) int {
	flags := flag.NewFlagSet(
		"portfolio-response-email",
		flag.ContinueOnError,
	)
	flags.SetOutput(stderr)
	databasePath := flags.String(
		"db",
		defaultDatabasePath,
		"SQLite database path",
	)
	envPath := flags.String("env", ".env", "environment file path")
	runID := flags.Int64(
		"run-id",
		0,
		"stored portfolio_brief analysis run ID",
	)
	responsePath := flags.String(
		"file",
		"",
		"ChatGPT Plus response Markdown or text file",
	)
	send := flags.Bool(
		"send",
		false,
		"send the imported response through configured SMTP",
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
	switch {
	case *runID <= 0:
		return commandInputError(
			*outputFormat,
			stdout,
			stderr,
			"run-id must be greater than zero",
		)
	case strings.TrimSpace(*responsePath) == "":
		return commandInputError(
			*outputFormat,
			stdout,
			stderr,
			"file is required",
		)
	}
	response, err := readPortfolioResponseFile(*responsePath)
	if err != nil {
		return commandInputError(
			*outputFormat,
			stdout,
			stderr,
			err.Error(),
		)
	}

	settings := config.Load(*envPath)
	timeZone := strings.TrimSpace(settings.EmailReportTimeZone)
	if timeZone == "" {
		timeZone = "Asia/Seoul"
	}
	location, err := time.LoadLocation(timeZone)
	if err != nil {
		return commandInputError(
			*outputFormat,
			stdout,
			stderr,
			fmt.Sprintf(
				"EMAIL_REPORT_TIMEZONE %q is invalid: %v",
				timeZone,
				err,
			),
		)
	}

	generatedAt := portfolioResponseEmailNow()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	store, err := sqlitestore.Open(ctx, *databasePath)
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
	run, err := store.AnalysisRun(ctx, *runID)
	if err != nil {
		return writeRuntimeFailure(
			*outputFormat,
			stdout,
			stderr,
			"analysis_run",
			err,
		)
	}
	briefResult, err := decodePortfolioBriefAnalysisRun(run)
	if err != nil {
		return writeRuntimeFailure(
			*outputFormat,
			stdout,
			stderr,
			"analysis_run",
			err,
		)
	}
	if samePortfolioResponseAndPrompt(
		response,
		briefResult.Brief.Prompt,
	) {
		return commandInputError(
			*outputFormat,
			stdout,
			stderr,
			"portfolio response file contains the original prompt instead of the ChatGPT response",
		)
	}
	report, err := reporting.BuildPortfolioResponseReport(
		reporting.PortfolioResponseReportInput{
			AnalysisRunID:          run.ID,
			AnalysisRunKind:        run.Kind,
			AnalysisRunGeneratedAt: run.GeneratedAt,
			InputSHA256:            run.InputSHA256,
			AnalysisOutputSHA256:   run.OutputSHA256,
			PromptSHA256:           briefResult.Brief.PromptSHA256,
			Response:               response,
		},
		generatedAt,
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
	result := portfolioResponseEmailCommandResult{
		Report:  report,
		Subject: subject,
		Body:    body,
		Delivery: dailyEmailDeliveryResult{
			Requested: *send,
		},
	}
	if *send {
		deliveryConfig, err := parseDailyEmailConfig(settings)
		if err != nil {
			return commandInputError(
				*outputFormat,
				stdout,
				stderr,
				err.Error(),
			)
		}
		message, err := emaildelivery.NewMessage(
			deliveryConfig.From,
			deliveryConfig.To,
			subject,
			body,
			generatedAt,
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
		sender, err := newPortfolioResponseEmailSender(
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
		if err := sender.Send(ctx, message); err != nil {
			return writeRuntimeFailure(
				*outputFormat,
				stdout,
				stderr,
				"email_delivery",
				err,
			)
		}
		sentAt := generatedAt.UTC()
		result.Delivery.Sent = true
		result.Delivery.RecipientCount = len(deliveryConfig.To)
		result.Delivery.SentAt = &sentAt
	}
	return writePortfolioResponseEmailResult(
		*outputFormat,
		stdout,
		stderr,
		result,
	)
}

func readPortfolioResponseFile(path string) (string, error) {
	path = strings.TrimSpace(path)
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf(
			"read portfolio response file %q: %w",
			path,
			err,
		)
	}
	if len(content) > maxPortfolioResponseBytes {
		return "", fmt.Errorf(
			"portfolio response file %q exceeds %d bytes",
			path,
			maxPortfolioResponseBytes,
		)
	}
	response, err := reporting.ValidatePortfolioResponse(string(content))
	if err != nil {
		return "", fmt.Errorf(
			"portfolio response file %q is invalid: %w",
			path,
			err,
		)
	}
	return response, nil
}

func samePortfolioResponseAndPrompt(
	response string,
	prompt string,
) bool {
	normalize := func(value string) string {
		value = strings.ReplaceAll(value, "\r\n", "\n")
		value = strings.ReplaceAll(value, "\r", "\n")
		return strings.TrimSpace(value)
	}
	return normalize(response) == normalize(prompt)
}

func writePortfolioResponseEmailResult(
	outputFormat string,
	stdout io.Writer,
	stderr io.Writer,
	result portfolioResponseEmailCommandResult,
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
	if result.Delivery.Sent {
		fmt.Fprintf(
			stdout,
			"\n이메일 전송 완료: 수신자 %d명 / 분석 실행 ID %d\n",
			result.Delivery.RecipientCount,
			result.Report.AnalysisRunID,
		)
	} else {
		fmt.Fprintln(
			stdout,
			"\n미리보기 전용: 이메일을 전송하지 않았습니다.",
		)
	}
	return 0
}
