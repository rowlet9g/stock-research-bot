package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db  *sql.DB
	now func() time.Time
}

func Open(ctx context.Context, path string) (*Store, error) {
	return openWithClock(ctx, path, time.Now)
}

func openWithClock(ctx context.Context, path string, now func() time.Time) (*Store, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("SQLite path is required")
	}

	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve SQLite path %q: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(absolutePath), 0o750); err != nil {
		return nil, fmt.Errorf("create SQLite directory: %w", err)
	}

	escapedPath := (&url.URL{Path: filepath.ToSlash(absolutePath)}).EscapedPath()
	query := url.Values{}
	query.Add("_pragma", "foreign_keys(1)")
	query.Add("_pragma", "busy_timeout(5000)")
	query.Add("_pragma", "journal_mode(WAL)")
	dsn := "file:" + escapedPath + "?" + query.Encode()

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open SQLite database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	store := &Store{db: db, now: now}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping SQLite database: %w", err)
	}
	if err := store.Migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if err := os.Chmod(absolutePath, 0o600); err != nil {
		db.Close()
		return nil, fmt.Errorf("restrict SQLite file permissions: %w", err)
	}
	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Ping(ctx context.Context) error {
	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping SQLite database: %w", err)
	}
	return nil
}
