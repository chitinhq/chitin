package gov

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestMigrateBudget_Idempotent(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", dir+"/test.db")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	// First migration
	if err := migrateBudget(db); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	// Second migration (idempotent)
	if err := migrateBudget(db); err != nil {
		t.Fatalf("second migrate: %v", err)
	}

	// Verify schema_version row exists
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_version WHERE component = 'budget'").Scan(&count); err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 schema_version row, got %d", count)
	}
}

func TestMigrateBudget_CreateTables(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", dir+"/test.db")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if err := migrateBudget(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Verify tables exist by querying them
	tables := []string{"schema_version", "envelopes", "envelope_grants"}
	for _, table := range tables {
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
			t.Errorf("table %s should exist: %v", table, err)
		}
	}
}