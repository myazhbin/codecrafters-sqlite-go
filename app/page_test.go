package main

import (
	"bytes"
	"encoding/binary"
	"reflect"
	"strings"
	"testing"
)

func TestPageCell(t *testing.T) {
	cells := [][]byte{[]byte("first cell"), []byte("second"), []byte("3rd")}
	// The cell pointer array comes after the page header, which is 8 bytes
	// long on leaf pages and 12 bytes long on interior pages, and comes after
	// the database header on page 1.
	data := buildDatabase(512,
		testPage{pageType: pageTypeLeafTable, cells: cells},
		testPage{pageType: pageTypeLeafTable, cells: cells},
		testPage{pageType: pageTypeInteriorTable, cells: cells},
		testPage{pageType: pageTypeLeafIndex, cells: cells},
		testPage{pageType: pageTypeInteriorIndex, cells: cells},
	)
	db := newTestDatabase(t, data)

	for number := 1; number <= 5; number++ {
		p, err := db.readPage(number)
		if err != nil {
			t.Fatalf("readPage(%d) returned an error: %v", number, err)
		}
		if p.cellCount != len(cells) {
			t.Fatalf("page %d: cellCount = %d, want %d", number, p.cellCount, len(cells))
		}

		for i, want := range cells {
			got, err := p.cell(i)
			if err != nil {
				t.Errorf("page %d: cell(%d) returned an error: %v", number, i, err)
				continue
			}
			if !bytes.HasPrefix(got, want) {
				t.Errorf("page %d: cell(%d) starts with %q, want %q", number, i, got[:len(want)], want)
			}
		}
	}
}

func TestPageCellRejectsInvalidIndex(t *testing.T) {
	data := buildDatabase(512, testPage{pageType: pageTypeLeafTable, cells: [][]byte{{1}, {2}}})
	p, err := newTestDatabase(t, data).readPage(1)
	if err != nil {
		t.Fatalf("readPage returned an error: %v", err)
	}

	for _, index := range []int{-1, 2, 3} {
		if _, err := p.cell(index); err == nil {
			t.Errorf("cell(%d) returned no error", index)
		}
	}
}

func TestPageCellRejectsCorruptPages(t *testing.T) {
	const pageSize = 512
	// On page 2, the cell pointer array of a leaf page starts at offset 8.
	const pointersOffset = pageSize + leafPageHeaderSize

	tests := []struct {
		name    string
		corrupt func(data []byte)
	}{
		{
			"cell pointer past the end of the page",
			func(data []byte) { binary.BigEndian.PutUint16(data[pointersOffset:], pageSize) },
		},
		{
			"cell pointer into the page header",
			func(data []byte) { binary.BigEndian.PutUint16(data[pointersOffset:], 4) },
		},
		{
			"cell pointer into the cell pointer array",
			func(data []byte) { binary.BigEndian.PutUint16(data[pointersOffset:], leafPageHeaderSize+2) },
		},
		{
			"more cells than fit on the page",
			func(data []byte) { binary.BigEndian.PutUint16(data[pageSize+3:], 0xffff) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := buildDatabase(pageSize,
				testPage{pageType: pageTypeLeafTable},
				testPage{pageType: pageTypeLeafTable, cells: [][]byte{{1}, {2}}},
			)
			tt.corrupt(data)

			p, err := newTestDatabase(t, data).readPage(2)
			if err != nil {
				t.Fatalf("readPage returned an error: %v", err)
			}
			if _, err := p.cell(0); err == nil {
				t.Error("cell(0) returned no error")
			}
		})
	}
}

func TestTableInteriorCell(t *testing.T) {
	// Page 1 starts with the database header, which moves its page header.
	root := testPage{
		pageType:       pageTypeInteriorTable,
		rightMostChild: 0x01020304,
		cells: [][]byte{
			encodeTableInteriorCell(7, 100),
			// A child page number and a key that take several bytes.
			encodeTableInteriorCell(0x0a0b0c0d, 1_000_000),
			encodeTableInteriorCell(9, -1),
		},
	}
	db := newTestDatabase(t, buildDatabase(512, root, root))

	want := []tableInteriorCell{
		{leftChild: 7, rowID: 100},
		{leftChild: 0x0a0b0c0d, rowID: 1_000_000},
		{leftChild: 9, rowID: -1},
	}
	for number := 1; number <= 2; number++ {
		p, err := db.readPage(number)
		if err != nil {
			t.Fatalf("readPage(%d) returned an error: %v", number, err)
		}
		if got := p.rightMostChild(); got != 0x01020304 {
			t.Errorf("page %d: rightMostChild() = %#x, want 0x01020304", number, got)
		}
		for i, wantCell := range want {
			cell, err := p.tableInteriorCell(i)
			if err != nil {
				t.Errorf("page %d: tableInteriorCell(%d) returned an error: %v", number, i, err)
				continue
			}
			if cell != wantCell {
				t.Errorf("page %d: tableInteriorCell(%d) = %+v, want %+v", number, i, cell, wantCell)
			}
		}
		if _, err := p.tableInteriorCell(len(want)); err == nil {
			t.Errorf("page %d: tableInteriorCell(%d) returned no error", number, len(want))
		}
	}
}

func TestTableInteriorCellOfSampleDatabase(t *testing.T) {
	requireFile(t, superheroesDB)
	// Page 2 is the root page of the superheroes table.
	p, err := openDatabase(t, superheroesDB).readPage(2)
	if err != nil {
		t.Fatalf("readPage returned an error: %v", err)
	}
	if p.pageType != pageTypeInteriorTable {
		t.Fatalf("pageType = 0x%02x, want 0x%02x", p.pageType, pageTypeInteriorTable)
	}

	// The keys of the cells of an interior page are in ascending order.
	previous := int64(0)
	for i := 0; i < p.cellCount; i++ {
		cell, err := p.tableInteriorCell(i)
		if err != nil {
			t.Fatalf("tableInteriorCell(%d) returned an error: %v", i, err)
		}
		if cell.rowID <= previous {
			t.Errorf("cell %d has key %d, which isn't above the key %d of the previous cell", i, cell.rowID, previous)
		}
		if cell.leftChild < 2 {
			t.Errorf("cell %d points to page %d", i, cell.leftChild)
		}
		previous = cell.rowID
	}
}

func TestTableInteriorCellRejectsOtherPageTypes(t *testing.T) {
	cells := [][]byte{encodeTableInteriorCell(2, 1)}
	data := buildDatabase(512,
		testPage{pageType: pageTypeLeafTable, cells: cells},
		testPage{pageType: pageTypeLeafIndex, cells: cells},
		testPage{pageType: pageTypeInteriorIndex, cells: cells},
	)
	db := newTestDatabase(t, data)

	for number := 1; number <= 3; number++ {
		p, err := db.readPage(number)
		if err != nil {
			t.Fatalf("readPage(%d) returned an error: %v", number, err)
		}
		if _, err := p.tableInteriorCell(0); err == nil {
			t.Errorf("page %d: tableInteriorCell returned no error for page type 0x%02x", number, p.pageType)
		}
	}
}

func TestTableInteriorCellRejectsInvalidCells(t *testing.T) {
	tests := []struct {
		name string
		cell []byte
	}{
		// The cell is stored at the very end of the page, so these cells run
		// into the end of the page.
		{"truncated child page number", []byte{0x00, 0x00, 0x02}},
		{"missing key", []byte{0x00, 0x00, 0x00, 0x02}},
		{"truncated key", []byte{0x00, 0x00, 0x00, 0x02, 0x81}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := buildDatabase(512, testPage{pageType: pageTypeInteriorTable, cells: [][]byte{tt.cell}})
			p, err := newTestDatabase(t, data).readPage(1)
			if err != nil {
				t.Fatalf("readPage returned an error: %v", err)
			}
			if cell, err := p.tableInteriorCell(0); err == nil {
				t.Errorf("tableInteriorCell = %+v, want an error", cell)
			}
		})
	}
}

func TestTableLeafCell(t *testing.T) {
	// The cell of the sqlite_schema row that describes the oranges table of
	// the sample database: a payload of 120 bytes for the row with rowid 3.
	orangesCell := append([]byte{0x78, 0x03}, orangesSchemaRecord()...)
	// Both the payload size (200) and the rowid (1,000,000) of this cell take
	// more than one byte.
	wideCell := encodeTableLeafCell(1_000_000, strings.Repeat("x", 197))

	data := buildDatabase(4096, testPage{
		pageType: pageTypeLeafTable,
		cells:    [][]byte{orangesCell, wideCell, encodeTableLeafCell(-1, nil)},
	})
	p, err := newTestDatabase(t, data).readPage(1)
	if err != nil {
		t.Fatalf("readPage returned an error: %v", err)
	}

	tests := []struct {
		index       int
		wantRowID   int64
		wantPayload []byte
	}{
		{0, 3, orangesSchemaRecord()},
		{1, 1_000_000, encodeRecord(strings.Repeat("x", 197))},
		{2, -1, encodeRecord(nil)},
	}

	for _, tt := range tests {
		cell, err := p.tableLeafCell(tt.index)
		if err != nil {
			t.Errorf("tableLeafCell(%d) returned an error: %v", tt.index, err)
			continue
		}
		if cell.rowID != tt.wantRowID {
			t.Errorf("tableLeafCell(%d).rowID = %d, want %d", tt.index, cell.rowID, tt.wantRowID)
		}
		if !bytes.Equal(cell.payload, tt.wantPayload) {
			t.Errorf("tableLeafCell(%d).payload = % x, want % x", tt.index, cell.payload, tt.wantPayload)
		}
	}
}

func TestTableLeafCellOfSampleDatabase(t *testing.T) {
	// Page 2 of the sample database is the only page of the apples table.
	p, err := openDatabase(t, sampleDB).readPage(2)
	if err != nil {
		t.Fatalf("readPage returned an error: %v", err)
	}

	wantRows := [][]any{
		{nil, "Granny Smith", "Light Green"},
		{nil, "Fuji", "Red"},
		{nil, "Honeycrisp", "Blush Red"},
		{nil, "Golden Delicious", "Yellow"},
	}
	if p.cellCount != len(wantRows) {
		t.Fatalf("cellCount = %d, want %d", p.cellCount, len(wantRows))
	}

	for i, want := range wantRows {
		cell, err := p.tableLeafCell(i)
		if err != nil {
			t.Fatalf("tableLeafCell(%d) returned an error: %v", i, err)
		}
		if wantRowID := int64(i + 1); cell.rowID != wantRowID {
			t.Errorf("tableLeafCell(%d).rowID = %d, want %d", i, cell.rowID, wantRowID)
		}

		got, err := decodeRecord(cell.payload)
		if err != nil {
			t.Fatalf("decodeRecord of cell %d returned an error: %v", i, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("row %d = %#v, want %#v", i, got, want)
		}
	}
}

func TestTableLeafCellRejectsOtherPageTypes(t *testing.T) {
	cells := [][]byte{encodeTableLeafCell(1, "x")}
	data := buildDatabase(512,
		testPage{pageType: pageTypeInteriorTable, cells: cells},
		testPage{pageType: pageTypeLeafIndex, cells: cells},
		testPage{pageType: pageTypeInteriorIndex, cells: cells},
	)
	db := newTestDatabase(t, data)

	for number := 1; number <= 3; number++ {
		p, err := db.readPage(number)
		if err != nil {
			t.Fatalf("readPage(%d) returned an error: %v", number, err)
		}
		if _, err := p.tableLeafCell(0); err == nil {
			t.Errorf("page %d: tableLeafCell returned no error for page type 0x%02x", number, p.pageType)
		}
	}
}

func TestTableLeafCellRejectsInvalidCells(t *testing.T) {
	tests := []struct {
		name string
		cell []byte
	}{
		// The cell is stored at the very end of the page, so these varints
		// run into the end of the page.
		{"truncated payload size", []byte{0x81}},
		{"truncated rowid", []byte{0x05, 0x81}},
		{"payload past the end of the page", []byte{0x05, 0x01, 0x02, 0x01}},
		// A page of 512 bytes can hold payloads of up to 512 - 35 = 477 bytes
		// (0x83 0x5d). Larger payloads spill over to overflow pages.
		{"payload on overflow pages", []byte{0x83, 0x5e, 0x01}},
		{"negative payload size", []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x01}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := buildDatabase(512, testPage{pageType: pageTypeLeafTable, cells: [][]byte{tt.cell}})
			p, err := newTestDatabase(t, data).readPage(1)
			if err != nil {
				t.Fatalf("readPage returned an error: %v", err)
			}
			if cell, err := p.tableLeafCell(0); err == nil {
				t.Errorf("tableLeafCell = %+v, want an error", cell)
			}
		})
	}
}

func TestTableLeafCellAcceptsLargestLocalPayload(t *testing.T) {
	// A payload of 477 bytes is the largest that a table leaf page of 512
	// bytes stores without overflow pages. On page 1 it wouldn't fit next to
	// the database header, so store it on page 2.
	record := encodeRecord(strings.Repeat("x", 474))
	if len(record) != 477 {
		t.Fatalf("the test record is %d bytes long, want 477", len(record))
	}
	data := buildDatabase(512,
		testPage{pageType: pageTypeLeafTable},
		testPage{pageType: pageTypeLeafTable, cells: [][]byte{encodeTableLeafCell(1, strings.Repeat("x", 474))}},
	)
	p, err := newTestDatabase(t, data).readPage(2)
	if err != nil {
		t.Fatalf("readPage returned an error: %v", err)
	}

	cell, err := p.tableLeafCell(0)
	if err != nil {
		t.Fatalf("tableLeafCell returned an error: %v", err)
	}
	if !bytes.Equal(cell.payload, record) {
		t.Errorf("tableLeafCell returned a payload of %d bytes, want the record of %d bytes", len(cell.payload), len(record))
	}
}

func TestReadPageHonorsReservedSpace(t *testing.T) {
	// A database can reserve bytes at the end of every page. That space isn't
	// part of the usable page, which lowers the largest payload that fits on
	// a page: 512 - 32 - 35 = 445 bytes here.
	fits := encodeTableLeafCell(1, strings.Repeat("x", 442))
	overflows := encodeTableLeafCell(2, strings.Repeat("x", 443))

	for _, tt := range []struct {
		name    string
		cell    []byte
		wantErr bool
	}{
		{"largest local payload", fits, false},
		{"payload on overflow pages", overflows, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			data := buildDatabase(512,
				testPage{pageType: pageTypeLeafTable},
				testPage{pageType: pageTypeLeafTable},
			)
			data[20] = 32 // The number of reserved bytes per page.
			// Store the cell right at the start of the cell content area of
			// page 2, so that it stays clear of the reserved space.
			binary.BigEndian.PutUint16(data[512+3:], 1)
			binary.BigEndian.PutUint16(data[512+leafPageHeaderSize:], 10)
			copy(data[512+10:], tt.cell)

			db := newTestDatabase(t, data)
			if db.pageSize != 512 || db.usableSize != 480 {
				t.Fatalf("pageSize, usableSize = %d, %d, want 512, 480", db.pageSize, db.usableSize)
			}
			p, err := db.readPage(2)
			if err != nil {
				t.Fatalf("readPage returned an error: %v", err)
			}
			if len(p.data) != 480 {
				t.Errorf("page holds %d bytes, want the usable 480", len(p.data))
			}

			_, err = p.tableLeafCell(0)
			if gotErr := err != nil; gotErr != tt.wantErr {
				t.Errorf("tableLeafCell returned error %v, want an error: %t", err, tt.wantErr)
			}
		})
	}
}

func TestIndexCell(t *testing.T) {
	leafCells := [][]byte{
		encodeIndexLeafCell("eritrea", 121311),
		encodeIndexLeafCell(nil, 7),
		// A payload of more than 127 bytes, whose size takes a 2-byte varint.
		encodeIndexLeafCell(strings.Repeat("x", 150), 9),
	}
	interiorCells := [][]byte{
		encodeIndexInteriorCell(7, "eritrea", 121311),
		encodeIndexInteriorCell(0x0a0b0c0d, strings.Repeat("x", 150), 9),
	}
	db := newTestDatabase(t, buildDatabase(4096,
		testPage{pageType: pageTypeLeafIndex, cells: leafCells},
		testPage{pageType: pageTypeLeafIndex, cells: leafCells},
		testPage{pageType: pageTypeInteriorIndex, cells: interiorCells, rightMostChild: 5},
	))

	tests := []struct {
		page          int
		index         int
		wantLeftChild int
		wantPayload   []byte
	}{
		{1, 0, 0, encodeRecord("eritrea", 121311)},
		{1, 1, 0, encodeRecord(nil, 7)},
		{1, 2, 0, encodeRecord(strings.Repeat("x", 150), 9)},
		{2, 0, 0, encodeRecord("eritrea", 121311)},
		{2, 2, 0, encodeRecord(strings.Repeat("x", 150), 9)},
		{3, 0, 7, encodeRecord("eritrea", 121311)},
		{3, 1, 0x0a0b0c0d, encodeRecord(strings.Repeat("x", 150), 9)},
	}

	for _, tt := range tests {
		p, err := db.readPage(tt.page)
		if err != nil {
			t.Fatalf("readPage(%d) returned an error: %v", tt.page, err)
		}
		cell, err := p.indexCell(tt.index)
		if err != nil {
			t.Errorf("page %d: indexCell(%d) returned an error: %v", tt.page, tt.index, err)
			continue
		}
		if cell.leftChild != tt.wantLeftChild {
			t.Errorf("page %d: indexCell(%d).leftChild = %d, want %d", tt.page, tt.index, cell.leftChild, tt.wantLeftChild)
		}
		if !bytes.Equal(cell.payload, tt.wantPayload) {
			t.Errorf("page %d: indexCell(%d).payload = % x, want % x", tt.page, tt.index, cell.payload, tt.wantPayload)
		}
	}
}

func TestIndexCellRejectsOtherPageTypes(t *testing.T) {
	cells := [][]byte{encodeIndexLeafCell("x", 1)}
	db := newTestDatabase(t, buildDatabase(512,
		testPage{pageType: pageTypeLeafTable, cells: cells},
		testPage{pageType: pageTypeInteriorTable, cells: cells},
	))

	for number := 1; number <= 2; number++ {
		p, err := db.readPage(number)
		if err != nil {
			t.Fatalf("readPage(%d) returned an error: %v", number, err)
		}
		if _, err := p.indexCell(0); err == nil {
			t.Errorf("page %d: indexCell returned no error for page type 0x%02x", number, p.pageType)
		}
	}
}

func TestIndexCellRejectsInvalidCells(t *testing.T) {
	// A page of 512 bytes stores index payloads of up to
	// (512 - 12) * 64 / 255 - 23 = 102 bytes without overflow pages.
	largestLocal := encodeIndexLeafCell(strings.Repeat("x", 98), 1)
	overflowing := encodeIndexLeafCell(strings.Repeat("x", 99), 1)

	tests := []struct {
		name     string
		pageType byte
		cell     []byte
		wantErr  bool
	}{
		{"largest local payload", pageTypeLeafIndex, largestLocal, false},
		{"payload on overflow pages", pageTypeLeafIndex, overflowing, true},
		// The cell is stored at the very end of the page, so these cells run
		// into the end of the page.
		{"missing payload size", pageTypeInteriorIndex, []byte{0x00, 0x00, 0x00, 0x02}, true},
		{"truncated child page number", pageTypeInteriorIndex, []byte{0x00, 0x00, 0x02}, true},
		{"truncated payload size", pageTypeLeafIndex, []byte{0x81}, true},
		{"payload past the end of the page", pageTypeLeafIndex, []byte{0x05, 0x02, 0x01}, true},
		{
			"negative payload size", pageTypeLeafIndex,
			[]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}, true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := newTestDatabase(t, buildDatabase(512,
				testPage{pageType: pageTypeLeafTable},
				testPage{pageType: tt.pageType, cells: [][]byte{tt.cell}},
			))
			p, err := db.readPage(2)
			if err != nil {
				t.Fatalf("readPage returned an error: %v", err)
			}

			_, err = p.indexCell(0)
			if gotErr := err != nil; gotErr != tt.wantErr {
				t.Errorf("indexCell returned error %v, want an error: %t", err, tt.wantErr)
			}
		})
	}
}
