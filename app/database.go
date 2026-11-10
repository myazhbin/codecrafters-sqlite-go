package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// headerSize is the size in bytes of the database header that every SQLite
// database file starts with.
const headerSize = 100

// headerMagic is the string every SQLite database file starts with.
var headerMagic = []byte("SQLite format 3\x00")

// schemaPageNumber is the number of the page that holds the sqlite_schema
// table. Page numbers start at 1.
const schemaPageNumber = 1

// The types of b-tree pages, as stored in the first byte of a page header.
const (
	pageTypeInteriorIndex = 0x02
	pageTypeInteriorTable = 0x05
	pageTypeLeafIndex     = 0x0a
	pageTypeLeafTable     = 0x0d
)

// database provides access to the pages of a SQLite database file.
type database struct {
	file     io.ReaderAt
	pageSize int
}

// newDatabase reads the database header from file and returns a database
// that reads its pages from file.
func newDatabase(file io.ReaderAt) (*database, error) {
	header := make([]byte, headerSize)
	if err := readFullAt(file, header, 0); err != nil {
		return nil, fmt.Errorf("failed to read database header: %w", err)
	}
	if !bytes.HasPrefix(header, headerMagic) {
		return nil, errors.New("file is not a SQLite database: bad magic string")
	}

	// The page size is a 2-byte big-endian integer stored right after the
	// magic string. A page size of 65536 doesn't fit in 2 bytes, so it is
	// stored as 1.
	pageSize := int(binary.BigEndian.Uint16(header[16:18]))
	if pageSize == 1 {
		pageSize = 65536
	}
	if pageSize < 512 || pageSize&(pageSize-1) != 0 {
		return nil, fmt.Errorf("invalid database page size %d", pageSize)
	}

	return &database{file: file, pageSize: pageSize}, nil
}

// page is a b-tree page of a database.
type page struct {
	// number is the page's number. Page numbers start at 1.
	number int
	// data holds the bytes of the whole page. For page 1, that includes the
	// database header.
	data []byte
	// pageType is one of the pageType constants.
	pageType byte
	// cellCount is the number of cells stored on the page.
	cellCount int
}

// readPage reads the b-tree page with the given number.
func (db *database) readPage(number int) (*page, error) {
	if number < 1 {
		return nil, fmt.Errorf("invalid page number %d", number)
	}

	data := make([]byte, db.pageSize)
	offset := int64(number-1) * int64(db.pageSize)
	if err := readFullAt(db.file, data, offset); err != nil {
		return nil, fmt.Errorf("failed to read page %d: %w", number, err)
	}

	// The b-tree page header is at the start of the page, except on page 1
	// where the database header comes first.
	header := data
	if number == 1 {
		header = data[headerSize:]
	}

	p := &page{
		number:   number,
		data:     data,
		pageType: header[0],
		// The cell count is a 2-byte big-endian integer at offset 3 of the
		// page header.
		cellCount: int(binary.BigEndian.Uint16(header[3:5])),
	}

	switch p.pageType {
	case pageTypeInteriorIndex, pageTypeInteriorTable, pageTypeLeafIndex, pageTypeLeafTable:
		return p, nil
	default:
		return nil, fmt.Errorf("page %d is not a b-tree page: unknown page type 0x%02x", number, p.pageType)
	}
}

// readFullAt reads exactly len(buf) bytes from r starting at offset.
func readFullAt(r io.ReaderAt, buf []byte, offset int64) error {
	n, err := r.ReadAt(buf, offset)
	if n == len(buf) {
		// ReadAt is allowed to return io.EOF along with a full read that ends
		// exactly at the end of the input.
		return nil
	}
	if err == nil || err == io.EOF {
		return io.ErrUnexpectedEOF
	}
	return err
}
