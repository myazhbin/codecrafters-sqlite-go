package main

import (
	"reflect"
	"strings"
	"testing"
)

// The CREATE TABLE statements of the databases that download_sample_databases.sh
// downloads.
const (
	superheroesSchemaSQL = `CREATE TABLE "superheroes" (id integer primary key autoincrement, name text not null, eye_color text, hair_color text, appearance_count integer, first_appearance text, first_appearance_year text)`
	companiesSchemaSQL   = "CREATE TABLE companies\n(\n\tid integer primary key autoincrement\n, name text, domain text, year_founded text, industry text, \"size range\" text, locality text, country text, current_employees text, total_employees text)"
)

// columns builds column definitions from pairs of names and declared types.
func columns(namesAndTypes ...string) []columnDefinition {
	var definitions []columnDefinition
	for i := 0; i < len(namesAndTypes); i += 2 {
		definitions = append(definitions, columnDefinition{name: namesAndTypes[i], declaredType: namesAndTypes[i+1]})
	}
	return definitions
}

func TestParseCreateTable(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want tableDefinition
	}{
		{
			"apples table of the sample database",
			applesSchemaSQL,
			tableDefinition{
				name:        "apples",
				columns:     columns("id", "INTEGER", "name", "TEXT", "color", "TEXT"),
				rowIDColumn: 0,
			},
		},
		{
			"columns without types",
			"CREATE TABLE sqlite_sequence(name,seq)",
			tableDefinition{name: "sqlite_sequence", columns: columns("name", "", "seq", ""), rowIDColumn: -1},
		},
		{
			"superheroes table",
			superheroesSchemaSQL,
			tableDefinition{
				name: "superheroes",
				columns: columns("id", "INTEGER", "name", "TEXT", "eye_color", "TEXT", "hair_color", "TEXT",
					"appearance_count", "INTEGER", "first_appearance", "TEXT", "first_appearance_year", "TEXT"),
				rowIDColumn: 0,
			},
		},
		{
			"companies table",
			companiesSchemaSQL,
			tableDefinition{
				name: "companies",
				columns: columns("id", "INTEGER", "name", "TEXT", "domain", "TEXT", "year_founded", "TEXT",
					"industry", "TEXT", "size range", "TEXT", "locality", "TEXT", "country", "TEXT",
					"current_employees", "TEXT", "total_employees", "TEXT"),
				rowIDColumn: 0,
			},
		},
		{
			"keywords in any case",
			"create table Fruits (Id Integer Primary Key, Name Text)",
			tableDefinition{name: "Fruits", columns: columns("Id", "INTEGER", "Name", "TEXT"), rowIDColumn: 0},
		},
		{
			"quoted names",
			"CREATE TABLE [my table] (\"first name\" TEXT, `last name` TEXT, [the age] INT, 'nick name' TEXT)",
			tableDefinition{
				name:        "my table",
				columns:     columns("first name", "TEXT", "last name", "TEXT", "the age", "INT", "nick name", "TEXT"),
				rowIDColumn: -1,
			},
		},
		{
			"quoted keywords as names",
			`CREATE TABLE "table" ("primary" TEXT, "check" INT, "constraint" TEXT)`,
			tableDefinition{
				name:        "table",
				columns:     columns("primary", "TEXT", "check", "INT", "constraint", "TEXT"),
				rowIDColumn: -1,
			},
		},
		{
			"types of several words and with arguments",
			"CREATE TABLE t (a VARCHAR(255), b DECIMAL(10, 2), c UNSIGNED BIG INT, d DOUBLE PRECISION, e)",
			tableDefinition{
				name:        "t",
				columns:     columns("a", "VARCHAR", "b", "DECIMAL", "c", "UNSIGNED BIG INT", "d", "DOUBLE PRECISION", "e", ""),
				rowIDColumn: -1,
			},
		},
		{
			"column constraints",
			`CREATE TABLE t (
				a TEXT NOT NULL DEFAULT 'x, y' COLLATE NOCASE,
				b INT CHECK (b IN (1, 2, 3)) REFERENCES other (id) ON DELETE CASCADE,
				c DEFAULT (1 + 2) UNIQUE,
				d NULL,
				e TEXT CONSTRAINT named NOT NULL
			)`,
			tableDefinition{
				name:        "t",
				columns:     columns("a", "TEXT", "b", "INT", "c", "", "d", "", "e", "TEXT"),
				rowIDColumn: -1,
			},
		},
		{
			"table constraints",
			`CREATE TABLE t (
				a TEXT,
				b INT,
				UNIQUE (a, b),
				CHECK (a <> b),
				FOREIGN KEY (b) REFERENCES other (id),
				CONSTRAINT pk PRIMARY KEY (a, b)
			)`,
			tableDefinition{name: "t", columns: columns("a", "TEXT", "b", "INT"), rowIDColumn: -1},
		},
		{
			"comments",
			"CREATE TABLE t (\n  id INTEGER PRIMARY KEY, -- the id, of course\n  /* the name */ name TEXT\n)",
			tableDefinition{name: "t", columns: columns("id", "INTEGER", "name", "TEXT"), rowIDColumn: 0},
		},
		{
			"temporary table",
			"CREATE TEMP TABLE t (a)",
			tableDefinition{name: "t", columns: columns("a", ""), rowIDColumn: -1},
		},
		{
			"qualified name and IF NOT EXISTS",
			"CREATE TEMPORARY TABLE IF NOT EXISTS main.t (a)",
			tableDefinition{name: "t", columns: columns("a", ""), rowIDColumn: -1},
		},
		{
			"table options",
			"CREATE TABLE t (a INTEGER PRIMARY KEY, b TEXT) STRICT",
			tableDefinition{name: "t", columns: columns("a", "INTEGER", "b", "TEXT"), rowIDColumn: 0},
		},
		{
			"trailing semicolon",
			"CREATE TABLE t (a);",
			tableDefinition{name: "t", columns: columns("a", ""), rowIDColumn: -1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseCreateTable(tt.sql)
			if err != nil {
				t.Fatalf("parseCreateTable(%q) returned an error: %v", tt.sql, err)
			}
			if !reflect.DeepEqual(*got, tt.want) {
				t.Errorf("parseCreateTable(%q)\n got %+v\nwant %+v", tt.sql, *got, tt.want)
			}
		})
	}
}

// TestParseCreateTableRowIDAlias checks which column, if any, is recognized
// as an alias for the rowid: the one that is the whole primary key of the
// table and is declared with the type INTEGER.
func TestParseCreateTableRowIDAlias(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want int
	}{
		{"integer primary key", "CREATE TABLE t (a INTEGER PRIMARY KEY, b TEXT)", 0},
		{"lowercase", "create table t (a integer primary key, b text)", 0},
		{"autoincrement", "CREATE TABLE t (a INTEGER PRIMARY KEY AUTOINCREMENT, b TEXT)", 0},
		{"ascending", "CREATE TABLE t (a INTEGER PRIMARY KEY ASC, b TEXT)", 0},
		{"not null", "CREATE TABLE t (a INTEGER NOT NULL PRIMARY KEY, b TEXT)", 0},
		{"named constraint", "CREATE TABLE t (a INTEGER CONSTRAINT pk PRIMARY KEY, b TEXT)", 0},
		{"second column", "CREATE TABLE t (a TEXT, b INTEGER PRIMARY KEY, c TEXT)", 1},
		{"last column", "CREATE TABLE t (a TEXT, b TEXT, c INTEGER PRIMARY KEY)", 2},
		{"quoted type", `CREATE TABLE t (a "INTEGER" PRIMARY KEY, b TEXT)`, 0},
		{"table constraint", "CREATE TABLE t (a TEXT, b INTEGER, PRIMARY KEY (b))", 1},
		{"named table constraint", "CREATE TABLE t (a INTEGER, b TEXT, CONSTRAINT pk PRIMARY KEY (a))", 0},
		{"ascending table constraint", "CREATE TABLE t (a INTEGER, b TEXT, PRIMARY KEY (a ASC))", 0},
		{"descending table constraint", "CREATE TABLE t (a INTEGER, b TEXT, PRIMARY KEY (a DESC))", 0},
		{"table constraint in another case", "CREATE TABLE t (Id INTEGER, b TEXT, PRIMARY KEY (ID))", 0},
		{"table constraint with a quoted name", `CREATE TABLE t ("my id" INTEGER, b TEXT, PRIMARY KEY ("my id"))`, 0},

		{"no primary key", "CREATE TABLE t (a INTEGER, b TEXT)", -1},
		// A quirk of SQLite.
		{"descending", "CREATE TABLE t (a INTEGER PRIMARY KEY DESC, b TEXT)", -1},
		{"INT primary key", "CREATE TABLE t (a INT PRIMARY KEY, b TEXT)", -1},
		{"BIGINT primary key", "CREATE TABLE t (a BIGINT PRIMARY KEY, b TEXT)", -1},
		{"UNSIGNED INTEGER primary key", "CREATE TABLE t (a UNSIGNED INTEGER PRIMARY KEY, b TEXT)", -1},
		{"TEXT primary key", "CREATE TABLE t (a TEXT PRIMARY KEY, b INTEGER)", -1},
		{"untyped primary key", "CREATE TABLE t (a PRIMARY KEY, b INTEGER)", -1},
		{"composite primary key", "CREATE TABLE t (a INTEGER, b INTEGER, PRIMARY KEY (a, b))", -1},
		{"INT table constraint", "CREATE TABLE t (a INT, b TEXT, PRIMARY KEY (a))", -1},
		{"unique integer", "CREATE TABLE t (a INTEGER UNIQUE, b TEXT)", -1},
		{"primary key in a default value", "CREATE TABLE t (a INTEGER DEFAULT ('PRIMARY KEY'), b TEXT)", -1},
		{"primary key in a check", "CREATE TABLE t (a INTEGER CHECK (a NOT IN (PRIMARY, KEY)), b TEXT)", -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseCreateTable(tt.sql)
			if err != nil {
				t.Fatalf("parseCreateTable(%q) returned an error: %v", tt.sql, err)
			}
			if got.rowIDColumn != tt.want {
				t.Errorf("parseCreateTable(%q).rowIDColumn = %d, want %d", tt.sql, got.rowIDColumn, tt.want)
			}
		})
	}
}

func TestParseCreateTableRejectsInvalidStatements(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		wantErr string
	}{
		{"empty statement", "", "incomplete input"},
		{"other statement", "CREATE INDEX idx ON t (a)", `near "INDEX": syntax error`},
		{"SELECT statement", "SELECT a FROM t", `near "SELECT": syntax error`},
		{"virtual table", "CREATE VIRTUAL TABLE t USING fts5 (a)", `near "VIRTUAL": syntax error`},
		{"missing name", "CREATE TABLE (a)", `near "(": syntax error`},
		{"missing columns", "CREATE TABLE t", "incomplete input"},
		{"no columns", "CREATE TABLE t ()", `near ")": syntax error`},
		{"trailing comma", "CREATE TABLE t (a,)", `near ")": syntax error`},
		{"doubled comma", "CREATE TABLE t (a,, b)", `near ",": syntax error`},
		{"missing closing parenthesis", "CREATE TABLE t (a, b", "incomplete input"},
		{"unbalanced parentheses", "CREATE TABLE t (a VARCHAR(10, b TEXT)", "incomplete input"},
		{"number as column name", "CREATE TABLE t (1 TEXT)", `near "1": syntax error`},
		{"created from a SELECT", "CREATE TABLE t AS SELECT 1", `near "AS": syntax error`},
		{"unterminated string", "CREATE TABLE t (a DEFAULT 'x)", "unterminated string literal: 'x)"},
		{"without rowid", "CREATE TABLE t (a PRIMARY KEY) WITHOUT ROWID", "WITHOUT ROWID tables are not supported"},
		{
			"generated column",
			"CREATE TABLE t (a INT, b INT GENERATED ALWAYS AS (a * 2) VIRTUAL)",
			"tables with generated columns are not supported",
		},
		{
			"generated column in short form",
			"CREATE TABLE t (a INT, b INT AS (a * 2))",
			"tables with generated columns are not supported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			definition, err := parseCreateTable(tt.sql)
			if err == nil {
				t.Fatalf("parseCreateTable(%q) = %+v, want an error", tt.sql, *definition)
			}
			if err.Error() != tt.wantErr {
				t.Errorf("parseCreateTable(%q) returned error %q, want %q", tt.sql, err, tt.wantErr)
			}
		})
	}
}

func TestColumnDefinitionHasRealAffinity(t *testing.T) {
	tests := []struct {
		declaredType string
		want         bool
	}{
		{"REAL", true},
		{"DOUBLE", true},
		{"DOUBLE PRECISION", true},
		{"FLOAT", true},
		{"INTEGER", false},
		{"INT", false},
		{"TEXT", false},
		{"VARCHAR", false},
		{"BLOB", false},
		{"", false},
		{"NUMERIC", false},
		{"DECIMAL", false},
		{"BOOLEAN", false},
		{"DATETIME", false},
		// The rule for integer affinity comes first: "FLOATING POINT" contains
		// "INT".
		{"FLOATING POINT", false},
		// So does the rule for text affinity.
		{"REAL TEXT", false},
	}

	for _, tt := range tests {
		column := columnDefinition{name: "c", declaredType: tt.declaredType}
		if got := column.hasRealAffinity(); got != tt.want {
			t.Errorf("column of type %q: hasRealAffinity() = %t, want %t", tt.declaredType, got, tt.want)
		}
	}
}

// TestParseCreateTableOfGeneratedSchemas checks that the statements that
// SQLite itself stores in sqlite_schema can be parsed.
func TestParseCreateTableOfGeneratedSchemas(t *testing.T) {
	path := createDatabase(t, `
		CREATE TABLE IF NOT EXISTS plain (id integer primary key, name text);
		CREATE TABLE "quoted name" ("a b" text, [c d] int, `+"`e f`"+` real);
		CREATE TABLE with_constraints (
			a INTEGER NOT NULL,
			b VARCHAR(20) DEFAULT 'a,b' COLLATE NOCASE,
			c NUMERIC(10, 2) CHECK (c > 0),
			PRIMARY KEY (a),
			UNIQUE (b, c)
		);
		CREATE TABLE from_select AS SELECT 1 AS one, 'two' AS two, 3.0 AS three;
		CREATE TABLE renamed_before (x, y);
		ALTER TABLE renamed_before RENAME TO renamed_after;
		ALTER TABLE renamed_after ADD COLUMN z TEXT DEFAULT 'zed';
		ALTER TABLE renamed_after RENAME COLUMN x TO "x y";
		ANALYZE;
	`)
	db := openDatabase(t, path)

	tests := []struct {
		table           string
		wantColumns     []string
		wantRowIDColumn int
	}{
		{"plain", []string{"id", "name"}, 0},
		{"quoted name", []string{"a b", "c d", "e f"}, -1},
		{"with_constraints", []string{"a", "b", "c"}, 0},
		{"from_select", []string{"one", "two", "three"}, -1},
		{"renamed_after", []string{"x y", "y", "z"}, -1},
		{"sqlite_stat1", []string{"tbl", "idx", "stat"}, -1},
	}

	for _, tt := range tests {
		t.Run(tt.table, func(t *testing.T) {
			table, err := db.findTable(tt.table)
			if err != nil {
				t.Fatalf("findTable(%q) returned an error: %v", tt.table, err)
			}

			var names []string
			for _, column := range table.columns {
				names = append(names, column.name)
			}
			if !reflect.DeepEqual(names, tt.wantColumns) {
				t.Errorf("columns = %q, want %q", names, tt.wantColumns)
			}
			if table.rowIDColumn != tt.wantRowIDColumn {
				t.Errorf("rowIDColumn = %d, want %d", table.rowIDColumn, tt.wantRowIDColumn)
			}
			if !strings.EqualFold(table.name, tt.table) {
				t.Errorf("name = %q, want %q", table.name, tt.table)
			}
		})
	}
}
