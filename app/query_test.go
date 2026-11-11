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

func TestSelectColumns(t *testing.T) {
	tests := []struct {
		sql  string
		want string
	}{
		{
			"SELECT name, color FROM apples",
			"Granny Smith|Light Green\nFuji|Red\nHoneycrisp|Blush Red\nGolden Delicious|Yellow\n",
		},
		{
			"select color,name from apples",
			"Light Green|Granny Smith\nRed|Fuji\nBlush Red|Honeycrisp\nYellow|Golden Delicious\n",
		},
		{
			"SELECT id, name, color FROM apples;",
			"1|Granny Smith|Light Green\n2|Fuji|Red\n3|Honeycrisp|Blush Red\n4|Golden Delicious|Yellow\n",
		},
		{
			"SELECT name, id, name FROM apples",
			"Granny Smith|1|Granny Smith\nFuji|2|Fuji\nHoneycrisp|3|Honeycrisp\nGolden Delicious|4|Golden Delicious\n",
		},
		{
			`SELECT "Name", DESCRIPTION FROM Oranges`,
			"Mandarin|great for snacking\n" +
				"Tangelo|sweet and tart\n" +
				"Tangerine|great for sweeter juice\n" +
				"Clementine|usually seedless, great for snacking\n" +
				"Valencia Orange|best for juicing\n" +
				"Navel Orange|sweet with slight bitterness\n",
		},
		{"SELECT name, seq FROM sqlite_sequence", "apples|4\noranges|6\n"},
	}

	for _, tt := range tests {
		t.Run(tt.sql, func(t *testing.T) {
			if got := runCommand(t, sampleDB, tt.sql); got != tt.want {
				t.Errorf("output = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestSelectColumnsMatchesSQLite compares the output of multi-column queries
// with the output of the sqlite3 command-line shell.
func TestSelectColumnsMatchesSQLite(t *testing.T) {
	path := createDatabase(t, `
		CREATE TABLE products (
			id INTEGER PRIMARY KEY,
			name TEXT,
			price REAL,
			stock INTEGER,
			data BLOB,
			"release date" TEXT
		);
		INSERT INTO products VALUES (1, 'apple', 1.5, 10, x'68656c6c6f', '2024-01-02');
		INSERT INTO products VALUES (2, 'banana', 2, 0, NULL, NULL);
		INSERT INTO products VALUES (5, NULL, NULL, NULL, NULL, NULL);
		INSERT INTO products VALUES (7, '', 0.25, -1, x'', '');
		INSERT INTO products VALUES (8, 'a|b', 1e100, 9223372036854775807, x'41', 'pipe | pipe');
		ALTER TABLE products ADD COLUMN added TEXT;
		INSERT INTO products (name, added) VALUES ('late', 'new column');
	`)

	for _, sql := range []string{
		"SELECT name, price FROM products",
		"SELECT id, name, price, stock, data, \"release date\", added FROM products",
		"SELECT added, data, stock, price, name, id FROM products",
		"SELECT price, price, id, id FROM products",
		"SELECT data, added FROM products",
	} {
		t.Run(sql, func(t *testing.T) {
			got := runCommand(t, path, sql)
			if want := sqlite3Output(t, path, sql); got != want {
				t.Errorf("output = %q, sqlite3 prints %q", got, want)
			}
		})
	}
}

func TestSelectColumnsRejectsUnknownColumns(t *testing.T) {
	for _, sql := range []string{
		"SELECT flavor, name FROM apples",
		"SELECT name, flavor FROM apples",
		"SELECT name, color, flavor FROM apples",
	} {
		err := runSQLError(t, sampleDB, sql)

		if want := "no such column: flavor"; err.Error() != want {
			t.Errorf("%s: error = %q, want %q", sql, err, want)
		}
	}
}

func TestSelectColumnOfEmptyTable(t *testing.T) {
	path := createDatabase(t, "CREATE TABLE fruits (id integer primary key, name text);")

	for _, sql := range []string{"SELECT name FROM fruits", "SELECT id, name FROM fruits"} {
		if got := runCommand(t, path, sql); got != "" {
			t.Errorf("%s: output = %q, want no output", sql, got)
		}
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

func TestSelectWhere(t *testing.T) {
	tests := []struct {
		sql  string
		want string
	}{
		{"SELECT name, color FROM apples WHERE color = 'Yellow'", "Golden Delicious|Yellow\n"},
		{"select name, color from apples where color = 'Red'", "Fuji|Red\n"},
		{"SELECT name FROM apples WHERE color='Blush Red';", "Honeycrisp\n"},
		// The column of the condition doesn't have to be selected.
		{"SELECT id FROM apples WHERE name = 'Granny Smith'", "1\n"},
		{"SELECT name FROM oranges WHERE description = 'usually seedless, great for snacking'", "Clementine\n"},
		{`SELECT Name FROM Apples WHERE "COLOR" == 'Yellow'`, "Golden Delicious\n"},
		// Text is compared exactly: the case and the spaces matter.
		{"SELECT name FROM apples WHERE color = 'yellow'", ""},
		{"SELECT name FROM apples WHERE color = 'Yellow '", ""},
		{"SELECT name FROM apples WHERE color = 'Purple'", ""},
		{"SELECT name FROM apples WHERE color = ''", ""},

		{"SELECT name FROM apples WHERE color != 'Red'", "Granny Smith\nHoneycrisp\nGolden Delicious\n"},
		{"SELECT name FROM apples WHERE color <> 'Red'", "Granny Smith\nHoneycrisp\nGolden Delicious\n"},
		{"SELECT name FROM apples WHERE name < 'Golden Delicious'", "Fuji\n"},
		{"SELECT name FROM apples WHERE name <= 'Golden Delicious'", "Fuji\nGolden Delicious\n"},
		{"SELECT name FROM apples WHERE name > 'Golden Delicious'", "Granny Smith\nHoneycrisp\n"},
		{"SELECT name FROM apples WHERE name >= 'Golden Delicious'", "Granny Smith\nHoneycrisp\nGolden Delicious\n"},

		// The id column is an alias for the rowid.
		{"SELECT name FROM apples WHERE id = 2", "Fuji\n"},
		{"SELECT name FROM apples WHERE id = '2'", "Fuji\n"},
		{"SELECT name FROM apples WHERE id = 2.0", "Fuji\n"},
		{"SELECT name FROM apples WHERE id > 2", "Honeycrisp\nGolden Delicious\n"},
		{"SELECT name FROM apples WHERE id <= 2", "Granny Smith\nFuji\n"},
		{"SELECT name FROM apples WHERE id != 1", "Fuji\nHoneycrisp\nGolden Delicious\n"},
		{"SELECT name FROM apples WHERE id < 1.5", "Granny Smith\n"},
		{"SELECT name FROM apples WHERE id = -1", ""},
		{"SELECT name FROM apples WHERE id = 'two'", ""},

		// No value equals NULL, or differs from it.
		{"SELECT name FROM apples WHERE color = NULL", ""},
		{"SELECT name FROM apples WHERE color != NULL", ""},

		{"SELECT COUNT(*) FROM apples WHERE color = 'Red'", "1\n"},
		{"SELECT COUNT(*) FROM apples WHERE color != 'Red'", "3\n"},
		{"SELECT COUNT(*) FROM apples WHERE color = 'Purple'", "0\n"},
		{"select count(*) from oranges where id > 2", "4\n"},
	}

	for _, tt := range tests {
		t.Run(tt.sql, func(t *testing.T) {
			if got := runCommand(t, sampleDB, tt.sql); got != tt.want {
				t.Errorf("output = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSelectWhereReturnsEveryMatchingRow(t *testing.T) {
	path := createDatabase(t, `
		CREATE TABLE fruits (id integer primary key, name text, color text);
		INSERT INTO fruits (name, color) VALUES
			('banana', 'Yellow'), ('cherry', 'Red'), ('lemon', 'Yellow'), ('lime', 'Green'),
			('pineapple', 'Yellow'), ('strawberry', 'Red'), ('blank', NULL);
	`)

	got := runCommand(t, path, "SELECT id, name FROM fruits WHERE color = 'Yellow'")

	if want := "1|banana\n3|lemon\n5|pineapple\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

// TestSelectWhereMatchesSQLite compares the rows that conditions select with
// the rows that the sqlite3 command-line shell selects: SQLite's rules for
// comparing values of different types are intricate.
func TestSelectWhereMatchesSQLite(t *testing.T) {
	path := createDatabase(t, `
		CREATE TABLE items (
			id INTEGER PRIMARY KEY,
			name TEXT,
			nocase TEXT COLLATE NOCASE,
			rtrim TEXT COLLATE RTRIM,
			qty INTEGER,
			price REAL,
			num NUMERIC,
			anything
		);
		INSERT INTO items VALUES (1, 'apple', 'apple', 'apple', 10, 1.5, 3, 5);
		INSERT INTO items VALUES (2, 'Apple', 'APPLE', 'apple   ', 5, 2, 3.5, '5');
		INSERT INTO items VALUES (3, 'banana', 'Banana', ' apple', -3, 10.25, '7', 'text');
		INSERT INTO items VALUES (4, '', '', '', 0, 0, 0, '');
		INSERT INTO items VALUES (5, NULL, NULL, NULL, NULL, NULL, NULL, NULL);
		INSERT INTO items VALUES (6, '10', '10', '10', '10', '10', '10', 1.25);
		INSERT INTO items VALUES (7, 'cherry', 'CHERRY', 'cherry ', 'abc', 'abc', 'abc', x'6162');
		INSERT INTO items VALUES (8, '5', 'b', 'b', 9007199254740993, 9007199254740992, 1e3, 5.0);
		INSERT INTO items VALUES (9, '5.0', 'B', 'b  ', 100, -1.5, -2, -5);
		INSERT INTO items VALUES (100, 'zebra', 'Zebra', 'zebra', 2147483648, 1e100, 12, 10);
	`)

	conditions := []string{
		"name = 'apple'", "name = 'Apple'", "name != 'apple'", "name < 'b'", "name >= 'banana'",
		"name = ''", "name > ''", "name = 5", "name = 5.0", "name = 10", "name < 5", "name > 5",
		"name = NULL", "name <> NULL",

		"nocase = 'APPLE'", "nocase = 'apple'", "nocase != 'Apple'", "nocase > 'b'", "nocase >= 'B'",
		"nocase < 'c'", "nocase = 'cherry'", "nocase = 10",

		"rtrim = 'apple'", "rtrim = 'apple     '", "rtrim != 'apple'", "rtrim = 'b'", "rtrim > 'b'",
		"rtrim = ''", "rtrim = '    '", "rtrim = ' apple'",

		"qty = 10", "qty = '10'", "qty = 10.0", "qty = '10.0'", "qty = ' 10 '", "qty = '1e1'",
		"qty > 5", "qty >= 5", "qty < 5.5", "qty <= 0", "qty != 10", "qty = 'abc'", "qty > 'abc'",
		"qty < 'abc'", "qty = -3", "qty = 0", "qty = 0.0", "qty = ''",
		"qty = 9007199254740993", "qty = 9007199254740992", "qty = 9007199254740992.0",
		"qty > 9007199254740992.0", "qty > 2147483647",

		"price = 1.5", "price = '1.5'", "price > 1", "price = 2", "price = '2'", "price = 2.0",
		"price <= 2.0", "price < 0", "price = 10", "price = '10'", "price = 'abc'", "price >= 1e100",
		"price = 9007199254740992", "price = 9007199254740993", "price != 0",

		"num = 3", "num = '3'", "num = 3.0", "num = 3.5", "num = '3.5'", "num = 7", "num = '7'",
		"num = 1000", "num = '1e3'", "num > 5", "num < 0", "num = 'abc'", "num = 10",

		"anything = 5", "anything = '5'", "anything = 5.0", "anything = 'text'", "anything = 1.25",
		"anything > 1", "anything < 'u'", "anything >= ''", "anything = ''", "anything = -5",
		"anything != 5", "anything > 'zzz'", "anything < 0", "anything = 10", "anything = '10'",

		"id = 1", "id = '1'", "id = 1.0", "id = 1.5", "id > 2", "id >= 2", "id < 3.5", "id != 3",
		"id = 'abc'", "id > 'abc'", "id < 'abc'", "id = -5", "id = 100", "id = '100'", "id <= 0",
		"id = NULL",
	}

	for _, condition := range conditions {
		t.Run(condition, func(t *testing.T) {
			for _, sql := range []string{
				"SELECT id, name FROM items WHERE " + condition,
				"SELECT COUNT(*) FROM items WHERE " + condition,
			} {
				got := runCommand(t, path, sql)
				if want := sqlite3Output(t, path, sql); got != want {
					t.Errorf("%s\noutput = %q, sqlite3 prints %q", sql, got, want)
				}
			}
		})
	}
}

func TestSelectWhereRejectsUnknownColumns(t *testing.T) {
	for _, sql := range []string{
		"SELECT name FROM apples WHERE flavor = 'sweet'",
		"SELECT COUNT(*) FROM apples WHERE flavor = 'sweet'",
	} {
		err := runSQLError(t, sampleDB, sql)

		if want := "no such column: flavor"; err.Error() != want {
			t.Errorf("%s: error = %q, want %q", sql, err, want)
		}
	}
}

func TestSelectWhereRejectsUnknownCollations(t *testing.T) {
	// The definition of a table can name a collating sequence that an
	// application registered with SQLite, which the sqlite3 shell doesn't
	// know either. Build the database by hand, since sqlite3 refuses to
	// create such a table.
	data := buildDatabase(512,
		testPage{pageType: pageTypeLeafTable, cells: [][]byte{
			encodeTableLeafCell(1, "table", "words", "words", 2, "CREATE TABLE words (word TEXT COLLATE german)"),
		}},
		testPage{pageType: pageTypeLeafTable, cells: [][]byte{encodeTableLeafCell(1, "Straße")}},
	)
	path := writeDatabaseFile(t, data)

	// The collating sequence only matters for comparisons.
	if got, want := runCommand(t, path, "SELECT word FROM words"), "Straße\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}

	err := runSQLError(t, path, "SELECT word FROM words WHERE word = 'Strasse'")
	if want := "no such collation sequence: GERMAN"; err.Error() != want {
		t.Errorf("error = %q, want %q", err, want)
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
		"SELECT name FROM apples WHERE",
		"SELECT name FROM apples WHERE color = 'Yellow' OR color = 'Red'",
	} {
		t.Run(sql, func(t *testing.T) {
			runSQLError(t, sampleDB, sql)
		})
	}
}
