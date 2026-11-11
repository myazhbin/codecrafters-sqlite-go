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

func TestRowValueOfColumnsAddedLater(t *testing.T) {
	// The rows that were written before a column was added to their table
	// have no value for the column, which then has its default value.
	table := &table{
		name: "t",
		columns: []columnDefinition{
			{name: "name", declaredType: "TEXT"},
			{name: "is_admin", declaredType: "INTEGER", defaultValue: int64(0)},
			{name: "label", declaredType: "TEXT", defaultValue: "none"},
			{name: "score", declaredType: "REAL", defaultValue: int64(5)},
			{name: "notes", declaredType: "TEXT"},
		},
		rowIDColumn: -1,
	}

	tests := []struct {
		name   string
		values []any
		want   []any
	}{
		{"row from before the columns were added", []any{"old"}, []any{"old", int64(0), "none", 5.0, nil}},
		{"row from before the last columns were added", []any{"mid", int64(1), "x"}, []any{"mid", int64(1), "x", 5.0, nil}},
		// A value that is stored in the row is used, even if it's NULL.
		{"row with all the columns", []any{"new", nil, nil, nil, "n"}, []any{"new", nil, nil, nil, "n"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := row{table: table, rowID: 1, values: tt.values}
			got := make([]any, len(table.columns))
			for i := range table.columns {
				got[i] = r.value(i)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("values = %#v, want %#v", got, tt.want)
			}
		})
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
		testPage{pageType: pageTypeLeafIndex},
	)
	db := newTestDatabase(t, data)

	tests := []struct {
		name     string
		rootPage int
	}{
		{"interior page without a right-most child", 2},
		{"row with an invalid record", 3},
		{"root page of an index", 4},
		{"root page past the end of the file", 5},
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

func TestScanTableThatSpansMultiplePages(t *testing.T) {
	db := newTestDatabase(t, buildDatabase(512,
		testPage{pageType: pageTypeLeafTable},
		// Page 2 is the root page: an interior page with three child pages.
		testPage{
			pageType:       pageTypeInteriorTable,
			rightMostChild: 5,
			cells:          [][]byte{encodeTableInteriorCell(3, 2), encodeTableInteriorCell(4, 4)},
		},
		testPage{pageType: pageTypeLeafTable, cells: [][]byte{
			encodeTableLeafCell(1, nil, "apple"),
			encodeTableLeafCell(2, nil, "banana"),
		}},
		testPage{pageType: pageTypeLeafTable, cells: [][]byte{
			encodeTableLeafCell(3, nil, "cherry"),
			encodeTableLeafCell(4, nil, "date"),
		}},
		testPage{pageType: pageTypeLeafTable, cells: [][]byte{
			encodeTableLeafCell(5, nil, "elderberry"),
		}},
	))
	fruits := &table{name: "fruits", rootPage: 2, columns: columns("id", "INTEGER", "name", "TEXT"), rowIDColumn: 0}

	got := collectRows(t, db, fruits)

	want := [][]any{
		{int64(1), "apple"},
		{int64(2), "banana"},
		{int64(3), "cherry"},
		{int64(4), "date"},
		{int64(5), "elderberry"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("rows = %#v, want %#v", got, want)
	}

	count, err := db.countRows(fruits)
	if err != nil {
		t.Fatalf("countRows returned an error: %v", err)
	}
	if count != len(want) {
		t.Errorf("countRows = %d, want %d", count, len(want))
	}
}

func TestCountRowsRejectsInvalidTables(t *testing.T) {
	db := newTestDatabase(t, buildDatabase(512,
		testPage{pageType: pageTypeLeafTable},
		testPage{pageType: pageTypeInteriorTable, rightMostChild: 2},
		testPage{pageType: pageTypeLeafIndex},
	))

	for name, rootPage := range map[string]int{
		"interior page that points to itself": 2,
		"root page of an index":               3,
		"root page past the end of the file":  4,
	} {
		table := &table{name: "t", rootPage: rootPage, columns: columns("a", "TEXT"), rowIDColumn: -1}
		if count, err := db.countRows(table); err == nil {
			t.Errorf("%s: countRows = %d, want an error", name, count)
		}
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

func TestFindTableFindsIndexes(t *testing.T) {
	path := createDatabase(t, `
		CREATE TABLE fruits (id INTEGER PRIMARY KEY, name TEXT UNIQUE, color TEXT, price REAL);
		CREATE TABLE vegetables (id INTEGER PRIMARY KEY, name TEXT, color TEXT);
		CREATE INDEX idx_fruits_color ON fruits (color);
		CREATE INDEX idx_vegetables_name ON vegetables (name);
		CREATE UNIQUE INDEX idx_fruits_price ON fruits (price DESC, name COLLATE NOCASE);
		CREATE INDEX idx_fruits_lower ON fruits (lower(name));
		CREATE INDEX idx_fruits_red ON fruits (name) WHERE color = 'Red';
	`)
	db := openDatabase(t, path)

	fruits, err := db.findTable("FRUITS")
	if err != nil {
		t.Fatalf("findTable returned an error: %v", err)
	}
	// The index that SQLite created for the UNIQUE constraint has no
	// definition, and the partial index doesn't cover every row.
	want := []index{
		{name: "idx_fruits_color", rootPage: 5, columns: []indexedColumn{{name: "color"}}},
		{
			name: "idx_fruits_price", rootPage: 7,
			columns: []indexedColumn{{name: "price", descending: true}, {name: "name", collation: "NOCASE"}},
		},
		{name: "idx_fruits_lower", rootPage: 8, columns: []indexedColumn{{}}},
	}
	if !reflect.DeepEqual(fruits.indexes, want) {
		t.Errorf("indexes of fruits = %+v, want %+v", fruits.indexes, want)
	}

	vegetables, err := db.findTable("vegetables")
	if err != nil {
		t.Fatalf("findTable returned an error: %v", err)
	}
	want = []index{{name: "idx_vegetables_name", rootPage: 6, columns: []indexedColumn{{name: "name"}}}}
	if !reflect.DeepEqual(vegetables.indexes, want) {
		t.Errorf("indexes of vegetables = %+v, want %+v", vegetables.indexes, want)
	}
}

func TestFindTableSkipsIndexesWithInvalidDefinitions(t *testing.T) {
	data := buildDatabase(1024, testPage{pageType: pageTypeLeafTable, cells: [][]byte{
		encodeTableLeafCell(1, "table", "fruits", "fruits", 2, "CREATE TABLE fruits (name TEXT, color TEXT)"),
		encodeTableLeafCell(2, "index", "broken", "fruits", 3, "CREATE INDEX broken ON fruits (name"),
		encodeTableLeafCell(3, "index", "sqlite_autoindex_fruits_1", "fruits", 4, nil),
		encodeTableLeafCell(4, "index", "idx_color", "Fruits", 5, "CREATE INDEX idx_color ON Fruits (color)"),
		encodeTableLeafCell(5, "index", "idx_other", "others", 6, "CREATE INDEX idx_other ON others (name)"),
	}})

	table, err := newTestDatabase(t, data).findTable("fruits")
	if err != nil {
		t.Fatalf("findTable returned an error: %v", err)
	}

	want := []index{{name: "idx_color", rootPage: 5, columns: []indexedColumn{{name: "color"}}}}
	if !reflect.DeepEqual(table.indexes, want) {
		t.Errorf("indexes = %+v, want %+v", table.indexes, want)
	}
}

func TestTableFindIndex(t *testing.T) {
	fruits := &table{
		name: "fruits",
		columns: []columnDefinition{
			{name: "id", declaredType: "INTEGER"},
			{name: "name", declaredType: "TEXT"},
			{name: "color", declaredType: "TEXT"},
			{name: "nickname", declaredType: "TEXT", collation: "NOCASE"},
			{name: "price", declaredType: "REAL"},
			{name: "origin", declaredType: "TEXT"},
			{name: "stock", declaredType: "INTEGER"},
		},
		rowIDColumn: 0,
		indexes: []index{
			{name: "idx_expression", columns: []indexedColumn{{}}},
			{name: "idx_price_descending", columns: []indexedColumn{{name: "price", descending: true}}},
			{name: "idx_name_nocase", columns: []indexedColumn{{name: "name", collation: "NOCASE"}}},
			{name: "idx_name", columns: []indexedColumn{{name: "NAME"}, {name: "color"}}},
			{name: "idx_name_again", columns: []indexedColumn{{name: "name"}}},
			{name: "idx_color_binary", columns: []indexedColumn{{name: "color", collation: "BINARY"}}},
			{name: "idx_nickname", columns: []indexedColumn{{name: "nickname"}}},
			{name: "idx_origin_rtrim", columns: []indexedColumn{{name: "origin", collation: "RTRIM"}}},
			{name: "idx_stock_color", columns: []indexedColumn{{name: "stock"}, {name: "color", descending: true}}},
		},
	}

	tests := []struct {
		name      string
		column    string
		collation string
		want      string // The name of the index, or "" for no index.
	}{
		// The first index whose first column is the column is used, as long
		// as it is ordered by the collating sequence of the comparison.
		{"first of several indexes", "name", collationBinary, "idx_name"},
		{"index with another collating sequence", "name", collationNoCase, "idx_name_nocase"},
		{"index that names the collating sequence", "color", collationBinary, "idx_color_binary"},
		// An index that doesn't name a collating sequence uses the one of
		// its column.
		{"index that uses the collating sequence of its column", "nickname", collationNoCase, "idx_nickname"},
		{"index with other columns after the first", "stock", collationBinary, "idx_stock_color"},

		{"index with only another collating sequence", "origin", collationBinary, ""},
		{"index in descending order", "price", collationBinary, ""},
		{"column without an index", "id", collationBinary, ""},
		// The color column is the second column of idx_name, which isn't
		// ordered by color.
		{"second column of an index", "color", collationNoCase, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			column, err := fruits.columnIndex(tt.column)
			if err != nil {
				t.Fatalf("columnIndex returned an error: %v", err)
			}

			got := ""
			if index := fruits.findIndex(column, tt.collation); index != nil {
				got = index.name
			}
			if got != tt.want {
				t.Errorf("findIndex(%s, %s) = %q, want %q", tt.column, tt.collation, got, tt.want)
			}
		})
	}
}

func TestFindRow(t *testing.T) {
	db := openDatabase(t, sampleDB)
	apples, err := db.findTable("apples")
	if err != nil {
		t.Fatalf("findTable returned an error: %v", err)
	}

	r, err := db.findRow(apples, 3)
	if err != nil {
		t.Fatalf("findRow returned an error: %v", err)
	}
	got := []any{r.value(0), r.value(1), r.value(2)}
	if want := []any{int64(3), "Honeycrisp", "Blush Red"}; !reflect.DeepEqual(got, want) {
		t.Errorf("findRow(3) = %#v, want %#v", got, want)
	}

	// An index never points to a row that doesn't exist.
	for _, rowID := range []int64{0, 5, -1} {
		if r, err := db.findRow(apples, rowID); err == nil {
			t.Errorf("findRow(%d) = %+v, want an error", rowID, r)
		}
	}
}
