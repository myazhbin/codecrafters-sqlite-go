package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// databaseHeaderSize is the size in bytes of the database header that every
// SQLite database file starts with.
const databaseHeaderSize = 100

// databaseHeaderMagic is the string every SQLite database file starts with.
var databaseHeaderMagic = []byte("SQLite format 3\x00")

// database provides access to the pages of a SQLite database file.
type database struct {
	file io.ReaderAt
	// pageSize is the size in bytes of every page of the database.
	pageSize int
	// usableSize is the number of bytes at the start of every page that are
	// available to SQLite. It is smaller than the page size when the database
	// reserves space at the end of every page for extensions.
	usableSize int
}

// newDatabase reads the database header from file and returns a database
// that reads its pages from file.
func newDatabase(file io.ReaderAt) (*database, error) {
	header := make([]byte, databaseHeaderSize)
	if err := readFullAt(file, header, 0); err != nil {
		return nil, fmt.Errorf("failed to read database header: %w", err)
	}
	if !bytes.HasPrefix(header, databaseHeaderMagic) {
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

	// Byte 20 holds the number of bytes reserved at the end of every page.
	// SQLite requires a usable size of at least 480 bytes.
	usableSize := pageSize - int(header[20])
	if usableSize < 480 {
		return nil, fmt.Errorf("invalid usable page size %d", usableSize)
	}

	return &database{file: file, pageSize: pageSize, usableSize: usableSize}, nil
}

// readPage reads the b-tree page with the given number. Page numbers start
// at 1.
func (db *database) readPage(number int) (*page, error) {
	if number < 1 {
		return nil, fmt.Errorf("invalid page number %d", number)
	}

	data := make([]byte, db.pageSize)
	offset := int64(number-1) * int64(db.pageSize)
	if err := readFullAt(db.file, data, offset); err != nil {
		return nil, fmt.Errorf("failed to read page %d: %w", number, err)
	}
	return newPage(number, data[:db.usableSize])
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
