package main

import (
	"errors"
	"fmt"
	"strings"
)

// selectStatement is a parsed SELECT statement.
type selectStatement struct {
	// count reports whether the statement selects COUNT(*), the number of
	// rows, rather than columns.
	count bool
	// columns holds the names of the columns to select, in the order in
	// which to output them. It is empty if count is set.
	columns []string
	// table is the name of the table to read from.
	table string
	// where is the condition that the rows to select must meet. It is nil if
	// the statement has no WHERE clause.
	where *comparison
}

// comparison is a condition that compares the value of a column with a
// literal value, like color = 'Yellow'.
type comparison struct {
	// column is the name of the column.
	column string
	// operator is the comparison operator: =, !=, <, <=, > or >=.
	operator string
	// value is the literal value: a string, an int64, a float64, or nil for
	// NULL.
	value any
}

// comparisonOperators maps the comparison operators that can be written in
// SQL to the operators of a comparison.
var comparisonOperators = map[string]string{
	"=": "=", "==": "=", "!=": "!=", "<>": "!=", "<": "<", "<=": "<=", ">": ">", ">=": ">=",
}

// parseSelect parses a SELECT statement. The supported forms are:
//
//	SELECT COUNT(*) FROM table [WHERE column operator literal]
//	SELECT column, ... FROM table [WHERE column operator literal]
func parseSelect(sql string) (*selectStatement, error) {
	tokens, err := tokenize(sql)
	if err != nil {
		return nil, err
	}
	p := &parser{tokens: tokens}
	statement := &selectStatement{}

	if err := p.expectKeyword("SELECT"); err != nil {
		return nil, err
	}
	for resultColumns := 1; ; resultColumns++ {
		name, err := p.expectName()
		if err != nil {
			return nil, err
		}
		if p.acceptSymbol("(") {
			// A name that is followed by a parenthesis is a function call.
			if !strings.EqualFold(name, "COUNT") {
				return nil, fmt.Errorf("no such function: %s", name)
			}
			if err := p.expectSymbols("*", ")"); err != nil {
				return nil, err
			}
			statement.count = true
		} else {
			statement.columns = append(statement.columns, name)
		}
		if statement.count && resultColumns > 1 {
			return nil, errors.New("selecting COUNT(*) along with other columns is not supported")
		}

		if !p.acceptSymbol(",") {
			break
		}
	}

	if err := p.expectKeyword("FROM"); err != nil {
		return nil, err
	}
	if statement.table, err = p.expectName(); err != nil {
		return nil, err
	}
	if p.acceptKeyword("WHERE") {
		if statement.where, err = p.parseComparison(); err != nil {
			return nil, err
		}
	}

	// A statement may end with a semicolon.
	p.acceptSymbol(";")
	if t := p.peek(); t.kind != tokenEOF {
		return nil, syntaxError(t)
	}
	return statement, nil
}

// parser reads through the tokens of a SQL statement.
type parser struct {
	tokens []token
	// pos is the index of the next token to read.
	pos int
}

// peek returns the next token without consuming it.
func (p *parser) peek() token {
	return p.tokens[p.pos]
}

// next consumes the next token and returns it. The tokenEOF token that ends
// the statement is never consumed.
func (p *parser) next() token {
	t := p.tokens[p.pos]
	if t.kind != tokenEOF {
		p.pos++
	}
	return t
}

// acceptKeyword consumes the next token if it is the given keyword. It
// reports whether it did.
func (p *parser) acceptKeyword(keyword string) bool {
	if !p.peek().isKeyword(keyword) {
		return false
	}
	p.next()
	return true
}

// expectKeyword consumes the next token, which must be the given keyword.
func (p *parser) expectKeyword(keyword string) error {
	if !p.acceptKeyword(keyword) {
		return syntaxError(p.peek())
	}
	return nil
}

// expectKeywords consumes the next tokens, which must be the given keywords.
func (p *parser) expectKeywords(keywords ...string) error {
	for _, keyword := range keywords {
		if err := p.expectKeyword(keyword); err != nil {
			return err
		}
	}
	return nil
}

// acceptSymbol consumes the next token if it is the given symbol. It reports
// whether it did.
func (p *parser) acceptSymbol(symbol string) bool {
	if !p.peek().isSymbol(symbol) {
		return false
	}
	p.next()
	return true
}

// expectSymbol consumes the next token, which must be the given symbol.
func (p *parser) expectSymbol(symbol string) error {
	if !p.acceptSymbol(symbol) {
		return syntaxError(p.peek())
	}
	return nil
}

// expectSymbols consumes the next tokens, which must be the given symbols.
func (p *parser) expectSymbols(symbols ...string) error {
	for _, symbol := range symbols {
		if err := p.expectSymbol(symbol); err != nil {
			return err
		}
	}
	return nil
}

// expectName consumes the next token, which must be the name of something
// like a table or a column, and returns the name. A keyword that SQLite
// reserves can only be used as a name when it is quoted.
func (p *parser) expectName() (string, error) {
	t := p.peek()
	if t.kind != tokenIdentifier || (!t.quoted && reservedKeywords[strings.ToUpper(t.text)]) {
		return "", syntaxError(t)
	}
	p.next()
	return t.text, nil
}

// reservedKeywords holds the keywords that SQLite doesn't accept as names
// unless they are quoted. SQLite has many more keywords, but it falls back to
// reading those as names where a name is expected.
var reservedKeywords = map[string]bool{
	"ADD": true, "ALL": true, "ALTER": true, "AND": true, "AS": true, "AUTOINCREMENT": true,
	"BETWEEN": true, "CASE": true, "CHECK": true, "COLLATE": true, "COMMIT": true, "CONSTRAINT": true,
	"CREATE": true, "DEFAULT": true, "DEFERRABLE": true, "DELETE": true, "DISTINCT": true, "DROP": true,
	"ELSE": true, "ESCAPE": true, "EXCEPT": true, "EXISTS": true, "FOREIGN": true, "FROM": true,
	"GROUP": true, "HAVING": true, "IN": true, "INDEX": true, "INSERT": true, "INTERSECT": true,
	"INTO": true, "IS": true, "ISNULL": true, "JOIN": true, "LIMIT": true, "NOT": true,
	"NOTHING": true, "NOTNULL": true, "NULL": true, "ON": true, "OR": true, "ORDER": true,
	"PRIMARY": true, "REFERENCES": true, "RETURNING": true, "SELECT": true, "SET": true, "TABLE": true,
	"THEN": true, "TO": true, "TRANSACTION": true, "UNION": true, "UNIQUE": true, "UPDATE": true,
	"USING": true, "VALUES": true, "WHEN": true, "WHERE": true,
}

// parseComparison parses a comparison of a column with a literal value.
func (p *parser) parseComparison() (*comparison, error) {
	column, err := p.expectName()
	if err != nil {
		return nil, err
	}

	t := p.peek()
	operator, ok := comparisonOperators[t.text]
	if t.kind != tokenSymbol || !ok {
		return nil, syntaxError(t)
	}
	p.next()

	value, err := p.parseLiteral()
	if err != nil {
		return nil, err
	}
	return &comparison{column: column, operator: operator, value: value}, nil
}

// parseLiteral parses a literal value: a string, a number or NULL.
func (p *parser) parseLiteral() (any, error) {
	t := p.next()
	sign := ""
	if t.isSymbol("-") || t.isSymbol("+") {
		// A sign must be followed by a number.
		sign = t.text
		if t = p.next(); t.kind != tokenNumber {
			return nil, syntaxError(t)
		}
	}

	switch {
	case t.kind == tokenString:
		return t.text, nil
	case t.kind == tokenNumber:
		if number, ok := parseNumber(sign + t.text); ok {
			return number, nil
		}
	case t.isKeyword("NULL"):
		return nil, nil
	}
	return nil, syntaxError(t)
}

// syntaxError returns the error for a token that can't appear where it does.
func syntaxError(t token) error {
	if t.kind == tokenEOF {
		return errors.New("incomplete input")
	}
	return fmt.Errorf("near %q: syntax error", t.text)
}
