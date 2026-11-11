package main

import (
	"reflect"
	"testing"
)

func TestParseSelect(t *testing.T) {
	countApples := selectStatement{count: true, table: "apples"}

	tests := []struct {
		name string
		sql  string
		want selectStatement
	}{
		{"count", "SELECT COUNT(*) FROM apples", countApples},
		{"lowercase keywords", "select count(*) from apples", countApples},
		// The case of names is kept as written.
		{"mixed case", "SeLeCt CoUnT(*) fRoM Apples", selectStatement{count: true, table: "Apples"}},
		{"extra whitespace", "  SELECT\n\tCOUNT ( * )\r\nFROM\tapples  ", countApples},
		{"no optional whitespace", "SELECT COUNT(*)FROM apples", countApples},
		{"trailing semicolon", "SELECT COUNT(*) FROM apples;", countApples},
		{"double-quoted table name", `SELECT COUNT(*) FROM "my table"`, selectStatement{count: true, table: "my table"}},
		{"backtick-quoted table name", "SELECT COUNT(*) FROM `my table`", selectStatement{count: true, table: "my table"}},
		{"bracket-quoted table name", "SELECT COUNT(*) FROM [my table]", selectStatement{count: true, table: "my table"}},
		{"table named like a keyword", `SELECT COUNT(*) FROM "from"`, selectStatement{count: true, table: "from"}},
		{"comments", "SELECT /* how many */ COUNT(*) FROM apples -- all of them", countApples},

		{"column", "SELECT name FROM apples", selectStatement{column: "name", table: "apples"}},
		{"column with lowercase keywords", "select name from apples", selectStatement{column: "name", table: "apples"}},
		{"column in mixed case", "SELECT Name FROM Apples;", selectStatement{column: "Name", table: "Apples"}},
		{"quoted column", `SELECT "size range" FROM companies`, selectStatement{column: "size range", table: "companies"}},
		{"column named like a keyword", `SELECT "select" FROM "from"`, selectStatement{column: "select", table: "from"}},
		// COUNT is only a function when it's called.
		{"column named count", "SELECT count FROM apples", selectStatement{column: "count", table: "apples"}},
		// Most of SQLite's keywords can be used as names.
		{"column named key", "SELECT key FROM apples", selectStatement{column: "key", table: "apples"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSelect(tt.sql)
			if err != nil {
				t.Fatalf("parseSelect(%q) returned an error: %v", tt.sql, err)
			}
			if !reflect.DeepEqual(*got, tt.want) {
				t.Errorf("parseSelect(%q) = %+v, want %+v", tt.sql, *got, tt.want)
			}
		})
	}
}

func TestParseSelectRejectsInvalidStatements(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		wantErr string
	}{
		{"empty statement", "", "incomplete input"},
		{"nothing but SELECT", "SELECT", "incomplete input"},
		{"missing FROM clause", "SELECT COUNT(*)", "incomplete input"},
		{"missing table name", "SELECT COUNT(*) FROM", "incomplete input"},
		{"missing table name after a column", "SELECT name FROM", "incomplete input"},
		{"missing FROM keyword", "SELECT COUNT(*) apples", `near "apples": syntax error`},
		{"missing closing parenthesis", "SELECT COUNT(* FROM apples", `near "FROM": syntax error`},
		{"missing star", "SELECT COUNT() FROM apples", `near ")": syntax error`},
		{"missing result column", "SELECT FROM apples", `near "FROM": syntax error`},
		{"keyword as column name", "SELECT where FROM apples", `near "where": syntax error`},
		{"keyword as table name", "SELECT name FROM select", `near "select": syntax error`},
		{"two table names", "SELECT COUNT(*) FROM apples oranges", `near "oranges": syntax error`},
		{"two statements", "SELECT COUNT(*) FROM apples; SELECT COUNT(*) FROM oranges", `near "SELECT": syntax error`},
		{"two semicolons", "SELECT COUNT(*) FROM apples;;", `near ";": syntax error`},
		{"string as table name", "SELECT COUNT(*) FROM 'apples'", `near "apples": syntax error`},
		{"number as table name", "SELECT COUNT(*) FROM 42", `near "42": syntax error`},
		{"number as column", "SELECT 42 FROM apples", `near "42": syntax error`},
		{"quoted keyword", `"SELECT" COUNT(*) FROM apples`, `near "SELECT": syntax error`},
		{"other statement", "DELETE FROM apples", `near "DELETE": syntax error`},
		{"other function", "SELECT max(id) FROM apples", "no such function: max"},
		{"unterminated string", "SELECT COUNT(*) FROM 'apples", "unterminated string literal: 'apples"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			statement, err := parseSelect(tt.sql)
			if err == nil {
				t.Fatalf("parseSelect(%q) = %+v, want an error", tt.sql, *statement)
			}
			if err.Error() != tt.wantErr {
				t.Errorf("parseSelect(%q) returned error %q, want %q", tt.sql, err, tt.wantErr)
			}
		})
	}
}
