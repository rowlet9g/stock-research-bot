package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/models"
)

type DARTFinancialStatementSyncResult struct {
	CorpCode       string    `json:"corp_code"`
	BusinessYear   int       `json:"business_year"`
	ReportCode     string    `json:"report_code"`
	FSKind         string    `json:"fs_kind"`
	StatementID    int64     `json:"statement_id"`
	Accounts       int       `json:"accounts"`
	Inserted       bool      `json:"inserted"`
	VersionChanged bool      `json:"version_changed"`
	SyncedAt       time.Time `json:"synced_at"`
}

func (s *Store) SyncDARTFinancialStatement(
	ctx context.Context,
	statement models.DARTFinancialStatement,
) (DARTFinancialStatementSyncResult, error) {
	result := DARTFinancialStatementSyncResult{
		CorpCode:     statement.CorpCode,
		BusinessYear: statement.BusinessYear,
		ReportCode:   statement.ReportCode,
		FSKind:       statement.FSKind,
		Accounts:     len(statement.Accounts),
		SyncedAt:     s.now().UTC(),
	}
	if err := validateDARTFinancialStatement(statement); err != nil {
		return result, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, fmt.Errorf(
			"begin OpenDART financial statement sync for %s: %w",
			statement.CorpCode,
			err,
		)
	}
	defer tx.Rollback()

	var existingID int64
	err = tx.QueryRowContext(ctx, `
		SELECT id
		FROM dart_financial_statements
		WHERE corp_code = ?
		  AND business_year = ?
		  AND report_code = ?
		  AND fs_kind = ?
		  AND content_sha256 = ?
	`,
		statement.CorpCode,
		statement.BusinessYear,
		statement.ReportCode,
		statement.FSKind,
		statement.ContentSHA256,
	).Scan(&existingID)
	switch {
	case err == nil:
	case err == sql.ErrNoRows:
		result.Inserted = true
	default:
		return result, fmt.Errorf(
			"inspect OpenDART financial statement version: %w",
			err,
		)
	}

	var currentHash string
	err = tx.QueryRowContext(ctx, `
		SELECT content_sha256
		FROM dart_financial_statements
		WHERE corp_code = ?
		  AND business_year = ?
		  AND report_code = ?
		  AND fs_kind = ?
		  AND is_current = 1
		LIMIT 1
	`,
		statement.CorpCode,
		statement.BusinessYear,
		statement.ReportCode,
		statement.FSKind,
	).Scan(&currentHash)
	switch {
	case err == nil:
		result.VersionChanged = currentHash != statement.ContentSHA256
	case err == sql.ErrNoRows:
	default:
		return result, fmt.Errorf(
			"inspect current OpenDART financial statement: %w",
			err,
		)
	}

	now := result.SyncedAt.Format(timeFormat)
	if _, err := tx.ExecContext(ctx, `
		UPDATE dart_financial_statements
		SET is_current = 0, updated_at = ?
		WHERE corp_code = ?
		  AND business_year = ?
		  AND report_code = ?
		  AND fs_kind = ?
		  AND id <> ?
		  AND is_current = 1
	`,
		now,
		statement.CorpCode,
		statement.BusinessYear,
		statement.ReportCode,
		statement.FSKind,
		existingID,
	); err != nil {
		return result, fmt.Errorf(
			"retire previous OpenDART financial statement: %w",
			err,
		)
	}

	observedAt := statement.Source.ObservedAt.UTC().Format(timeFormat)
	fetchedAt := statement.Source.FetchedAt.UTC().Format(timeFormat)
	if result.Inserted {
		insertResult, err := tx.ExecContext(ctx, `
			INSERT INTO dart_financial_statements(
				corp_code,
				business_year,
				report_code,
				fs_kind,
				receipt_no,
				content_sha256,
				source_url,
				observed_at,
				fetched_at,
				is_current,
				first_seen_at,
				last_seen_at,
				created_at,
				updated_at
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?, ?)
		`,
			statement.CorpCode,
			statement.BusinessYear,
			statement.ReportCode,
			statement.FSKind,
			statement.ReceiptNo,
			statement.ContentSHA256,
			statement.Source.SourceURL,
			observedAt,
			fetchedAt,
			now,
			now,
			now,
			now,
		)
		if err != nil {
			return result, fmt.Errorf(
				"insert OpenDART financial statement: %w",
				err,
			)
		}
		result.StatementID, err = insertResult.LastInsertId()
		if err != nil {
			return result, fmt.Errorf(
				"read OpenDART financial statement ID: %w",
				err,
			)
		}
		if err := insertDARTFinancialAccounts(
			ctx,
			tx,
			result.StatementID,
			statement.Accounts,
		); err != nil {
			return result, err
		}
	} else {
		result.StatementID = existingID
		if _, err := tx.ExecContext(ctx, `
			UPDATE dart_financial_statements
			SET
				source_url = ?,
				observed_at = ?,
				fetched_at = ?,
				is_current = 1,
				last_seen_at = ?,
				updated_at = ?
			WHERE id = ?
		`,
			statement.Source.SourceURL,
			observedAt,
			fetchedAt,
			now,
			now,
			existingID,
		); err != nil {
			return result, fmt.Errorf(
				"update OpenDART financial statement: %w",
				err,
			)
		}
	}

	if err := tx.Commit(); err != nil {
		return result, fmt.Errorf(
			"commit OpenDART financial statement: %w",
			err,
		)
	}
	return result, nil
}

func insertDARTFinancialAccounts(
	ctx context.Context,
	tx *sql.Tx,
	statementID int64,
	accounts []models.DARTFinancialAccount,
) error {
	prepared, err := tx.PrepareContext(ctx, `
		INSERT INTO dart_financial_accounts(
			statement_id,
			row_index,
			statement_kind,
			statement_name,
			account_id,
			account_name,
			account_detail,
			current_term_name,
			current_amount,
			current_add_amount,
			previous_term_name,
			previous_amount,
			previous_interim_term_name,
			previous_interim_amount,
			previous_add_amount,
			before_previous_term_name,
			before_previous_amount,
			order_no,
			currency
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("prepare OpenDART financial account insert: %w", err)
	}
	defer prepared.Close()

	for _, account := range accounts {
		if _, err := prepared.ExecContext(
			ctx,
			statementID,
			account.Index,
			account.StatementKind,
			account.StatementName,
			account.AccountID,
			account.AccountName,
			account.AccountDetail,
			account.CurrentTermName,
			account.CurrentAmount,
			account.CurrentAddAmount,
			account.PreviousTermName,
			account.PreviousAmount,
			account.PreviousInterimTermName,
			account.PreviousInterimAmount,
			account.PreviousAddAmount,
			account.BeforePreviousTermName,
			account.BeforePreviousAmount,
			account.Order,
			account.Currency,
		); err != nil {
			return fmt.Errorf(
				"insert OpenDART financial account %d: %w",
				account.Index,
				err,
			)
		}
	}
	return nil
}

func (s *Store) ListDARTFinancialStatementVersions(
	ctx context.Context,
	corpCode string,
	businessYear int,
	reportCode string,
	fsKind string,
	limit int,
) ([]models.DARTFinancialStatement, error) {
	corpCode = strings.TrimSpace(corpCode)
	reportCode = strings.TrimSpace(reportCode)
	fsKind = strings.ToUpper(strings.TrimSpace(fsKind))
	if err := validateDARTFinancialQuery(
		corpCode,
		businessYear,
		reportCode,
		fsKind,
	); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 100 {
		return nil, fmt.Errorf(
			"financial statement version limit must be between 1 and 100",
		)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			id,
			corp_code,
			business_year,
			report_code,
			fs_kind,
			receipt_no,
			content_sha256,
			is_current,
			source_url,
			observed_at,
			fetched_at,
			created_at,
			updated_at
		FROM dart_financial_statements
		WHERE corp_code = ?
		  AND business_year = ?
		  AND report_code = ?
		  AND fs_kind = ?
		ORDER BY is_current DESC, created_at DESC
		LIMIT ?
	`,
		corpCode,
		businessYear,
		reportCode,
		fsKind,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"list OpenDART financial statement versions: %w",
			err,
		)
	}
	defer rows.Close()

	statements := []models.DARTFinancialStatement{}
	for rows.Next() {
		statement, err := scanDARTFinancialStatement(rows)
		if err != nil {
			return nil, fmt.Errorf(
				"scan OpenDART financial statement: %w",
				err,
			)
		}
		statements = append(statements, statement)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf(
			"iterate OpenDART financial statements: %w",
			err,
		)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf(
			"close OpenDART financial statement rows: %w",
			err,
		)
	}

	for index := range statements {
		accounts, err := s.listDARTFinancialAccounts(
			ctx,
			statements[index].ID,
		)
		if err != nil {
			return nil, err
		}
		statements[index].Accounts = accounts
	}
	return statements, nil
}

func (s *Store) CurrentDARTFinancialStatement(
	ctx context.Context,
	corpCode string,
	businessYear int,
	reportCode string,
	fsKind string,
) (models.DARTFinancialStatement, error) {
	statements, err := s.ListDARTFinancialStatementVersions(
		ctx,
		corpCode,
		businessYear,
		reportCode,
		fsKind,
		1,
	)
	if err != nil {
		return models.DARTFinancialStatement{}, err
	}
	if len(statements) == 0 || !statements[0].IsCurrent {
		return models.DARTFinancialStatement{}, fmt.Errorf(
			"%w: OpenDART financial statement %s/%d/%s/%s",
			ErrNotFound,
			corpCode,
			businessYear,
			reportCode,
			fsKind,
		)
	}
	return statements[0], nil
}

func scanDARTFinancialStatement(
	row scanner,
) (models.DARTFinancialStatement, error) {
	var statement models.DARTFinancialStatement
	var isCurrent int
	var observedAt string
	var fetchedAt string
	var createdAt string
	var updatedAt string
	if err := row.Scan(
		&statement.ID,
		&statement.CorpCode,
		&statement.BusinessYear,
		&statement.ReportCode,
		&statement.FSKind,
		&statement.ReceiptNo,
		&statement.ContentSHA256,
		&isCurrent,
		&statement.Source.SourceURL,
		&observedAt,
		&fetchedAt,
		&createdAt,
		&updatedAt,
	); err != nil {
		return models.DARTFinancialStatement{}, err
	}
	statement.IsCurrent = isCurrent == 1
	statement.Source.Provider = "opendart"
	parsedObservedAt, err := parseTime(observedAt)
	if err != nil {
		return models.DARTFinancialStatement{}, fmt.Errorf(
			"parse financial statement observed time: %w",
			err,
		)
	}
	statement.Source.ObservedAt = &parsedObservedAt
	statement.Source.FetchedAt, err = parseTime(fetchedAt)
	if err != nil {
		return models.DARTFinancialStatement{}, fmt.Errorf(
			"parse financial statement fetched time: %w",
			err,
		)
	}
	statement.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return models.DARTFinancialStatement{}, fmt.Errorf(
			"parse financial statement created time: %w",
			err,
		)
	}
	statement.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return models.DARTFinancialStatement{}, fmt.Errorf(
			"parse financial statement updated time: %w",
			err,
		)
	}
	statement.Accounts = []models.DARTFinancialAccount{}
	return statement, nil
}

func (s *Store) listDARTFinancialAccounts(
	ctx context.Context,
	statementID int64,
) ([]models.DARTFinancialAccount, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			row_index,
			statement_kind,
			statement_name,
			account_id,
			account_name,
			account_detail,
			current_term_name,
			current_amount,
			current_add_amount,
			previous_term_name,
			previous_amount,
			previous_interim_term_name,
			previous_interim_amount,
			previous_add_amount,
			before_previous_term_name,
			before_previous_amount,
			order_no,
			currency
		FROM dart_financial_accounts
		WHERE statement_id = ?
		ORDER BY row_index
	`, statementID)
	if err != nil {
		return nil, fmt.Errorf("list OpenDART financial accounts: %w", err)
	}
	defer rows.Close()

	accounts := []models.DARTFinancialAccount{}
	for rows.Next() {
		var account models.DARTFinancialAccount
		if err := rows.Scan(
			&account.Index,
			&account.StatementKind,
			&account.StatementName,
			&account.AccountID,
			&account.AccountName,
			&account.AccountDetail,
			&account.CurrentTermName,
			&account.CurrentAmount,
			&account.CurrentAddAmount,
			&account.PreviousTermName,
			&account.PreviousAmount,
			&account.PreviousInterimTermName,
			&account.PreviousInterimAmount,
			&account.PreviousAddAmount,
			&account.BeforePreviousTermName,
			&account.BeforePreviousAmount,
			&account.Order,
			&account.Currency,
		); err != nil {
			return nil, fmt.Errorf("scan OpenDART financial account: %w", err)
		}
		accounts = append(accounts, account)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf(
			"iterate OpenDART financial accounts: %w",
			err,
		)
	}
	return accounts, nil
}

func validateDARTFinancialStatement(
	statement models.DARTFinancialStatement,
) error {
	if err := validateDARTFinancialQuery(
		statement.CorpCode,
		statement.BusinessYear,
		statement.ReportCode,
		statement.FSKind,
	); err != nil {
		return err
	}
	switch {
	case !fixedDigits(statement.ReceiptNo, 14):
		return fmt.Errorf(
			"invalid OpenDART financial receipt number %q",
			statement.ReceiptNo,
		)
	case !fixedLowerHex(statement.ContentSHA256, 64):
		return fmt.Errorf(
			"invalid OpenDART financial content SHA-256 %q",
			statement.ContentSHA256,
		)
	case len(statement.Accounts) == 0:
		return fmt.Errorf("OpenDART financial statement has no accounts")
	case statement.Source.Provider != "opendart":
		return fmt.Errorf(
			"OpenDART financial statement has unexpected provider %q",
			statement.Source.Provider,
		)
	case strings.TrimSpace(statement.Source.SourceURL) == "":
		return fmt.Errorf("OpenDART financial statement source URL is required")
	case statement.Source.ObservedAt == nil:
		return fmt.Errorf("OpenDART financial statement observed time is required")
	case statement.Source.FetchedAt.IsZero():
		return fmt.Errorf("OpenDART financial statement fetched time is required")
	}

	seenIndexes := make(map[int]struct{}, len(statement.Accounts))
	for position, account := range statement.Accounts {
		switch {
		case account.Index < 0 || account.Index >= len(statement.Accounts):
			return fmt.Errorf(
				"OpenDART financial account %d has invalid index %d",
				position+1,
				account.Index,
			)
		case !validDARTFinancialSection(account.StatementKind):
			return fmt.Errorf(
				"OpenDART financial account %d has invalid statement kind %q",
				position+1,
				account.StatementKind,
			)
		case strings.TrimSpace(account.StatementName) == "":
			return fmt.Errorf(
				"OpenDART financial account %d has no statement name",
				position+1,
			)
		case strings.TrimSpace(account.AccountName) == "":
			return fmt.Errorf(
				"OpenDART financial account %d has no account name",
				position+1,
			)
		case strings.TrimSpace(account.CurrentTermName) == "":
			return fmt.Errorf(
				"OpenDART financial account %d has no current term name",
				position+1,
			)
		case account.Order < 0:
			return fmt.Errorf(
				"OpenDART financial account %d has negative order",
				position+1,
			)
		case !fixedUpperAlpha(account.Currency, 3):
			return fmt.Errorf(
				"OpenDART financial account %d has invalid currency %q",
				position+1,
				account.Currency,
			)
		}
		for _, amount := range []string{
			account.CurrentAmount,
			account.CurrentAddAmount,
			account.PreviousAmount,
			account.PreviousInterimAmount,
			account.PreviousAddAmount,
			account.BeforePreviousAmount,
		} {
			if !validCanonicalFinancialAmount(amount) {
				return fmt.Errorf(
					"OpenDART financial account %d has invalid normalized amount %q",
					position+1,
					amount,
				)
			}
		}
		if _, exists := seenIndexes[account.Index]; exists {
			return fmt.Errorf(
				"OpenDART financial account %d duplicates index %d",
				position+1,
				account.Index,
			)
		}
		seenIndexes[account.Index] = struct{}{}
	}
	return nil
}

func validateDARTFinancialQuery(
	corpCode string,
	businessYear int,
	reportCode string,
	fsKind string,
) error {
	switch {
	case !fixedDigits(strings.TrimSpace(corpCode), 8):
		return fmt.Errorf(
			"invalid OpenDART corporation code %q",
			corpCode,
		)
	case businessYear < 2015 || businessYear > 9999:
		return fmt.Errorf(
			"OpenDART financial business year must be between 2015 and 9999",
		)
	case !validDARTFinancialReportCode(strings.TrimSpace(reportCode)):
		return fmt.Errorf(
			"invalid OpenDART financial report code %q",
			reportCode,
		)
	case !validDARTFinancialFSKind(strings.ToUpper(strings.TrimSpace(fsKind))):
		return fmt.Errorf(
			"invalid OpenDART financial statement kind %q",
			fsKind,
		)
	}
	return nil
}

func validDARTFinancialReportCode(value string) bool {
	switch value {
	case "11011", "11012", "11013", "11014":
		return true
	default:
		return false
	}
}

func validDARTFinancialFSKind(value string) bool {
	return value == "CFS" || value == "OFS"
}

func validDARTFinancialSection(value string) bool {
	switch value {
	case "BS", "IS", "CIS", "CF", "SCE":
		return true
	default:
		return false
	}
}

func fixedUpperAlpha(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for _, character := range value {
		if character < 'A' || character > 'Z' {
			return false
		}
	}
	return true
}

func validCanonicalFinancialAmount(value string) bool {
	if value == "" || value == "0" {
		return true
	}
	if strings.HasPrefix(value, "-") {
		value = value[1:]
	}
	if value == "" || strings.HasPrefix(value, "0") {
		return false
	}
	return fixedDigits(value, len(value))
}
