package main

import (
	"errors"
	"strings"
)

// tableDefinition is a parsed CREATE TABLE statement.
type tableDefinition struct {
	// name is the name of the table.
	name string
	// columns holds the columns of the table, in the order in which their
	// values are stored in the rows of the table.
	columns []columnDefinition
	// rowIDColumn is the index of the column that is an alias for the rowid,
	// or -1 if the table has no such column.
	rowIDColumn int
}

// columnDefinition is the definition of a column in a CREATE TABLE statement.
type columnDefinition struct {
	// name is the name of the column.
	name string
	// declaredType is the type that the column is declared with, in upper
	// case. It is empty for a column that is declared without a type.
	declaredType string
	// collation is the name of the collating sequence that the column is
	// declared with, in upper case. It is empty for a column that is declared
	// without one, which then uses the default of collationBinary.
	collation string
}

// affinity returns the type that the column prefers to store its values as.
func (c columnDefinition) affinity() affinity {
	return typeAffinity(c.declaredType)
}

// tableConstraintKeywords are the keywords that start a table constraint,
// rather than a column definition, in a CREATE TABLE statement.
var tableConstraintKeywords = []string{"CONSTRAINT", "PRIMARY", "UNIQUE", "CHECK", "FOREIGN"}

// columnConstraintKeywords are the keywords that start a column constraint,
// and so end the declared type of a column.
var columnConstraintKeywords = []string{
	"CONSTRAINT", "PRIMARY", "NOT", "NULL", "UNIQUE", "CHECK", "DEFAULT", "COLLATE", "REFERENCES", "GENERATED", "AS",
}

// parseCreateTable parses a CREATE TABLE statement, as stored in the sql
// column of the sqlite_schema table.
func parseCreateTable(sql string) (*tableDefinition, error) {
	tokens, err := tokenize(sql)
	if err != nil {
		return nil, err
	}
	p := &parser{tokens: tokens}

	if err := p.expectKeyword("CREATE"); err != nil {
		return nil, err
	}
	if !p.acceptKeyword("TEMP") {
		p.acceptKeyword("TEMPORARY")
	}
	if err := p.expectKeyword("TABLE"); err != nil {
		return nil, err
	}
	if p.acceptKeyword("IF") {
		if err := p.expectKeywords("NOT", "EXISTS"); err != nil {
			return nil, err
		}
	}
	name, err := p.expectName()
	if err != nil {
		return nil, err
	}
	if p.acceptSymbol(".") {
		// The name was qualified with the name of its schema, as in
		// main.apples.
		if name, err = p.expectName(); err != nil {
			return nil, err
		}
	}
	if err := p.expectSymbol("("); err != nil {
		return nil, err
	}

	table := &tableDefinition{name: name, rowIDColumn: -1}
	// primaryKey is the name of the column of a PRIMARY KEY table constraint
	// that covers a single column.
	primaryKey := ""
	for {
		item, err := p.definitionItem()
		if err != nil {
			return nil, err
		}

		if isTableConstraint(item) {
			if column, ok := singleColumnPrimaryKey(item); ok {
				primaryKey = column
			}
		} else {
			column, isRowID, err := parseColumnDefinition(item)
			if err != nil {
				return nil, err
			}
			if isRowID {
				table.rowIDColumn = len(table.columns)
			}
			table.columns = append(table.columns, column)
		}

		if !p.acceptSymbol(",") {
			break
		}
	}
	if err := p.expectSymbol(")"); err != nil {
		return nil, err
	}

	// The table options follow the closing parenthesis.
	for p.peek().kind != tokenEOF {
		if p.next().isKeyword("WITHOUT") && p.acceptKeyword("ROWID") {
			return nil, errors.New("WITHOUT ROWID tables are not supported")
		}
	}

	// A column of type INTEGER that is the whole primary key of the table is
	// an alias for the rowid.
	for i, column := range table.columns {
		if primaryKey != "" && strings.EqualFold(column.name, primaryKey) && column.declaredType == "INTEGER" {
			table.rowIDColumn = i
		}
	}
	return table, nil
}

// definitionItem consumes the tokens of the next column definition or table
// constraint of a CREATE TABLE statement. That is every token up to the comma
// or the closing parenthesis that ends the item.
func (p *parser) definitionItem() ([]token, error) {
	start := p.pos
	depth := 0
	for {
		t := p.peek()
		switch {
		case t.kind == tokenEOF:
			return nil, syntaxError(t)
		case t.isSymbol("("):
			depth++
		case t.isSymbol(")") && depth > 0:
			depth--
		case t.isSymbol(")"), t.isSymbol(",") && depth == 0:
			if p.pos == start {
				return nil, syntaxError(t)
			}
			return p.tokens[start:p.pos], nil
		}
		p.next()
	}
}

// isTableConstraint reports whether an item of a CREATE TABLE statement is a
// table constraint, as opposed to a column definition.
func isTableConstraint(item []token) bool {
	return isAnyKeyword(item[0], tableConstraintKeywords)
}

// isAnyKeyword reports whether a token is one of the given keywords.
func isAnyKeyword(t token, keywords []string) bool {
	for _, keyword := range keywords {
		if t.isKeyword(keyword) {
			return true
		}
	}
	return false
}

// singleColumnPrimaryKey returns the name of the column that a table
// constraint declares to be the primary key of its table. ok is false if the
// constraint is not a PRIMARY KEY constraint that covers a single column.
func singleColumnPrimaryKey(constraint []token) (column string, ok bool) {
	if len(constraint) >= 2 && constraint[0].isKeyword("CONSTRAINT") {
		// Skip the name of the constraint.
		constraint = constraint[2:]
	}
	// The shortest constraint is PRIMARY KEY ( column ).
	if len(constraint) < 5 || !constraint[0].isKeyword("PRIMARY") || !constraint[1].isKeyword("KEY") ||
		!constraint[2].isSymbol("(") || constraint[3].kind != tokenIdentifier {
		return "", false
	}

	// A comma before the closing parenthesis means that there are more
	// columns.
	for _, t := range constraint[4:] {
		if t.isSymbol(",") {
			return "", false
		}
		if t.isSymbol(")") {
			break
		}
	}
	return constraint[3].text, true
}

// parseColumnDefinition parses the tokens of a column definition of a CREATE
// TABLE statement. isRowID reports whether the column is an alias for the
// rowid of its table.
func parseColumnDefinition(definition []token) (column columnDefinition, isRowID bool, err error) {
	// Like SQLite, accept a string literal as the name of a column.
	name := definition[0]
	if name.kind != tokenIdentifier && name.kind != tokenString {
		return columnDefinition{}, false, syntaxError(name)
	}

	// The declared type is made up of the words between the name of the
	// column and its first constraint. Arguments in parentheses, as in
	// VARCHAR(10), are not part of it.
	var typeWords []string
	depth := 0
	i := 1
	for ; i < len(definition); i++ {
		t := definition[i]
		if depth == 0 && isAnyKeyword(t, columnConstraintKeywords) {
			break
		}
		switch {
		case t.isSymbol("("):
			depth++
		case t.isSymbol(")"):
			depth--
		case depth == 0:
			typeWords = append(typeWords, strings.ToUpper(t.text))
		}
	}
	column = columnDefinition{name: name.text, declaredType: strings.Join(typeWords, " ")}

	// Look through the constraints of the column, skipping over the
	// expressions in parentheses.
	for ; i < len(definition); i++ {
		t := definition[i]
		following := func(offset int) token {
			if i+offset < len(definition) {
				return definition[i+offset]
			}
			return token{kind: tokenEOF}
		}

		switch {
		case t.isSymbol("("):
			depth++
		case t.isSymbol(")"):
			depth--
		case depth > 0:
		case t.isKeyword("GENERATED"), t.isKeyword("AS") && following(1).isSymbol("("):
			// The values of generated columns may not be stored in the rows
			// of the table.
			return columnDefinition{}, false, errors.New("tables with generated columns are not supported")
		case t.isKeyword("COLLATE") && following(1).kind == tokenIdentifier:
			column.collation = strings.ToUpper(following(1).text)
		case t.isKeyword("PRIMARY") && following(1).isKeyword("KEY"):
			// A column declared as INTEGER PRIMARY KEY is an alias for the
			// rowid. Due to a quirk of SQLite, that's not the case for
			// INTEGER PRIMARY KEY DESC.
			isRowID = column.declaredType == "INTEGER" && !following(2).isKeyword("DESC")
		}
	}
	return column, isRowID, nil
}
