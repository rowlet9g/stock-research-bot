package dart

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/models"
	"github.com/rowlet9g/stock-research-bot/internal/provider"
)

const (
	maxCorporationArchiveBytes = 32 << 20
	maxCorporationXMLBytes     = 128 << 20
)

type CorporationCodeResult struct {
	Status       models.DataStatus        `json:"status"`
	Corporations []models.DARTCorporation `json:"corporations"`
	Source       models.SourceMetadata    `json:"source"`
}

func (c *Client) CorporationCodes(ctx context.Context) (CorporationCodeResult, error) {
	sourceURL := strings.TrimRight(c.baseURL, "/") + "/corpCode.xml"
	result := CorporationCodeResult{
		Status: models.DataStatusUnavailable,
		Source: models.SourceMetadata{
			Provider:  providerName,
			SourceURL: sourceURL,
			FetchedAt: c.now().UTC(),
		},
		Corporations: []models.DARTCorporation{},
	}
	if c.apiKey == "" {
		return result, dartRequestError(
			"fetch_corporation_codes",
			provider.ErrorKindInvalidRequest,
			"API key is required",
		)
	}

	params := url.Values{}
	params.Set("crtfc_key", c.apiKey)
	requestURL := sourceURL + "?" + params.Encode()
	response, err := c.fetchCorporationArchive(ctx, requestURL)
	if err != nil {
		return result, err
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		kind := provider.ErrorKindBadResponse
		if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
			kind = provider.ErrorKindUnavailable
		}
		return result, &provider.Error{
			Provider:   providerName,
			Operation:  "fetch_corporation_codes",
			Kind:       kind,
			StatusCode: response.StatusCode,
			Message:    "unexpected HTTP status",
		}
	}

	archive, err := readLimited(response.Body, maxCorporationArchiveBytes)
	if err != nil {
		return result, &provider.Error{
			Provider:  providerName,
			Operation: "read_corporation_codes",
			Kind:      provider.ErrorKindBadResponse,
			Message:   "corporation archive exceeds the size limit or could not be read",
			Err:       err,
		}
	}
	corporations, observedAt, err := decodeCorporationArchive(archive, result.Source)
	if err != nil {
		if status, message, ok := decodeDARTError(archive); ok {
			return result, dartAPIError("fetch_corporation_codes", status, message)
		}
		return result, &provider.Error{
			Provider:  providerName,
			Operation: "decode_corporation_codes",
			Kind:      provider.ErrorKindBadResponse,
			Message:   "invalid OpenDART corporation archive: " + err.Error(),
			Err:       err,
		}
	}
	if len(corporations) == 0 {
		result.Status = models.DataStatusEmpty
		return result, nil
	}

	result.Status = models.DataStatusAvailable
	result.Corporations = corporations
	result.Source.ObservedAt = &observedAt
	return result, nil
}

func (c *Client) fetchCorporationArchive(
	ctx context.Context,
	requestURL string,
) (*http.Response, error) {
	attempts := c.maxAttempts
	if attempts <= 0 {
		attempts = 1
	}

	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
		if err != nil {
			return nil, &provider.Error{
				Provider:  providerName,
				Operation: "build_corporation_code_request",
				Kind:      provider.ErrorKindInvalidRequest,
				Message:   "could not build corporation code request",
				Err:       err,
			}
		}
		request.Header.Set("User-Agent", "forgetmenot-research-bot/0.1")

		response, err := c.httpClient.Do(request)
		if err == nil && !retryableHTTPStatus(response.StatusCode) {
			return response, nil
		}
		if err != nil {
			lastErr = err
		}
		if attempt == attempts {
			if response != nil {
				return response, nil
			}
			break
		}
		if response != nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
			response.Body.Close()
		}
		if err := waitForRetry(ctx, c.retryDelay*time.Duration(attempt)); err != nil {
			lastErr = err
			break
		}
	}

	return nil, &provider.Error{
		Provider:  providerName,
		Operation: "fetch_corporation_codes",
		Kind:      provider.ErrorKindUnavailable,
		Message:   "request failed: " + safeTransportError(lastErr),
		Err:       lastErr,
	}
}

func decodeCorporationArchive(
	archive []byte,
	source models.SourceMetadata,
) ([]models.DARTCorporation, time.Time, error) {
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("open ZIP archive: %w", err)
	}

	var corporationFile *zip.File
	for _, file := range reader.File {
		if strings.EqualFold(filepath.Base(file.Name), "CORPCODE.xml") {
			corporationFile = file
			break
		}
		if corporationFile == nil && strings.EqualFold(filepath.Ext(file.Name), ".xml") {
			corporationFile = file
		}
	}
	if corporationFile == nil {
		return nil, time.Time{}, fmt.Errorf("corporation XML file is missing")
	}

	input, err := corporationFile.Open()
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("open corporation XML: %w", err)
	}
	defer input.Close()

	content, err := readLimited(input, maxCorporationXMLBytes)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("read corporation XML: %w", err)
	}
	var payload corporationCodeXML
	if err := xml.Unmarshal(content, &payload); err != nil {
		return nil, time.Time{}, fmt.Errorf("decode corporation XML: %w", err)
	}

	corporations := make([]models.DARTCorporation, 0, len(payload.List))
	seenCorpCodes := make(map[string]struct{}, len(payload.List))
	seenStockCodes := make(map[string]struct{}, len(payload.List))
	var latestModifiedAt time.Time
	for index, item := range payload.List {
		corporation, err := normalizeCorporation(item, source)
		if err != nil {
			return nil, time.Time{}, fmt.Errorf("corporation row %d: %w", index+1, err)
		}
		if _, exists := seenCorpCodes[corporation.CorpCode]; exists {
			return nil, time.Time{}, fmt.Errorf(
				"corporation row %d: duplicate corporation code %q",
				index+1,
				corporation.CorpCode,
			)
		}
		seenCorpCodes[corporation.CorpCode] = struct{}{}
		if corporation.StockCode != "" {
			if _, exists := seenStockCodes[corporation.StockCode]; exists {
				return nil, time.Time{}, fmt.Errorf(
					"corporation row %d: duplicate stock code %q",
					index+1,
					corporation.StockCode,
				)
			}
			seenStockCodes[corporation.StockCode] = struct{}{}
		}
		if corporation.ModifiedAt.After(latestModifiedAt) {
			latestModifiedAt = corporation.ModifiedAt
		}
		corporations = append(corporations, corporation)
	}
	return corporations, latestModifiedAt, nil
}

func normalizeCorporation(
	item corporationCodeDTO,
	source models.SourceMetadata,
) (models.DARTCorporation, error) {
	corpCode := strings.TrimSpace(item.CorpCode)
	name := strings.TrimSpace(item.CorpName)
	stockCode := strings.ToUpper(strings.TrimSpace(item.StockCode))
	modifyDate := strings.TrimSpace(item.ModifyDate)

	switch {
	case !isFixedDigits(corpCode, 8):
		return models.DARTCorporation{}, fmt.Errorf("invalid corporation code %q", corpCode)
	case name == "":
		return models.DARTCorporation{}, fmt.Errorf("corporation name is required")
	case stockCode != "" && !isFixedUpperAlphanumeric(stockCode, 6):
		return models.DARTCorporation{}, fmt.Errorf("invalid stock code %q", stockCode)
	}
	modifiedAt, err := time.Parse("20060102", modifyDate)
	if err != nil {
		return models.DARTCorporation{}, fmt.Errorf("invalid modify date %q", modifyDate)
	}
	modifiedAt = modifiedAt.UTC()
	rowSource := source
	rowSource.ObservedAt = &modifiedAt
	return models.DARTCorporation{
		CorpCode:    corpCode,
		Name:        name,
		EnglishName: strings.TrimSpace(item.CorpEnglishName),
		StockCode:   stockCode,
		ModifiedAt:  modifiedAt,
		Source:      rowSource,
	}, nil
}

func decodeDARTError(content []byte) (string, string, bool) {
	var payload dartErrorXML
	if err := xml.Unmarshal(content, &payload); err != nil {
		return "", "", false
	}
	status := strings.TrimSpace(payload.Status)
	if status == "" {
		return "", "", false
	}
	return status, strings.TrimSpace(payload.Message), true
}

func dartAPIError(operation string, status string, message string) error {
	kind := provider.ErrorKindBadResponse
	switch status {
	case "010", "011", "012", "901":
		kind = provider.ErrorKindInvalidRequest
	case "013", "014":
		kind = provider.ErrorKindNoData
	case "020", "800":
		kind = provider.ErrorKindUnavailable
	}
	return dartRequestError(
		operation,
		kind,
		fmt.Sprintf("OpenDART error %s: %s", status, message),
	)
}

func dartRequestError(operation string, kind provider.ErrorKind, message string) error {
	return &provider.Error{
		Provider:  providerName,
		Operation: operation,
		Kind:      kind,
		Message:   message,
	}
}

func retryableHTTPStatus(statusCode int) bool {
	return statusCode == http.StatusTooManyRequests || statusCode >= 500
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func readLimited(input io.Reader, limit int64) ([]byte, error) {
	content, err := io.ReadAll(io.LimitReader(input, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > limit {
		return nil, fmt.Errorf("content exceeds %d bytes", limit)
	}
	return content, nil
}

func safeTransportError(err error) string {
	if err == nil {
		return "unknown transport error"
	}
	var urlError *url.Error
	if errors.As(err, &urlError) && urlError.Err != nil {
		err = urlError.Err
	}
	return err.Error()
}

func isFixedDigits(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func isFixedUpperAlphanumeric(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') &&
			(character < 'A' || character > 'Z') {
			return false
		}
	}
	return true
}

type corporationCodeXML struct {
	List []corporationCodeDTO `xml:"list"`
}

type corporationCodeDTO struct {
	CorpCode        string `xml:"corp_code"`
	CorpName        string `xml:"corp_name"`
	CorpEnglishName string `xml:"corp_eng_name"`
	StockCode       string `xml:"stock_code"`
	ModifyDate      string `xml:"modify_date"`
}

type dartErrorXML struct {
	Status  string `xml:"status"`
	Message string `xml:"message"`
}
