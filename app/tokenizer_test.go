package main

import (
	"reflect"
	"testing"
)

// Constructors for the tokens that the tokenizer tests expect.
func ident(text string) token  { return token{kind: tokenIdentifier, text: text} }
func quoted(text string) token { return token{kind: tokenIdentifier, text: text, quoted: true} }
func str(text string) token    { return token{kind: tokenString, text: text} }
func num(text string) token    { return token{kind: tokenNumber, text: text} }
func sym(text string) token    { return token{kind: tokenSymbol, text: text} }

var eof = token{kind: tokenEOF}

func TestTokenize(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want []token
	}{
		{"empty text", "", []token{eof}},
		{"nothing but whitespace", " \t\r\n\f ", []token{eof}},
		{
			"count statement",
			"SELECT COUNT(*) FROM apples",
			[]token{ident("SELECT"), ident("COUNT"), sym("("), sym("*"), sym(")"), ident("FROM"), ident("apples"), eof},
		},
		{
			"statement without optional whitespace",
			"select name,color from apples where color='Yellow';",
			[]token{
				ident("select"), ident("name"), sym(","), ident("color"), ident("from"), ident("apples"),
				ident("where"), ident("color"), sym("="), str("Yellow"), sym(";"), eof,
			},
		},
		{
			"whitespace between tokens",
			"SELECT\n\tCOUNT ( * )\r\nFROM  apples",
			[]token{ident("SELECT"), ident("COUNT"), sym("("), sym("*"), sym(")"), ident("FROM"), ident("apples"), eof},
		},
		{
			"characters of identifiers",
			"_a1 b$2 éclair x_y Z9",
			[]token{ident("_a1"), ident("b$2"), ident("éclair"), ident("x_y"), ident("Z9"), eof},
		},
		{"identifier in double quotes", `"size range"`, []token{quoted("size range"), eof}},
		{"identifier in backticks", "`size range`", []token{quoted("size range"), eof}},
		{"identifier in square brackets", "[size range]", []token{quoted("size range"), eof}},
		{"quoted keyword", `"select"`, []token{quoted("select"), eof}},
		{"doubled quote in an identifier", `"a ""b"" c"`, []token{quoted(`a "b" c`), eof}},
		{"doubled backtick in an identifier", "`a``b`", []token{quoted("a`b"), eof}},
		{"empty quoted identifier", `""`, []token{quoted(""), eof}},
		{"string", "'Pink Eyes'", []token{str("Pink Eyes"), eof}},
		{"empty string", "''", []token{str(""), eof}},
		{"doubled quote in a string", "'it''s'", []token{str("it's"), eof}},
		{"string of one quote", "''''", []token{str("'"), eof}},
		{"adjacent strings", "'a' 'b'", []token{str("a"), str("b"), eof}},
		{
			"string that looks like SQL",
			`'-- not a comment /* nor this */ "x" SELECT'`,
			[]token{str(`-- not a comment /* nor this */ "x" SELECT`), eof},
		},
		{"string with a newline", "'a\nb'", []token{str("a\nb"), eof}},
		{
			"numbers",
			"0 42 3.14 .5 1. 1e10 2.5E-3 7e+2",
			[]token{num("0"), num("42"), num("3.14"), num(".5"), num("1."), num("1e10"), num("2.5E-3"), num("7e+2"), eof},
		},
		{
			"single-character symbols",
			"( ) , * = . ; < > + - / % ?",
			[]token{
				sym("("), sym(")"), sym(","), sym("*"), sym("="), sym("."), sym(";"), sym("<"), sym(">"),
				sym("+"), sym("-"), sym("/"), sym("%"), sym("?"), eof,
			},
		},
		{
			"two-character symbols",
			"<= >= <> != == || << >>",
			[]token{sym("<="), sym(">="), sym("<>"), sym("!="), sym("=="), sym("||"), sym("<<"), sym(">>"), eof},
		},
		{"symbols without whitespace", "a<=b", []token{ident("a"), sym("<="), ident("b"), eof}},
		{"qualified name", "apples.name", []token{ident("apples"), sym("."), ident("name"), eof}},
		{
			"comment to the end of the line",
			"SELECT -- the count\nCOUNT",
			[]token{ident("SELECT"), ident("COUNT"), eof},
		},
		{"comment at the end of the text", "apples -- trailing", []token{ident("apples"), eof}},
		{"empty comment at the end of the text", "apples --", []token{ident("apples"), eof}},
		{
			"block comment",
			"SELECT /* the\ncount */ COUNT/**/(",
			[]token{ident("SELECT"), ident("COUNT"), sym("("), eof},
		},
		{"unterminated block comment", "apples /* never closed", []token{ident("apples"), eof}},
		{"minus that doesn't start a comment", "1 - 2 -3", []token{num("1"), sym("-"), num("2"), sym("-"), num("3"), eof}},
		{"slash that doesn't start a comment", "1 / 2", []token{num("1"), sym("/"), num("2"), eof}},
		{
			"column definitions",
			`id integer primary key, "size range" varchar(10) DEFAULT 'n/a'`,
			[]token{
				ident("id"), ident("integer"), ident("primary"), ident("key"), sym(","),
				quoted("size range"), ident("varchar"), sym("("), num("10"), sym(")"),
				ident("DEFAULT"), str("n/a"), eof,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tokenize(tt.sql)
			if err != nil {
				t.Fatalf("tokenize(%q) returned an error: %v", tt.sql, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("tokenize(%q)\n got %+v\nwant %+v", tt.sql, got, tt.want)
			}
		})
	}
}

func TestTokenizeRejectsInvalidText(t *testing.T) {
	tests := []struct {
		name string
		sql  string
	}{
		{"unterminated string", "SELECT 'abc"},
		{"string that ends with a doubled quote", "'abc''"},
		{"unterminated double-quoted identifier", `SELECT "abc`},
		{"unterminated backtick identifier", "SELECT `abc"},
		{"unterminated bracket identifier", "SELECT [abc"},
		{"number followed by letters", "12abc"},
		{"number with an empty exponent", "1e"},
		{"number with a signed empty exponent", "1e+"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tokens, err := tokenize(tt.sql); err == nil {
				t.Errorf("tokenize(%q) = %+v, want an error", tt.sql, tokens)
			}
		})
	}
}

func TestTokenIsKeyword(t *testing.T) {
	tests := []struct {
		token   token
		keyword string
		want    bool
	}{
		{ident("SELECT"), "SELECT", true},
		{ident("select"), "SELECT", true},
		{ident("SeLeCt"), "SELECT", true},
		{ident("selects"), "SELECT", false},
		{ident("FROM"), "SELECT", false},
		// A quoted identifier is a name, even if it spells a keyword.
		{quoted("select"), "SELECT", false},
		{str("select"), "SELECT", false},
		{sym("*"), "*", false},
		{eof, "", false},
	}

	for _, tt := range tests {
		if got := tt.token.isKeyword(tt.keyword); got != tt.want {
			t.Errorf("%+v.isKeyword(%q) = %t, want %t", tt.token, tt.keyword, got, tt.want)
		}
	}
}

func TestTokenIsSymbol(t *testing.T) {
	tests := []struct {
		token  token
		symbol string
		want   bool
	}{
		{sym("*"), "*", true},
		{sym("<="), "<=", true},
		{sym("<"), "<=", false},
		{sym("("), ")", false},
		{str("*"), "*", false},
		{ident("x"), "x", false},
		{eof, "", false},
	}

	for _, tt := range tests {
		if got := tt.token.isSymbol(tt.symbol); got != tt.want {
			t.Errorf("%+v.isSymbol(%q) = %t, want %t", tt.token, tt.symbol, got, tt.want)
		}
	}
}
