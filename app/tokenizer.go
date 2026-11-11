package main

import (
	"fmt"
	"strings"
)

// tokenKind is the type of a token of SQL text.
type tokenKind int

const (
	// tokenEOF marks the end of the SQL text.
	tokenEOF tokenKind = iota
	// tokenIdentifier is a name or a keyword, like apples or SELECT.
	tokenIdentifier
	// tokenString is a string literal, like 'Granny Smith'.
	tokenString
	// tokenNumber is a numeric literal, like 42 or 1.5.
	tokenNumber
	// tokenSymbol is a punctuation mark or an operator, like ( or <=.
	tokenSymbol
)

// token is a token of SQL text.
type token struct {
	kind tokenKind
	// text is the content of the token. For identifiers and string literals,
	// that is the name or string without its quotes. Numbers and symbols are
	// kept as they are written.
	text string
	// quoted reports whether an identifier was written in quotes, like
	// "size range". A quoted identifier is always a name, never a keyword.
	quoted bool
}

// isKeyword reports whether the token is the given keyword. Keywords are
// case-insensitive.
func (t token) isKeyword(keyword string) bool {
	return t.kind == tokenIdentifier && !t.quoted && strings.EqualFold(t.text, keyword)
}

// isSymbol reports whether the token is the given symbol.
func (t token) isSymbol(symbol string) bool {
	return t.kind == tokenSymbol && t.text == symbol
}

// twoCharSymbols lists the operators that are made up of two characters.
var twoCharSymbols = []string{"<=", ">=", "<>", "!=", "==", "||", "<<", ">>"}

// tokenize splits SQL text into tokens. Whitespace and comments are skipped.
// The returned tokens always end with a tokenEOF token.
func tokenize(sql string) ([]token, error) {
	var tokens []token
	for i := 0; i < len(sql); {
		c := sql[i]
		rest := sql[i:]

		switch {
		case isSpace(c):
			i++

		case strings.HasPrefix(rest, "--"):
			// A comment that runs to the end of the line.
			end := strings.IndexByte(rest, '\n')
			if end < 0 {
				end = len(rest) - 1
			}
			i += end + 1

		case strings.HasPrefix(rest, "/*"):
			// A comment that runs to the next */. Like SQLite, let an
			// unterminated comment run to the end of the text.
			end := strings.Index(rest[2:], "*/")
			if end < 0 {
				i = len(sql)
			} else {
				i += 2 + end + 2
			}

		case c == '\'':
			text, size, ok := readQuoted(rest)
			if !ok {
				return nil, fmt.Errorf("unterminated string literal: %s", rest)
			}
			tokens = append(tokens, token{kind: tokenString, text: text})
			i += size

		case c == '"' || c == '`':
			text, size, ok := readQuoted(rest)
			if !ok {
				return nil, fmt.Errorf("unterminated quoted identifier: %s", rest)
			}
			tokens = append(tokens, token{kind: tokenIdentifier, text: text, quoted: true})
			i += size

		case c == '[':
			// An identifier in square brackets, as used by SQL Server.
			end := strings.IndexByte(rest, ']')
			if end < 0 {
				return nil, fmt.Errorf("unterminated quoted identifier: %s", rest)
			}
			tokens = append(tokens, token{kind: tokenIdentifier, text: rest[1:end], quoted: true})
			i += end + 1

		case isIdentifierStart(c):
			size := 1
			for size < len(rest) && isIdentifierPart(rest[size]) {
				size++
			}
			tokens = append(tokens, token{kind: tokenIdentifier, text: rest[:size]})
			i += size

		case isDigit(c) || (c == '.' && len(rest) > 1 && isDigit(rest[1])):
			size := numberSize(rest)
			if size < len(rest) && isIdentifierPart(rest[size]) {
				return nil, fmt.Errorf("unrecognized token: %s", rest)
			}
			tokens = append(tokens, token{kind: tokenNumber, text: rest[:size]})
			i += size

		default:
			size := 1
			for _, symbol := range twoCharSymbols {
				if strings.HasPrefix(rest, symbol) {
					size = 2
					break
				}
			}
			tokens = append(tokens, token{kind: tokenSymbol, text: rest[:size]})
			i += size
		}
	}
	return append(tokens, token{kind: tokenEOF}), nil
}

// readQuoted reads the quoted string or identifier at the start of sql, whose
// first byte is the quote character. Inside the quotes, the quote character
// is written twice. readQuoted returns the unquoted text and the number of
// bytes that its quoted form takes up in sql. ok is false if the closing
// quote is missing.
func readQuoted(sql string) (text string, size int, ok bool) {
	quote := sql[0]

	var unquoted strings.Builder
	for i := 1; i < len(sql); i++ {
		if sql[i] != quote {
			unquoted.WriteByte(sql[i])
			continue
		}
		if i+1 < len(sql) && sql[i+1] == quote {
			// A doubled quote character stands for one quote character.
			unquoted.WriteByte(quote)
			i++
			continue
		}
		return unquoted.String(), i + 1, true
	}
	return "", 0, false
}

// numberSize returns the number of bytes that the numeric literal at the
// start of sql takes up: digits with an optional fractional part and an
// optional exponent.
func numberSize(sql string) int {
	digits := func(i int) int {
		for i < len(sql) && isDigit(sql[i]) {
			i++
		}
		return i
	}

	size := digits(0)
	if size < len(sql) && sql[size] == '.' {
		size = digits(size + 1)
	}
	if size < len(sql) && (sql[size] == 'e' || sql[size] == 'E') {
		// An exponent only counts if it has at least one digit.
		exponent := size + 1
		if exponent < len(sql) && (sql[exponent] == '+' || sql[exponent] == '-') {
			exponent++
		}
		if end := digits(exponent); end > exponent {
			size = end
		}
	}
	return size
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f'
}

func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

// isIdentifierStart reports whether c can start an unquoted identifier.
// Bytes outside of ASCII are parts of UTF-8 encoded characters, all of which
// are allowed in identifiers.
func isIdentifierStart(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' || c >= 0x80
}

// isIdentifierPart reports whether c can follow the first character of an
// unquoted identifier.
func isIdentifierPart(c byte) bool {
	return isIdentifierStart(c) || isDigit(c) || c == '$'
}
