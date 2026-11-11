package main

import (
	"reflect"
	"testing"
)

func TestParseSelect(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want selectStatement
	}{
		{"count", "SELECT COUNT(*) FROM apples", selectStatement{table: "apples"}},
		{"lowercase keywords", "select count(*) from apples", selectStatement{table: "apples"}},
		// The case of names is kept as written.
		{"mixed case", "SeLeCt CoUnT(*) fRoM Apples", selectStatement{table: "Apples"}},
		{"extra whitespace", "  SELECT\n\tCOUNT ( * )\r\nFROM\tapples  ", selectStatement{table: "apples"}},
		{"no optional whitespace", "SELECT COUNT(*)FROM apples", selectStatement{table: "apples"}},
		{"trailing semicolon", "SELECT COUNT(*) FROM apples;", selectStatement{table: "apples"}},
		{"double-quoted table name", `SELECT COUNT(*) FROM "my table"`, selectStatement{table: "my table"}},
		{"backtick-quoted table name", "SELECT COUNT(*) FROM `my table`", selectStatement{table: "my table"}},
		{"bracket-quoted table name", "SELECT COUNT(*) FROM [my table]", selectStatement{table: "my table"}},
		{"table named like a keyword", `SELECT COUNT(*) FROM "from"`, selectStatement{table: "from"}},
		{"comments", "SELECT /* how many */ COUNT(*) FROM apples -- all of them", selectStatement{table: "apples"}},
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
		{"missing FROM keyword", "SELECT COUNT(*) apples", `near "apples": syntax error`},
		{"missing closing parenthesis", "SELECT COUNT(* FROM apples", `near "FROM": syntax error`},
		{"missing star", "SELECT COUNT() FROM apples", `near ")": syntax error`},
		{"missing result column", "SELECT FROM apples", `near "FROM": syntax error`},
		{"two table names", "SELECT COUNT(*) FROM apples oranges", `near "oranges": syntax error`},
		{"two statements", "SELECT COUNT(*) FROM apples; SELECT COUNT(*) FROM oranges", `near "SELECT": syntax error`},
		{"two semicolons", "SELECT COUNT(*) FROM apples;;", `near ";": syntax error`},
		{"string as table name", "SELECT COUNT(*) FROM 'apples'", `near "apples": syntax error`},
		{"number as table name", "SELECT COUNT(*) FROM 42", `near "42": syntax error`},
		{"quoted keyword", `"SELECT" COUNT(*) FROM apples`, `near "SELECT": syntax error`},
		{"other statement", "DELETE FROM apples", `near "DELETE": syntax error`},
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
