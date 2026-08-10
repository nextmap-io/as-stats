package migrate_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nextmap-io/as-stats/internal/migrate"
	schema "github.com/nextmap-io/as-stats/migrations"
)

func TestLoadEmbeddedMigrations(t *testing.T) {
	migrations, err := migrate.Load(schema.Files, ".")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := len(migrations), 15; got != want {
		t.Fatalf("migration count = %d, want %d", got, want)
	}
	for i, migration := range migrations {
		if migration.Version != uint64(i+1) {
			t.Fatalf("migration[%d].Version = %d", i, migration.Version)
		}
		if len(migration.Checksum) != 64 {
			t.Fatalf("migration %d checksum length = %d", migration.Version, len(migration.Checksum))
		}
	}
}

func TestLoadRejectsGapAndDuplicate(t *testing.T) {
	tests := map[string]fstest.MapFS{
		"gap": {
			"000001_one.up.sql":   {Data: []byte("SELECT 1")},
			"000003_three.up.sql": {Data: []byte("SELECT 3")},
		},
		"duplicate": {
			"000001_one.up.sql":   {Data: []byte("SELECT 1")},
			"000001_other.up.sql": {Data: []byte("SELECT 2")},
		},
	}
	for name, files := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := migrate.Load(files, "."); err == nil {
				t.Fatal("Load succeeded, want error")
			}
		})
	}
}

func TestSplitStatementsHonoursQuotesAndComments(t *testing.T) {
	sql := "-- comment; ignored\nSELECT 'a; b'; /* block; */ SELECT `semi;colon`; SELECT \"x;y\";"
	statements, err := migrate.SplitStatements(sql)
	if err != nil {
		t.Fatalf("SplitStatements: %v", err)
	}
	if got, want := len(statements), 3; got != want {
		t.Fatalf("statement count = %d, want %d: %#v", got, want, statements)
	}
	if !strings.Contains(statements[0], "'a; b'") || !strings.Contains(statements[1], "`semi;colon`") {
		t.Fatalf("quotes were split incorrectly: %#v", statements)
	}
}

func TestRunnerAppliesAndThenSkipsVersionedMigration(t *testing.T) {
	store := &fakeStore{}
	runner := migrate.Runner{Store: store, Migrations: testMigrations(), Owner: "test", AutoBaseline: true}
	if err := runner.Run(context.Background()); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	if got, want := len(store.executed), 2; got != want {
		t.Fatalf("executed = %d, want %d", got, want)
	}
	if err := runner.Run(context.Background()); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if got, want := len(store.executed), 2; got != want {
		t.Fatalf("migration replayed: executed = %d, want %d", got, want)
	}
}

func TestRunnerBaselinesVerifiedExistingSchemaWithoutExecutingOldSQL(t *testing.T) {
	store := &fakeStore{hasSchema: true, baseline: 1}
	runner := migrate.Runner{Store: store, Migrations: testMigrations(), Owner: "test", AutoBaseline: true}
	if err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got, want := store.executed, []string{"SELECT 2"}; len(got) != 1 || got[0] != want[0] {
		t.Fatalf("executed = %#v, want %#v", got, want)
	}
	if store.events[0].Version != 1 || store.events[0].Status != migrate.StatusApplied {
		t.Fatalf("baseline event = %#v", store.events[0])
	}
}

func TestRunnerRefusesChecksumMismatch(t *testing.T) {
	store := &fakeStore{history: []migrate.Record{{Version: 1, Name: "one", Checksum: "changed", Status: migrate.StatusApplied}}}
	runner := migrate.Runner{Store: store, Migrations: testMigrations(), Owner: "test"}
	err := runner.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "checksum/name mismatch") {
		t.Fatalf("Run error = %v", err)
	}
	if len(store.executed) != 0 {
		t.Fatalf("executed despite mismatch: %#v", store.executed)
	}
}

func TestRunnerRecordsDirtyFailureAndNeverRetries(t *testing.T) {
	store := &fakeStore{execErr: errors.New("DDL failed")}
	runner := migrate.Runner{Store: store, Migrations: testMigrations(), Owner: "test"}
	err := runner.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "will not be retried automatically") {
		t.Fatalf("first Run error = %v", err)
	}
	store.execErr = nil
	err = runner.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "dirty") {
		t.Fatalf("second Run error = %v", err)
	}
	if got, want := len(store.executed), 1; got != want {
		t.Fatalf("executed = %d, want %d", got, want)
	}
}

func testMigrations() []migrate.Migration {
	return []migrate.Migration{
		{Version: 1, Name: "one", Checksum: "checksum-one", SQL: "SELECT 1;"},
		{Version: 2, Name: "two", Checksum: "checksum-two", SQL: "SELECT 2;"},
	}
}

type fakeStore struct {
	locked    bool
	hasSchema bool
	baseline  uint64
	history   []migrate.Record
	events    []migrate.Record
	executed  []string
	execErr   error
}

func (s *fakeStore) AcquireLock(context.Context, string) error {
	if s.locked {
		return errors.New("locked")
	}
	s.locked = true
	return nil
}

func (s *fakeStore) ReleaseLock(context.Context, string) error      { s.locked = false; return nil }
func (s *fakeStore) EnsureHistory(context.Context) error            { return nil }
func (s *fakeStore) HasUserSchema(context.Context) (bool, error)    { return s.hasSchema, nil }
func (s *fakeStore) DetectBaseline(context.Context) (uint64, error) { return s.baseline, nil }

func (s *fakeStore) History(context.Context) ([]migrate.Record, error) {
	return append([]migrate.Record(nil), s.history...), nil
}

func (s *fakeStore) Append(_ context.Context, record migrate.Record, _ string) error {
	s.events = append(s.events, record)
	updated := false
	for i := range s.history {
		if s.history[i].Version == record.Version {
			s.history[i] = record
			updated = true
		}
	}
	if !updated {
		s.history = append(s.history, record)
	}
	return nil
}

func (s *fakeStore) Exec(_ context.Context, statement string) error {
	s.executed = append(s.executed, strings.TrimSpace(statement))
	return s.execErr
}
