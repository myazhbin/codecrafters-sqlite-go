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

func TestFindTableRecord(t *testing.T) {
	db := newTestDatabase(t, buildDatabase(512,
		testPage{pageType: pageTypeLeafTable},
		interiorPage(4, 3, 20),        // Page 2: the root page of a tree of three levels.
		interiorPage(6, 5, 10),        // Page 3: rowids up to 20.
		interiorPage(9, 7, 30, 8, 40), // Page 4: rowids above 20.
		leafPage(1, 10),               // Page 5.
		leafPage(15, 20),              // Page 6.
		leafPage(21, 30),              // Page 7.
		leafPage(40),                  // Page 8.
		leafPage(41, 42, 43),          // Page 9.
		leafPage(-5, 3, 4, 8),         // Page 10: a tree of a single page.
		leafPage(),                    // Page 11: an empty tree.
	))

	tests := []struct {
		rootPage int
		present  []int64
		absent   []int64
	}{
		{
			rootPage: 2,
			present:  []int64{1, 10, 15, 20, 21, 30, 40, 41, 42, 43},
			absent:   []int64{-1, 0, 2, 9, 11, 14, 16, 19, 22, 29, 31, 39, 44, 1000},
		},
		{rootPage: 10, present: []int64{-5, 3, 4, 8}, absent: []int64{-6, 0, 2, 5, 9}},
		{rootPage: 11, absent: []int64{0, 1}},
	}

	for _, tt := range tests {
		for _, rowID := range tt.present {
			values, found, err := db.findTableRecord(tt.rootPage, rowID)
			if err != nil {
				t.Errorf("findTableRecord(%d, %d) returned an error: %v", tt.rootPage, rowID, err)
				continue
			}
			if want := []any{fmt.Sprintf("row %d", rowID)}; !found || !reflect.DeepEqual(values, want) {
				t.Errorf("findTableRecord(%d, %d) = (%#v, %t), want (%#v, true)", tt.rootPage, rowID, values, found, want)
			}
		}
		for _, rowID := range tt.absent {
			values, found, err := db.findTableRecord(tt.rootPage, rowID)
			if err != nil {
				t.Errorf("findTableRecord(%d, %d) returned an error: %v", tt.rootPage, rowID, err)
				continue
			}
			if found {
				t.Errorf("findTableRecord(%d, %d) found %#v, want no row", tt.rootPage, rowID, values)
			}
		}
	}
}

func TestFindTableRecordRejectsCorruptTrees(t *testing.T) {
	tests := []struct {
		name  string
		pages []testPage // The pages that follow page 1. The root page is page 2.
	}{
		{"interior page that points to itself", []testPage{interiorPage(2)}},
		{"interior page whose cell points to itself", []testPage{interiorPage(3, 2, 10), leafPage(11)}},
		{"child of zero", []testPage{interiorPage(0)}},
		{"child past the end of the file", []testPage{interiorPage(3)}},
		{"child that is an index page", []testPage{interiorPage(3), {pageType: pageTypeLeafIndex}}},
		{"root that is an index page", []testPage{{pageType: pageTypeInteriorIndex}}},
		{
			"row with an invalid record",
			[]testPage{{pageType: pageTypeLeafTable, cells: [][]byte{{0x02, 0x05, 0x09, 0x00}}}},
		},
		{
			"leaf cell that is cut short",
			[]testPage{{pageType: pageTypeLeafTable, cells: [][]byte{{0x05, 0x05, 0x02}}}},
		},
		{
			"interior cell that is cut short",
			[]testPage{{pageType: pageTypeInteriorTable, rightMostChild: 3, cells: [][]byte{{0, 0, 0, 3}}}, leafPage(5)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pages := append([]testPage{{pageType: pageTypeLeafTable}}, tt.pages...)
			db := newTestDatabase(t, buildDatabase(512, pages...))

			if values, found, err := db.findTableRecord(2, 5); err == nil {
				t.Errorf("findTableRecord = (%#v, %t), want an error", values, found)
			}
		})
	}
}

// TestFindTableRecordOfGeneratedTables looks up rows in tables that SQLite
// built, with b-trees of up to four levels.
func TestFindTableRecordOfGeneratedTables(t *testing.T) {
	tests := []struct {
		pageSize int
		rowCount int
	}{
		{512, 1},
		{512, 100},
		{512, 100000},
		{4096, 20000},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d rows in pages of %d bytes", tt.rowCount, tt.pageSize), func(t *testing.T) {
			// The rowids are the multiples of 3.
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

			// Look up every row in small tables, and a sample of the rows in
			// large ones.
			step := tt.rowCount/5000 + 1
			for n := 1; n <= tt.rowCount; n += step {
				values, found, err := db.findTableRecord(table.rootPage, int64(n*3))
				if err != nil {
					t.Fatalf("findTableRecord(%d) returned an error: %v", n*3, err)
				}
				if want := []any{nil, fmt.Sprintf("number %d", n)}; !found || !reflect.DeepEqual(values, want) {
					t.Fatalf("findTableRecord(%d) = (%#v, %t), want (%#v, true)", n*3, values, found, want)
				}

				for _, absent := range []int64{int64(n*3 - 1), int64(n*3 + 1)} {
					if values, found, err := db.findTableRecord(table.rootPage, absent); err != nil || found {
						t.Fatalf("findTableRecord(%d) = (%#v, %t, %v), want no row", absent, values, found, err)
					}
				}
			}

			for _, absent := range []int64{-1, 0, int64(tt.rowCount*3 + 3), 1 << 40} {
				if values, found, err := db.findTableRecord(table.rootPage, absent); err != nil || found {
					t.Errorf("findTableRecord(%d) = (%#v, %t, %v), want no row", absent, values, found, err)
				}
			}
		})
	}
}

// indexLeaf returns an index leaf page with the given entries, which are
// pairs of a key and a rowid.
func indexLeaf(keysAndRowIDs ...any) testPage {
	p := testPage{pageType: pageTypeLeafIndex}
	for i := 0; i < len(keysAndRowIDs); i += 2 {
		p.cells = append(p.cells, encodeIndexLeafCell(keysAndRowIDs[i], keysAndRowIDs[i+1]))
	}
	return p
}

// indexInterior returns an index interior page with the given right-most
// child and cells, which are triples of a child page number, a key and a
// rowid.
func indexInterior(rightMostChild uint32, childrenKeysAndRowIDs ...any) testPage {
	p := testPage{pageType: pageTypeInteriorIndex, rightMostChild: rightMostChild}
	for i := 0; i < len(childrenKeysAndRowIDs); i += 3 {
		child := uint32(childrenKeysAndRowIDs[i].(int))
		p.cells = append(p.cells, encodeIndexInteriorCell(child, childrenKeysAndRowIDs[i+1], childrenKeysAndRowIDs[i+2]))
	}
	return p
}

// searchIndexRowIDs returns the rowids of the entries of the index b-tree
// with the given root page whose key equals the given key, in the order in
// which searchIndex visits them.
func searchIndexRowIDs(t *testing.T, db *database, rootPage int, key any) []int64 {
	t.Helper()

	rowIDs := []int64{}
	err := db.searchIndex(rootPage, key, collationBinary, func(rowID int64) error {
		rowIDs = append(rowIDs, rowID)
		return nil
	})
	if err != nil {
		t.Fatalf("searchIndex(%d, %#v) returned an error: %v", rootPage, key, err)
	}
	return rowIDs
}

func TestSearchIndexOfSingleLeaf(t *testing.T) {
	db := newTestDatabase(t, buildDatabase(512,
		testPage{pageType: pageTypeLeafTable},
		// The entries of an index are ordered by key, and then by rowid.
		indexLeaf(nil, 8, 5, 12, "chad", 3, "chad", 9, "eritrea", 1, "eritrea", 4, "eritrea", 7, "tonga", 2),
		indexLeaf(),
	))

	tests := []struct {
		key  any
		want []int64
	}{
		{"chad", []int64{3, 9}},
		{"eritrea", []int64{1, 4, 7}},
		{"tonga", []int64{2}},
		{int64(5), []int64{12}},
		// Keys that sort before, between and after the entries.
		{int64(4), []int64{}},
		{"aruba", []int64{}},
		{"denmark", []int64{}},
		{"zambia", []int64{}},
		// Text is compared exactly.
		{"Chad", []int64{}},
		{"chad ", []int64{}},
		{"5", []int64{}},
		{[]byte("chad"), []int64{}},
	}
	for _, tt := range tests {
		if got := searchIndexRowIDs(t, db, 2, tt.key); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("searchIndex(%#v) visited rowids %v, want %v", tt.key, got, tt.want)
		}
	}

	if got := searchIndexRowIDs(t, db, 3, "chad"); len(got) != 0 {
		t.Errorf("searchIndex of an empty index visited rowids %v, want none", got)
	}
}

func TestSearchIndexOfTree(t *testing.T) {
	// The cells of the interior pages of an index hold entries too: they
	// aren't repeated on the leaf pages. Entries with the same key can be
	// spread over many pages.
	db := newTestDatabase(t, buildDatabase(512,
		testPage{pageType: pageTypeLeafTable},
		// Page 2: the root page.
		indexInterior(4, 3, "eritrea", 30),
		// Page 3: the entries that sort before ("eritrea", 30).
		indexInterior(7, 5, "chad", 12, 6, "eritrea", 10),
		// Page 4: the entries that sort after ("eritrea", 30).
		indexInterior(10, 8, "eritrea", 50, 9, "tonga", 2),
		indexLeaf("aruba", 1, "chad", 3, "chad", 11),      // Page 5.
		indexLeaf("chad", 20, "cuba", 5, "eritrea", 4),    // Page 6.
		indexLeaf("eritrea", 15, "eritrea", 21),           // Page 7.
		indexLeaf("eritrea", 31, "eritrea", 40),           // Page 8.
		indexLeaf("eritrea", 60, "fiji", 6, "togo", 8),    // Page 9.
		indexLeaf("tonga", 13, "tonga", 14, "zambia", 16), // Page 10.
	))

	tests := []struct {
		key  any
		want []int64
	}{
		{"aruba", []int64{1}},
		{"chad", []int64{3, 11, 12, 20}},
		{"cuba", []int64{5}},
		{"eritrea", []int64{4, 10, 15, 21, 30, 31, 40, 50, 60}},
		{"fiji", []int64{6}},
		{"togo", []int64{8}},
		{"tonga", []int64{2, 13, 14}},
		{"zambia", []int64{16}},
		{"albania", []int64{}},
		{"benin", []int64{}},
		{"denmark", []int64{}},
		{"estonia", []int64{}},
		{"spain", []int64{}},
		{"turkey", []int64{}},
		{"zimbabwe", []int64{}},
		{int64(7), []int64{}},
	}
	for _, tt := range tests {
		if got := searchIndexRowIDs(t, db, 2, tt.key); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("searchIndex(%#v) visited rowids %v, want %v", tt.key, got, tt.want)
		}
	}
}

func TestSearchIndexUsesCollation(t *testing.T) {
	// An index on a column with the NOCASE collating sequence is ordered
	// without regard to case.
	db := newTestDatabase(t, buildDatabase(512,
		testPage{pageType: pageTypeLeafTable},
		indexLeaf("apple", 1, "Banana", 2, "banana", 3, "BANANA", 4, "cherry", 5),
	))

	var rowIDs []int64
	err := db.searchIndex(2, "banana", collationNoCase, func(rowID int64) error {
		rowIDs = append(rowIDs, rowID)
		return nil
	})
	if err != nil {
		t.Fatalf("searchIndex returned an error: %v", err)
	}
	if want := []int64{2, 3, 4}; !reflect.DeepEqual(rowIDs, want) {
		t.Errorf("searchIndex visited rowids %v, want %v", rowIDs, want)
	}
}

func TestSearchIndexStopsWhenVisitFails(t *testing.T) {
	db := newTestDatabase(t, buildDatabase(512,
		testPage{pageType: pageTypeLeafTable},
		indexInterior(4, 3, "chad", 3),
		indexLeaf("chad", 1, "chad", 2),
		indexLeaf("chad", 4, "chad", 5),
	))
	errStop := errors.New("stop")

	for _, stopAt := range []int64{1, 2, 3, 4, 5} {
		var rowIDs []int64
		err := db.searchIndex(2, "chad", collationBinary, func(rowID int64) error {
			rowIDs = append(rowIDs, rowID)
			if rowID == stopAt {
				return errStop
			}
			return nil
		})
		if err != errStop {
			t.Errorf("searchIndex returned error %v, want %v", err, errStop)
		}
		if got := int64(len(rowIDs)); got != stopAt {
			t.Errorf("searchIndex visited %d entries before stopping at rowid %d", got, stopAt)
		}
	}
}

func TestSearchIndexRejectsCorruptTrees(t *testing.T) {
	tests := []struct {
		name  string
		pages []testPage // The pages that follow page 1. The root page is page 2.
	}{
		{"interior page that points to itself", []testPage{indexInterior(2)}},
		{"interior page whose cell points to itself", []testPage{indexInterior(3, 2, "chad", 1), indexLeaf("chad", 2)}},
		{"child of zero", []testPage{indexInterior(0)}},
		{"left child of zero", []testPage{indexInterior(3, 0, "chad", 1), indexLeaf("chad", 2)}},
		{"child past the end of the file", []testPage{indexInterior(3)}},
		{"child that is a table page", []testPage{indexInterior(3), leafPage(1)}},
		{"root that is a table page", []testPage{leafPage(1)}},
		{
			"entry without a rowid",
			[]testPage{{pageType: pageTypeLeafIndex, cells: [][]byte{encodeIndexLeafCell("chad")}}},
		},
		{
			"entry whose rowid isn't an integer",
			[]testPage{{pageType: pageTypeLeafIndex, cells: [][]byte{encodeIndexLeafCell("chad", "one")}}},
		},
		{
			"entry with an invalid record",
			[]testPage{{pageType: pageTypeLeafIndex, cells: [][]byte{{0x02, 0x09, 0x00}}}},
		},
		{
			"entry that is cut short",
			[]testPage{{pageType: pageTypeLeafIndex, cells: [][]byte{{0x05, 0x02, 0x01}}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pages := append([]testPage{{pageType: pageTypeLeafTable}}, tt.pages...)
			db := newTestDatabase(t, buildDatabase(512, pages...))

			err := db.searchIndex(2, "chad", collationBinary, func(int64) error { return nil })
			if err == nil {
				t.Error("searchIndex returned no error")
			}
		})
	}
}

// TestSearchIndexOfGeneratedIndexes searches indexes that SQLite built for
// every one of their keys.
func TestSearchIndexOfGeneratedIndexes(t *testing.T) {
	tests := []struct {
		pageSize int
		rowCount int
		keyCount int
	}{
		{512, 10, 3},
		{512, 2000, 7},
		{512, 30000, 500},
		{512, 30000, 30000},
		{4096, 30000, 40},
		{4096, 100000, 3},
	}

	for _, tt := range tests {
		name := fmt.Sprintf("%d rows with %d keys in pages of %d bytes", tt.rowCount, tt.keyCount, tt.pageSize)
		t.Run(name, func(t *testing.T) {
			// Row n has the key "key k" with k = n*7919 mod keyCount. Without
			// an integer primary key, the rowid of row n is n.
			path := createDatabase(t, fmt.Sprintf(`
				PRAGMA page_size = %d;
				CREATE TABLE items (key TEXT, label TEXT);
				WITH RECURSIVE sequence (n) AS (SELECT 1 UNION ALL SELECT n + 1 FROM sequence WHERE n < %d)
				INSERT INTO items SELECT 'key ' || ((n * 7919) %% %d), 'item ' || n FROM sequence;
				CREATE INDEX idx_items_key ON items (key);
			`, tt.pageSize, tt.rowCount, tt.keyCount))
			db := openDatabase(t, path)
			table, err := db.findTable("items")
			if err != nil {
				t.Fatalf("findTable returned an error: %v", err)
			}
			if len(table.indexes) != 1 {
				t.Fatalf("the table has %d indexes, want 1", len(table.indexes))
			}

			want := make(map[int][]int64)
			for n := 1; n <= tt.rowCount; n++ {
				k := n * 7919 % tt.keyCount
				want[k] = append(want[k], int64(n))
			}

			// Search for every key of small indexes, and for a sample of the
			// keys of large ones.
			step := tt.keyCount/500 + 1
			for k := 0; k < tt.keyCount; k += step {
				got := searchIndexRowIDs(t, db, table.indexes[0].rootPage, fmt.Sprintf("key %d", k))
				if !reflect.DeepEqual(got, want[k]) && (len(got) > 0 || len(want[k]) > 0) {
					t.Fatalf("searchIndex(\"key %d\") visited %d rowids, want %d:\n got %v\nwant %v",
						k, len(got), len(want[k]), got, want[k])
				}
			}

			for _, absent := range []any{"key", "key -1", "key 1x", "kez", "", "zzz", int64(1), nil} {
				// NULL isn't a key that entries can be searched by, but no
				// row of this table has a NULL key anyway.
				if got := searchIndexRowIDs(t, db, table.indexes[0].rootPage, absent); len(got) != 0 {
					t.Errorf("searchIndex(%#v) visited rowids %v, want none", absent, got)
				}
			}
		})
	}
}
