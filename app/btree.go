package main

import (
	"fmt"
	"sort"
)

// maxBTreeDepth is the largest number of pages between the root page and a
// leaf page of a b-tree. SQLite never creates deeper b-trees, so going any
// deeper means that the pages of a corrupt database point to each other in a
// cycle.
const maxBTreeDepth = 20

// walkTableLeaves calls visit for every leaf page of the table b-tree with
// the given root page, in the order of the rowids of the rows they hold. It
// stops at the first error that visit returns, and returns that error.
func (db *database) walkTableLeaves(rootPage int, visit func(leaf *page) error) error {
	return db.walkTablePage(rootPage, 0, visit)
}

// walkTablePage calls visit for every leaf page under the page of a table
// b-tree with the given number, which is depth levels below the root page.
func (db *database) walkTablePage(number, depth int, visit func(leaf *page) error) error {
	if depth > maxBTreeDepth {
		return fmt.Errorf("page %d is corrupt: it is part of a b-tree that is too deep", number)
	}
	p, err := db.readPage(number)
	if err != nil {
		return err
	}

	switch p.pageType {
	case pageTypeLeafTable:
		return visit(p)

	case pageTypeInteriorTable:
		// The cells of an interior page point to its child pages in the order
		// of the rowids of the rows under them. The child page with the
		// highest rowids is the right-most child.
		for i := 0; i < p.cellCount; i++ {
			cell, err := p.tableInteriorCell(i)
			if err != nil {
				return err
			}
			if err := db.walkTablePage(cell.leftChild, depth+1, visit); err != nil {
				return err
			}
		}
		return db.walkTablePage(p.rightMostChild(), depth+1, visit)

	default:
		return fmt.Errorf("page %d is not a page of a table b-tree", number)
	}
}

// scanTableBTree calls visit with the rowid and the column values of every
// row that is stored in the table b-tree with the given root page, in the
// order of the rowids. It stops at the first error that visit returns, and
// returns that error.
func (db *database) scanTableBTree(rootPage int, visit func(rowID int64, values []any) error) error {
	return db.walkTableLeaves(rootPage, func(leaf *page) error {
		for i := 0; i < leaf.cellCount; i++ {
			cell, err := leaf.tableLeafCell(i)
			if err != nil {
				return err
			}
			values, err := decodeRecord(cell.payload)
			if err != nil {
				return fmt.Errorf("page %d, cell %d: %w", leaf.number, i, err)
			}
			if err := visit(cell.rowID, values); err != nil {
				return err
			}
		}
		return nil
	})
}

// findTableRecord looks up the row with the given rowid in the table b-tree
// with the given root page, and returns the column values that are stored in
// the row. found is false if the table has no such row.
func (db *database) findTableRecord(rootPage int, rowID int64) (values []any, found bool, err error) {
	number := rootPage
	for depth := 0; depth <= maxBTreeDepth; depth++ {
		p, err := db.readPage(number)
		if err != nil {
			return nil, false, err
		}

		switch p.pageType {
		case pageTypeInteriorTable:
			// The row is under the child page of the first cell whose key
			// isn't lower than the rowid, or under the right-most child if
			// there is no such cell.
			i, err := searchCells(p.cellCount, func(i int) (bool, error) {
				cell, err := p.tableInteriorCell(i)
				return cell.rowID >= rowID, err
			})
			if err != nil {
				return nil, false, err
			}
			if i == p.cellCount {
				number = p.rightMostChild()
				continue
			}
			cell, err := p.tableInteriorCell(i)
			if err != nil {
				return nil, false, err
			}
			number = cell.leftChild

		case pageTypeLeafTable:
			// The cells of a leaf page are in the order of their rowids.
			i, err := searchCells(p.cellCount, func(i int) (bool, error) {
				cell, err := p.tableLeafCell(i)
				return cell.rowID >= rowID, err
			})
			if err != nil || i == p.cellCount {
				return nil, false, err
			}
			cell, err := p.tableLeafCell(i)
			if err != nil || cell.rowID != rowID {
				return nil, false, err
			}
			values, err := decodeRecord(cell.payload)
			if err != nil {
				return nil, false, fmt.Errorf("page %d, cell %d: %w", p.number, i, err)
			}
			return values, true, nil

		default:
			return nil, false, fmt.Errorf("page %d is not a page of a table b-tree", number)
		}
	}
	return nil, false, fmt.Errorf("page %d is corrupt: it is part of a b-tree that is too deep", number)
}

// searchIndex calls visit with the rowid of every entry of the index b-tree
// with the given root page whose first column equals key. TEXT values are
// compared with the given collating sequence, which must be the one that the
// index is ordered by. The entries are visited in the order of the index, so
// in the order of their rowids. searchIndex stops at the first error that
// visit returns, and returns that error.
func (db *database) searchIndex(rootPage int, key any, collation string, visit func(rowID int64) error) error {
	return db.searchIndexPage(rootPage, 0, key, collation, visit)
}

// searchIndexPage calls visit for the entries whose first column equals key
// on and under the page of an index b-tree with the given number, which is
// depth levels below the root page.
func (db *database) searchIndexPage(number, depth int, key any, collation string, visit func(rowID int64) error) error {
	if depth > maxBTreeDepth {
		return fmt.Errorf("page %d is corrupt: it is part of a b-tree that is too deep", number)
	}
	p, err := db.readPage(number)
	if err != nil {
		return err
	}
	if p.pageType != pageTypeLeafIndex && p.pageType != pageTypeInteriorIndex {
		return fmt.Errorf("page %d is not a page of an index b-tree", number)
	}

	// The entries of a page are in ascending order. Skip the ones that sort
	// before the key.
	first, err := searchCells(p.cellCount, func(i int) (bool, error) {
		entry, err := p.indexEntry(i)
		if err != nil {
			return false, err
		}
		return compareValues(entry.key, key, collation) >= 0, nil
	})
	if err != nil {
		return err
	}

	for i := first; i < p.cellCount; i++ {
		entry, err := p.indexEntry(i)
		if err != nil {
			return err
		}
		// Unlike the cells of a table interior page, the cells of an index
		// interior page hold entries of their own. The entries under the
		// child page of a cell sort between the entry of the previous cell
		// and the entry of this cell, so they can match the key as well.
		if !p.isLeaf() {
			if err := db.searchIndexPage(entry.leftChild, depth+1, key, collation, visit); err != nil {
				return err
			}
		}
		if compareValues(entry.key, key, collation) != 0 {
			// This entry sorts after the key, and so do all the ones that
			// follow it.
			return nil
		}
		if err := visit(entry.rowID); err != nil {
			return err
		}
	}

	// The entries that sort after the last entry of an interior page are
	// under its right-most child.
	if !p.isLeaf() {
		return db.searchIndexPage(p.rightMostChild(), depth+1, key, collation, visit)
	}
	return nil
}

// indexEntry is an entry of an index, decoded from a cell of an index page.
type indexEntry struct {
	// leftChild is the number of the child page of the cell that stores the
	// entry. It is 0 on leaf pages.
	leftChild int
	// key is the value of the first indexed column.
	key any
	// rowID is the rowid of the row that the entry points to.
	rowID int64
}

// indexEntry decodes the entry that is stored in the cell with the given
// index of an index page.
func (p *page) indexEntry(index int) (indexEntry, error) {
	cell, err := p.indexCell(index)
	if err != nil {
		return indexEntry{}, err
	}
	values, err := decodeRecord(cell.payload)
	if err != nil {
		return indexEntry{}, fmt.Errorf("page %d, cell %d: %w", p.number, index, err)
	}

	// The record of an entry holds the values of the indexed columns,
	// followed by the rowid.
	if len(values) < 2 {
		return indexEntry{}, fmt.Errorf("page %d, cell %d: the index entry has %d values, want at least 2", p.number, index, len(values))
	}
	rowID, ok := values[len(values)-1].(int64)
	if !ok {
		return indexEntry{}, fmt.Errorf("page %d, cell %d: the rowid of the index entry is not an integer", p.number, index)
	}
	return indexEntry{leftChild: cell.leftChild, key: values[0], rowID: rowID}, nil
}

// searchCells returns the index of the first of the n cells of a page for
// which isMatch returns true, or n if there is no such cell. It does a binary
// search, which takes cells that are ordered so that no cell that matches is
// followed by one that doesn't. searchCells stops calling isMatch once it has
// returned an error, and returns that error.
func searchCells(n int, isMatch func(i int) (bool, error)) (int, error) {
	var searchErr error
	i := sort.Search(n, func(i int) bool {
		if searchErr != nil {
			return true
		}
		match, err := isMatch(i)
		if err != nil {
			searchErr = err
			return true
		}
		return match
	})
	return i, searchErr
}
