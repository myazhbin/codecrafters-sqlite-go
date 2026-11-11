package main

import (
	"reflect"
	"testing"
)

// applesSchemaSQL is the CREATE TABLE statement of the apples table of the
// sample database.
const applesSchemaSQL = "CREATE TABLE apples\n(\n\tid integer primary key autoincrement,\n\tname text,\n\tcolor text\n)"

func TestReadSchemaOfSampleDatabase(t *testing.T) {
	got, err := openDatabase(t, sampleDB).readSchema()
	if err != nil {
		t.Fatalf("readSchema returned an error: %v", err)
	}

	want := []schemaObject{
		{kind: "table", name: "apples", tableName: "apples", rootPage: 2, sql: applesSchemaSQL},
		{
			kind: "table", name: "sqlite_sequence", tableName: "sqlite_sequence", rootPage: 3,
			sql: "CREATE TABLE sqlite_sequence(name,seq)",
		},
		{kind: "table", name: "oranges", tableName: "oranges", rootPage: 4, sql: orangesSchemaSQL},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("readSchema = %+v, want %+v", got, want)
	}
}

func TestReadSchemaReadsEveryKindOfObject(t *testing.T) {
	path := createDatabase(t, `
		CREATE TABLE fruits (id integer primary key, name text unique);
		CREATE INDEX idx_fruits_name ON fruits (name);
		CREATE VIEW fruit_names AS SELECT name FROM fruits;
		CREATE TRIGGER fruits_insert AFTER INSERT ON fruits BEGIN SELECT 1; END;
	`)

	got, err := openDatabase(t, path).readSchema()
	if err != nil {
		t.Fatalf("readSchema returned an error: %v", err)
	}

	want := []schemaObject{
		{
			kind: "table", name: "fruits", tableName: "fruits", rootPage: 2,
			sql: "CREATE TABLE fruits (id integer primary key, name text unique)",
		},
		// SQLite creates this index to enforce the UNIQUE constraint. It has
		// no SQL text.
		{kind: "index", name: "sqlite_autoindex_fruits_1", tableName: "fruits", rootPage: 3},
		{
			kind: "index", name: "idx_fruits_name", tableName: "fruits", rootPage: 4,
			sql: "CREATE INDEX idx_fruits_name ON fruits (name)",
		},
		// Views and triggers have no b-tree, and so no root page.
		{
			kind: "view", name: "fruit_names", tableName: "fruit_names", rootPage: 0,
			sql: "CREATE VIEW fruit_names AS SELECT name FROM fruits",
		},
		{
			kind: "trigger", name: "fruits_insert", tableName: "fruits", rootPage: 0,
			sql: "CREATE TRIGGER fruits_insert AFTER INSERT ON fruits BEGIN SELECT 1; END",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("readSchema = %+v, want %+v", got, want)
	}
}

func TestReadSchemaOfDatabaseWithoutObjects(t *testing.T) {
	data := buildDatabase(512, testPage{pageType: pageTypeLeafTable})

	got, err := newTestDatabase(t, data).readSchema()
	if err != nil {
		t.Fatalf("readSchema returned an error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("readSchema = %+v, want no objects", got)
	}
}

func TestReadSchemaRejectsInvalidSchemas(t *testing.T) {
	tests := []struct {
		name string
		page testPage
	}{
		{
			"schema that spans multiple pages",
			testPage{pageType: pageTypeInteriorTable},
		},
		{
			"row with a missing column",
			testPage{pageType: pageTypeLeafTable, cells: [][]byte{
				encodeTableLeafCell(1, "table", "apples", "apples", 2),
			}},
		},
		{
			"row with a type that isn't text",
			testPage{pageType: pageTypeLeafTable, cells: [][]byte{
				encodeTableLeafCell(1, 7, "apples", "apples", 2, "CREATE TABLE apples (id)"),
			}},
		},
		{
			"row with a name that isn't text",
			testPage{pageType: pageTypeLeafTable, cells: [][]byte{
				encodeTableLeafCell(1, "table", nil, "apples", 2, "CREATE TABLE apples (id)"),
			}},
		},
		{
			"row with a table name that isn't text",
			testPage{pageType: pageTypeLeafTable, cells: [][]byte{
				encodeTableLeafCell(1, "table", "apples", []byte("apples"), 2, "CREATE TABLE apples (id)"),
			}},
		},
		{
			"row with a root page that isn't an integer",
			testPage{pageType: pageTypeLeafTable, cells: [][]byte{
				encodeTableLeafCell(1, "table", "apples", "apples", "2", "CREATE TABLE apples (id)"),
			}},
		},
		{
			"row with SQL that isn't text",
			testPage{pageType: pageTypeLeafTable, cells: [][]byte{
				encodeTableLeafCell(1, "table", "apples", "apples", 2, 3.5),
			}},
		},
		{
			"row with an invalid record",
			testPage{pageType: pageTypeLeafTable, cells: [][]byte{
				// A payload of 2 bytes whose record header claims 9 bytes.
				{0x02, 0x01, 0x09, 0x00},
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := buildDatabase(512, tt.page)
			if objects, err := newTestDatabase(t, data).readSchema(); err == nil {
				t.Errorf("readSchema = %+v, want an error", objects)
			}
		})
	}
}

func TestReadSchemaOfBuiltDatabase(t *testing.T) {
	data := buildDatabase(512, testPage{pageType: pageTypeLeafTable, cells: [][]byte{
		encodeTableLeafCell(1, "table", "apples", "apples", 2, "CREATE TABLE apples (id)"),
		// Views are stored with a root page of 0, and automatic indexes
		// without SQL text.
		encodeTableLeafCell(2, "view", "v", "v", 0, "CREATE VIEW v AS SELECT 1"),
		encodeTableLeafCell(3, "index", "sqlite_autoindex_apples_1", "apples", 300, nil),
		// Columns that newer versions of SQLite may add are ignored.
		encodeTableLeafCell(4, "table", "pears", "pears", nil, nil, "extra"),
	}})

	got, err := newTestDatabase(t, data).readSchema()
	if err != nil {
		t.Fatalf("readSchema returned an error: %v", err)
	}

	want := []schemaObject{
		{kind: "table", name: "apples", tableName: "apples", rootPage: 2, sql: "CREATE TABLE apples (id)"},
		{kind: "view", name: "v", tableName: "v", rootPage: 0, sql: "CREATE VIEW v AS SELECT 1"},
		{kind: "index", name: "sqlite_autoindex_apples_1", tableName: "apples", rootPage: 300},
		{kind: "table", name: "pears", tableName: "pears"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("readSchema = %+v, want %+v", got, want)
	}
}

func TestSchemaObjectIsInternal(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"sqlite_sequence", true},
		{"sqlite_stat1", true},
		{"sqlite_autoindex_apples_1", true},
		{"SQLITE_SEQUENCE", true},
		{"Sqlite_Stat4", true},
		{"sqlite_", true},
		{"apples", false},
		{"sqlite", false},
		{"sqlitedata", false},
		{"my_sqlite_table", false},
		{"", false},
	}

	for _, tt := range tests {
		if got := (schemaObject{name: tt.name}).isInternal(); got != tt.want {
			t.Errorf("schemaObject{name: %q}.isInternal() = %t, want %t", tt.name, got, tt.want)
		}
	}
}
