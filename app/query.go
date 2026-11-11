package main

import (
	"fmt"
	"io"
)

// executeSQL runs a SQL statement against db and writes its result to stdout.
func executeSQL(db *database, sql string, stdout io.Writer) error {
	statement, err := parseSelect(sql)
	if err != nil {
		return err
	}

	table, err := db.findTable(statement.table)
	if err != nil {
		return err
	}
	rowCount, err := db.countRows(table)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintln(stdout, rowCount)
	return err
}

// countRows returns the number of rows in a table.
func (db *database) countRows(table schemaObject) (int, error) {
	root, err := db.readPage(table.rootPage)
	if err != nil {
		return 0, err
	}
	// A table that fits on a single page is stored on a leaf page, which holds
	// one cell for every row.
	if root.pageType != pageTypeLeafTable {
		return 0, fmt.Errorf("table %s spans multiple pages, which is not supported", table.name)
	}
	return root.cellCount, nil
}
