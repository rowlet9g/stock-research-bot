package sqlite

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

func (s *Store) Migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			checksum TEXT NOT NULL,
			applied_at TEXT NOT NULL
		)
	`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return fmt.Errorf("read embedded migrations: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		if err := s.applyMigration(ctx, entry.Name()); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) applyMigration(ctx context.Context, name string) error {
	versionText, _, ok := strings.Cut(name, "_")
	if !ok {
		return fmt.Errorf("invalid migration filename %q", name)
	}
	version, err := strconv.Atoi(versionText)
	if err != nil {
		return fmt.Errorf("parse migration version %q: %w", name, err)
	}

	content, err := migrationFiles.ReadFile("migrations/" + name)
	if err != nil {
		return fmt.Errorf("read migration %q: %w", name, err)
	}
	normalizedContent := bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
	normalizedContent = bytes.TrimPrefix(normalizedContent, []byte{0xEF, 0xBB, 0xBF})
	sum := sha256.Sum256(normalizedContent)
	checksum := hex.EncodeToString(sum[:])

	var appliedChecksum string
	err = s.db.QueryRowContext(
		ctx,
		`SELECT checksum FROM schema_migrations WHERE version = ?`,
		version,
	).Scan(&appliedChecksum)
	switch {
	case err == nil:
		if appliedChecksum != checksum {
			return fmt.Errorf("migration %d checksum mismatch", version)
		}
		return nil
	case err != sql.ErrNoRows:
		return fmt.Errorf("query migration %d: %w", version, err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration %d: %w", version, err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, string(content)); err != nil {
		return fmt.Errorf("apply migration %d: %w", version, err)
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO schema_migrations(version, name, checksum, applied_at) VALUES (?, ?, ?, ?)`,
		version,
		name,
		checksum,
		s.now().UTC().Format(timeFormat),
	); err != nil {
		return fmt.Errorf("record migration %d: %w", version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %d: %w", version, err)
	}
	return nil
}

const timeFormat = "2006-01-02T15:04:05.999999999Z07:00"
