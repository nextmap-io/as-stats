// Package migrate applies the embedded ClickHouse schema migrations.
//
// ClickHouse does not provide a transaction spanning multiple DDL statements.
// The runner therefore records an "applying" event before the first statement
// and refuses to retry a dirty migration automatically. This favours an
// explicit operator recovery over silently replaying destructive SQL.
package migrate

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	StatusApplying = "applying"
	StatusApplied  = "applied"
	StatusFailed   = "failed"
)

var migrationFilename = regexp.MustCompile(`^(\d{6})_([a-z0-9_]+)\.up\.sql$`)

// Migration is one immutable, ordered schema change.
type Migration struct {
	Version  uint64
	Name     string
	Filename string
	Checksum string
	SQL      string
}

// Record is the latest durable state recorded for a migration version.
type Record struct {
	Version  uint64
	Name     string
	Checksum string
	Status   string
	Error    string
}

// Store isolates ClickHouse-specific coordination and history persistence.
type Store interface {
	AcquireLock(context.Context, string) error
	ReleaseLock(context.Context, string) error
	EnsureHistory(context.Context) error
	History(context.Context) ([]Record, error)
	HasUserSchema(context.Context) (bool, error)
	DetectBaseline(context.Context) (uint64, error)
	Append(context.Context, Record, string) error
	Exec(context.Context, string) error
}

// Runner applies migrations in order.
type Runner struct {
	Store        Store
	Migrations   []Migration
	Owner        string
	AutoBaseline bool
}

// Load reads, validates, orders, and checksums all *.up.sql migrations.
func Load(fsys fs.FS, dir string) ([]Migration, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}

	var out []Migration
	seen := make(map[uint64]string)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		matches := migrationFilename.FindStringSubmatch(entry.Name())
		if matches == nil {
			continue
		}
		version, err := strconv.ParseUint(matches[1], 10, 64)
		if err != nil || version == 0 {
			return nil, fmt.Errorf("invalid migration filename %q", entry.Name())
		}
		if previous, ok := seen[version]; ok {
			return nil, fmt.Errorf("duplicate migration version %06d: %s and %s", version, previous, entry.Name())
		}
		body, err := fs.ReadFile(fsys, path.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}
		sum := sha256.Sum256(body)
		seen[version] = entry.Name()
		out = append(out, Migration{
			Version:  version,
			Name:     matches[2],
			Filename: entry.Name(),
			Checksum: fmt.Sprintf("%x", sum),
			SQL:      string(body),
		})
	}
	if len(out) == 0 {
		return nil, errors.New("no migrations found")
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	for i, migration := range out {
		expected := uint64(i + 1)
		if migration.Version != expected {
			return nil, fmt.Errorf("migration sequence has a gap: expected %06d, found %06d", expected, migration.Version)
		}
	}
	return out, nil
}

// Run acquires the exclusive DDL lock, adopts a verified legacy schema when
// necessary, verifies checksums, and applies every pending migration.
func (r Runner) Run(ctx context.Context) (retErr error) {
	if r.Store == nil || len(r.Migrations) == 0 || strings.TrimSpace(r.Owner) == "" {
		return errors.New("invalid migration runner configuration")
	}
	if err := r.Store.AcquireLock(ctx, r.Owner); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	defer func() {
		if err := r.Store.ReleaseLock(context.WithoutCancel(ctx), r.Owner); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("release migration lock: %w", err))
		}
	}()

	if err := r.Store.EnsureHistory(ctx); err != nil {
		return fmt.Errorf("ensure migration history: %w", err)
	}
	history, err := r.Store.History(ctx)
	if err != nil {
		return fmt.Errorf("read migration history: %w", err)
	}
	if len(history) == 0 {
		hasSchema, err := r.Store.HasUserSchema(ctx)
		if err != nil {
			return fmt.Errorf("inspect existing schema: %w", err)
		}
		if hasSchema {
			if !r.AutoBaseline {
				return errors.New("existing schema has no migration history; automatic adoption is disabled")
			}
			baseline, err := r.Store.DetectBaseline(ctx)
			if err != nil {
				return fmt.Errorf("existing schema cannot be adopted safely: %w", err)
			}
			if baseline == 0 || baseline > uint64(len(r.Migrations)) {
				return fmt.Errorf("existing schema reported invalid baseline %d", baseline)
			}
			for _, migration := range r.Migrations[:baseline] {
				record := Record{Version: migration.Version, Name: migration.Name, Checksum: migration.Checksum, Status: StatusApplied}
				if err := r.Store.Append(ctx, record, "baseline:auto"); err != nil {
					return fmt.Errorf("record baseline %06d: %w", migration.Version, err)
				}
			}
			history, err = r.Store.History(ctx)
			if err != nil {
				return fmt.Errorf("reload migration history: %w", err)
			}
		}
	}

	applied, err := r.validateHistory(history)
	if err != nil {
		return err
	}
	for _, migration := range r.Migrations {
		if migration.Version <= applied {
			continue
		}
		applying := Record{Version: migration.Version, Name: migration.Name, Checksum: migration.Checksum, Status: StatusApplying}
		if err := r.Store.Append(ctx, applying, r.Owner); err != nil {
			return fmt.Errorf("mark migration %06d applying: %w", migration.Version, err)
		}
		statements, err := SplitStatements(migration.SQL)
		if err != nil {
			return r.fail(ctx, migration, fmt.Errorf("parse SQL: %w", err))
		}
		for index, statement := range statements {
			if err := r.Store.Exec(ctx, statement); err != nil {
				return r.fail(ctx, migration, fmt.Errorf("statement %d/%d: %w", index+1, len(statements), err))
			}
		}
		appliedRecord := Record{Version: migration.Version, Name: migration.Name, Checksum: migration.Checksum, Status: StatusApplied}
		if err := r.Store.Append(ctx, appliedRecord, r.Owner); err != nil {
			return fmt.Errorf("migration %06d executed but could not be marked applied: %w; migration is dirty and must be inspected manually", migration.Version, err)
		}
		applied = migration.Version
	}
	return nil
}

func (r Runner) fail(ctx context.Context, migration Migration, cause error) error {
	record := Record{Version: migration.Version, Name: migration.Name, Checksum: migration.Checksum, Status: StatusFailed, Error: cause.Error()}
	if err := r.Store.Append(ctx, record, r.Owner); err != nil {
		cause = errors.Join(cause, fmt.Errorf("record failure: %w", err))
	}
	return fmt.Errorf("migration %06d failed and will not be retried automatically; inspect the partial ClickHouse DDL and repair the history before restarting: %w", migration.Version, cause)
}

func (r Runner) validateHistory(history []Record) (uint64, error) {
	byVersion := make(map[uint64]Record, len(history))
	for _, record := range history {
		byVersion[record.Version] = record
	}
	var applied uint64
	for version := uint64(1); version <= uint64(len(r.Migrations)); version++ {
		record, ok := byVersion[version]
		if !ok {
			for later := version + 1; later <= uint64(len(r.Migrations)); later++ {
				if _, exists := byVersion[later]; exists {
					return 0, fmt.Errorf("migration history has a gap at %06d", version)
				}
			}
			break
		}
		migration := r.Migrations[version-1]
		if record.Name != migration.Name || record.Checksum != migration.Checksum {
			return 0, fmt.Errorf("migration %06d checksum/name mismatch: recorded %s/%s, embedded %s/%s", version, record.Name, record.Checksum, migration.Name, migration.Checksum)
		}
		if record.Status != StatusApplied {
			return 0, fmt.Errorf("migration %06d is dirty (status %q): %s; automatic replay is disabled", version, record.Status, record.Error)
		}
		applied = version
	}
	for version := range byVersion {
		if version == 0 || version > uint64(len(r.Migrations)) {
			return 0, fmt.Errorf("database contains unknown migration version %06d; use a compatible/newer migrator", version)
		}
	}
	return applied, nil
}

// SplitStatements separates ClickHouse statements without splitting inside
// quoted strings, quoted identifiers, or comments.
func SplitStatements(sql string) ([]string, error) {
	var statements []string
	start := 0
	quote := byte(0)
	lineComment := false
	blockComment := false
	escaped := false
	for i := 0; i < len(sql); i++ {
		ch := sql[i]
		next := byte(0)
		if i+1 < len(sql) {
			next = sql[i+1]
		}
		if lineComment {
			if ch == '\n' {
				lineComment = false
			}
			continue
		}
		if blockComment {
			if ch == '*' && next == '/' {
				blockComment = false
				i++
			}
			continue
		}
		if quote != 0 {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == quote {
				if i+1 < len(sql) && sql[i+1] == quote {
					i++
					continue
				}
				quote = 0
			}
			continue
		}
		switch {
		case ch == '-' && next == '-':
			lineComment = true
			i++
		case ch == '/' && next == '*':
			blockComment = true
			i++
		case ch == '\'' || ch == '"' || ch == '`':
			quote = ch
		case ch == ';':
			if statement := strings.TrimSpace(sql[start:i]); statement != "" {
				statements = append(statements, statement)
			}
			start = i + 1
		}
	}
	if quote != 0 || blockComment {
		return nil, errors.New("unterminated quote or block comment")
	}
	if statement := strings.TrimSpace(sql[start:]); statement != "" {
		statements = append(statements, statement)
	}
	return statements, nil
}
