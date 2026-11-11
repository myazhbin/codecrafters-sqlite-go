package main

import (
	"reflect"
	"testing"
)

// selectColumns returns the statement that selects the given columns of a
// table.
func selectColumns(table string, columns ...string) selectStatement {
	return selectStatement{columns: columns, table: table}
}

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

		{"column", "SELECT name FROM apples", selectColumns("apples", "name")},
		{"column with lowercase keywords", "select name from apples", selectColumns("apples", "name")},
		{"column in mixed case", "SELECT Name FROM Apples;", selectColumns("Apples", "Name")},
		{"quoted column", `SELECT "size range" FROM companies`, selectColumns("companies", "size range")},
		{"column named like a keyword", `SELECT "select" FROM "from"`, selectColumns("from", "select")},
		// COUNT is only a function when it's called.
		{"column named count", "SELECT count FROM apples", selectColumns("apples", "count")},
		// Most of SQLite's keywords can be used as names.
		{"column named key", "SELECT key FROM apples", selectColumns("apples", "key")},

		{"two columns", "SELECT name, color FROM apples", selectColumns("apples", "name", "color")},
		{"three columns", "SELECT id, name, color FROM apples", selectColumns("apples", "id", "name", "color")},
		{"columns without whitespace", "select name,color from apples", selectColumns("apples", "name", "color")},
		{"columns with extra whitespace", "SELECT name ,\n\tcolor\nFROM apples", selectColumns("apples", "name", "color")},
		{"repeated column", "SELECT name, name FROM apples", selectColumns("apples", "name", "name")},
		{
			"quoted columns",
			"SELECT \"size range\", [total employees], `id` FROM companies",
			selectColumns("companies", "size range", "total employees", "id"),
		},
		{"columns named like functions", "SELECT count, max FROM apples", selectColumns("apples", "count", "max")},
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

func TestParseSelectWhere(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want comparison
	}{
		{
			"text",
			"SELECT name, color FROM apples WHERE color = 'Yellow'",
			comparison{column: "color", operator: "=", value: "Yellow"},
		},
		{
			"lowercase keywords",
			"select name from apples where color = 'Yellow'",
			comparison{column: "color", operator: "=", value: "Yellow"},
		},
		{
			"no optional whitespace",
			"SELECT name FROM apples WHERE color='Yellow';",
			comparison{column: "color", operator: "=", value: "Yellow"},
		},
		{
			"count",
			"SELECT COUNT(*) FROM apples WHERE color = 'Yellow'",
			comparison{column: "color", operator: "=", value: "Yellow"},
		},
		{
			"quoted column",
			`SELECT name FROM companies WHERE "size range" = '1 - 10'`,
			comparison{column: "size range", operator: "=", value: "1 - 10"},
		},
		{"empty text", "SELECT a FROM t WHERE a = ''", comparison{column: "a", operator: "=", value: ""}},
		{"text with a quote", "SELECT a FROM t WHERE a = 'it''s'", comparison{column: "a", operator: "=", value: "it's"}},
		{
			"text that looks like SQL",
			"SELECT a FROM t WHERE a = 'x'' OR 1 = 1 --'",
			comparison{column: "a", operator: "=", value: "x' OR 1 = 1 --"},
		},

		{"double equals", "SELECT a FROM t WHERE a == 'x'", comparison{column: "a", operator: "=", value: "x"}},
		{"not equal", "SELECT a FROM t WHERE a != 'x'", comparison{column: "a", operator: "!=", value: "x"}},
		{"not equal with brackets", "SELECT a FROM t WHERE a <> 'x'", comparison{column: "a", operator: "!=", value: "x"}},
		{"less than", "SELECT a FROM t WHERE a < 'x'", comparison{column: "a", operator: "<", value: "x"}},
		{"less than or equal", "SELECT a FROM t WHERE a <= 'x'", comparison{column: "a", operator: "<=", value: "x"}},
		{"greater than", "SELECT a FROM t WHERE a > 'x'", comparison{column: "a", operator: ">", value: "x"}},
		{"greater than or equal", "SELECT a FROM t WHERE a>=3", comparison{column: "a", operator: ">=", value: int64(3)}},

		{"integer", "SELECT a FROM t WHERE a = 42", comparison{column: "a", operator: "=", value: int64(42)}},
		{"negative integer", "SELECT a FROM t WHERE a = -42", comparison{column: "a", operator: "=", value: int64(-42)}},
		{"integer with a plus sign", "SELECT a FROM t WHERE a = +42", comparison{column: "a", operator: "=", value: int64(42)}},
		{"sign apart from its number", "SELECT a FROM t WHERE a = - 42", comparison{column: "a", operator: "=", value: int64(-42)}},
		{
			"smallest integer",
			"SELECT a FROM t WHERE a = -9223372036854775808",
			comparison{column: "a", operator: "=", value: int64(-9223372036854775808)},
		},
		{
			"integer that is too large",
			"SELECT a FROM t WHERE a = 9223372036854775808",
			comparison{column: "a", operator: "=", value: 9223372036854775808.0},
		},
		{"real", "SELECT a FROM t WHERE a = 1.5", comparison{column: "a", operator: "=", value: 1.5}},
		{"negative real", "SELECT a FROM t WHERE a = -.5", comparison{column: "a", operator: "=", value: -0.5}},
		{"real without fraction digits", "SELECT a FROM t WHERE a = 2.", comparison{column: "a", operator: "=", value: 2.0}},
		{"real with an exponent", "SELECT a FROM t WHERE a = 1e3", comparison{column: "a", operator: "=", value: 1000.0}},
		{"null", "SELECT a FROM t WHERE a = NULL", comparison{column: "a", operator: "=", value: nil}},
		{"lowercase null", "SELECT a FROM t WHERE a != null", comparison{column: "a", operator: "!=", value: nil}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			statement, err := parseSelect(tt.sql)
			if err != nil {
				t.Fatalf("parseSelect(%q) returned an error: %v", tt.sql, err)
			}
			if statement.where == nil {
				t.Fatalf("parseSelect(%q) found no WHERE clause", tt.sql)
			}
			if !reflect.DeepEqual(*statement.where, tt.want) {
				t.Errorf("parseSelect(%q).where = %#v, want %#v", tt.sql, *statement.where, tt.want)
			}
		})
	}
}

func TestParseSelectWithoutWhere(t *testing.T) {
	for _, sql := range []string{"SELECT name FROM apples", "SELECT COUNT(*) FROM apples;"} {
		statement, err := parseSelect(sql)
		if err != nil {
			t.Fatalf("parseSelect(%q) returned an error: %v", sql, err)
		}
		if statement.where != nil {
			t.Errorf("parseSelect(%q).where = %#v, want nil", sql, *statement.where)
		}
	}
}

func TestParseSelectKeepsTheRestOfAStatementWithWhere(t *testing.T) {
	got, err := parseSelect("SELECT name, color FROM apples WHERE color = 'Yellow'")
	if err != nil {
		t.Fatalf("parseSelect returned an error: %v", err)
	}

	want := selectStatement{
		columns: []string{"name", "color"},
		table:   "apples",
		where:   &comparison{column: "color", operator: "=", value: "Yellow"},
	}
	if !reflect.DeepEqual(*got, want) {
		t.Errorf("parseSelect = %+v, want %+v", *got, want)
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
		{"trailing comma", "SELECT name, FROM apples", `near "FROM": syntax error`},
		{"leading comma", "SELECT , name FROM apples", `near ",": syntax error`},
		{"doubled comma", "SELECT name,, color FROM apples", `near ",": syntax error`},
		{"missing comma", "SELECT name color FROM apples", `near "color": syntax error`},
		{"column list without FROM", "SELECT name, color", "incomplete input"},
		{"column list that ends early", "SELECT name,", "incomplete input"},
		{
			"count after a column",
			"SELECT name, COUNT(*) FROM apples",
			"selecting COUNT(*) along with other columns is not supported",
		},
		{
			"count before a column",
			"SELECT COUNT(*), name FROM apples",
			"selecting COUNT(*) along with other columns is not supported",
		},
		{
			"two counts",
			"SELECT COUNT(*), COUNT(*) FROM apples",
			"selecting COUNT(*) along with other columns is not supported",
		},

		{"empty WHERE clause", "SELECT name FROM apples WHERE", "incomplete input"},
		{"WHERE clause without an operator", "SELECT name FROM apples WHERE color", "incomplete input"},
		{"WHERE clause without a value", "SELECT name FROM apples WHERE color =", "incomplete input"},
		{"WHERE clause that ends with a sign", "SELECT name FROM apples WHERE id = -", "incomplete input"},
		{"WHERE clause without a column", "SELECT name FROM apples WHERE = 'Yellow'", `near "=": syntax error`},
		{"missing operator", "SELECT name FROM apples WHERE color 'Yellow'", `near "Yellow": syntax error`},
		{"unsupported operator", "SELECT name FROM apples WHERE color LIKE 'Y%'", `near "LIKE": syntax error`},
		{"IS NULL", "SELECT name FROM apples WHERE color IS NULL", `near "IS": syntax error`},
		{"tripled equals sign", "SELECT name FROM apples WHERE color === 'Yellow'", `near "=": syntax error`},
		{"value before the column", "SELECT name FROM apples WHERE 'Yellow' = color", `near "Yellow": syntax error`},
		{"column as value", "SELECT name FROM apples WHERE color = name", `near "name": syntax error`},
		{"signed text", "SELECT name FROM apples WHERE color = -'Yellow'", `near "Yellow": syntax error`},
		{"doubled sign", "SELECT name FROM apples WHERE id = --1", "incomplete input"},
		{"two values", "SELECT name FROM apples WHERE color = 'Yellow' 'Red'", `near "Red": syntax error`},
		{"two conditions", "SELECT name FROM apples WHERE color = 'Yellow' AND id = 4", `near "AND": syntax error`},
		{"two WHERE clauses", "SELECT name FROM apples WHERE id = 1 WHERE id = 2", `near "WHERE": syntax error`},
		{"WHERE before FROM", "SELECT name WHERE id = 1 FROM apples", `near "WHERE": syntax error`},
		{"unterminated text", "SELECT name FROM apples WHERE color = 'Yellow", "unterminated string literal: 'Yellow"},
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
