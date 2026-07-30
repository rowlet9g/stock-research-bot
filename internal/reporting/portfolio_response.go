package reporting

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

const PortfolioResponseReportVersion = "portfolio-response-report/v1"

type PortfolioResponseReportInput struct {
	AnalysisRunID          int64
	AnalysisRunKind        string
	AnalysisRunGeneratedAt time.Time
	InputSHA256            string
	AnalysisOutputSHA256   string
	PromptSHA256           string
	Response               string
}

type PortfolioResponseReport struct {
	Version                string    `json:"version"`
	ReportDate             string    `json:"report_date"`
	TimeZone               string    `json:"time_zone"`
	GeneratedAt            time.Time `json:"generated_at"`
	AnalysisRunID          int64     `json:"analysis_run_id"`
	AnalysisRunKind        string    `json:"analysis_run_kind"`
	AnalysisRunGeneratedAt time.Time `json:"analysis_run_generated_at"`
	InputSHA256            string    `json:"input_sha256"`
	AnalysisOutputSHA256   string    `json:"analysis_output_sha256"`
	PromptSHA256           string    `json:"prompt_sha256"`
	ResponseSHA256         string    `json:"response_sha256"`
	ResponseOrigin         string    `json:"response_origin"`
	Response               string    `json:"response"`
}

func BuildPortfolioResponseReport(
	input PortfolioResponseReportInput,
	generatedAt time.Time,
	location *time.Location,
) (PortfolioResponseReport, error) {
	input.AnalysisRunKind = strings.TrimSpace(input.AnalysisRunKind)
	input.InputSHA256 = strings.ToLower(strings.TrimSpace(input.InputSHA256))
	input.AnalysisOutputSHA256 = strings.ToLower(
		strings.TrimSpace(input.AnalysisOutputSHA256),
	)
	input.PromptSHA256 = strings.ToLower(strings.TrimSpace(input.PromptSHA256))
	response := normalizePortfolioResponse(input.Response)
	switch {
	case generatedAt.IsZero():
		return PortfolioResponseReport{}, fmt.Errorf(
			"portfolio response report generation time is required",
		)
	case location == nil:
		return PortfolioResponseReport{}, fmt.Errorf(
			"portfolio response report time zone is required",
		)
	case input.AnalysisRunID <= 0:
		return PortfolioResponseReport{}, fmt.Errorf(
			"portfolio response analysis run ID must be greater than zero",
		)
	case input.AnalysisRunKind != "portfolio_brief":
		return PortfolioResponseReport{}, fmt.Errorf(
			"portfolio response analysis run kind must be %q, got %q",
			"portfolio_brief",
			input.AnalysisRunKind,
		)
	case input.AnalysisRunGeneratedAt.IsZero():
		return PortfolioResponseReport{}, fmt.Errorf(
			"portfolio response analysis run generation time is required",
		)
	case !validReportSHA256(input.InputSHA256):
		return PortfolioResponseReport{}, fmt.Errorf(
			"portfolio response input SHA-256 is invalid",
		)
	case !validReportSHA256(input.AnalysisOutputSHA256):
		return PortfolioResponseReport{}, fmt.Errorf(
			"portfolio response analysis output SHA-256 is invalid",
		)
	case !validReportSHA256(input.PromptSHA256):
		return PortfolioResponseReport{}, fmt.Errorf(
			"portfolio response prompt SHA-256 is invalid",
		)
	case response == "":
		return PortfolioResponseReport{}, fmt.Errorf(
			"portfolio response content is required",
		)
	}

	responseHash := sha256.Sum256([]byte(response))
	return PortfolioResponseReport{
		Version:                PortfolioResponseReportVersion,
		ReportDate:             generatedAt.In(location).Format("2006-01-02"),
		TimeZone:               location.String(),
		GeneratedAt:            generatedAt.UTC(),
		AnalysisRunID:          input.AnalysisRunID,
		AnalysisRunKind:        input.AnalysisRunKind,
		AnalysisRunGeneratedAt: input.AnalysisRunGeneratedAt.UTC(),
		InputSHA256:            input.InputSHA256,
		AnalysisOutputSHA256:   input.AnalysisOutputSHA256,
		PromptSHA256:           input.PromptSHA256,
		ResponseSHA256:         hex.EncodeToString(responseHash[:]),
		ResponseOrigin:         "manual_chatgpt_plus_import",
		Response:               response,
	}, nil
}

func (r PortfolioResponseReport) Subject(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "[ForgetMeNot]"
	}
	return fmt.Sprintf(
		"%s 포트폴리오 상세 분석 - %s",
		prefix,
		r.ReportDate,
	)
}

func (r PortfolioResponseReport) TextBody(
	location *time.Location,
) (string, error) {
	switch {
	case r.Version != PortfolioResponseReportVersion:
		return "", fmt.Errorf(
			"portfolio response report version must be %q, got %q",
			PortfolioResponseReportVersion,
			r.Version,
		)
	case location == nil:
		return "", fmt.Errorf(
			"portfolio response report time zone is required",
		)
	case strings.TrimSpace(r.Response) == "":
		return "", fmt.Errorf(
			"portfolio response report content is required",
		)
	}

	var builder strings.Builder
	fmt.Fprintf(
		&builder,
		"ForgetMeNot 포트폴리오 상세 분석\n\n"+
			"보고서 날짜: %s\n"+
			"생성시각: %s\n"+
			"기준 시간대: %s\n"+
			"원본 분석 실행 ID: %d\n"+
			"원본 분석 생성시각: %s\n"+
			"포트폴리오 입력 SHA-256: %s\n"+
			"분석 payload SHA-256: %s\n"+
			"프롬프트 SHA-256: %s\n"+
			"응답 SHA-256: %s\n"+
			"응답 반입 방식: %s\n\n",
		r.ReportDate,
		r.GeneratedAt.In(location).Format(time.RFC3339),
		r.TimeZone,
		r.AnalysisRunID,
		r.AnalysisRunGeneratedAt.In(location).Format(time.RFC3339),
		r.InputSHA256,
		r.AnalysisOutputSHA256,
		r.PromptSHA256,
		r.ResponseSHA256,
		r.ResponseOrigin,
	)
	builder.WriteString(
		"주의: 아래 내용은 사용자가 ChatGPT Plus 응답으로 저장한 파일을 " +
			"가져온 것입니다. ForgetMeNot은 응답의 모델, 대화 ID, 사실 정확성을 " +
			"독립적으로 검증하지 않았습니다.\n\n",
	)
	builder.WriteString("상세 분석\n\n")
	builder.WriteString(r.Response)
	builder.WriteString(
		"\n\n이 보고서는 투자 검토를 위한 참고 자료이며 자동 매수·매도 지시가 아닙니다.\n",
	)
	return builder.String(), nil
}

func normalizePortfolioResponse(value string) string {
	value = strings.TrimPrefix(value, "\uFEFF")
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	return strings.TrimSpace(value)
}

func validReportSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}
