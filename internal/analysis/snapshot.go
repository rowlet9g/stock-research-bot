package analysis

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/models"
)

const AnalysisInputSchemaVersion = "analysis-input/v1"

type AnalysisInputIssue struct {
	Scope   string `json:"scope"`
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

type AnalysisDisclosureInput struct {
	Status      models.DataStatus       `json:"status"`
	Disclosures []models.DARTDisclosure `json:"disclosures"`
}

type AnalysisRuleVersions struct {
	PriceSignals     string `json:"price_signals"`
	FinancialMetrics string `json:"financial_metrics"`
}

type AnalysisInput struct {
	Portfolio   models.PortfolioRecord
	Price       models.PriceSnapshot
	Signals     []Signal
	Disclosures AnalysisDisclosureInput
	Financials  FinancialMetricReport
	Issues      []AnalysisInputIssue
}

type AnalysisInputSnapshot struct {
	SchemaVersion string                  `json:"schema_version"`
	Status        models.DataStatus       `json:"status"`
	GeneratedAt   time.Time               `json:"generated_at"`
	InputSHA256   string                  `json:"input_sha256"`
	RuleVersions  AnalysisRuleVersions    `json:"rule_versions"`
	Portfolio     models.PortfolioRecord  `json:"portfolio"`
	Price         models.PriceSnapshot    `json:"price"`
	Signals       []Signal                `json:"signals"`
	Disclosures   AnalysisDisclosureInput `json:"disclosures"`
	Financials    FinancialMetricReport   `json:"financials"`
	Issues        []AnalysisInputIssue    `json:"issues"`
}

type analysisInputHashPayload struct {
	SchemaVersion string                  `json:"schema_version"`
	Status        models.DataStatus       `json:"status"`
	RuleVersions  AnalysisRuleVersions    `json:"rule_versions"`
	Portfolio     models.PortfolioRecord  `json:"portfolio"`
	Price         models.PriceSnapshot    `json:"price"`
	Signals       []Signal                `json:"signals"`
	Disclosures   AnalysisDisclosureInput `json:"disclosures"`
	Financials    FinancialMetricReport   `json:"financials"`
	Issues        []AnalysisInputIssue    `json:"issues"`
}

func BuildAnalysisInputSnapshot(
	generatedAt time.Time,
	input AnalysisInput,
) (AnalysisInputSnapshot, error) {
	if generatedAt.IsZero() {
		return AnalysisInputSnapshot{}, fmt.Errorf(
			"analysis snapshot generation time is required",
		)
	}
	if strings.TrimSpace(input.Portfolio.Instrument.Ticker) == "" {
		return AnalysisInputSnapshot{}, fmt.Errorf(
			"analysis snapshot instrument ticker is required",
		)
	}

	normalizeAnalysisInput(&input)
	snapshot := AnalysisInputSnapshot{
		SchemaVersion: AnalysisInputSchemaVersion,
		Status:        analysisInputStatus(input),
		GeneratedAt:   generatedAt.UTC(),
		RuleVersions: AnalysisRuleVersions{
			PriceSignals:     PriceSignalRuleVersion,
			FinancialMetrics: FinancialMetricRuleVersion,
		},
		Portfolio:   input.Portfolio,
		Price:       input.Price,
		Signals:     input.Signals,
		Disclosures: input.Disclosures,
		Financials:  input.Financials,
		Issues:      input.Issues,
	}
	payload := analysisInputHashPayload{
		SchemaVersion: snapshot.SchemaVersion,
		Status:        snapshot.Status,
		RuleVersions:  snapshot.RuleVersions,
		Portfolio:     snapshot.Portfolio,
		Price:         snapshot.Price,
		Signals:       snapshot.Signals,
		Disclosures:   snapshot.Disclosures,
		Financials:    snapshot.Financials,
		Issues:        snapshot.Issues,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return AnalysisInputSnapshot{}, fmt.Errorf(
			"encode analysis snapshot hash payload: %w",
			err,
		)
	}
	hash := sha256.Sum256(encoded)
	snapshot.InputSHA256 = hex.EncodeToString(hash[:])
	return snapshot, nil
}

func normalizeAnalysisInput(input *AnalysisInput) {
	if input.Portfolio.Trades == nil {
		input.Portfolio.Trades = []models.Trade{}
	}
	if input.Portfolio.Thesis != nil &&
		input.Portfolio.Thesis.CheckMetrics == nil {
		input.Portfolio.Thesis.CheckMetrics = []string{}
	}
	if input.Signals == nil {
		input.Signals = []Signal{}
	}
	for index := range input.Signals {
		if input.Signals[index].Evidence == nil {
			input.Signals[index].Evidence = []SignalEvidence{}
		}
	}
	if input.Disclosures.Status == "" {
		input.Disclosures.Status = models.DataStatusNotRequested
	}
	if input.Disclosures.Disclosures == nil {
		input.Disclosures.Disclosures = []models.DARTDisclosure{}
	}
	if input.Financials.Status == "" {
		input.Financials.Status = models.DataStatusNotRequested
	}
	if input.Financials.Metrics == nil {
		input.Financials.Metrics = []FinancialMetric{}
	}
	if input.Financials.Ratios == nil {
		input.Financials.Ratios = []FinancialRatio{}
	}
	if input.Issues == nil {
		input.Issues = []AnalysisInputIssue{}
	}
}

func analysisInputStatus(input AnalysisInput) models.DataStatus {
	if len(input.Issues) > 0 ||
		input.Price.Status != models.DataStatusAvailable {
		return models.DataStatusPartial
	}
	if strings.TrimSpace(input.Portfolio.Instrument.DARTCorpCode) == "" {
		return models.DataStatusAvailable
	}
	if input.Disclosures.Status != models.DataStatusAvailable ||
		input.Financials.Status != models.DataStatusAvailable {
		return models.DataStatusPartial
	}
	return models.DataStatusAvailable
}
