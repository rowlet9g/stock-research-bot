package files

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"
)

func TestDARTDocumentStoreSavesAndVerifiesExistingArchive(t *testing.T) {
	content := []byte("deterministic ZIP fixture")
	sum := sha256.Sum256(content)
	hash := hex.EncodeToString(sum[:])
	store, err := NewDARTDocumentStore(t.TempDir())
	if err != nil {
		t.Fatalf("create document store: %v", err)
	}

	first, err := store.Save(
		context.Background(),
		"20260724000123",
		hash,
		content,
	)
	if err != nil {
		t.Fatalf("save document: %v", err)
	}
	if first.AlreadyPresent || first.RelativePath == "" {
		t.Fatalf("unexpected first save result: %#v", first)
	}
	stored, err := os.ReadFile(first.AbsolutePath)
	if err != nil {
		t.Fatalf("read stored document: %v", err)
	}
	if string(stored) != string(content) {
		t.Fatalf("stored document content mismatch")
	}

	second, err := store.Save(
		context.Background(),
		"20260724000123",
		hash,
		content,
	)
	if err != nil {
		t.Fatalf("repeat document save: %v", err)
	}
	if !second.AlreadyPresent || second.RelativePath != first.RelativePath {
		t.Fatalf("unexpected repeated save result: %#v", second)
	}

	verified, err := store.Verify(
		context.Background(),
		first.RelativePath,
		hash,
		int64(len(content)),
	)
	if err != nil {
		t.Fatalf("verify stored document: %v", err)
	}
	if !verified.AlreadyPresent || verified.AbsolutePath != first.AbsolutePath {
		t.Fatalf("unexpected verified document: %#v", verified)
	}
}

func TestDARTDocumentStoreRejectsHashMismatch(t *testing.T) {
	store, err := NewDARTDocumentStore(t.TempDir())
	if err != nil {
		t.Fatalf("create document store: %v", err)
	}
	_, err = store.Save(
		context.Background(),
		"20260724000123",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		[]byte("different"),
	)
	if err == nil {
		t.Fatal("expected hash mismatch error")
	}
}

func TestDARTDocumentStoreRejectsUnsafeVerificationPath(t *testing.T) {
	store, err := NewDARTDocumentStore(t.TempDir())
	if err != nil {
		t.Fatalf("create document store: %v", err)
	}
	_, err = store.Verify(
		context.Background(),
		"../outside.zip",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		1,
	)
	if err == nil {
		t.Fatal("expected unsafe path error")
	}
}
