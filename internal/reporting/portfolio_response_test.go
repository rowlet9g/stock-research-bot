package reporting

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

func TestBuildPortfolioResponseReportPreservesProvenance(t *testing.T) {
	generatedAt := time.Date(2026, 7, 30, 1, 30, 0, 0, time.UTC)
	sourceGeneratedAt := generatedAt.Add(-time.Hour)
	location := time.FixedZone("Asia/Seoul", 9*60*60)
	inputHash := strings.Repeat("a", 64)
	outputHash := strings.Repeat("d", 64)
	promptHash := strings.Repeat("b", 64)
	responseBody := "# 핵심 요약\r\n\r\n유지 가설을 확인한다.\r\n\r\n" +
		strings.Repeat(
			"포트폴리오 집중도와 투자 가설을 사실, 해석, 반론으로 구분해 검토한다. ",
			8,
		)
	report, err := BuildPortfolioResponseReport(
		PortfolioResponseReportInput{
			AnalysisRunID:          7,
			AnalysisRunKind:        "portfolio_brief",
			AnalysisRunGeneratedAt: sourceGeneratedAt,
			InputSHA256:            inputHash,
			AnalysisOutputSHA256:   outputHash,
			PromptSHA256:           promptHash,
			Response:               "\uFEFF" + responseBody,
		},
		generatedAt,
		location,
	)
	if err != nil {
		t.Fatalf("build portfolio response report: %v", err)
	}
	expectedResponse := "# 핵심 요약\n\n유지 가설을 확인한다.\n\n" +
		strings.TrimSpace(strings.Repeat(
			"포트폴리오 집중도와 투자 가설을 사실, 해석, 반론으로 구분해 검토한다. ",
			8,
		))
	expectedHash := sha256.Sum256([]byte(expectedResponse))
	if report.Version != PortfolioResponseReportVersion ||
		report.ReportDate != "2026-07-30" ||
		report.AnalysisRunID != 7 ||
		report.InputSHA256 != inputHash ||
		report.AnalysisOutputSHA256 != outputHash ||
		report.PromptSHA256 != promptHash ||
		report.Response != expectedResponse ||
		report.ResponseSHA256 != hex.EncodeToString(expectedHash[:]) {
		t.Fatalf("unexpected portfolio response report: %#v", report)
	}
	body, err := report.TextBody(location)
	if err != nil {
		t.Fatalf("render portfolio response report: %v", err)
	}
	for _, expected := range []string{
		"ForgetMeNot 포트폴리오 상세 분석",
		"원본 분석 실행 ID: 7",
		"분석 payload SHA-256: " + outputHash,
		"프롬프트 SHA-256: " + promptHash,
		"응답 SHA-256: " + report.ResponseSHA256,
		"모델, 대화 ID, 사실 정확성을 독립적으로 검증하지 않았습니다",
		expectedResponse,
		"자동 매수·매도 지시가 아닙니다",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("response report body missing %q:\n%s", expected, body)
		}
	}
	if subject := report.Subject(""); subject !=
		"[ForgetMeNot] 포트폴리오 상세 분석 - 2026-07-30" {
		t.Fatalf("unexpected subject: %s", subject)
	}
}

func TestBuildPortfolioResponseReportRejectsInvalidInputs(t *testing.T) {
	valid := PortfolioResponseReportInput{
		AnalysisRunID:          1,
		AnalysisRunKind:        "portfolio_brief",
		AnalysisRunGeneratedAt: time.Now(),
		InputSHA256:            strings.Repeat("a", 64),
		AnalysisOutputSHA256:   strings.Repeat("d", 64),
		PromptSHA256:           strings.Repeat("b", 64),
		Response: strings.Repeat(
			"포트폴리오 위험과 투자 가설을 근거 중심으로 검토한다. ",
			10,
		),
	}
	tests := []struct {
		name   string
		mutate func(*PortfolioResponseReportInput)
	}{
		{
			name: "run ID",
			mutate: func(input *PortfolioResponseReportInput) {
				input.AnalysisRunID = 0
			},
		},
		{
			name: "run kind",
			mutate: func(input *PortfolioResponseReportInput) {
				input.AnalysisRunKind = "risk_assessment"
			},
		},
		{
			name: "input hash",
			mutate: func(input *PortfolioResponseReportInput) {
				input.InputSHA256 = "invalid"
			},
		},
		{
			name: "analysis output hash",
			mutate: func(input *PortfolioResponseReportInput) {
				input.AnalysisOutputSHA256 = "invalid"
			},
		},
		{
			name: "prompt hash",
			mutate: func(input *PortfolioResponseReportInput) {
				input.PromptSHA256 = "invalid"
			},
		},
		{
			name: "response",
			mutate: func(input *PortfolioResponseReportInput) {
				input.Response = " \r\n"
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := valid
			test.mutate(&input)
			if _, err := BuildPortfolioResponseReport(
				input,
				time.Now(),
				time.UTC,
			); err == nil {
				t.Fatal("invalid portfolio response input was accepted")
			}
		})
	}
}

func TestValidatePortfolioResponseRejectsCaptureCommandAndShortText(
	t *testing.T,
) {
	if _, err := ValidatePortfolioResponse(
		"Get-Clipboard -Raw |\n" +
			"Set-Content -LiteralPath data/reports/response.md",
	); err == nil || !strings.Contains(err.Error(), "clipboard capture") {
		t.Fatalf("clipboard capture command was accepted: %v", err)
	}
	if _, err := ValidatePortfolioResponse(
		"짧은 분석 응답",
	); err == nil || !strings.Contains(err.Error(), "too short") {
		t.Fatalf("short response was accepted: %v", err)
	}
}
