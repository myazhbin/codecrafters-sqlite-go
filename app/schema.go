package main

import (
	"errors"
	"fmt"
	"strings"
)

// schemaPageNumber is the number of the root page of the sqlite_schema table.
const schemaPageNumber = 1

// internalNamePrefix is the prefix of the names that SQLite reserves for its
// internal schema objects, like the sqlite_sequence table.
const internalNamePrefix = "sqlite_"

// schemaObject is a row of the sqlite_schema table. It describes a table,
// index, view or trigger of the database.
type schemaObject struct {
	// kind is the type of the object: "table", "index", "view" or "trigger".
	kind string
	// name is the name of the object.
	name string
	// tableName is the name of the table or view that the object is
	// associated with. For a table or a view, that's its own name.
	tableName string
	// rootPage is the number of the root b-tree page of a table or an index.
	// It is 0 for objects that don't have a b-tree, such as views.
	rootPage int
	// sql is the SQL text that describes the object, such as a CREATE TABLE
	// statement. It is empty for indexes that SQLite creates automatically.
	sql string
}

// isInternal reports whether the object is one that SQLite creates for its
// own use.
func (o schemaObject) isInternal() bool {
	return len(o.name) >= len(internalNamePrefix) &&
		strings.EqualFold(o.name[:len(internalNamePrefix)], internalNamePrefix)
}

// readSchema reads all the rows of the sqlite_schema table.
func (db *database) readSchema() ([]schemaObject, error) {
	p, err := db.readPage(schemaPageNumber)
	if err != nil {
		return nil, err
	}
	if p.pageType != pageTypeLeafTable {
		return nil, errors.New("a sqlite_schema table that spans multiple pages is not supported")
	}

	objects := make([]schemaObject, 0, p.cellCount)
	for i := 0; i < p.cellCount; i++ {
		cell, err := p.tableLeafCell(i)
		if err != nil {
			return nil, err
		}
		values, err := decodeRecord(cell.payload)
		if err != nil {
			return nil, fmt.Errorf("failed to decode row %d of sqlite_schema: %w", cell.rowID, err)
		}
		object, err := newSchemaObject(values)
		if err != nil {
			return nil, fmt.Errorf("invalid row %d of sqlite_schema: %w", cell.rowID, err)
		}
		objects = append(objects, object)
	}
	return objects, nil
}

// newSchemaObject builds a schemaObject from the values of the columns of a
// sqlite_schema row: type, name, tbl_name, rootpage and sql.
func newSchemaObject(values []any) (schemaObject, error) {
	if len(values) < 5 {
		return schemaObject{}, fmt.Errorf("got %d columns, want 5", len(values))
	}

	var object schemaObject
	texts := []struct {
		column string
		dest   *string
	}{
		{"type", &object.kind},
		{"name", &object.name},
		{"tbl_name", &object.tableName},
	}
	for i, text := range texts {
		value, ok := values[i].(string)
		if !ok {
			return schemaObject{}, fmt.Errorf("column %s is not text", text.column)
		}
		*text.dest = value
	}

	switch rootPage := values[3].(type) {
	case nil:
	case int64:
		object.rootPage = int(rootPage)
	default:
		return schemaObject{}, errors.New("column rootpage is not an integer")
	}

	switch sql := values[4].(type) {
	case nil:
	case string:
		object.sql = sql
	default:
		return schemaObject{}, errors.New("column sql is not text")
	}

	return object, nil
}
