package main

import (
	"encoding/binary"
	"fmt"
)

// The types of b-tree pages, as stored in the first byte of a page header.
const (
	pageTypeInteriorIndex = 0x02
	pageTypeInteriorTable = 0x05
	pageTypeLeafIndex     = 0x0a
	pageTypeLeafTable     = 0x0d
)

// The sizes in bytes of the headers of b-tree pages.
const (
	leafPageHeaderSize     = 8
	interiorPageHeaderSize = 12
)

// page is a b-tree page of a database.
type page struct {
	// number is the page's number. Page numbers start at 1.
	number int
	// data holds the usable bytes of the whole page. For page 1, that
	// includes the database header.
	data []byte
	// headerOffset is the offset of the b-tree page header within data.
	headerOffset int
	// pageType is one of the pageType constants.
	pageType byte
	// cellCount is the number of cells stored on the page.
	cellCount int
}

// newPage parses the header of the b-tree page with the given number. data
// holds the usable bytes of the whole page.
func newPage(number int, data []byte) (*page, error) {
	// The b-tree page header is at the start of the page, except on page 1
	// where the database header comes first.
	headerOffset := 0
	if number == 1 {
		headerOffset = databaseHeaderSize
	}
	header := data[headerOffset:]

	p := &page{
		number:       number,
		data:         data,
		headerOffset: headerOffset,
		pageType:     header[0],
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

// isLeaf reports whether the page is a leaf page, as opposed to an interior
// page.
func (p *page) isLeaf() bool {
	return p.pageType == pageTypeLeafTable || p.pageType == pageTypeLeafIndex
}

// cell returns the contents of the page from the start of the cell with the
// given index. Where a cell ends depends on its contents, so the returned
// bytes run to the end of the page.
func (p *page) cell(index int) ([]byte, error) {
	if index < 0 || index >= p.cellCount {
		return nil, fmt.Errorf("page %d has no cell %d", p.number, index)
	}

	// The cell pointer array comes right after the page header. It holds the
	// offset of every cell from the start of the page, as 2-byte big-endian
	// integers.
	pointersOffset := p.headerOffset + leafPageHeaderSize
	if !p.isLeaf() {
		pointersOffset = p.headerOffset + interiorPageHeaderSize
	}
	pointersEnd := pointersOffset + 2*p.cellCount
	if pointersEnd > len(p.data) {
		return nil, fmt.Errorf("page %d is corrupt: its cell pointer array doesn't fit on the page", p.number)
	}

	cellOffset := int(binary.BigEndian.Uint16(p.data[pointersOffset+2*index:]))
	if cellOffset < pointersEnd || cellOffset >= len(p.data) {
		return nil, fmt.Errorf("page %d is corrupt: cell %d is out of bounds", p.number, index)
	}
	return p.data[cellOffset:], nil
}

// tableLeafCell is a cell of a leaf page of a table b-tree. It stores one row
// of the table.
type tableLeafCell struct {
	// rowID is the row's unique integer key.
	rowID int64
	// payload is the row's record. See decodeRecord.
	payload []byte
}

// tableLeafCell parses the cell with the given index of a table leaf page.
func (p *page) tableLeafCell(index int) (tableLeafCell, error) {
	if p.pageType != pageTypeLeafTable {
		return tableLeafCell{}, fmt.Errorf("page %d is not a table leaf page", p.number)
	}
	data, err := p.cell(index)
	if err != nil {
		return tableLeafCell{}, err
	}

	// The cell starts with two varints: the size of the payload in bytes,
	// then the rowid. The payload follows.
	payloadSize, n, err := readVarint(data)
	if err != nil {
		return tableLeafCell{}, fmt.Errorf("page %d, cell %d: invalid payload size: %w", p.number, index, err)
	}
	data = data[n:]

	rowID, n, err := readVarint(data)
	if err != nil {
		return tableLeafCell{}, fmt.Errorf("page %d, cell %d: invalid rowid: %w", p.number, index, err)
	}
	data = data[n:]

	// A payload that is too large to fit on the page spills over to overflow
	// pages. On table leaf pages, that happens when the payload takes up more
	// than the usable size of the page minus 35 bytes.
	if payloadSize < 0 || payloadSize > int64(len(p.data)-35) {
		return tableLeafCell{}, fmt.Errorf("page %d, cell %d: payloads stored on overflow pages are not supported", p.number, index)
	}
	if payloadSize > int64(len(data)) {
		return tableLeafCell{}, fmt.Errorf("page %d is corrupt: the payload of cell %d is out of bounds", p.number, index)
	}

	return tableLeafCell{rowID: rowID, payload: data[:payloadSize]}, nil
}
