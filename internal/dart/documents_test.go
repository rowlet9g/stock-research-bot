package dart

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/models"
	"github.com/rowlet9g/stock-research-bot/internal/provider"
)

func TestDisclosureDocumentReturnsValidatedArchiveMetadata(t *testing.T) {
	document, err := os.ReadFile("testdata/document.xml")
	if err != nil {
		t.Fatalf("read document fixture: %v", err)
	}
	archive := documentArchiveFixture(t, "20260724000123.xml", document)
	server := httptest.NewServer(http.HandlerFunc(func(
		response http.ResponseWriter,
		request *http.Request,
	) {
		if request.URL.Path != "/document.xml" {
			t.Errorf("unexpected request path: %s", request.URL.Path)
		}
		if request.URL.Query().Get("crtfc_key") != "test-secret" {
			t.Errorf("unexpected API key")
		}
		if request.URL.Query().Get("rcept_no") != "20260724000123" {
			t.Errorf("unexpected receipt number")
		}
		response.Header().Set("Content-Type", "application/zip")
		_, _ = response.Write(archive)
	}))
	defer server.Close()

	fetchedAt := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	client := &Client{
		apiKey:      "test-secret",
		baseURL:     server.URL,
		httpClient:  server.Client(),
		now:         func() time.Time { return fetchedAt },
		maxAttempts: 1,
	}
	result, err := client.DisclosureDocument(
		context.Background(),
		"20260724000123",
	)
	if err != nil {
		t.Fatalf("fetch disclosure document: %v", err)
	}
	if result.Status != models.DataStatusAvailable ||
		len(result.Content) != len(archive) ||
		result.Archive.SizeBytes != int64(len(archive)) ||
		len(result.Archive.Entries) != 1 {
		t.Fatalf("unexpected document result: %#v", result)
	}
	if result.Archive.Entries[0].Name != "20260724000123.xml" {
		t.Fatalf("unexpected document entry: %#v", result.Archive.Entries[0])
	}
	if len(result.Archive.SHA256) != 64 {
		t.Fatalf("unexpected archive hash: %q", result.Archive.SHA256)
	}
	if len(result.Archive.ContentSHA256) != 64 ||
		len(result.Archive.Entries[0].SHA256) != 64 {
		t.Fatalf("unexpected semantic hashes: %#v", result.Archive)
	}
	if strings.Contains(result.Archive.Source.SourceURL, "test-secret") {
		t.Fatalf("source URL leaked API key: %q", result.Archive.Source.SourceURL)
	}
}

func TestDisclosureDocumentClassifiesMissingFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		response http.ResponseWriter,
		_ *http.Request,
	) {
		_, _ = response.Write([]byte(
			`<?xml version="1.0" encoding="UTF-8"?><result><status>014</status><message>파일이 존재하지 않습니다.</message></result>`,
		))
	}))
	defer server.Close()

	client := &Client{
		apiKey:      "test-secret",
		baseURL:     server.URL,
		httpClient:  server.Client(),
		now:         time.Now,
		maxAttempts: 1,
	}
	_, err := client.DisclosureDocument(
		context.Background(),
		"20260724000123",
	)
	if provider.KindOf(err) != provider.ErrorKindNoData {
		t.Fatalf("expected no data for missing file, got %v", err)
	}
}

func TestDisclosureDocumentRejectsUnsafeZIPPath(t *testing.T) {
	archive := documentArchiveFixture(t, "../outside.xml", []byte("<xml/>"))
	server := httptest.NewServer(http.HandlerFunc(func(
		response http.ResponseWriter,
		_ *http.Request,
	) {
		_, _ = response.Write(archive)
	}))
	defer server.Close()

	client := &Client{
		apiKey:      "test-secret",
		baseURL:     server.URL,
		httpClient:  server.Client(),
		now:         time.Now,
		maxAttempts: 1,
	}
	_, err := client.DisclosureDocument(
		context.Background(),
		"20260724000123",
	)
	if provider.KindOf(err) != provider.ErrorKindBadResponse {
		t.Fatalf("expected bad response for unsafe archive, got %v", err)
	}
}

func TestInspectDocumentArchiveContentHashIgnoresZIPMetadata(t *testing.T) {
	content := []byte("<document>same logical content</document>")
	first := documentArchiveFixtureAt(
		t,
		"document.xml",
		content,
		time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC),
	)
	second := documentArchiveFixtureAt(
		t,
		"document.xml",
		content,
		time.Date(2026, 7, 27, 12, 1, 0, 0, time.UTC),
	)
	if bytes.Equal(first, second) {
		t.Fatal("test ZIP containers unexpectedly have identical bytes")
	}

	firstEntries, firstHash, err := inspectDocumentArchive(first)
	if err != nil {
		t.Fatalf("inspect first archive: %v", err)
	}
	secondEntries, secondHash, err := inspectDocumentArchive(second)
	if err != nil {
		t.Fatalf("inspect second archive: %v", err)
	}
	if firstHash != secondHash ||
		firstEntries[0].SHA256 != secondEntries[0].SHA256 {
		t.Fatalf(
			"ZIP metadata changed semantic hash: first=%s second=%s",
			firstHash,
			secondHash,
		)
	}
}

func documentArchiveFixture(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	return documentArchiveFixtureAt(
		t,
		name,
		content,
		time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC),
	)
}

func documentArchiveFixtureAt(
	t *testing.T,
	name string,
	content []byte,
	modifiedAt time.Time,
) []byte {
	t.Helper()
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	header := &zip.FileHeader{
		Name:   name,
		Method: zip.Deflate,
	}
	header.SetModTime(modifiedAt)
	entry, err := writer.CreateHeader(header)
	if err != nil {
		t.Fatalf("create ZIP entry: %v", err)
	}
	if _, err := entry.Write(content); err != nil {
		t.Fatalf("write ZIP entry: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close ZIP fixture: %v", err)
	}
	return output.Bytes()
}
