package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func TestDBInfo(t *testing.T) {
	got := runCommand(t, sampleDB, ".dbinfo")

	want := "database page size: 4096\nnumber of tables: 3\n"
	if got != want {
		t.Errorf(".dbinfo output = %q, want %q", got, want)
	}
}

func TestDBInfoCountsTables(t *testing.T) {
	for _, tableCount := range []int{1, 2, 3, 5, 8} {
		t.Run(fmt.Sprintf("%d tables", tableCount), func(t *testing.T) {
			var sql strings.Builder
			for i := 0; i < tableCount; i++ {
				fmt.Fprintf(&sql, "CREATE TABLE table_%d (id integer primary key, name text);\n", i)
			}
			path := createDatabase(t, sql.String())

			got := runCommand(t, path, ".dbinfo")

			want := fmt.Sprintf("database page size: 4096\nnumber of tables: %d\n", tableCount)
			if got != want {
				t.Errorf(".dbinfo output = %q, want %q", got, want)
			}
		})
	}
}

func TestDBInfoCountsOnlyTables(t *testing.T) {
	// Like sqlite3, count the internal sqlite_sequence table that
	// AUTOINCREMENT creates, but not the indexes, views and triggers.
	path := createDatabase(t, `
		CREATE TABLE apples (id integer primary key autoincrement, name text unique);
		CREATE TABLE oranges (id integer primary key, name text);
		CREATE INDEX idx_oranges_name ON oranges (name);
		CREATE VIEW orange_names AS SELECT name FROM oranges;
		CREATE TRIGGER oranges_insert AFTER INSERT ON oranges BEGIN SELECT 1; END;
	`)

	got := runCommand(t, path, ".dbinfo")

	want := "database page size: 4096\nnumber of tables: 3\n"
	if got != want {
		t.Errorf(".dbinfo output = %q, want %q", got, want)
	}
}

func TestDBInfoSupportsEveryPageSize(t *testing.T) {
	for _, pageSize := range []int{512, 1024, 2048, 4096, 8192, 16384, 32768, 65536} {
		t.Run(fmt.Sprint(pageSize), func(t *testing.T) {
			path := createDatabase(t, fmt.Sprintf(`
				PRAGMA page_size = %d;
				CREATE TABLE apples (id integer primary key, name text);
				CREATE TABLE oranges (id integer primary key, name text);
			`, pageSize))

			got := runCommand(t, path, ".dbinfo")

			want := fmt.Sprintf("database page size: %d\nnumber of tables: 2\n", pageSize)
			if got != want {
				t.Errorf(".dbinfo output = %q, want %q", got, want)
			}
		})
	}
}

func TestTables(t *testing.T) {
	// The sample database also contains the internal sqlite_sequence table,
	// which isn't listed.
	got := runCommand(t, sampleDB, ".tables")

	if want := "apples oranges\n"; got != want {
		t.Errorf(".tables output = %q, want %q", got, want)
	}
}

func TestTablesSortsNames(t *testing.T) {
	path := createDatabase(t, `
		CREATE TABLE pears (id integer primary key, name text);
		CREATE TABLE apples (id integer primary key, name text);
		CREATE TABLE Zucchinis (id integer primary key, name text);
		CREATE TABLE oranges (id integer primary key, name text);
		CREATE TABLE bananas (id integer primary key, name text);
	`)

	got := runCommand(t, path, ".tables")

	// Like sqlite3, compare the names byte by byte: uppercase letters sort
	// before lowercase ones.
	if want := "Zucchinis apples bananas oranges pears\n"; got != want {
		t.Errorf(".tables output = %q, want %q", got, want)
	}
}

func TestTablesListsOnlyTablesAndViews(t *testing.T) {
	path := createDatabase(t, `
		CREATE TABLE oranges (id integer primary key autoincrement, name text unique);
		CREATE INDEX idx_oranges_name ON oranges (name);
		CREATE VIEW apple_names AS SELECT name FROM oranges;
		CREATE TRIGGER oranges_insert AFTER INSERT ON oranges BEGIN SELECT 1; END;
		CREATE TABLE melons (id integer primary key, name text);
		ANALYZE;
	`)

	got := runCommand(t, path, ".tables")

	if want := "apple_names melons oranges\n"; got != want {
		t.Errorf(".tables output = %q, want %q", got, want)
	}
}

func TestTablesOfSingleTableDatabase(t *testing.T) {
	path := createDatabase(t, "CREATE TABLE grapes (id integer primary key, name text);")

	got := runCommand(t, path, ".tables")

	if want := "grapes\n"; got != want {
		t.Errorf(".tables output = %q, want %q", got, want)
	}
}

func TestTablesOfDatabaseWithoutTables(t *testing.T) {
	path := createDatabase(t, `
		CREATE TABLE grapes (id integer primary key, name text);
		DROP TABLE grapes;
	`)

	if got := runCommand(t, path, ".tables"); got != "" {
		t.Errorf(".tables output = %q, want no output", got)
	}
}

func TestTablesWithManyTables(t *testing.T) {
	// 30 tables still fit on the single sqlite_schema page.
	var sql strings.Builder
	var names []string
	for i := 0; i < 30; i++ {
		name := fmt.Sprintf("table_%02d", i)
		names = append(names, name)
		fmt.Fprintf(&sql, "CREATE TABLE %s (id integer primary key, name text);\n", name)
	}
	path := createDatabase(t, sql.String())

	got := runCommand(t, path, ".tables")

	if want := strings.Join(names, " ") + "\n"; got != want {
		t.Errorf(".tables output = %q, want %q", got, want)
	}
}

// TestCommandsWithSchemaThatSpansMultiplePages runs the commands on a
// database with so many tables that the sqlite_schema table doesn't fit on
// page 1.
func TestCommandsWithSchemaThatSpansMultiplePages(t *testing.T) {
	const tableCount = 300

	var sql strings.Builder
	var names []string
	for i := 0; i < tableCount; i++ {
		name := fmt.Sprintf("table_%03d", i)
		names = append(names, name)
		fmt.Fprintf(&sql, "CREATE TABLE %s (id integer primary key, name text);\n", name)
	}
	sql.WriteString("INSERT INTO table_299 (name) VALUES ('first'), ('second');\n")
	path := createDatabase(t, sql.String())

	p, err := openDatabase(t, path).readPage(schemaPageNumber)
	if err != nil {
		t.Fatalf("readPage returned an error: %v", err)
	}
	if p.pageType != pageTypeInteriorTable {
		t.Fatalf("the schema fits on a single page: page 1 has type 0x%02x", p.pageType)
	}

	want := fmt.Sprintf("database page size: 4096\nnumber of tables: %d\n", tableCount)
	if got := runCommand(t, path, ".dbinfo"); got != want {
		t.Errorf(".dbinfo output = %q, want %q", got, want)
	}
	if got, want := runCommand(t, path, ".tables"), strings.Join(names, " ")+"\n"; got != want {
		t.Errorf(".tables output = %q, want %q", got, want)
	}
	if got, want := runCommand(t, path, "SELECT COUNT(*) FROM table_150"), "0\n"; got != want {
		t.Errorf("count of table_150 = %q, want %q", got, want)
	}
	if got, want := runCommand(t, path, "SELECT id, name FROM table_299"), "1|first\n2|second\n"; got != want {
		t.Errorf("rows of table_299 = %q, want %q", got, want)
	}
}

func TestRunRejectsUnknownCommand(t *testing.T) {
	var stdout bytes.Buffer
	err := run(sampleDB, ".nope", &stdout)

	if err == nil {
		t.Fatal("run with an unknown command returned no error")
	}
	if !strings.Contains(err.Error(), ".nope") {
		t.Errorf("error %q does not mention the unknown command", err)
	}
	if stdout.Len() != 0 {
		t.Errorf("unknown command wrote %q to stdout, want nothing", stdout.String())
	}
}

func TestRunReportsMissingDatabaseFile(t *testing.T) {
	var stdout bytes.Buffer
	if err := run("does-not-exist.db", ".dbinfo", &stdout); err == nil {
		t.Fatal("run with a missing database file returned no error")
	}
}

func TestRunRejectsDatabasesThatAreNotUTF8(t *testing.T) {
	// Text is only decoded correctly if it is stored as UTF-8.
	path := createDatabase(t, `
		PRAGMA encoding = 'UTF-16le';
		CREATE TABLE fruits (id integer primary key, name text);
		INSERT INTO fruits (name) VALUES ('apple');
	`)

	for _, command := range []string{".dbinfo", ".tables", "SELECT name FROM fruits"} {
		var stdout bytes.Buffer
		err := run(path, command, &stdout)
		if err == nil {
			t.Errorf("%s printed %q and returned no error", command, stdout.String())
			continue
		}
		if !strings.Contains(err.Error(), "text encoding") {
			t.Errorf("%s returned error %q, want it to mention the text encoding", command, err)
		}
	}
}

func TestRunRejectsFilesThatAreNotDatabases(t *testing.T) {
	var stdout bytes.Buffer
	if err := run("main.go", ".tables", &stdout); err == nil {
		t.Fatal("run with a file that isn't a database returned no error")
	}
}
