package kanban

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// migrationTables is the ordered list of tables we copy. Parent rows
// before child rows so the order is friendly to any future FK
// constraint, though SQLite doesn't enforce them by default.
var migrationTables = []string{
	"tasks",
	"task_links",
	"task_comments",
	"task_events",
	"task_runs",
	"kanban_notify_subs",
}

// Migrate creates destPath (overwriting any existing DB at that path),
// applies the canonical schema, then copies every row from srcPath
// table-by-table. Only columns that exist in both source and
// canonical schemas are copied; canonical-only columns get their
// default value on the destination.
//
// Full refresh, not incremental sync — calling it twice produces a
// destination that matches the source.
func Migrate(srcPath, destPath string) error {
	src, err := sql.Open("sqlite", "file:"+srcPath+"?mode=ro")
	if err != nil {
		return fmt.Errorf("open src: %w", err)
	}
	defer src.Close()

	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("mkdir dest: %w", err)
	}
	if err := os.Remove(destPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove dest: %w", err)
	}

	dest, err := sql.Open("sqlite", destPath)
	if err != nil {
		return fmt.Errorf("open dest: %w", err)
	}
	defer dest.Close()

	if err := ApplySchema(dest); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}

	for _, table := range migrationTables {
		if err := copyTable(src, dest, table); err != nil {
			return fmt.Errorf("copy %s: %w", table, err)
		}
	}
	return nil
}

// copyTable reads every row from src.<table> and inserts into
// dest.<table>, mapping by column name. Columns present in source
// but not in dest are skipped; columns in dest but not in source
// get the schema default.
func copyTable(src, dest *sql.DB, table string) error {
	srcCols, err := tableColumns(src, table)
	if err != nil {
		return fmt.Errorf("src columns: %w", err)
	}
	if len(srcCols) == 0 {
		return nil
	}
	destCols, err := tableColumns(dest, table)
	if err != nil {
		return fmt.Errorf("dest columns: %w", err)
	}
	destSet := map[string]bool{}
	for _, c := range destCols {
		destSet[c] = true
	}

	var cols []string
	for _, c := range srcCols {
		if destSet[c] {
			cols = append(cols, c)
		}
	}
	if len(cols) == 0 {
		return nil
	}

	colList := strings.Join(cols, ", ")
	rows, err := src.Query("SELECT " + colList + " FROM " + table)
	if err != nil {
		return fmt.Errorf("select: %w", err)
	}
	defer rows.Close()

	placeholders := strings.TrimSuffix(strings.Repeat("?, ", len(cols)), ", ")
	insertSQL := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", table, colList, placeholders)

	tx, err := dest.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	stmt, err := tx.Prepare(insertSQL)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("scan: %w", err)
		}
		if _, err := stmt.Exec(vals...); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("insert: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("rows: %w", err)
	}
	return tx.Commit()
}

// RowCounts returns a map of table -> row count for every canonical
// table. Missing tables map to 0.
func RowCounts(db *sql.DB) (map[string]int, error) {
	out := map[string]int{}
	for _, table := range migrationTables {
		cols, err := tableColumns(db, table)
		if err != nil {
			return nil, err
		}
		if len(cols) == 0 {
			out[table] = 0
			continue
		}
		var n int
		if err := db.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil {
			return nil, fmt.Errorf("count %s: %w", table, err)
		}
		out[table] = n
	}
	return out, nil
}

// VerifyCounts compares row counts table-by-table between two DBs and
// returns an error listing every mismatch. nil means the two DBs
// agree on every canonical table.
func VerifyCounts(src, dest *sql.DB) error {
	srcCounts, err := RowCounts(src)
	if err != nil {
		return fmt.Errorf("src: %w", err)
	}
	destCounts, err := RowCounts(dest)
	if err != nil {
		return fmt.Errorf("dest: %w", err)
	}
	var mismatches []string
	for _, table := range migrationTables {
		if srcCounts[table] != destCounts[table] {
			mismatches = append(mismatches,
				fmt.Sprintf("%s: src=%d dest=%d", table, srcCounts[table], destCounts[table]))
		}
	}
	if len(mismatches) > 0 {
		return fmt.Errorf("row-count mismatch: %s", strings.Join(mismatches, "; "))
	}
	return nil
}

// tableColumns returns the ordered column names of a table, or an
// empty slice if the table doesn't exist.
func tableColumns(db *sql.DB, table string) ([]string, error) {
	rows, err := db.Query("SELECT name FROM pragma_table_info(?)", table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
