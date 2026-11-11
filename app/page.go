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

// rightMostChild returns the number of the right-most child page of an
// interior page. An interior page with n cells has n+1 child pages: every cell
// points to one, and the page header points to the last one.
func (p *page) rightMostChild() int {
	// The page number is a 4-byte big-endian integer at offset 8 of the page
	// header.
	return int(binary.BigEndian.Uint32(p.data[p.headerOffset+8:]))
}

// tableInteriorCell is a cell of an interior page of a table b-tree. It
// points to a child page.
type tableInteriorCell struct {
	// leftChild is the number of the child page.
	leftChild int
	// rowID is the key of the cell. The rows under the child page all have
	// rowids that are lower than or equal to it, and greater than the key of
	// the previous cell.
	rowID int64
}

// tableInteriorCell parses the cell with the given index of a table interior
// page.
func (p *page) tableInteriorCell(index int) (tableInteriorCell, error) {
	if p.pageType != pageTypeInteriorTable {
		return tableInteriorCell{}, fmt.Errorf("page %d is not a table interior page", p.number)
	}
	data, err := p.cell(index)
	if err != nil {
		return tableInteriorCell{}, err
	}

	// The cell is made up of the number of the child page, as a 4-byte
	// big-endian integer, and of the key, as a varint.
	if len(data) < 4 {
		return tableInteriorCell{}, fmt.Errorf("page %d is corrupt: cell %d is out of bounds", p.number, index)
	}
	rowID, _, err := readVarint(data[4:])
	if err != nil {
		return tableInteriorCell{}, fmt.Errorf("page %d, cell %d: invalid rowid: %w", p.number, index, err)
	}
	return tableInteriorCell{leftChild: int(binary.BigEndian.Uint32(data)), rowID: rowID}, nil
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

	// On table leaf pages, a payload spills over to overflow pages when it
	// takes up more than the usable size of the page minus 35 bytes.
	payload, err := p.localPayload(index, data, payloadSize, len(p.data)-35)
	if err != nil {
		return tableLeafCell{}, err
	}
	return tableLeafCell{rowID: rowID, payload: payload}, nil
}

// indexCell is a cell of a page of an index b-tree. It stores one entry of
// the index.
type indexCell struct {
	// leftChild is the number of the child page with the entries that sort
	// between the entry of the previous cell and the entry of this cell. It
	// is 0 on leaf pages, whose cells have no child pages.
	leftChild int
	// payload is the record of the entry. Its values are the indexed columns
	// of a row, followed by the rowid of that row.
	payload []byte
}

// indexCell parses the cell with the given index of a page of an index
// b-tree.
func (p *page) indexCell(index int) (indexCell, error) {
	if p.pageType != pageTypeLeafIndex && p.pageType != pageTypeInteriorIndex {
		return indexCell{}, fmt.Errorf("page %d is not an index page", p.number)
	}
	data, err := p.cell(index)
	if err != nil {
		return indexCell{}, err
	}

	// On interior pages, the cell starts with the number of its child page,
	// as a 4-byte big-endian integer.
	var cell indexCell
	if p.pageType == pageTypeInteriorIndex {
		if len(data) < 4 {
			return indexCell{}, fmt.Errorf("page %d is corrupt: cell %d is out of bounds", p.number, index)
		}
		cell.leftChild = int(binary.BigEndian.Uint32(data))
		data = data[4:]
	}

	// The size of the payload follows as a varint, and then the payload.
	payloadSize, n, err := readVarint(data)
	if err != nil {
		return indexCell{}, fmt.Errorf("page %d, cell %d: invalid payload size: %w", p.number, index, err)
	}

	// On index pages, a payload spills over to overflow pages when it takes
	// up more than about a quarter of the usable size of the page.
	maxLocal := (len(p.data)-12)*64/255 - 23
	if cell.payload, err = p.localPayload(index, data[n:], payloadSize, maxLocal); err != nil {
		return indexCell{}, err
	}
	return cell, nil
}

// localPayload returns the payload of the cell with the given index, which
// is payloadSize bytes long and starts at the beginning of data. Payloads of
// more than maxLocal bytes are too large to be stored on the page alone:
// their end is stored on overflow pages, which is not supported.
func (p *page) localPayload(index int, data []byte, payloadSize int64, maxLocal int) ([]byte, error) {
	if payloadSize < 0 || payloadSize > int64(maxLocal) {
		return nil, fmt.Errorf("page %d, cell %d: payloads stored on overflow pages are not supported", p.number, index)
	}
	if payloadSize > int64(len(data)) {
		return nil, fmt.Errorf("page %d is corrupt: the payload of cell %d is out of bounds", p.number, index)
	}
	return data[:payloadSize], nil
}
