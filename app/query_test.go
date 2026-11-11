package main

import (
	"bytes"
	"fmt"
	"math"
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

func TestSelectColumn(t *testing.T) {
	tests := []struct {
		sql  string
		want string
	}{
		{"SELECT name FROM apples", "Granny Smith\nFuji\nHoneycrisp\nGolden Delicious\n"},
		{"select color from apples", "Light Green\nRed\nBlush Red\nYellow\n"},
		{"SELECT name FROM oranges;", "Mandarin\nTangelo\nTangerine\nClementine\nValencia Orange\nNavel Orange\n"},
		// Like SQLite, ignore the case of the names of tables and columns.
		{"SELECT NAME FROM Apples", "Granny Smith\nFuji\nHoneycrisp\nGolden Delicious\n"},
		{`SELECT "color" FROM "apples"`, "Light Green\nRed\nBlush Red\nYellow\n"},
		// The id column is an alias for the rowid: its values aren't stored
		// in the records of the rows.
		{"SELECT id FROM apples", "1\n2\n3\n4\n"},
		{"SELECT seq FROM sqlite_sequence", "4\n6\n"},
	}

	for _, tt := range tests {
		t.Run(tt.sql, func(t *testing.T) {
			if got := runCommand(t, sampleDB, tt.sql); got != tt.want {
				t.Errorf("output = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestSelectColumnMatchesSQLite compares the output of single-column queries
// with the output of the sqlite3 command-line shell.
func TestSelectColumnMatchesSQLite(t *testing.T) {
	path := createDatabase(t, `
		CREATE TABLE products (
			id INTEGER PRIMARY KEY,
			name TEXT,
			price REAL,
			stock INTEGER,
			data BLOB,
			"release date" TEXT,
			anything
		);
		INSERT INTO products VALUES (1, 'apple', 1.5, 10, x'68656c6c6f', '2024-01-02', 'text');
		INSERT INTO products VALUES (2, 'banana', 2, 0, NULL, NULL, 12);
		INSERT INTO products VALUES (5, NULL, NULL, NULL, x'', '', 1.25);
		INSERT INTO products VALUES (-3, '', -0.0, -1, x'41', 'x', NULL);
		INSERT INTO products VALUES (9000000000, 'naïve ☃ | pipe', 1e100, 9223372036854775807, x'e29883', 'y', -9223372036854775808);
		INSERT INTO products VALUES (300, 'it''s', 0.1, 256, NULL, 'z', 1e-7);
		INSERT INTO products VALUES (70000, ' padded ', 123456789012345678.0, 65536, NULL, NULL, 100000000000000.0);
		INSERT INTO products VALUES (16777216, 'tab	bed', 3.14159265358979323846, 2147483648, NULL, NULL, 0.5);
		INSERT INTO products VALUES (1099511627776, 'UPPER', 1e15, 140737488355328, NULL, NULL, 1e14);
		ALTER TABLE products ADD COLUMN added TEXT;
		INSERT INTO products (name, added) VALUES ('late', 'new column');
	`)

	for _, column := range []string{"id", "name", "price", "stock", "data", `"release date"`, "anything", "added"} {
		sql := fmt.Sprintf("SELECT %s FROM products", column)
		t.Run(sql, func(t *testing.T) {
			got := runCommand(t, path, sql)
			if want := sqlite3Output(t, path, sql); got != want {
				t.Errorf("output = %q, sqlite3 prints %q", got, want)
			}
		})
	}
}

func TestSelectColumnOfEmptyTable(t *testing.T) {
	path := createDatabase(t, "CREATE TABLE fruits (id integer primary key, name text);")

	if got := runCommand(t, path, "SELECT name FROM fruits"); got != "" {
		t.Errorf("output = %q, want no output", got)
	}
}

func TestSelectColumnRejectsUnknownColumns(t *testing.T) {
	for _, column := range []string{"flavor", "names", "rowid2"} {
		err := runSQLError(t, sampleDB, fmt.Sprintf("SELECT %s FROM apples", column))

		if want := "no such column: " + column; err.Error() != want {
			t.Errorf("selecting %s returned error %q, want %q", column, err, want)
		}
	}
}

func TestSelectColumnRejectsUnknownTables(t *testing.T) {
	err := runSQLError(t, sampleDB, "SELECT name FROM pears")

	if want := "no such table: pears"; err.Error() != want {
		t.Errorf("error = %q, want %q", err, want)
	}
}

func TestFormatValue(t *testing.T) {
	tests := []struct {
		value any
		want  string
	}{
		{nil, ""},
		{int64(0), "0"},
		{int64(42), "42"},
		{int64(-7), "-7"},
		{int64(math.MaxInt64), "9223372036854775807"},
		{int64(math.MinInt64), "-9223372036854775808"},
		{1.5, "1.5"},
		{2.0, "2.0"},
		{"", ""},
		{"Granny Smith", "Granny Smith"},
		{[]byte("hello"), "hello"},
		{[]byte{}, ""},
	}

	for _, tt := range tests {
		if got := formatValue(tt.value); got != tt.want {
			t.Errorf("formatValue(%#v) = %q, want %q", tt.value, got, tt.want)
		}
	}
}

// TestFormatFloat checks formatFloat against the text that sqlite3 displays
// for the same numbers.
func TestFormatFloat(t *testing.T) {
	tests := []struct {
		value float64
		want  string
	}{
		{1, "1.0"},
		{0.1, "0.1"},
		{-2.5, "-2.5"},
		{0, "0.0"},
		{math.Copysign(0, -1), "0.0"},
		{100, "100.0"},
		{0.1 + 0.2, "0.3"},
		{1.0 / 3, "0.333333333333333"},
		{2.0 / 3, "0.666666666666667"},
		{math.Pi, "3.14159265358979"},
		{123456789.123456789, "123456789.123457"},
		{0.0001, "0.0001"},
		// Exponents are used below 1e-4 and from 1e15 up.
		{0.00001, "1.0e-05"},
		{1.5e-7, "1.5e-07"},
		{1e14, "100000000000000.0"},
		{99999999999999.99, "100000000000000.0"},
		{1e15, "1.0e+15"},
		{999999999999999.9, "1.0e+15"},
		{123456789012345678, "1.23456789012346e+17"},
		{1e100, "1.0e+100"},
		{-1e100, "-1.0e+100"},
		{math.MaxFloat64, "1.79769313486232e+308"},
		{math.SmallestNonzeroFloat64, "4.94065645841247e-324"},
		{math.Inf(1), "Inf"},
		{math.Inf(-1), "-Inf"},
	}

	for _, tt := range tests {
		if got := formatFloat(tt.value); got != tt.want {
			t.Errorf("formatFloat(%v) = %q, want %q", tt.value, got, tt.want)
		}
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
