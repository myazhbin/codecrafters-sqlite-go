package main

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// leafPage returns a table leaf page that holds one row, with a single text
// column, for each of the given rowids.
func leafPage(rowIDs ...int64) testPage {
	p := testPage{pageType: pageTypeLeafTable}
	for _, rowID := range rowIDs {
		p.cells = append(p.cells, encodeTableLeafCell(rowID, fmt.Sprintf("row %d", rowID)))
	}
	return p
}

// interiorPage returns a table interior page with the given right-most child
// and cells, which are pairs of a child page number and a key.
func interiorPage(rightMostChild uint32, childrenAndKeys ...int64) testPage {
	p := testPage{pageType: pageTypeInteriorTable, rightMostChild: rightMostChild}
	for i := 0; i < len(childrenAndKeys); i += 2 {
		p.cells = append(p.cells, encodeTableInteriorCell(uint32(childrenAndKeys[i]), childrenAndKeys[i+1]))
	}
	return p
}

// collectLeaves returns the numbers of the leaf pages of the table b-tree
// with the given root page, in the order in which walkTableLeaves visits
// them.
func collectLeaves(t *testing.T, db *database, rootPage int) []int {
	t.Helper()

	var leaves []int
	err := db.walkTableLeaves(rootPage, func(leaf *page) error {
		leaves = append(leaves, leaf.number)
		return nil
	})
	if err != nil {
		t.Fatalf("walkTableLeaves(%d) returned an error: %v", rootPage, err)
	}
	return leaves
}

// collectRowIDs returns the rowids of the rows of the table b-tree with the
// given root page, in the order in which scanTableBTree visits them.
func collectRowIDs(t *testing.T, db *database, rootPage int) []int64 {
	t.Helper()

	var rowIDs []int64
	err := db.scanTableBTree(rootPage, func(rowID int64, values []any) error {
		if want := []any{fmt.Sprintf("row %d", rowID)}; !reflect.DeepEqual(values, want) {
			t.Errorf("row %d has values %#v, want %#v", rowID, values, want)
		}
		rowIDs = append(rowIDs, rowID)
		return nil
	})
	if err != nil {
		t.Fatalf("scanTableBTree(%d) returned an error: %v", rootPage, err)
	}
	return rowIDs
}

func TestWalkTableLeavesOfSingleLeaf(t *testing.T) {
	db := newTestDatabase(t, buildDatabase(512,
		testPage{pageType: pageTypeLeafTable},
		leafPage(1, 2, 3),
	))

	if got, want := collectLeaves(t, db, 2), []int{2}; !reflect.DeepEqual(got, want) {
		t.Errorf("leaves = %v, want %v", got, want)
	}
	if got, want := collectRowIDs(t, db, 2), []int64{1, 2, 3}; !reflect.DeepEqual(got, want) {
		t.Errorf("rowids = %v, want %v", got, want)
	}
}

func TestWalkTableLeavesOfTwoLevelTree(t *testing.T) {
	// The root page points to its leaves out of page order, to check that
	// they are visited in the order of the cells, with the right-most child
	// last.
	db := newTestDatabase(t, buildDatabase(512,
		testPage{pageType: pageTypeLeafTable},
		interiorPage(3, 5, 2, 4, 10), // Page 2: the root page.
		leafPage(11, 12, 500),        // Page 3: the right-most child.
		leafPage(3, 7, 10),           // Page 4.
		leafPage(1, 2),               // Page 5.
	))

	if got, want := collectLeaves(t, db, 2), []int{5, 4, 3}; !reflect.DeepEqual(got, want) {
		t.Errorf("leaves = %v, want %v", got, want)
	}
	if got, want := collectRowIDs(t, db, 2), []int64{1, 2, 3, 7, 10, 11, 12, 500}; !reflect.DeepEqual(got, want) {
		t.Errorf("rowids = %v, want %v", got, want)
	}
}

func TestWalkTableLeavesOfThreeLevelTree(t *testing.T) {
	db := newTestDatabase(t, buildDatabase(512,
		testPage{pageType: pageTypeLeafTable},
		interiorPage(4, 3, 20),        // Page 2: the root page.
		interiorPage(6, 5, 10),        // Page 3: rowids up to 20.
		interiorPage(9, 7, 30, 8, 40), // Page 4: rowids above 20.
		leafPage(1, 10),               // Page 5.
		leafPage(15, 20),              // Page 6.
		leafPage(21, 30),              // Page 7.
		leafPage(40),                  // Page 8.
		leafPage(41, 42, 43),          // Page 9.
		// An interior page without cells has nothing but a right-most child.
		interiorPage(11), // Page 10.
		leafPage(),       // Page 11: a leaf page without rows.
	))

	if got, want := collectLeaves(t, db, 2), []int{5, 6, 7, 8, 9}; !reflect.DeepEqual(got, want) {
		t.Errorf("leaves = %v, want %v", got, want)
	}
	want := []int64{1, 10, 15, 20, 21, 30, 40, 41, 42, 43}
	if got := collectRowIDs(t, db, 2); !reflect.DeepEqual(got, want) {
		t.Errorf("rowids = %v, want %v", got, want)
	}

	if got, want := collectLeaves(t, db, 10), []int{11}; !reflect.DeepEqual(got, want) {
		t.Errorf("leaves under page 10 = %v, want %v", got, want)
	}
	if got := collectRowIDs(t, db, 10); len(got) != 0 {
		t.Errorf("rowids under page 10 = %v, want none", got)
	}
}

func TestWalkTableLeavesStopsWhenVisitFails(t *testing.T) {
	db := newTestDatabase(t, buildDatabase(512,
		testPage{pageType: pageTypeLeafTable},
		interiorPage(5, 3, 2, 4, 4),
		leafPage(1, 2),
		leafPage(3, 4),
		leafPage(5, 6),
	))
	errStop := errors.New("stop")

	var leaves []int
	err := db.walkTableLeaves(2, func(leaf *page) error {
		leaves = append(leaves, leaf.number)
		if leaf.number == 4 {
			return errStop
		}
		return nil
	})
	if err != errStop {
		t.Errorf("walkTableLeaves returned error %v, want %v", err, errStop)
	}
	if want := []int{3, 4}; !reflect.DeepEqual(leaves, want) {
		t.Errorf("visited leaves %v, want %v", leaves, want)
	}

	var rowIDs []int64
	err = db.scanTableBTree(2, func(rowID int64, values []any) error {
		rowIDs = append(rowIDs, rowID)
		if rowID == 3 {
			return errStop
		}
		return nil
	})
	if err != errStop {
		t.Errorf("scanTableBTree returned error %v, want %v", err, errStop)
	}
	if want := []int64{1, 2, 3}; !reflect.DeepEqual(rowIDs, want) {
		t.Errorf("visited rows %v, want %v", rowIDs, want)
	}
}

func TestWalkTableLeavesRejectsCorruptTrees(t *testing.T) {
	tests := []struct {
		name  string
		pages []testPage // The pages that follow page 1. The root page is page 2.
	}{
		{
			"interior page that points to itself",
			[]testPage{interiorPage(2)},
		},
		{
			"interior page whose cell points to itself",
			[]testPage{interiorPage(3, 2, 10), leafPage(11)},
		},
		{
			"interior pages that point to each other",
			[]testPage{interiorPage(3), interiorPage(2)},
		},
		{
			"right-most child of zero",
			[]testPage{interiorPage(0, 3, 10), leafPage(1)},
		},
		{
			"left child of zero",
			[]testPage{interiorPage(3, 0, 10), leafPage(11)},
		},
		{
			"child past the end of the file",
			[]testPage{interiorPage(4, 3, 10), leafPage(1)},
		},
		{
			"child that is an index page",
			[]testPage{interiorPage(3), {pageType: pageTypeLeafIndex}},
		},
		{
			"root that is an index page",
			[]testPage{{pageType: pageTypeInteriorIndex}},
		},
		{
			"interior cell that is cut short",
			// The cell is stored at the end of the page, where there is room
			// for its child page number but not for its key.
			[]testPage{{pageType: pageTypeInteriorTable, rightMostChild: 3, cells: [][]byte{{0, 0, 0, 3}}}, leafPage(1)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pages := append([]testPage{{pageType: pageTypeLeafTable}}, tt.pages...)
			db := newTestDatabase(t, buildDatabase(512, pages...))

			err := db.walkTableLeaves(2, func(*page) error { return nil })
			if err == nil {
				t.Error("walkTableLeaves returned no error")
			}
		})
	}
}

func TestScanTableBTreeReportsInvalidRecords(t *testing.T) {
	db := newTestDatabase(t, buildDatabase(512,
		testPage{pageType: pageTypeLeafTable},
		interiorPage(4, 3, 2),
		leafPage(1, 2),
		testPage{pageType: pageTypeLeafTable, cells: [][]byte{
			encodeTableLeafCell(3, "fine"),
			// A payload of 2 bytes whose record header claims 9 bytes.
			{0x02, 0x04, 0x09, 0x00},
		}},
	))

	var rowIDs []int64
	err := db.scanTableBTree(2, func(rowID int64, values []any) error {
		rowIDs = append(rowIDs, rowID)
		return nil
	})

	if err == nil {
		t.Fatal("scanTableBTree returned no error")
	}
	if !strings.Contains(err.Error(), "page 4, cell 1") {
		t.Errorf("error %q doesn't say where the invalid record is", err)
	}
	if want := []int64{1, 2, 3}; !reflect.DeepEqual(rowIDs, want) {
		t.Errorf("visited rows %v before failing, want %v", rowIDs, want)
	}
}

// TestScanTableBTreeOfGeneratedTables scans tables that SQLite built, with
// b-trees of up to four levels.
func TestScanTableBTreeOfGeneratedTables(t *testing.T) {
	tests := []struct {
		pageSize int
		rowCount int
	}{
		{512, 1},
		{512, 100},
		{512, 5000},
		{512, 100000},
		{1024, 5000},
		{4096, 5000},
		{65536, 30000},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d rows in pages of %d bytes", tt.rowCount, tt.pageSize), func(t *testing.T) {
			path := createDatabase(t, fmt.Sprintf(`
				PRAGMA page_size = %d;
				CREATE TABLE numbers (id INTEGER PRIMARY KEY, label TEXT);
				WITH RECURSIVE sequence (n) AS (SELECT 1 UNION ALL SELECT n + 1 FROM sequence WHERE n < %d)
				INSERT INTO numbers SELECT n * 3, 'number ' || n FROM sequence;
			`, tt.pageSize, tt.rowCount))
			db := openDatabase(t, path)
			table, err := db.findTable("numbers")
			if err != nil {
				t.Fatalf("findTable returned an error: %v", err)
			}

			next := int64(1)
			err = db.scanTableBTree(table.rootPage, func(rowID int64, values []any) error {
				want := []any{nil, fmt.Sprintf("number %d", next)}
				if rowID != next*3 || !reflect.DeepEqual(values, want) {
					return fmt.Errorf("row %d is (%d, %#v), want (%d, %#v)", next, rowID, values, next*3, want)
				}
				next++
				return nil
			})
			if err != nil {
				t.Fatalf("scanTableBTree returned an error: %v", err)
			}
			if got := int(next - 1); got != tt.rowCount {
				t.Errorf("scanTableBTree visited %d rows, want %d", got, tt.rowCount)
			}

			got, err := db.countRows(table)
			if err != nil {
				t.Fatalf("countRows returned an error: %v", err)
			}
			if got != tt.rowCount {
				t.Errorf("countRows = %d, want %d", got, tt.rowCount)
			}
		})
	}
}
