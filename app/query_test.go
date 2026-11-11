package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// runSQLError runs a SQL statement against the database file at path, and
// returns the error that it fails with. The statement must fail without
// writing anything to stdout.
func runSQLError(t *testing.T, path, sql string) error {
	t.Helper()

	var stdout bytes.Buffer
	err := run(path, sql, &stdout)
	if err == nil {
		t.Fatalf("run(%q) wrote %q and returned no error, want an error", sql, stdout.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("run(%q) wrote %q to stdout before failing, want nothing", sql, stdout.String())
	}
	return err
}

func TestCountRows(t *testing.T) {
	tests := []struct {
		sql  string
		want string
	}{
		{"SELECT COUNT(*) FROM apples", "4\n"},
		{"SELECT COUNT(*) FROM oranges", "6\n"},
		{"select count(*) from apples", "4\n"},
		{"SELECT COUNT(*) FROM oranges;", "6\n"},
		// Like SQLite, ignore the case of table names.
		{"SELECT COUNT(*) FROM APPLES", "4\n"},
		{`SELECT COUNT(*) FROM "Oranges"`, "6\n"},
		// Internal tables can be queried like any other table.
		{"SELECT COUNT(*) FROM sqlite_sequence", "2\n"},
	}

	for _, tt := range tests {
		t.Run(tt.sql, func(t *testing.T) {
			if got := runCommand(t, sampleDB, tt.sql); got != tt.want {
				t.Errorf("output = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCountRowsOfGeneratedTables(t *testing.T) {
	for _, rowCount := range []int{0, 1, 2, 17, 100} {
		t.Run(fmt.Sprintf("%d rows", rowCount), func(t *testing.T) {
			var sql strings.Builder
			sql.WriteString("CREATE TABLE fruits (id integer primary key, name text);\n")
			sql.WriteString("CREATE TABLE other (id integer primary key, name text);\n")
			sql.WriteString("INSERT INTO other (name) VALUES ('a'), ('b'), ('c');\n")
			for i := 0; i < rowCount; i++ {
				fmt.Fprintf(&sql, "INSERT INTO fruits (name) VALUES ('fruit %d');\n", i)
			}
			path := createDatabase(t, sql.String())

			got := runCommand(t, path, "SELECT COUNT(*) FROM fruits")

			if want := fmt.Sprintf("%d\n", rowCount); got != want {
				t.Errorf("output = %q, want %q", got, want)
			}
		})
	}
}

func TestCountRowsIgnoresDeletedRows(t *testing.T) {
	path := createDatabase(t, `
		CREATE TABLE fruits (id integer primary key, name text);
		INSERT INTO fruits (name) VALUES ('apple'), ('banana'), ('cherry'), ('date'), ('elderberry');
		DELETE FROM fruits WHERE name IN ('banana', 'date');
	`)

	if got, want := runCommand(t, path, "SELECT COUNT(*) FROM fruits"), "3\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestCountRowsRejectsUnknownTables(t *testing.T) {
	path := createDatabase(t, `
		CREATE TABLE fruits (id integer primary key, name text);
		CREATE INDEX idx_fruits_name ON fruits (name);
		CREATE VIEW fruit_names AS SELECT name FROM fruits;
	`)

	// Only tables can be queried: indexes and views can't.
	for _, name := range []string{"vegetables", "idx_fruits_name", "fruit_names"} {
		err := runSQLError(t, path, "SELECT COUNT(*) FROM "+name)

		if want := "no such table: " + name; err.Error() != want {
			t.Errorf("querying %s returned error %q, want %q", name, err, want)
		}
	}
}

func TestCountRowsRejectsTablesThatSpanMultiplePages(t *testing.T) {
	// 1,000 rows of about 60 bytes don't fit on a single 4096-byte page.
	path := createDatabase(t, `
		CREATE TABLE fruits (id integer primary key, name text);
		WITH RECURSIVE numbers (n) AS (SELECT 1 UNION ALL SELECT n + 1 FROM numbers WHERE n < 1000)
		INSERT INTO fruits (name) SELECT printf('fruit number %050d', n) FROM numbers;
	`)

	err := runSQLError(t, path, "SELECT COUNT(*) FROM fruits")

	if !strings.Contains(err.Error(), "multiple pages") {
		t.Errorf("error = %q, want it to mention that the table spans multiple pages", err)
	}
}

func TestSQLSyntaxErrors(t *testing.T) {
	for _, sql := range []string{
		"",
		"SELECT",
		"SELECT COUNT(*) FROM",
		"SELECT COUNT(*) apples",
		"DROP TABLE apples",
		"SELECT COUNT(*) FROM 'apples",
	} {
		t.Run(sql, func(t *testing.T) {
			runSQLError(t, sampleDB, sql)
		})
	}
}
