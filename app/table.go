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
	// indexes holds the indexes on the table that have an entry for every
	// row of the table.
	indexes []index
}

// index is an index on a table.
type index struct {
	// name is the name of the index.
	name string
	// rootPage is the number of the root page of the index's b-tree.
	rootPage int
	// columns holds the indexed columns, in the order in which their values
	// are stored in the entries of the index.
	columns []indexedColumn
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
			indexes:     tableIndexes(schema, object.name),
		}, nil
	}
	return nil, fmt.Errorf("no such table: %s", name)
}

// tableIndexes returns the indexes on the table with the given name that have
// an entry for every row of the table. Indexes aren't needed to answer a
// query, so the ones that can't be used are left out: the partial indexes,
// which only cover some of the rows, and the indexes whose definitions are
// missing or can't be parsed.
func tableIndexes(schema []schemaObject, tableName string) []index {
	var indexes []index
	for _, object := range schema {
		if object.kind != "index" || !strings.EqualFold(object.tableName, tableName) {
			continue
		}
		definition, err := parseCreateIndex(object.sql)
		if err != nil || definition.partial {
			continue
		}
		indexes = append(indexes, index{name: object.name, rootPage: object.rootPage, columns: definition.columns})
	}
	return indexes
}

// findIndex returns an index that can be searched for the rows whose column
// with the given index equals some value, when TEXT values are compared with
// the given collating sequence. It returns nil if the table has no such
// index.
func (t *table) findIndex(column int, collation string) *index {
	for i, index := range t.indexes {
		// An index is ordered by its first column, so that's the only column
		// it can be searched by. The search expects the values of the column
		// to be in ascending order of the collating sequence. An index that
		// names no collating sequence uses the one of its column.
		first := index.columns[0]
		if !strings.EqualFold(first.name, t.columns[column].name) || first.descending {
			continue
		}
		if first.collation == "" || first.collation == collation {
			return &t.indexes[i]
		}
	}
	return nil
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

// findRow looks up the row of a table with the given rowid.
func (db *database) findRow(t *table, rowID int64) (row, error) {
	values, found, err := db.findTableRecord(t.rootPage, rowID)
	if err != nil {
		return row{}, err
	}
	if !found {
		return row{}, fmt.Errorf("table %s has no row with rowid %d", t.name, rowID)
	}
	return row{table: t, rowID: rowID, values: values}, nil
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
