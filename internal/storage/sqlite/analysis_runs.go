package sqlite

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/models"
)

type AnalysisRunInput struct {
	Kind        string
	Status      models.DataStatus
	InputSHA256 string
	RuleVersion string
	Payload     json.RawMessage
	GeneratedAt time.Time
}

func (s *Store) SaveAnalysisRun(
	ctx context.Context,
	input AnalysisRunInput,
) (models.AnalysisRun, bool, error) {
	normalized, err := normalizeAnalysisRunInput(input)
	if err != nil {
		return models.AnalysisRun{}, false, err
	}
	outputHash := sha256.Sum256(normalized.Payload)
	outputSHA256 := hex.EncodeToString(outputHash[:])
	idempotencyHash := sha256.Sum256([]byte(
		normalized.Kind +
			"\x1f" +
			normalized.InputSHA256 +
			"\x1f" +
			normalized.RuleVersion +
			"\x1f" +
			outputSHA256,
	))
	idempotencyKey := hex.EncodeToString(idempotencyHash[:])
	createdAt := s.now().UTC()

	result, err := s.db.ExecContext(ctx, `
		INSERT INTO analysis_runs(
			kind,
			status,
			input_sha256,
			output_sha256,
			rule_version,
			idempotency_key,
			payload_json,
			generated_at,
			created_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(idempotency_key) DO NOTHING
	`,
		normalized.Kind,
		normalized.Status,
		normalized.InputSHA256,
		outputSHA256,
		normalized.RuleVersion,
		idempotencyKey,
		string(normalized.Payload),
		normalized.GeneratedAt.UTC().Format(timeFormat),
		createdAt.Format(timeFormat),
	)
	if err != nil {
		return models.AnalysisRun{}, false, fmt.Errorf(
			"save analysis run %q: %w",
			normalized.Kind,
			err,
		)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return models.AnalysisRun{}, false, fmt.Errorf(
			"read analysis run save result: %w",
			err,
		)
	}
	run, err := s.analysisRunByIdempotencyKey(ctx, idempotencyKey)
	if err != nil {
		return models.AnalysisRun{}, false, err
	}
	return run, affected == 0, nil
}

func (s *Store) ListAnalysisRuns(
	ctx context.Context,
	kind string,
	limit int,
) ([]models.AnalysisRun, error) {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return nil, fmt.Errorf("analysis run kind is required")
	}
	if limit <= 0 || limit > 500 {
		return nil, fmt.Errorf(
			"analysis run limit must be between 1 and 500",
		)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			id,
			kind,
			status,
			input_sha256,
			output_sha256,
			rule_version,
			idempotency_key,
			payload_json,
			generated_at,
			created_at
		FROM analysis_runs
		WHERE kind = ?
		ORDER BY generated_at DESC, id DESC
		LIMIT ?
	`, kind, limit)
	if err != nil {
		return nil, fmt.Errorf("list analysis runs %q: %w", kind, err)
	}
	defer rows.Close()

	runs := []models.AnalysisRun{}
	for rows.Next() {
		run, err := scanAnalysisRun(rows)
		if err != nil {
			return nil, fmt.Errorf("scan analysis run: %w", err)
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate analysis runs: %w", err)
	}
	return runs, nil
}

func (s *Store) AnalysisRun(
	ctx context.Context,
	id int64,
) (models.AnalysisRun, error) {
	if id <= 0 {
		return models.AnalysisRun{}, fmt.Errorf(
			"analysis run ID must be greater than zero",
		)
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT
			id,
			kind,
			status,
			input_sha256,
			output_sha256,
			rule_version,
			idempotency_key,
			payload_json,
			generated_at,
			created_at
		FROM analysis_runs
		WHERE id = ?
	`, id)
	run, err := scanAnalysisRun(row)
	if err == sql.ErrNoRows {
		return models.AnalysisRun{}, fmt.Errorf(
			"%w: analysis run %d",
			ErrNotFound,
			id,
		)
	}
	if err != nil {
		return models.AnalysisRun{}, fmt.Errorf(
			"query analysis run %d: %w",
			id,
			err,
		)
	}
	return run, nil
}

func (s *Store) analysisRunByIdempotencyKey(
	ctx context.Context,
	key string,
) (models.AnalysisRun, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT
			id,
			kind,
			status,
			input_sha256,
			output_sha256,
			rule_version,
			idempotency_key,
			payload_json,
			generated_at,
			created_at
		FROM analysis_runs
		WHERE idempotency_key = ?
	`, key)
	run, err := scanAnalysisRun(row)
	if err == sql.ErrNoRows {
		return models.AnalysisRun{}, fmt.Errorf(
			"%w: analysis run %q",
			ErrNotFound,
			key,
		)
	}
	if err != nil {
		return models.AnalysisRun{}, fmt.Errorf(
			"query analysis run %q: %w",
			key,
			err,
		)
	}
	return run, nil
}

func normalizeAnalysisRunInput(
	input AnalysisRunInput,
) (AnalysisRunInput, error) {
	input.Kind = strings.TrimSpace(input.Kind)
	input.RuleVersion = strings.TrimSpace(input.RuleVersion)
	input.InputSHA256 = strings.ToLower(strings.TrimSpace(input.InputSHA256))
	switch {
	case input.Kind == "":
		return AnalysisRunInput{}, fmt.Errorf("analysis run kind is required")
	case !validDataStatus(input.Status):
		return AnalysisRunInput{}, fmt.Errorf(
			"analysis run status %q is invalid",
			input.Status,
		)
	case !validSHA256(input.InputSHA256):
		return AnalysisRunInput{}, fmt.Errorf(
			"analysis run input SHA-256 is invalid",
		)
	case input.RuleVersion == "":
		return AnalysisRunInput{}, fmt.Errorf(
			"analysis run rule version is required",
		)
	case input.GeneratedAt.IsZero():
		return AnalysisRunInput{}, fmt.Errorf(
			"analysis run generation time is required",
		)
	case len(input.Payload) == 0 || !json.Valid(input.Payload):
		return AnalysisRunInput{}, fmt.Errorf(
			"analysis run payload must be valid JSON",
		)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, input.Payload); err != nil {
		return AnalysisRunInput{}, fmt.Errorf(
			"compact analysis run payload: %w",
			err,
		)
	}
	input.Payload = append(json.RawMessage(nil), compact.Bytes()...)
	input.GeneratedAt = input.GeneratedAt.UTC()
	return input, nil
}

func validDataStatus(status models.DataStatus) bool {
	switch status {
	case models.DataStatusAvailable,
		models.DataStatusPartial,
		models.DataStatusEmpty,
		models.DataStatusUnavailable,
		models.DataStatusNotRequested:
		return true
	default:
		return false
	}
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func scanAnalysisRun(row scanner) (models.AnalysisRun, error) {
	var run models.AnalysisRun
	var payload string
	var generatedAt string
	var createdAt string
	if err := row.Scan(
		&run.ID,
		&run.Kind,
		&run.Status,
		&run.InputSHA256,
		&run.OutputSHA256,
		&run.RuleVersion,
		&run.IdempotencyKey,
		&payload,
		&generatedAt,
		&createdAt,
	); err != nil {
		return models.AnalysisRun{}, err
	}
	if !json.Valid([]byte(payload)) {
		return models.AnalysisRun{}, fmt.Errorf(
			"stored analysis run payload is invalid JSON",
		)
	}
	run.Payload = json.RawMessage(payload)
	var err error
	run.GeneratedAt, err = parseTime(generatedAt)
	if err != nil {
		return models.AnalysisRun{}, fmt.Errorf(
			"parse analysis run generated_at: %w",
			err,
		)
	}
	run.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return models.AnalysisRun{}, fmt.Errorf(
			"parse analysis run created_at: %w",
			err,
		)
	}
	return run, nil
}
