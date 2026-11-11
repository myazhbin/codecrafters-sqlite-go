package main

import "fmt"

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
