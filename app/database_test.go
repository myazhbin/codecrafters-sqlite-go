package main

import (
	"bytes"
	"testing"
)

func TestNewDatabaseReadsPageSize(t *testing.T) {
	tests := []struct {
		name string
		raw  uint16
		want int
	}{
		{name: "512", raw: 512, want: 512},
		{name: "1024", raw: 1024, want: 1024},
		{name: "2048", raw: 2048, want: 2048},
		{name: "4096", raw: 4096, want: 4096},
		{name: "8192", raw: 8192, want: 8192},
		{name: "16384", raw: 16384, want: 16384},
		{name: "32768", raw: 32768, want: 32768},
		// 65536 doesn't fit in the 2-byte field, so it is stored as 1.
		{name: "65536 stored as 1", raw: 1, want: 65536},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, err := newDatabase(bytes.NewReader(newHeader(tt.raw)))
			if err != nil {
				t.Fatalf("newDatabase returned an error: %v", err)
			}
			if db.pageSize != tt.want {
				t.Errorf("pageSize = %d, want %d", db.pageSize, tt.want)
			}
			if db.usableSize != tt.want {
				t.Errorf("usableSize = %d, want %d", db.usableSize, tt.want)
			}
		})
	}
}

func TestNewDatabaseReadsPageSizeAsBigEndian(t *testing.T) {
	header := newHeader(0)
	header[16], header[17] = 0x10, 0x00 // 0x1000 = 4096

	db, err := newDatabase(bytes.NewReader(header))
	if err != nil {
		t.Fatalf("newDatabase returned an error: %v", err)
	}
	if db.pageSize != 4096 {
		t.Errorf("pageSize = %d, want 4096", db.pageSize)
	}
}

func TestNewDatabaseReadsReservedSpace(t *testing.T) {
	header := newHeader(4096)
	header[20] = 16 // The number of bytes reserved at the end of every page.

	db, err := newDatabase(bytes.NewReader(header))
	if err != nil {
		t.Fatalf("newDatabase returned an error: %v", err)
	}
	if db.pageSize != 4096 || db.usableSize != 4080 {
		t.Errorf("pageSize, usableSize = %d, %d, want 4096, 4080", db.pageSize, db.usableSize)
	}
}

func TestNewDatabaseRejectsInvalidPageSize(t *testing.T) {
	for _, raw := range []uint16{0, 2, 256, 1000, 4097} {
		if _, err := newDatabase(bytes.NewReader(newHeader(raw))); err == nil {
			t.Errorf("newDatabase with page size %d returned no error", raw)
		}
	}
}

func TestNewDatabaseRejectsTooMuchReservedSpace(t *testing.T) {
	// The usable size of a page must be at least 480 bytes.
	header := newHeader(512)
	header[20] = 33

	if _, err := newDatabase(bytes.NewReader(header)); err == nil {
		t.Fatal("newDatabase with a usable page size of 479 bytes returned no error")
	}
}

func TestNewDatabaseChecksTextEncoding(t *testing.T) {
	tests := []struct {
		name     string
		encoding byte
		wantErr  bool
	}{
		{"database without content", 0, false},
		{"UTF-8", 1, false},
		{"UTF-16le", 2, true},
		{"UTF-16be", 3, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			header := newHeader(4096)
			header[59] = tt.encoding // The last byte of the big-endian field.

			_, err := newDatabase(bytes.NewReader(header))
			if gotErr := err != nil; gotErr != tt.wantErr {
				t.Errorf("newDatabase returned error %v, want an error: %t", err, tt.wantErr)
			}
		})
	}
}

func TestNewDatabaseRejectsTruncatedHeader(t *testing.T) {
	truncated := newHeader(4096)[:databaseHeaderSize-1]

	if _, err := newDatabase(bytes.NewReader(truncated)); err == nil {
		t.Fatal("newDatabase with a truncated header returned no error")
	}
}

func TestNewDatabaseRejectsBadMagic(t *testing.T) {
	header := newHeader(4096)
	copy(header, "Not a database!!")

	if _, err := newDatabase(bytes.NewReader(header)); err == nil {
		t.Fatal("newDatabase with a bad magic string returned no error")
	}
}

func TestReadPage(t *testing.T) {
	// Page 1 starts with the database header, so its b-tree page header is at
	// offset 100. On every other page it is at the start of the page.
	data := buildDatabase(512,
		testPage{pageType: pageTypeLeafTable, cellCount: 3},
		testPage{pageType: pageTypeInteriorTable, cellCount: 7},
		testPage{pageType: pageTypeLeafIndex, cellCount: 0x0102},
		testPage{pageType: pageTypeInteriorIndex, cellCount: 0},
	)
	db := newTestDatabase(t, data)

	tests := []struct {
		number        int
		wantPageType  byte
		wantCellCount int
		wantLeaf      bool
	}{
		{number: 1, wantPageType: pageTypeLeafTable, wantCellCount: 3, wantLeaf: true},
		{number: 2, wantPageType: pageTypeInteriorTable, wantCellCount: 7, wantLeaf: false},
		// The cell count is big-endian: 0x0102 is 258.
		{number: 3, wantPageType: pageTypeLeafIndex, wantCellCount: 258, wantLeaf: true},
		{number: 4, wantPageType: pageTypeInteriorIndex, wantCellCount: 0, wantLeaf: false},
	}

	for _, tt := range tests {
		p, err := db.readPage(tt.number)
		if err != nil {
			t.Errorf("readPage(%d) returned an error: %v", tt.number, err)
			continue
		}
		if p.number != tt.number {
			t.Errorf("readPage(%d).number = %d, want %d", tt.number, p.number, tt.number)
		}
		if p.pageType != tt.wantPageType {
			t.Errorf("readPage(%d).pageType = 0x%02x, want 0x%02x", tt.number, p.pageType, tt.wantPageType)
		}
		if p.cellCount != tt.wantCellCount {
			t.Errorf("readPage(%d).cellCount = %d, want %d", tt.number, p.cellCount, tt.wantCellCount)
		}
		if p.isLeaf() != tt.wantLeaf {
			t.Errorf("readPage(%d).isLeaf() = %t, want %t", tt.number, p.isLeaf(), tt.wantLeaf)
		}
		if len(p.data) != 512 {
			t.Errorf("readPage(%d) returned %d bytes of data, want 512", tt.number, len(p.data))
		}
	}
}

func TestReadPageRejectsInvalidPages(t *testing.T) {
	data := buildDatabase(512,
		testPage{pageType: pageTypeLeafTable},
		testPage{pageType: 0x07}, // Not a b-tree page type.
	)
	db := newTestDatabase(t, data)

	tests := []struct {
		name   string
		number int
	}{
		{name: "page number zero", number: 0},
		{name: "negative page number", number: -1},
		{name: "unknown page type", number: 2},
		{name: "page past the end of the file", number: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := db.readPage(tt.number); err == nil {
				t.Errorf("readPage(%d) returned no error", tt.number)
			}
		})
	}
}

func TestReadPageRejectsTruncatedPage(t *testing.T) {
	data := buildDatabase(512, testPage{pageType: pageTypeLeafTable})
	db := newTestDatabase(t, data[:len(data)-1])

	if _, err := db.readPage(1); err == nil {
		t.Fatal("readPage of a truncated page returned no error")
	}
}

func TestReadPageOfSampleDatabase(t *testing.T) {
	db := openDatabase(t, sampleDB)

	// Each table of the sample database fits on a single leaf page, so a
	// page's cell count is the number of rows in its table.
	tests := []struct {
		table         string
		number        int
		wantCellCount int
	}{
		{table: "sqlite_schema", number: 1, wantCellCount: 3},
		{table: "apples", number: 2, wantCellCount: 4},
		{table: "sqlite_sequence", number: 3, wantCellCount: 2},
		{table: "oranges", number: 4, wantCellCount: 6},
	}

	for _, tt := range tests {
		t.Run(tt.table, func(t *testing.T) {
			p, err := db.readPage(tt.number)
			if err != nil {
				t.Fatalf("readPage(%d) returned an error: %v", tt.number, err)
			}
			if p.pageType != pageTypeLeafTable {
				t.Errorf("pageType = 0x%02x, want 0x%02x", p.pageType, pageTypeLeafTable)
			}
			if p.cellCount != tt.wantCellCount {
				t.Errorf("cellCount = %d, want %d", p.cellCount, tt.wantCellCount)
			}
		})
	}
}
