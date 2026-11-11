package main

import (
	"errors"
	"fmt"
)

// selectStatement is a parsed SELECT statement.
type selectStatement struct {
	// table is the name of the table to read from.
	table string
}

// parseSelect parses a SELECT statement. The only supported form is:
//
//	SELECT COUNT(*) FROM table
func parseSelect(sql string) (*selectStatement, error) {
	tokens, err := tokenize(sql)
	if err != nil {
		return nil, err
	}
	p := &parser{tokens: tokens}

	if err := p.expectKeyword("SELECT"); err != nil {
		return nil, err
	}
	if err := p.expectKeyword("COUNT"); err != nil {
		return nil, err
	}
	for _, symbol := range []string{"(", "*", ")"} {
		if err := p.expectSymbol(symbol); err != nil {
			return nil, err
		}
	}
	if err := p.expectKeyword("FROM"); err != nil {
		return nil, err
	}
	table, err := p.expectName()
	if err != nil {
		return nil, err
	}

	// A statement may end with a semicolon.
	p.acceptSymbol(";")
	if t := p.peek(); t.kind != tokenEOF {
		return nil, syntaxError(t)
	}

	return &selectStatement{table: table}, nil
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

// expectName consumes the next token, which must be an identifier, and
// returns the name it holds.
func (p *parser) expectName() (string, error) {
	t := p.peek()
	if t.kind != tokenIdentifier {
		return "", syntaxError(t)
	}
	p.next()
	return t.text, nil
}

// syntaxError returns the error for a token that can't appear where it does.
func syntaxError(t token) error {
	if t.kind == tokenEOF {
		return errors.New("incomplete input")
	}
	return fmt.Errorf("near %q: syntax error", t.text)
}
