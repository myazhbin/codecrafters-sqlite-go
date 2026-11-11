package main

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// collectRows scans a table and returns the values of all the columns of
// every row.
func collectRows(t *testing.T, db *database, table *table) [][]any {
	t.Helper()

	var rows [][]any
	err := db.scanTable(table, func(r row) error {
		values := make([]any, len(table.columns))
		for i := range table.columns {
			values[i] = r.value(i)
		}
		rows = append(rows, values)
		return nil
	})
	if err != nil {
		t.Fatalf("scanTable returned an error: %v", err)
	}
	return rows
}

func TestFindTable(t *testing.T) {
	db := openDatabase(t, sampleDB)

	got, err := db.findTable("apples")
	if err != nil {
		t.Fatalf("findTable returned an error: %v", err)
	}

	want := &table{
		name:        "apples",
		rootPage:    2,
		columns:     columns("id", "INTEGER", "name", "TEXT", "color", "TEXT"),
		rowIDColumn: 0,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("findTable = %+v, want %+v", got, want)
	}
}

func TestFindTableIgnoresCase(t *testing.T) {
	db := openDatabase(t, sampleDB)

	for _, name := range []string{"oranges", "ORANGES", "Oranges", "oRaNgEs"} {
		table, err := db.findTable(name)
		if err != nil {
			t.Errorf("findTable(%q) returned an error: %v", name, err)
			continue
		}
		// The name of the table is the one that the schema stores.
		if table.name != "oranges" || table.rootPage != 4 {
			t.Errorf("findTable(%q) = table %q with root page %d, want oranges with root page 4",
				name, table.name, table.rootPage)
		}
	}
}

func TestFindTableRejectsUnknownTables(t *testing.T) {
	db := openDatabase(t, sampleDB)

	for _, name := range []string{"pears", "apple", "apples2", ""} {
		_, err := db.findTable(name)
		if err == nil {
			t.Errorf("findTable(%q) returned no error", name)
			continue
		}
		if want := "no such table: " + name; err.Error() != want {
			t.Errorf("findTable(%q) returned error %q, want %q", name, err, want)
		}
	}
}

func TestFindTableReportsInvalidDefinitions(t *testing.T) {
	data := buildDatabase(512, testPage{pageType: pageTypeLeafTable, cells: [][]byte{
		encodeTableLeafCell(1, "table", "broken", "broken", 2, "CREATE TABLE broken (a,"),
	}})

	_, err := newTestDatabase(t, data).findTable("broken")
	if err == nil {
		t.Fatal("findTable returned no error")
	}
	if !strings.Contains(err.Error(), "broken") {
		t.Errorf("error %q doesn't name the table", err)
	}
}

func TestTableColumnIndex(t *testing.T) {
	table := &table{name: "t", columns: columns("id", "INTEGER", "Name", "TEXT", "size range", "TEXT")}

	tests := []struct {
		name string
		want int
	}{
		{"id", 0},
		{"Name", 1},
		{"name", 1},
		{"NAME", 1},
		{"size range", 2},
		{"SIZE RANGE", 2},
	}
	for _, tt := range tests {
		got, err := table.columnIndex(tt.name)
		if err != nil {
			t.Errorf("columnIndex(%q) returned an error: %v", tt.name, err)
			continue
		}
		if got != tt.want {
			t.Errorf("columnIndex(%q) = %d, want %d", tt.name, got, tt.want)
		}
	}

	for _, name := range []string{"color", "nam", "names", "size", ""} {
		_, err := table.columnIndex(name)
		if err == nil {
			t.Errorf("columnIndex(%q) returned no error", name)
			continue
		}
		if want := "no such column: " + name; err.Error() != want {
			t.Errorf("columnIndex(%q) returned error %q, want %q", name, err, want)
		}
	}
}

func TestRowValue(t *testing.T) {
	table := &table{
		name:        "t",
		columns:     columns("id", "INTEGER", "name", "TEXT", "price", "REAL", "stock", "INTEGER", "added", "TEXT"),
		rowIDColumn: 0,
	}
	// The record of a row holds NULL for the rowid alias, and has no value
	// for the column that was added to the table after the row was written.
	r := row{table: table, rowID: 42, values: []any{nil, "apple", int64(3), int64(7)}}

	tests := []struct {
		column string
		index  int
		want   any
	}{
		{"rowid alias", 0, int64(42)},
		{"text", 1, "apple"},
		// An integer in a column with REAL affinity is a floating point
		// number that SQLite stored as an integer to save space.
		{"integer in a REAL column", 2, 3.0},
		{"integer", 3, int64(7)},
		{"column added later", 4, nil},
	}
	for _, tt := range tests {
		if got := r.value(tt.index); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s: value(%d) = %#v, want %#v", tt.column, tt.index, got, tt.want)
		}
	}
}

func TestRowValueKeepsStoredValues(t *testing.T) {
	table := &table{name: "t", columns: columns("a", "REAL", "b", "", "c", "TEXT"), rowIDColumn: -1}
	// SQLite is dynamically typed: any column can hold a value of any type.
	values := []any{2.5, []byte{0x01}, nil}
	r := row{table: table, rowID: 1, values: values}

	for i, want := range values {
		if got := r.value(i); !reflect.DeepEqual(got, want) {
			t.Errorf("value(%d) = %#v, want %#v", i, got, want)
		}
	}
}

func TestScanTableOfSampleDatabase(t *testing.T) {
	db := openDatabase(t, sampleDB)
	table, err := db.findTable("apples")
	if err != nil {
		t.Fatalf("findTable returned an error: %v", err)
	}

	got := collectRows(t, db, table)

	want := [][]any{
		{int64(1), "Granny Smith", "Light Green"},
		{int64(2), "Fuji", "Red"},
		{int64(3), "Honeycrisp", "Blush Red"},
		{int64(4), "Golden Delicious", "Yellow"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("rows = %#v, want %#v", got, want)
	}
}

func TestScanTable(t *testing.T) {
	data := buildDatabase(512,
		testPage{pageType: pageTypeLeafTable},
		testPage{pageType: pageTypeLeafTable, cells: [][]byte{
			encodeTableLeafCell(1, nil, "apple", 1.5),
			encodeTableLeafCell(5, nil, nil, 2),
			// A row that was written before the price column was added.
			encodeTableLeafCell(900, nil, "cherry"),
		}},
		testPage{pageType: pageTypeLeafTable},
	)
	db := newTestDatabase(t, data)
	fruits := &table{
		name:        "fruits",
		rootPage:    2,
		columns:     columns("id", "INTEGER", "name", "TEXT", "price", "REAL"),
		rowIDColumn: 0,
	}

	got := collectRows(t, db, fruits)

	want := [][]any{
		{int64(1), "apple", 1.5},
		{int64(5), nil, 2.0},
		{int64(900), "cherry", nil},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("rows = %#v, want %#v", got, want)
	}

	// A table without rows has an empty root page.
	empty := &table{name: "empty", rootPage: 3, columns: columns("a", "TEXT"), rowIDColumn: -1}
	if rows := collectRows(t, db, empty); len(rows) != 0 {
		t.Errorf("rows of an empty table = %#v, want none", rows)
	}
}

func TestScanTableStopsWhenVisitFails(t *testing.T) {
	db := openDatabase(t, sampleDB)
	table, err := db.findTable("oranges")
	if err != nil {
		t.Fatalf("findTable returned an error: %v", err)
	}

	errStop := errors.New("stop")
	visited := 0
	err = db.scanTable(table, func(row) error {
		visited++
		if visited == 2 {
			return errStop
		}
		return nil
	})

	if err != errStop {
		t.Errorf("scanTable returned error %v, want %v", err, errStop)
	}
	if visited != 2 {
		t.Errorf("visited %d rows, want 2", visited)
	}
}

func TestScanTableRejectsInvalidTables(t *testing.T) {
	data := buildDatabase(512,
		testPage{pageType: pageTypeLeafTable},
		testPage{pageType: pageTypeInteriorTable},
		testPage{pageType: pageTypeLeafTable, cells: [][]byte{
			encodeTableLeafCell(1, "fine"),
			// A payload of 2 bytes whose record header claims 9 bytes.
			{0x02, 0x02, 0x09, 0x00},
		}},
	)
	db := newTestDatabase(t, data)

	tests := []struct {
		name     string
		rootPage int
	}{
		{"table that spans multiple pages", 2},
		{"row with an invalid record", 3},
		{"root page past the end of the file", 4},
		{"root page zero", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			table := &table{name: "t", rootPage: tt.rootPage, columns: columns("a", "TEXT"), rowIDColumn: -1}
			err := db.scanTable(table, func(row) error { return nil })
			if err == nil {
				t.Error("scanTable returned no error")
			}
		})
	}
}

func TestCountRowsOfTable(t *testing.T) {
	db := openDatabase(t, sampleDB)

	for name, want := range map[string]int{"apples": 4, "oranges": 6, "sqlite_sequence": 2} {
		table, err := db.findTable(name)
		if err != nil {
			t.Fatalf("findTable(%q) returned an error: %v", name, err)
		}
		got, err := db.countRows(table)
		if err != nil {
			t.Fatalf("countRows(%q) returned an error: %v", name, err)
		}
		if got != want {
			t.Errorf("countRows(%q) = %d, want %d", name, got, want)
		}
	}
}
