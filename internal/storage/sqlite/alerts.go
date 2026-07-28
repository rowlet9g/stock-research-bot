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

type AlertCandidateInput struct {
	Fingerprint string
	Kind        string
	Severity    models.AlertSeverity
	Title       string
	Fact        string
	SourceRunID int64
	Payload     json.RawMessage
	DetectedAt  time.Time
}

type AlertCandidateResult struct {
	Alert              models.Alert `json:"alert"`
	AlertCreated       bool         `json:"alert_created"`
	ObservationCreated bool         `json:"observation_created"`
}

func (s *Store) ObserveAlertCandidate(
	ctx context.Context,
	input AlertCandidateInput,
) (AlertCandidateResult, error) {
	normalized, payloadSHA256, err := normalizeAlertCandidateInput(input)
	if err != nil {
		return AlertCandidateResult{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AlertCandidateResult{}, fmt.Errorf(
			"begin alert candidate observation: %w",
			err,
		)
	}
	defer tx.Rollback()

	now := s.now().UTC()
	insertResult, err := tx.ExecContext(ctx, `
		INSERT INTO alerts(
			fingerprint,
			kind,
			severity,
			status,
			title,
			fact,
			source_run_id,
			payload_json,
			occurrence_count,
			first_detected_at,
			last_detected_at,
			created_at,
			updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?, ?)
		ON CONFLICT(fingerprint) DO NOTHING
	`,
		normalized.Fingerprint,
		normalized.Kind,
		normalized.Severity,
		models.AlertStatusPending,
		normalized.Title,
		normalized.Fact,
		normalized.SourceRunID,
		string(normalized.Payload),
		normalized.DetectedAt.Format(timeFormat),
		normalized.DetectedAt.Format(timeFormat),
		now.Format(timeFormat),
		now.Format(timeFormat),
	)
	if err != nil {
		return AlertCandidateResult{}, fmt.Errorf(
			"insert alert candidate %q: %w",
			normalized.Fingerprint,
			err,
		)
	}
	alertCreated, err := exactlyOneRowAffected(insertResult)
	if err != nil {
		return AlertCandidateResult{}, fmt.Errorf(
			"read alert insert result: %w",
			err,
		)
	}
	alertID, err := alertIDByFingerprint(
		ctx,
		tx,
		normalized.Fingerprint,
	)
	if err != nil {
		return AlertCandidateResult{}, err
	}

	observationResult, err := tx.ExecContext(ctx, `
		INSERT INTO alert_observations(
			alert_id,
			source_run_id,
			payload_sha256,
			observed_at,
			created_at
		)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(alert_id, source_run_id) DO NOTHING
	`,
		alertID,
		normalized.SourceRunID,
		payloadSHA256,
		normalized.DetectedAt.Format(timeFormat),
		now.Format(timeFormat),
	)
	if err != nil {
		return AlertCandidateResult{}, fmt.Errorf(
			"insert alert observation: %w",
			err,
		)
	}
	observationCreated, err := exactlyOneRowAffected(observationResult)
	if err != nil {
		return AlertCandidateResult{}, fmt.Errorf(
			"read alert observation result: %w",
			err,
		)
	}
	if observationCreated {
		if _, err := tx.ExecContext(ctx, `
			UPDATE alerts
			SET
				kind = ?,
				severity = ?,
				status = CASE
					WHEN status = ? THEN status
					ELSE ?
				END,
				title = ?,
				fact = ?,
				source_run_id = ?,
				payload_json = ?,
				occurrence_count = occurrence_count + 1,
				last_detected_at = ?,
				sent_at = CASE
					WHEN status = ? THEN sent_at
					ELSE ''
				END,
				updated_at = ?
			WHERE id = ?
		`,
			normalized.Kind,
			normalized.Severity,
			models.AlertStatusAcknowledged,
			models.AlertStatusPending,
			normalized.Title,
			normalized.Fact,
			normalized.SourceRunID,
			string(normalized.Payload),
			normalized.DetectedAt.Format(timeFormat),
			models.AlertStatusAcknowledged,
			now.Format(timeFormat),
			alertID,
		); err != nil {
			return AlertCandidateResult{}, fmt.Errorf(
				"update observed alert %d: %w",
				alertID,
				err,
			)
		}
	}
	if err := tx.Commit(); err != nil {
		return AlertCandidateResult{}, fmt.Errorf(
			"commit alert candidate observation: %w",
			err,
		)
	}
	alert, err := s.Alert(ctx, alertID)
	if err != nil {
		return AlertCandidateResult{}, err
	}
	return AlertCandidateResult{
		Alert:              alert,
		AlertCreated:       alertCreated,
		ObservationCreated: observationCreated,
	}, nil
}

func (s *Store) Alert(
	ctx context.Context,
	id int64,
) (models.Alert, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT
			id,
			fingerprint,
			kind,
			severity,
			status,
			title,
			fact,
			source_run_id,
			payload_json,
			occurrence_count,
			first_detected_at,
			last_detected_at,
			sent_at,
			acknowledged_at,
			created_at,
			updated_at
		FROM alerts
		WHERE id = ?
	`, id)
	alert, err := scanAlert(row)
	if err == sql.ErrNoRows {
		return models.Alert{}, fmt.Errorf("%w: alert %d", ErrNotFound, id)
	}
	if err != nil {
		return models.Alert{}, fmt.Errorf("query alert %d: %w", id, err)
	}
	return alert, nil
}

func (s *Store) ListAlerts(
	ctx context.Context,
	status models.AlertStatus,
	limit int,
) ([]models.Alert, error) {
	if status != "" && !validAlertStatus(status) {
		return nil, fmt.Errorf("alert status %q is invalid", status)
	}
	if limit <= 0 || limit > 500 {
		return nil, fmt.Errorf("alert limit must be between 1 and 500")
	}
	query := `
		SELECT
			id,
			fingerprint,
			kind,
			severity,
			status,
			title,
			fact,
			source_run_id,
			payload_json,
			occurrence_count,
			first_detected_at,
			last_detected_at,
			sent_at,
			acknowledged_at,
			created_at,
			updated_at
		FROM alerts
	`
	args := []any{}
	if status != "" {
		query += " WHERE status = ?"
		args = append(args, status)
	}
	query += " ORDER BY last_detected_at DESC, id DESC LIMIT ?"
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list alerts: %w", err)
	}
	defer rows.Close()

	alerts := []models.Alert{}
	for rows.Next() {
		alert, err := scanAlert(rows)
		if err != nil {
			return nil, fmt.Errorf("scan alert: %w", err)
		}
		alerts = append(alerts, alert)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate alerts: %w", err)
	}
	return alerts, nil
}

func (s *Store) MarkAlertsSent(
	ctx context.Context,
	ids []int64,
	sentAt time.Time,
) error {
	normalizedIDs, err := normalizeAlertIDs(ids)
	if err != nil {
		return err
	}
	if sentAt.IsZero() {
		return fmt.Errorf("alert sent time is required")
	}
	sentAt = sentAt.UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin mark alerts sent: %w", err)
	}
	defer tx.Rollback()
	updatedAt := s.now().UTC()
	for _, id := range normalizedIDs {
		result, err := tx.ExecContext(ctx, `
			UPDATE alerts
			SET
				status = ?,
				sent_at = ?,
				updated_at = ?
			WHERE id = ? AND status = ?
		`,
			models.AlertStatusSent,
			sentAt.Format(timeFormat),
			updatedAt.Format(timeFormat),
			id,
			models.AlertStatusPending,
		)
		if err != nil {
			return fmt.Errorf("mark alert %d sent: %w", id, err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf(
				"read mark alert %d sent result: %w",
				id,
				err,
			)
		}
		if affected != 1 {
			status, err := alertStatusInTransaction(ctx, tx, id)
			if err != nil {
				return err
			}
			return fmt.Errorf(
				"alert %d must be pending to mark sent, got %q",
				id,
				status,
			)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit mark alerts sent: %w", err)
	}
	return nil
}

func normalizeAlertCandidateInput(
	input AlertCandidateInput,
) (AlertCandidateInput, string, error) {
	input.Fingerprint = strings.ToLower(strings.TrimSpace(input.Fingerprint))
	input.Kind = strings.TrimSpace(input.Kind)
	input.Title = strings.TrimSpace(input.Title)
	input.Fact = strings.TrimSpace(input.Fact)
	switch {
	case !validSHA256(input.Fingerprint):
		return AlertCandidateInput{}, "", fmt.Errorf(
			"alert fingerprint must be a SHA-256 value",
		)
	case input.Kind == "":
		return AlertCandidateInput{}, "", fmt.Errorf(
			"alert kind is required",
		)
	case !validAlertSeverity(input.Severity):
		return AlertCandidateInput{}, "", fmt.Errorf(
			"alert severity %q is invalid",
			input.Severity,
		)
	case input.Title == "":
		return AlertCandidateInput{}, "", fmt.Errorf(
			"alert title is required",
		)
	case input.Fact == "":
		return AlertCandidateInput{}, "", fmt.Errorf(
			"alert fact is required",
		)
	case input.SourceRunID <= 0:
		return AlertCandidateInput{}, "", fmt.Errorf(
			"alert source run ID must be greater than zero",
		)
	case input.DetectedAt.IsZero():
		return AlertCandidateInput{}, "", fmt.Errorf(
			"alert detection time is required",
		)
	case len(input.Payload) == 0 || !json.Valid(input.Payload):
		return AlertCandidateInput{}, "", fmt.Errorf(
			"alert payload must be valid JSON",
		)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, input.Payload); err != nil {
		return AlertCandidateInput{}, "", fmt.Errorf(
			"compact alert payload: %w",
			err,
		)
	}
	input.Payload = append(json.RawMessage(nil), compact.Bytes()...)
	input.DetectedAt = input.DetectedAt.UTC()
	hash := sha256.Sum256(input.Payload)
	return input, hex.EncodeToString(hash[:]), nil
}

func normalizeAlertIDs(ids []int64) ([]int64, error) {
	if len(ids) == 0 {
		return nil, fmt.Errorf("at least one alert ID is required")
	}
	normalized := make([]int64, 0, len(ids))
	seen := map[int64]struct{}{}
	for _, id := range ids {
		if id <= 0 {
			return nil, fmt.Errorf(
				"alert ID must be greater than zero",
			)
		}
		if _, exists := seen[id]; exists {
			return nil, fmt.Errorf("duplicate alert ID %d", id)
		}
		seen[id] = struct{}{}
		normalized = append(normalized, id)
	}
	return normalized, nil
}

func alertStatusInTransaction(
	ctx context.Context,
	tx *sql.Tx,
	id int64,
) (models.AlertStatus, error) {
	var status models.AlertStatus
	if err := tx.QueryRowContext(
		ctx,
		`SELECT status FROM alerts WHERE id = ?`,
		id,
	).Scan(&status); err == sql.ErrNoRows {
		return "", fmt.Errorf("%w: alert %d", ErrNotFound, id)
	} else if err != nil {
		return "", fmt.Errorf("query alert %d status: %w", id, err)
	}
	return status, nil
}

func alertIDByFingerprint(
	ctx context.Context,
	tx *sql.Tx,
	fingerprint string,
) (int64, error) {
	var id int64
	if err := tx.QueryRowContext(
		ctx,
		`SELECT id FROM alerts WHERE fingerprint = ?`,
		fingerprint,
	).Scan(&id); err != nil {
		return 0, fmt.Errorf(
			"query alert fingerprint %q: %w",
			fingerprint,
			err,
		)
	}
	return id, nil
}

func exactlyOneRowAffected(result sql.Result) (bool, error) {
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	switch affected {
	case 0:
		return false, nil
	case 1:
		return true, nil
	default:
		return false, fmt.Errorf("expected at most one affected row, got %d", affected)
	}
}

func validAlertSeverity(severity models.AlertSeverity) bool {
	switch severity {
	case models.AlertSeverityInfo,
		models.AlertSeverityWatch,
		models.AlertSeverityWarning:
		return true
	default:
		return false
	}
}

func validAlertStatus(status models.AlertStatus) bool {
	switch status {
	case models.AlertStatusPending,
		models.AlertStatusSent,
		models.AlertStatusFailed,
		models.AlertStatusAcknowledged:
		return true
	default:
		return false
	}
}

func scanAlert(row scanner) (models.Alert, error) {
	var alert models.Alert
	var payload string
	var firstDetectedAt string
	var lastDetectedAt string
	var sentAt string
	var acknowledgedAt string
	var createdAt string
	var updatedAt string
	if err := row.Scan(
		&alert.ID,
		&alert.Fingerprint,
		&alert.Kind,
		&alert.Severity,
		&alert.Status,
		&alert.Title,
		&alert.Fact,
		&alert.SourceRunID,
		&payload,
		&alert.OccurrenceCount,
		&firstDetectedAt,
		&lastDetectedAt,
		&sentAt,
		&acknowledgedAt,
		&createdAt,
		&updatedAt,
	); err != nil {
		return models.Alert{}, err
	}
	if !json.Valid([]byte(payload)) {
		return models.Alert{}, fmt.Errorf("stored alert payload is invalid JSON")
	}
	alert.Payload = json.RawMessage(payload)
	var err error
	alert.FirstDetectedAt, err = parseTime(firstDetectedAt)
	if err != nil {
		return models.Alert{}, fmt.Errorf(
			"parse alert first_detected_at: %w",
			err,
		)
	}
	alert.LastDetectedAt, err = parseTime(lastDetectedAt)
	if err != nil {
		return models.Alert{}, fmt.Errorf(
			"parse alert last_detected_at: %w",
			err,
		)
	}
	alert.SentAt, err = parseOptionalAlertTime(sentAt)
	if err != nil {
		return models.Alert{}, fmt.Errorf("parse alert sent_at: %w", err)
	}
	alert.AcknowledgedAt, err = parseOptionalAlertTime(acknowledgedAt)
	if err != nil {
		return models.Alert{}, fmt.Errorf(
			"parse alert acknowledged_at: %w",
			err,
		)
	}
	alert.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return models.Alert{}, fmt.Errorf("parse alert created_at: %w", err)
	}
	alert.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return models.Alert{}, fmt.Errorf("parse alert updated_at: %w", err)
	}
	return alert, nil
}

func parseOptionalAlertTime(value string) (*time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	parsed, err := parseTime(value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}
