package files

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type DARTDocumentStore struct {
	root string
}

type StoredDARTDocument struct {
	RelativePath   string `json:"relative_path"`
	AbsolutePath   string `json:"-"`
	AlreadyPresent bool   `json:"already_present"`
}

func NewDARTDocumentStore(root string) (*DARTDocumentStore, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("DART document storage root is required")
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve DART document storage root %q: %w", root, err)
	}
	if err := os.MkdirAll(absoluteRoot, 0o700); err != nil {
		return nil, fmt.Errorf("create DART document storage root: %w", err)
	}
	return &DARTDocumentStore{root: absoluteRoot}, nil
}

func (s *DARTDocumentStore) Save(
	ctx context.Context,
	receiptNo string,
	expectedSHA256 string,
	content []byte,
) (StoredDARTDocument, error) {
	receiptNo = strings.TrimSpace(receiptNo)
	expectedSHA256 = strings.ToLower(strings.TrimSpace(expectedSHA256))
	switch {
	case !fixedDigits(receiptNo, 14):
		return StoredDARTDocument{}, fmt.Errorf(
			"invalid DART receipt number %q",
			receiptNo,
		)
	case !fixedLowerHex(expectedSHA256, 64):
		return StoredDARTDocument{}, fmt.Errorf(
			"invalid DART document SHA-256 %q",
			expectedSHA256,
		)
	case len(content) == 0:
		return StoredDARTDocument{}, fmt.Errorf("DART document content is empty")
	}
	if err := ctx.Err(); err != nil {
		return StoredDARTDocument{}, err
	}

	sum := sha256.Sum256(content)
	actualSHA256 := hex.EncodeToString(sum[:])
	if actualSHA256 != expectedSHA256 {
		return StoredDARTDocument{}, fmt.Errorf(
			"DART document SHA-256 mismatch: expected %s, got %s",
			expectedSHA256,
			actualSHA256,
		)
	}

	receiptDirectory := filepath.Join(s.root, receiptNo)
	if err := os.MkdirAll(receiptDirectory, 0o700); err != nil {
		return StoredDARTDocument{}, fmt.Errorf(
			"create DART document receipt directory: %w",
			err,
		)
	}
	fileName := expectedSHA256 + ".zip"
	absolutePath := filepath.Join(receiptDirectory, fileName)
	relativePath := filepath.ToSlash(filepath.Join(receiptNo, fileName))
	if info, err := os.Stat(absolutePath); err == nil {
		if info.IsDir() {
			return StoredDARTDocument{}, fmt.Errorf(
				"DART document path %q is a directory",
				absolutePath,
			)
		}
		if err := verifyStoredFile(absolutePath, expectedSHA256, int64(len(content))); err != nil {
			return StoredDARTDocument{}, err
		}
		return StoredDARTDocument{
			RelativePath:   relativePath,
			AbsolutePath:   absolutePath,
			AlreadyPresent: true,
		}, nil
	} else if !os.IsNotExist(err) {
		return StoredDARTDocument{}, fmt.Errorf(
			"inspect DART document path %q: %w",
			absolutePath,
			err,
		)
	}

	tempFile, err := os.CreateTemp(receiptDirectory, ".document-*.tmp")
	if err != nil {
		return StoredDARTDocument{}, fmt.Errorf("create DART document temporary file: %w", err)
	}
	tempPath := tempFile.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()

	if err := tempFile.Chmod(0o600); err != nil {
		tempFile.Close()
		return StoredDARTDocument{}, fmt.Errorf("restrict DART document temporary file: %w", err)
	}
	if _, err := tempFile.Write(content); err != nil {
		tempFile.Close()
		return StoredDARTDocument{}, fmt.Errorf("write DART document temporary file: %w", err)
	}
	if err := tempFile.Sync(); err != nil {
		tempFile.Close()
		return StoredDARTDocument{}, fmt.Errorf("sync DART document temporary file: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return StoredDARTDocument{}, fmt.Errorf("close DART document temporary file: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return StoredDARTDocument{}, err
	}
	if err := os.Rename(tempPath, absolutePath); err != nil {
		return StoredDARTDocument{}, fmt.Errorf("publish DART document archive: %w", err)
	}
	removeTemp = false
	return StoredDARTDocument{
		RelativePath: relativePath,
		AbsolutePath: absolutePath,
	}, nil
}

func (s *DARTDocumentStore) Verify(
	ctx context.Context,
	relativePath string,
	expectedSHA256 string,
	expectedSize int64,
) (StoredDARTDocument, error) {
	relativePath = strings.TrimSpace(relativePath)
	expectedSHA256 = strings.ToLower(strings.TrimSpace(expectedSHA256))
	switch {
	case !fixedLowerHex(expectedSHA256, 64):
		return StoredDARTDocument{}, fmt.Errorf(
			"invalid DART document SHA-256 %q",
			expectedSHA256,
		)
	case expectedSize <= 0:
		return StoredDARTDocument{}, fmt.Errorf(
			"DART document size must be positive",
		)
	}
	if err := ctx.Err(); err != nil {
		return StoredDARTDocument{}, err
	}

	canonicalPath, err := canonicalDARTDocumentPath(
		relativePath,
		expectedSHA256,
	)
	if err != nil {
		return StoredDARTDocument{}, err
	}
	absolutePath := filepath.Join(s.root, filepath.FromSlash(canonicalPath))
	if err := verifyStoredFile(absolutePath, expectedSHA256, expectedSize); err != nil {
		return StoredDARTDocument{}, err
	}
	return StoredDARTDocument{
		RelativePath:   canonicalPath,
		AbsolutePath:   absolutePath,
		AlreadyPresent: true,
	}, nil
}

func canonicalDARTDocumentPath(
	relativePath string,
	expectedSHA256 string,
) (string, error) {
	normalized := strings.ReplaceAll(strings.TrimSpace(relativePath), "\\", "/")
	parts := strings.Split(normalized, "/")
	if len(parts) != 2 ||
		!fixedDigits(parts[0], 14) ||
		parts[1] != expectedSHA256+".zip" {
		return "", fmt.Errorf(
			"invalid DART document relative path %q",
			relativePath,
		)
	}
	return parts[0] + "/" + parts[1], nil
}

func verifyStoredFile(path string, expectedSHA256 string, expectedSize int64) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open existing DART document %q: %w", path, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("inspect existing DART document %q: %w", path, err)
	}
	if info.Size() != expectedSize {
		return fmt.Errorf(
			"existing DART document %q has size %d, expected %d",
			path,
			info.Size(),
			expectedSize,
		)
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return fmt.Errorf("hash existing DART document %q: %w", path, err)
	}
	if hex.EncodeToString(hash.Sum(nil)) != expectedSHA256 {
		return fmt.Errorf("existing DART document %q failed SHA-256 verification", path)
	}
	return nil
}

func fixedDigits(value string, length int) bool {
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

func fixedLowerHex(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') &&
			(character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}
