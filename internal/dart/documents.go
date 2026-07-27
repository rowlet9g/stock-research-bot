package dart

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rowlet9g/stock-research-bot/internal/models"
	"github.com/rowlet9g/stock-research-bot/internal/provider"
)

const (
	maxDocumentArchiveBytes      = 64 << 20
	maxDocumentEntries           = 1000
	maxDocumentEntryBytes        = 128 << 20
	maxDocumentUncompressedBytes = 256 << 20
)

type DocumentResult struct {
	Status  models.DataStatus          `json:"status"`
	Archive models.DARTDocumentArchive `json:"archive"`
	Content []byte                     `json:"-"`
}

func (c *Client) DisclosureDocument(
	ctx context.Context,
	receiptNo string,
) (DocumentResult, error) {
	receiptNo = strings.TrimSpace(receiptNo)
	sourceURL := strings.TrimRight(c.baseURL, "/") + "/document.xml"
	safeParams := url.Values{}
	safeParams.Set("rcept_no", receiptNo)
	safeSourceURL := sourceURL + "?" + safeParams.Encode()
	result := DocumentResult{
		Status: models.DataStatusUnavailable,
		Archive: models.DARTDocumentArchive{
			ReceiptNo: receiptNo,
			Entries:   []models.DARTDocumentEntry{},
			Source: models.SourceMetadata{
				Provider:  providerName,
				SourceURL: safeSourceURL,
				FetchedAt: c.now().UTC(),
			},
		},
	}
	switch {
	case c.apiKey == "":
		return result, dartRequestError(
			"fetch_disclosure_document",
			provider.ErrorKindInvalidRequest,
			"API key is required",
		)
	case !isFixedDigits(receiptNo, 14):
		return result, dartRequestError(
			"fetch_disclosure_document",
			provider.ErrorKindInvalidRequest,
			"receipt number must be 14 digits",
		)
	}

	params := cloneValues(safeParams)
	params.Set("crtfc_key", c.apiKey)
	response, err := c.fetchDocumentArchive(
		ctx,
		sourceURL+"?"+params.Encode(),
	)
	if err != nil {
		return result, err
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		kind := provider.ErrorKindBadResponse
		if response.StatusCode == http.StatusTooManyRequests ||
			response.StatusCode >= 500 {
			kind = provider.ErrorKindUnavailable
		}
		return result, &provider.Error{
			Provider:   providerName,
			Operation:  "fetch_disclosure_document",
			Kind:       kind,
			StatusCode: response.StatusCode,
			Message:    "unexpected HTTP status",
		}
	}

	content, err := readLimited(response.Body, maxDocumentArchiveBytes)
	if err != nil {
		return result, &provider.Error{
			Provider:  providerName,
			Operation: "read_disclosure_document",
			Kind:      provider.ErrorKindBadResponse,
			Message:   "document archive exceeds the size limit or could not be read",
			Err:       err,
		}
	}
	entries, contentSHA256, err := inspectDocumentArchive(content)
	if err != nil {
		if status, message, ok := decodeDARTError(content); ok {
			return result, dartAPIError(
				"fetch_disclosure_document",
				status,
				message,
			)
		}
		return result, &provider.Error{
			Provider:  providerName,
			Operation: "decode_disclosure_document",
			Kind:      provider.ErrorKindBadResponse,
			Message:   "invalid OpenDART document archive: " + err.Error(),
			Err:       err,
		}
	}

	sum := sha256.Sum256(content)
	result.Status = models.DataStatusAvailable
	result.Content = content
	result.Archive.SHA256 = hex.EncodeToString(sum[:])
	result.Archive.ContentSHA256 = contentSHA256
	result.Archive.SizeBytes = int64(len(content))
	result.Archive.Entries = entries
	result.Archive.IsCurrent = true
	return result, nil
}

func (c *Client) fetchDocumentArchive(
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
				Operation: "build_disclosure_document_request",
				Kind:      provider.ErrorKindInvalidRequest,
				Message:   "could not build disclosure document request",
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
		Operation: "fetch_disclosure_document",
		Kind:      provider.ErrorKindUnavailable,
		Message:   "request failed: " + safeTransportError(lastErr),
		Err:       lastErr,
	}
}

func inspectDocumentArchive(
	content []byte,
) ([]models.DARTDocumentEntry, string, error) {
	reader, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return nil, "", fmt.Errorf("open ZIP archive: %w", err)
	}
	if len(reader.File) == 0 {
		return nil, "", fmt.Errorf("ZIP archive is empty")
	}
	if len(reader.File) > maxDocumentEntries {
		return nil, "", fmt.Errorf(
			"ZIP archive contains more than %d entries",
			maxDocumentEntries,
		)
	}

	entries := make([]models.DARTDocumentEntry, 0, len(reader.File))
	seenNames := make(map[string]struct{}, len(reader.File))
	var totalUncompressed uint64
	for index, file := range reader.File {
		name := file.Name
		if !utf8.ValidString(name) {
			name = strings.ToValidUTF8(name, "\uFFFD")
		}
		cleanName := path.Clean(strings.ReplaceAll(name, "\\", "/"))
		if cleanName == "." ||
			strings.HasPrefix(cleanName, "/") ||
			cleanName == ".." ||
			strings.HasPrefix(cleanName, "../") {
			return nil, "", fmt.Errorf("ZIP entry %d has unsafe path %q", index+1, name)
		}
		if file.FileInfo().IsDir() {
			continue
		}
		if _, exists := seenNames[cleanName]; exists {
			return nil, "", fmt.Errorf("ZIP archive duplicates entry name %q", cleanName)
		}
		seenNames[cleanName] = struct{}{}
		if file.UncompressedSize64 > maxDocumentEntryBytes {
			return nil, "", fmt.Errorf(
				"ZIP entry %q exceeds the uncompressed size limit",
				cleanName,
			)
		}
		if totalUncompressed >
			uint64(maxDocumentUncompressedBytes)-file.UncompressedSize64 {
			return nil, "", fmt.Errorf("ZIP archive exceeds the uncompressed size limit")
		}
		totalUncompressed += file.UncompressedSize64

		input, err := file.Open()
		if err != nil {
			return nil, "", fmt.Errorf("open ZIP entry %q: %w", cleanName, err)
		}
		entryHash := sha256.New()
		written, copyErr := io.Copy(
			io.MultiWriter(io.Discard, entryHash),
			io.LimitReader(input, maxDocumentEntryBytes+1),
		)
		closeErr := input.Close()
		if copyErr != nil {
			return nil, "", fmt.Errorf("verify ZIP entry %q: %w", cleanName, copyErr)
		}
		if written > maxDocumentEntryBytes {
			return nil, "", fmt.Errorf(
				"ZIP entry %q exceeds the uncompressed size limit",
				cleanName,
			)
		}
		if closeErr != nil {
			return nil, "", fmt.Errorf("close ZIP entry %q: %w", cleanName, closeErr)
		}
		if written != int64(file.UncompressedSize64) {
			return nil, "", fmt.Errorf(
				"ZIP entry %q size mismatch: expected %d, read %d",
				cleanName,
				file.UncompressedSize64,
				written,
			)
		}
		entries = append(entries, models.DARTDocumentEntry{
			Index:             len(entries),
			Name:              cleanName,
			SHA256:            hex.EncodeToString(entryHash.Sum(nil)),
			CompressedBytes:   int64(file.CompressedSize64),
			UncompressedBytes: int64(file.UncompressedSize64),
			CRC32:             file.CRC32,
		})
	}
	if len(entries) == 0 {
		return nil, "", fmt.Errorf("ZIP archive contains no files")
	}
	contentHash := sha256.New()
	sortedEntries := append([]models.DARTDocumentEntry(nil), entries...)
	sort.Slice(sortedEntries, func(i, j int) bool {
		return sortedEntries[i].Name < sortedEntries[j].Name
	})
	for _, entry := range sortedEntries {
		fmt.Fprintf(
			contentHash,
			"%d:%s:%d:%s\n",
			len(entry.Name),
			entry.Name,
			entry.UncompressedBytes,
			entry.SHA256,
		)
	}
	return entries, hex.EncodeToString(contentHash.Sum(nil)), nil
}
