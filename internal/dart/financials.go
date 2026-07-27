package dart

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/models"
	"github.com/rowlet9g/stock-research-bot/internal/provider"
)

const (
	DARTReportCodeAnnual       = "11011"
	DARTReportCodeHalfYear     = "11012"
	DARTReportCodeFirstQuarter = "11013"
	DARTReportCodeThirdQuarter = "11014"

	DARTFinancialStatementConsolidated = "CFS"
	DARTFinancialStatementSeparate     = "OFS"

	maxFinancialStatementResponseBytes = 16 << 20
	maxFinancialStatementAccounts      = 10000
)

type FinancialStatementResult struct {
	Status    models.DataStatus             `json:"status"`
	Statement models.DARTFinancialStatement `json:"statement"`
	Warnings  []string                      `json:"warnings,omitempty"`
}

func (c *Client) FullFinancialStatement(
	ctx context.Context,
	corpCode string,
	businessYear int,
	reportCode string,
	fsKind string,
) (FinancialStatementResult, error) {
	corpCode = strings.TrimSpace(corpCode)
	reportCode = strings.TrimSpace(reportCode)
	fsKind = strings.ToUpper(strings.TrimSpace(fsKind))
	sourceURL := strings.TrimRight(c.baseURL, "/") + "/fnlttSinglAcntAll.json"
	safeParams := url.Values{}
	safeParams.Set("bsns_year", strconv.Itoa(businessYear))
	safeParams.Set("corp_code", corpCode)
	safeParams.Set("fs_div", fsKind)
	safeParams.Set("reprt_code", reportCode)
	result := FinancialStatementResult{
		Status: models.DataStatusUnavailable,
		Statement: models.DARTFinancialStatement{
			CorpCode:     corpCode,
			BusinessYear: businessYear,
			ReportCode:   reportCode,
			FSKind:       fsKind,
			Accounts:     []models.DARTFinancialAccount{},
			Source: models.SourceMetadata{
				Provider:  providerName,
				SourceURL: sourceURL + "?" + safeParams.Encode(),
				FetchedAt: c.now().UTC(),
			},
		},
		Warnings: []string{},
	}
	switch {
	case c.apiKey == "":
		return result, dartRequestError(
			"fetch_financial_statement",
			provider.ErrorKindInvalidRequest,
			"API key is required",
		)
	case !isFixedDigits(corpCode, 8):
		return result, dartRequestError(
			"fetch_financial_statement",
			provider.ErrorKindInvalidRequest,
			"corporation code must be 8 digits",
		)
	case businessYear < 2015 || businessYear > 9999:
		return result, dartRequestError(
			"fetch_financial_statement",
			provider.ErrorKindInvalidRequest,
			"business year must be between 2015 and 9999",
		)
	case !isDARTFinancialReportCode(reportCode):
		return result, dartRequestError(
			"fetch_financial_statement",
			provider.ErrorKindInvalidRequest,
			"unsupported financial report code",
		)
	case !isDARTFinancialStatementKind(fsKind):
		return result, dartRequestError(
			"fetch_financial_statement",
			provider.ErrorKindInvalidRequest,
			"financial statement kind must be CFS or OFS",
		)
	}

	params := cloneValues(safeParams)
	params.Set("crtfc_key", c.apiKey)
	payload, err := c.fetchFinancialStatement(
		ctx,
		sourceURL+"?"+params.Encode(),
	)
	if err != nil {
		return result, err
	}
	if payload.Status == "013" {
		result.Status = models.DataStatusEmpty
		return result, nil
	}
	if payload.Status != "000" {
		return result, dartAPIError(
			"fetch_financial_statement",
			payload.Status,
			payload.Message,
		)
	}
	if len(payload.List) == 0 {
		result.Status = models.DataStatusEmpty
		return result, nil
	}
	if len(payload.List) > maxFinancialStatementAccounts {
		return result, dartRequestError(
			"decode_financial_statement",
			provider.ErrorKindBadResponse,
			fmt.Sprintf(
				"OpenDART financial statement exceeds %d accounts",
				maxFinancialStatementAccounts,
			),
		)
	}

	statement, err := normalizeFinancialStatement(
		payload.List,
		corpCode,
		businessYear,
		reportCode,
		fsKind,
		result.Statement.Source,
	)
	if err != nil {
		return result, dartRequestError(
			"decode_financial_statement",
			provider.ErrorKindBadResponse,
			"invalid OpenDART financial statement: "+err.Error(),
		)
	}
	result.Status = models.DataStatusAvailable
	result.Statement = statement
	return result, nil
}

func (c *Client) fetchFinancialStatement(
	ctx context.Context,
	requestURL string,
) (financialStatementResponse, error) {
	attempts := c.maxAttempts
	if attempts <= 0 {
		attempts = 1
	}

	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		request, err := http.NewRequestWithContext(
			ctx,
			http.MethodGet,
			requestURL,
			nil,
		)
		if err != nil {
			return financialStatementResponse{}, &provider.Error{
				Provider:  providerName,
				Operation: "build_financial_statement_request",
				Kind:      provider.ErrorKindInvalidRequest,
				Message:   "could not build financial statement request",
				Err:       err,
			}
		}
		request.Header.Set("User-Agent", "forgetmenot-research-bot/0.1")
		response, err := c.httpClient.Do(request)
		if err == nil && !retryableHTTPStatus(response.StatusCode) {
			return decodeFinancialStatementResponse(response)
		}
		if err != nil {
			lastErr = err
		}
		if attempt == attempts {
			if response != nil {
				return decodeFinancialStatementResponse(response)
			}
			break
		}
		if response != nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
			response.Body.Close()
		}
		if err := waitForRetry(
			ctx,
			c.retryDelay*time.Duration(attempt),
		); err != nil {
			lastErr = err
			break
		}
	}
	return financialStatementResponse{}, &provider.Error{
		Provider:  providerName,
		Operation: "fetch_financial_statement",
		Kind:      provider.ErrorKindUnavailable,
		Message:   "request failed: " + safeTransportError(lastErr),
		Err:       lastErr,
	}
}

func decodeFinancialStatementResponse(
	response *http.Response,
) (financialStatementResponse, error) {
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		kind := provider.ErrorKindBadResponse
		if response.StatusCode == http.StatusTooManyRequests ||
			response.StatusCode >= 500 {
			kind = provider.ErrorKindUnavailable
		}
		return financialStatementResponse{}, &provider.Error{
			Provider:   providerName,
			Operation:  "fetch_financial_statement",
			Kind:       kind,
			StatusCode: response.StatusCode,
			Message:    "unexpected HTTP status",
		}
	}
	content, err := readLimited(
		response.Body,
		maxFinancialStatementResponseBytes,
	)
	if err != nil {
		return financialStatementResponse{}, &provider.Error{
			Provider:  providerName,
			Operation: "read_financial_statement",
			Kind:      provider.ErrorKindBadResponse,
			Message:   "financial statement response exceeds the size limit or could not be read",
			Err:       err,
		}
	}
	var payload financialStatementResponse
	if err := json.Unmarshal(content, &payload); err != nil {
		return financialStatementResponse{}, &provider.Error{
			Provider:  providerName,
			Operation: "decode_financial_statement",
			Kind:      provider.ErrorKindBadResponse,
			Message:   "invalid JSON response",
			Err:       err,
		}
	}
	return payload, nil
}

func normalizeFinancialStatement(
	rows []financialAccountDTO,
	corpCode string,
	businessYear int,
	reportCode string,
	fsKind string,
	source models.SourceMetadata,
) (models.DARTFinancialStatement, error) {
	statement := models.DARTFinancialStatement{
		CorpCode:     corpCode,
		BusinessYear: businessYear,
		ReportCode:   reportCode,
		FSKind:       fsKind,
		IsCurrent:    true,
		Accounts:     make([]models.DARTFinancialAccount, 0, len(rows)),
		Source:       source,
	}
	seenAccounts := make(map[string]struct{}, len(rows))
	var receiptDate time.Time
	for index, row := range rows {
		receiptNo := strings.TrimSpace(row.ReceiptNo)
		rowCorpCode := strings.TrimSpace(row.CorpCode)
		rowBusinessYear := strings.TrimSpace(row.BusinessYear)
		rowReportCode := strings.TrimSpace(row.ReportCode)
		statementKind := strings.ToUpper(strings.TrimSpace(row.StatementKind))
		statementName := strings.TrimSpace(row.StatementName)
		accountName := strings.TrimSpace(row.AccountName)
		order, err := strconv.Atoi(strings.TrimSpace(row.Order))
		switch {
		case !isFixedDigits(receiptNo, 14):
			return models.DARTFinancialStatement{}, fmt.Errorf(
				"row %d has invalid receipt number %q",
				index+1,
				receiptNo,
			)
		case rowCorpCode != corpCode:
			return models.DARTFinancialStatement{}, fmt.Errorf(
				"row %d belongs to corporation %q",
				index+1,
				rowCorpCode,
			)
		case rowBusinessYear != strconv.Itoa(businessYear):
			return models.DARTFinancialStatement{}, fmt.Errorf(
				"row %d has business year %q",
				index+1,
				rowBusinessYear,
			)
		case rowReportCode != reportCode:
			return models.DARTFinancialStatement{}, fmt.Errorf(
				"row %d has report code %q",
				index+1,
				rowReportCode,
			)
		case !isDARTFinancialStatementSection(statementKind):
			return models.DARTFinancialStatement{}, fmt.Errorf(
				"row %d has unsupported statement kind %q",
				index+1,
				statementKind,
			)
		case statementName == "":
			return models.DARTFinancialStatement{}, fmt.Errorf(
				"row %d has no statement name",
				index+1,
			)
		case accountName == "":
			return models.DARTFinancialStatement{}, fmt.Errorf(
				"row %d has no account name",
				index+1,
			)
		case strings.TrimSpace(row.CurrentTermName) == "":
			return models.DARTFinancialStatement{}, fmt.Errorf(
				"row %d has no current term name",
				index+1,
			)
		case err != nil || order < 0:
			return models.DARTFinancialStatement{}, fmt.Errorf(
				"row %d has invalid order %q",
				index+1,
				row.Order,
			)
		}
		currency := strings.ToUpper(strings.TrimSpace(row.Currency))
		if !isFixedUpperAlpha(currency, 3) {
			return models.DARTFinancialStatement{}, fmt.Errorf(
				"row %d has invalid currency %q",
				index+1,
				row.Currency,
			)
		}
		if statement.ReceiptNo == "" {
			statement.ReceiptNo = receiptNo
			receiptDate, err = time.Parse("20060102", receiptNo[:8])
			if err != nil {
				return models.DARTFinancialStatement{}, fmt.Errorf(
					"row %d has invalid receipt date in %q",
					index+1,
					receiptNo,
				)
			}
			receiptDate = receiptDate.UTC()
		} else if statement.ReceiptNo != receiptNo {
			return models.DARTFinancialStatement{}, fmt.Errorf(
				"row %d has receipt number %q, expected %q",
				index+1,
				receiptNo,
				statement.ReceiptNo,
			)
		}

		amounts := []*string{
			&row.CurrentAmount,
			&row.CurrentAddAmount,
			&row.PreviousAmount,
			&row.PreviousInterimAmount,
			&row.PreviousAddAmount,
			&row.BeforePreviousAmount,
		}
		for amountIndex, amount := range amounts {
			normalized, err := normalizeFinancialAmount(*amount)
			if err != nil {
				return models.DARTFinancialStatement{}, fmt.Errorf(
					"row %d amount %d: %w",
					index+1,
					amountIndex+1,
					err,
				)
			}
			*amount = normalized
		}
		account := models.DARTFinancialAccount{
			Index:                   index,
			StatementKind:           statementKind,
			StatementName:           statementName,
			AccountID:               strings.TrimSpace(row.AccountID),
			AccountName:             accountName,
			AccountDetail:           strings.TrimSpace(row.AccountDetail),
			CurrentTermName:         strings.TrimSpace(row.CurrentTermName),
			CurrentAmount:           row.CurrentAmount,
			CurrentAddAmount:        row.CurrentAddAmount,
			PreviousTermName:        strings.TrimSpace(row.PreviousTermName),
			PreviousAmount:          row.PreviousAmount,
			PreviousInterimTermName: strings.TrimSpace(row.PreviousInterimTermName),
			PreviousInterimAmount:   row.PreviousInterimAmount,
			PreviousAddAmount:       row.PreviousAddAmount,
			BeforePreviousTermName:  strings.TrimSpace(row.BeforePreviousTermName),
			BeforePreviousAmount:    row.BeforePreviousAmount,
			Order:                   order,
			Currency:                currency,
		}
		accountKey, err := financialAccountKey(account)
		if err != nil {
			return models.DARTFinancialStatement{}, err
		}
		if _, exists := seenAccounts[accountKey]; exists {
			return models.DARTFinancialStatement{}, fmt.Errorf(
				"row %d duplicates an identical account",
				index+1,
			)
		}
		seenAccounts[accountKey] = struct{}{}
		statement.Accounts = append(statement.Accounts, account)
	}
	statement.Source.ObservedAt = &receiptDate
	contentSHA256, err := financialStatementContentSHA256(statement)
	if err != nil {
		return models.DARTFinancialStatement{}, err
	}
	statement.ContentSHA256 = contentSHA256
	return statement, nil
}

func normalizeFinancialAmount(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == "-" {
		return "", nil
	}

	negative := false
	if strings.HasPrefix(value, "(") && strings.HasSuffix(value, ")") {
		negative = true
		value = strings.TrimSpace(value[1 : len(value)-1])
	}
	if strings.HasPrefix(value, "+") || strings.HasPrefix(value, "-") {
		if value[0] == '-' {
			negative = !negative
		}
		value = value[1:]
	}
	if value == "" {
		return "", fmt.Errorf("invalid amount")
	}

	groups := strings.Split(value, ",")
	if len(groups) > 1 {
		if len(groups[0]) < 1 || len(groups[0]) > 3 ||
			!allDigits(groups[0]) {
			return "", fmt.Errorf("invalid grouped amount %q", value)
		}
		for _, group := range groups[1:] {
			if len(group) != 3 || !allDigits(group) {
				return "", fmt.Errorf("invalid grouped amount %q", value)
			}
		}
	} else if !allDigits(value) {
		return "", fmt.Errorf("invalid amount %q", value)
	}

	digits := strings.TrimLeft(strings.Join(groups, ""), "0")
	if digits == "" {
		return "0", nil
	}
	if negative {
		return "-" + digits, nil
	}
	return digits, nil
}

func financialStatementContentSHA256(
	statement models.DARTFinancialStatement,
) (string, error) {
	type keyedAccount struct {
		key     string
		account models.DARTFinancialAccount
	}
	keyed := make([]keyedAccount, 0, len(statement.Accounts))
	for _, account := range statement.Accounts {
		account.Index = 0
		key, err := financialAccountKey(account)
		if err != nil {
			return "", err
		}
		keyed = append(keyed, keyedAccount{key: key, account: account})
	}
	sort.Slice(keyed, func(i, j int) bool {
		return keyed[i].key < keyed[j].key
	})
	accounts := make([]models.DARTFinancialAccount, 0, len(keyed))
	for index, item := range keyed {
		item.account.Index = index
		accounts = append(accounts, item.account)
	}
	payload := struct {
		CorpCode     string                        `json:"corp_code"`
		BusinessYear int                           `json:"business_year"`
		ReportCode   string                        `json:"report_code"`
		FSKind       string                        `json:"fs_kind"`
		ReceiptNo    string                        `json:"receipt_no"`
		Accounts     []models.DARTFinancialAccount `json:"accounts"`
	}{
		CorpCode:     statement.CorpCode,
		BusinessYear: statement.BusinessYear,
		ReportCode:   statement.ReportCode,
		FSKind:       statement.FSKind,
		ReceiptNo:    statement.ReceiptNo,
		Accounts:     accounts,
	}
	content, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode financial statement content hash: %w", err)
	}
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:]), nil
}

func financialAccountKey(account models.DARTFinancialAccount) (string, error) {
	account.Index = 0
	content, err := json.Marshal(account)
	if err != nil {
		return "", fmt.Errorf("encode financial account: %w", err)
	}
	return string(content), nil
}

func isDARTFinancialReportCode(value string) bool {
	switch value {
	case DARTReportCodeAnnual,
		DARTReportCodeHalfYear,
		DARTReportCodeFirstQuarter,
		DARTReportCodeThirdQuarter:
		return true
	default:
		return false
	}
}

func isDARTFinancialStatementKind(value string) bool {
	return value == DARTFinancialStatementConsolidated ||
		value == DARTFinancialStatementSeparate
}

func isDARTFinancialStatementSection(value string) bool {
	switch value {
	case "BS", "IS", "CIS", "CF", "SCE":
		return true
	default:
		return false
	}
}

func isFixedUpperAlpha(value string, length int) bool {
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

func allDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

type financialStatementResponse struct {
	Status  string                `json:"status"`
	Message string                `json:"message"`
	List    []financialAccountDTO `json:"list"`
}

type financialAccountDTO struct {
	ReceiptNo               string `json:"rcept_no"`
	ReportCode              string `json:"reprt_code"`
	BusinessYear            string `json:"bsns_year"`
	CorpCode                string `json:"corp_code"`
	StatementKind           string `json:"sj_div"`
	StatementName           string `json:"sj_nm"`
	AccountID               string `json:"account_id"`
	AccountName             string `json:"account_nm"`
	AccountDetail           string `json:"account_detail"`
	CurrentTermName         string `json:"thstrm_nm"`
	CurrentAmount           string `json:"thstrm_amount"`
	CurrentAddAmount        string `json:"thstrm_add_amount"`
	PreviousTermName        string `json:"frmtrm_nm"`
	PreviousAmount          string `json:"frmtrm_amount"`
	PreviousInterimTermName string `json:"frmtrm_q_nm"`
	PreviousInterimAmount   string `json:"frmtrm_q_amount"`
	PreviousAddAmount       string `json:"frmtrm_add_amount"`
	BeforePreviousTermName  string `json:"bfefrmtrm_nm"`
	BeforePreviousAmount    string `json:"bfefrmtrm_amount"`
	Order                   string `json:"ord"`
	Currency                string `json:"currency"`
}
