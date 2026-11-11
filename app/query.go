package main

import (
	"fmt"
	"io"
	"strings"
)

// executeSQL runs a SQL statement against db and writes its result to stdout,
// one row per line.
func executeSQL(db *database, sql string, stdout io.Writer) error {
	statement, err := parseSelect(sql)
	if err != nil {
		return err
	}
	table, err := db.findTable(statement.table)
	if err != nil {
		return err
	}
	where, err := newFilter(table, statement.where)
	if err != nil {
		return err
	}

	if statement.count {
		rowCount, err := db.countMatchingRows(table, where)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(stdout, rowCount)
		return err
	}

	columns := make([]int, len(statement.columns))
	for i, name := range statement.columns {
		if columns[i], err = table.columnIndex(name); err != nil {
			return err
		}
	}

	// Like the sqlite3 command-line shell does by default, separate the
	// values of a row with pipes.
	fields := make([]string, len(columns))
	return db.scanTable(table, func(r row) error {
		if !where.matches(r) {
			return nil
		}
		for i, column := range columns {
			fields[i] = formatValue(r.value(column))
		}
		_, err := fmt.Fprintln(stdout, strings.Join(fields, "|"))
		return err
	})
}

// countMatchingRows returns the number of rows of a table that meet the
// condition of a WHERE clause.
func (db *database) countMatchingRows(t *table, where *filter) (int, error) {
	if where == nil {
		// Counting all the rows of a table doesn't take reading them.
		return db.countRows(t)
	}

	rowCount := 0
	err := db.scanTable(t, func(r row) error {
		if where.matches(r) {
			rowCount++
		}
		return nil
	})
	return rowCount, err
}

// filter is the condition of a WHERE clause, resolved against the table whose
// rows it selects.
type filter struct {
	// column is the index of the column to compare.
	column int
	// operator is the comparison operator: =, !=, <, <=, > or >=.
	operator string
	// value is the value to compare the column with.
	value any
	// collation is the collating sequence to compare TEXT values with.
	collation string
}

// newFilter resolves the condition of a WHERE clause against a table. It
// returns nil if there is no condition.
func newFilter(t *table, condition *comparison) (*filter, error) {
	if condition == nil {
		return nil, nil
	}

	index, err := t.columnIndex(condition.column)
	if err != nil {
		return nil, err
	}
	column := t.columns[index]

	// A comparison uses the collating sequence of its column.
	collation := column.collation
	switch collation {
	case "":
		collation = collationBinary
	case collationBinary, collationNoCase, collationRTrim:
	default:
		return nil, fmt.Errorf("no such collation sequence: %s", collation)
	}

	return &filter{
		column:    index,
		operator:  condition.operator,
		value:     convertForComparison(condition.value, column.affinity()),
		collation: collation,
	}, nil
}

// matches reports whether a row meets the condition. Every row meets a nil
// filter, which stands for the absence of a WHERE clause.
func (f *filter) matches(r row) bool {
	if f == nil {
		return true
	}

	value := r.value(f.column)
	if value == nil || f.value == nil {
		// A comparison with NULL is neither true nor false, so the row isn't
		// selected.
		return false
	}

	order := compareValues(value, f.value, f.collation)
	switch f.operator {
	case "=":
		return order == 0
	case "!=":
		return order != 0
	case "<":
		return order < 0
	case "<=":
		return order <= 0
	case ">":
		return order > 0
	default:
		return order >= 0
	}
}
