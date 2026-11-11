package main

import (
	"fmt"
	"strings"
)

// table is a table of a database.
type table struct {
	// name is the name of the table.
	name string
	// rootPage is the number of the root page of the table's b-tree.
	rootPage int
	// columns holds the columns of the table, in the order in which their
	// values are stored in the rows of the table.
	columns []columnDefinition
	// rowIDColumn is the index of the column that is an alias for the rowid,
	// or -1 if the table has no such column.
	rowIDColumn int
}

// findTable returns the table with the given name. Like SQLite, it ignores
// the case of the name.
func (db *database) findTable(name string) (*table, error) {
	schema, err := db.readSchema()
	if err != nil {
		return nil, err
	}

	for _, object := range schema {
		if object.kind != "table" || !strings.EqualFold(object.name, name) {
			continue
		}
		definition, err := parseCreateTable(object.sql)
		if err != nil {
			return nil, fmt.Errorf("failed to parse the definition of table %s: %w", object.name, err)
		}
		return &table{
			name:        object.name,
			rootPage:    object.rootPage,
			columns:     definition.columns,
			rowIDColumn: definition.rowIDColumn,
		}, nil
	}
	return nil, fmt.Errorf("no such table: %s", name)
}

// columnIndex returns the index of the column with the given name. Like
// SQLite, it ignores the case of the name.
func (t *table) columnIndex(name string) (int, error) {
	for i, column := range t.columns {
		if strings.EqualFold(column.name, name) {
			return i, nil
		}
	}
	return 0, fmt.Errorf("no such column: %s", name)
}

// row is a row of a table.
type row struct {
	table *table
	// rowID is the row's unique integer key within its table.
	rowID int64
	// values holds the values that are stored in the row's record. There is
	// one for each column that the table had when the row was written.
	values []any
}

// value returns the value of the column with the given index.
func (r row) value(column int) any {
	if column == r.table.rowIDColumn {
		// The record holds a NULL in place of a column that is an alias for
		// the rowid.
		return r.rowID
	}
	if column >= len(r.values) {
		// The column was added to the table after the row was written.
		return nil
	}

	value := r.values[column]
	if integer, ok := value.(int64); ok && r.table.columns[column].affinity() == affinityReal {
		// To save space, SQLite stores a floating point number without a
		// fractional part as an integer.
		return float64(integer)
	}
	return value
}

// scanTable calls visit for every row of a table, in the order of their
// rowids. It stops at the first error that visit returns, and returns that
// error.
func (db *database) scanTable(t *table, visit func(row) error) error {
	return db.scanTableBTree(t.rootPage, func(rowID int64, values []any) error {
		return visit(row{table: t, rowID: rowID, values: values})
	})
}

// countRows returns the number of rows in a table.
func (db *database) countRows(t *table) (int, error) {
	rowCount := 0
	err := db.walkTableLeaves(t.rootPage, func(leaf *page) error {
		// A leaf page holds one cell for every row.
		rowCount += leaf.cellCount
		return nil
	})
	return rowCount, err
}
