package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// sampleDB is the sample database that ships with the repository. It uses a
// 4096-byte page size and contains three tables: apples (4 rows),
// sqlite_sequence (2 rows) and oranges (6 rows).
const sampleDB = "../sample.db"

// runCommand runs command against the database file at path and returns
// everything the command wrote to stdout.
func runCommand(t *testing.T, path, command string) string {
	t.Helper()

	var stdout bytes.Buffer
	if err := run(path, command, &stdout); err != nil {
		t.Fatalf("run(%q, %q) returned an error: %v", path, command, err)
	}
	return stdout.String()
}

// openDatabase opens the database file at path. The file is closed when the
// test ends.
func openDatabase(t *testing.T, path string) *database {
	t.Helper()

	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { file.Close() })

	db, err := newDatabase(file)
	if err != nil {
		t.Fatalf("newDatabase returned an error: %v", err)
	}
	return db
}

// createDatabase creates a database file in a temporary directory by running
// the given SQL statements through the sqlite3 command-line shell, and returns
// the path of the file. The calling test is skipped if sqlite3 isn't
// installed.
func createDatabase(t *testing.T, sql string) string {
	t.Helper()

	sqlite3, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("sqlite3 is not installed")
	}

	path := filepath.Join(t.TempDir(), "test.db")
	cmd := exec.Command(sqlite3, "-batch", path)
	cmd.Stdin = strings.NewReader(sql)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("sqlite3 failed: %v\n%s", err, output)
	}
	return path
}

// sqlite3Output runs a SQL statement against the database file at path with
// the sqlite3 command-line shell, and returns what the shell prints. The
// calling test is skipped if sqlite3 isn't installed.
func sqlite3Output(t *testing.T, path, sql string) string {
	t.Helper()

	sqlite3, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("sqlite3 is not installed")
	}

	// The options select the default output format of the shell, whatever
	// the configuration of the user that runs the tests.
	output, err := exec.Command(sqlite3, "-batch", "-list", "-noheader", path, sql).Output()
	if err != nil {
		t.Fatalf("sqlite3 failed to run %q: %v", sql, err)
	}
	return string(output)
}

// newHeader builds a database header whose page size field holds the given
// raw (on-disk) value.
func newHeader(rawPageSize uint16) []byte {
	header := make([]byte, databaseHeaderSize)
	copy(header, databaseHeaderMagic)
	binary.BigEndian.PutUint16(header[16:18], rawPageSize)
	return header
}

// testPage describes a b-tree page of a database built by buildDatabase.
type testPage struct {
	pageType byte
	// cells holds the cells to store on the page.
	cells [][]byte
	// cellCount is the cell count to write to the page header when the page
	// has no cells.
	cellCount uint16
	// rightMostChild is the number of the right-most child page to write to
	// the page header of an interior page.
	rightMostChild uint32
}

// buildDatabase returns the contents of a database file with the given page
// size that is made up of the given b-tree pages, the first of which is
// page 1.
func buildDatabase(pageSize int, pages ...testPage) []byte {
	rawPageSize := uint16(pageSize)
	if pageSize == 65536 {
		rawPageSize = 1
	}

	data := make([]byte, pageSize*len(pages))
	copy(data, newHeader(rawPageSize))
	for i, p := range pages {
		pageData := data[i*pageSize : (i+1)*pageSize]

		headerOffset := 0
		if i == 0 {
			// The database header comes first on page 1.
			headerOffset = databaseHeaderSize
		}
		pointersOffset := headerOffset + leafPageHeaderSize
		if p.pageType == pageTypeInteriorTable || p.pageType == pageTypeInteriorIndex {
			pointersOffset = headerOffset + interiorPageHeaderSize
			binary.BigEndian.PutUint32(pageData[headerOffset+8:], p.rightMostChild)
		}

		cellCount := p.cellCount
		if len(p.cells) > 0 {
			cellCount = uint16(len(p.cells))
		}
		pageData[headerOffset] = p.pageType
		binary.BigEndian.PutUint16(pageData[headerOffset+3:], cellCount)

		// Like SQLite does, store the cells at the end of the page, growing
		// towards the cell pointer array.
		cellOffset := pageSize
		for j, cell := range p.cells {
			cellOffset -= len(cell)
			if cellOffset < pointersOffset+2*len(p.cells) {
				panic("buildDatabase: the cells don't fit on the page")
			}
			copy(pageData[cellOffset:], cell)
			binary.BigEndian.PutUint16(pageData[pointersOffset+2*j:], uint16(cellOffset))
		}
	}
	return data
}

// writeDatabaseFile writes the contents of a database file to a temporary
// directory and returns the path of the file.
func writeDatabaseFile(t *testing.T, data []byte) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "test.db")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// newTestDatabase returns a database that reads the given file contents.
func newTestDatabase(t *testing.T, data []byte) *database {
	t.Helper()

	db, err := newDatabase(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("newDatabase returned an error: %v", err)
	}
	return db
}

// appendVarint appends the varint encoding of value to buf.
func appendVarint(buf []byte, value int64) []byte {
	v := uint64(value)
	if v >= 1<<56 {
		// The value needs all 9 bytes: 7 bits in each of the first 8 bytes,
		// and its lowest 8 bits in the last byte.
		for i := 7; i >= 0; i-- {
			buf = append(buf, 0x80|(byte(v>>(8+7*i))&0x7f))
		}
		return append(buf, byte(v))
	}

	size := 1
	for size < 8 && v>>(7*size) != 0 {
		size++
	}
	for i := size - 1; i > 0; i-- {
		buf = append(buf, 0x80|(byte(v>>(7*i))&0x7f))
	}
	return append(buf, byte(v)&0x7f)
}

// encodeRecord encodes the given column values in the record format. The
// values must be nil, int, int64, float64, string or []byte.
func encodeRecord(values ...any) []byte {
	var serialTypes, body []byte
	for _, value := range values {
		switch v := value.(type) {
		case nil:
			serialTypes = appendVarint(serialTypes, 0)
		case int:
			serialTypes, body = appendInteger(serialTypes, body, int64(v))
		case int64:
			serialTypes, body = appendInteger(serialTypes, body, v)
		case float64:
			serialTypes = appendVarint(serialTypes, 7)
			body = binary.BigEndian.AppendUint64(body, math.Float64bits(v))
		case string:
			serialTypes = appendVarint(serialTypes, int64(13+2*len(v)))
			body = append(body, v...)
		case []byte:
			serialTypes = appendVarint(serialTypes, int64(12+2*len(v)))
			body = append(body, v...)
		default:
			panic(fmt.Sprintf("encodeRecord: unsupported value type %T", value))
		}
	}

	// The header size includes the varint that stores it.
	headerSize := len(serialTypes) + 1
	if headerSize > 0x7f {
		headerSize++
	}
	if headerSize > 0x3fff {
		panic("encodeRecord: the record header is too large")
	}

	record := appendVarint(nil, int64(headerSize))
	record = append(record, serialTypes...)
	return append(record, body...)
}

// appendInteger appends the serial type of an integer to serialTypes and its
// encoding to body, using the smallest encoding that can hold the integer.
func appendInteger(serialTypes, body []byte, value int64) ([]byte, []byte) {
	switch value {
	case 0:
		return appendVarint(serialTypes, 8), body
	case 1:
		return appendVarint(serialTypes, 9), body
	}

	// The sizes in bytes of the integers with serial types 1 to 6.
	sizes := []int{1, 2, 3, 4, 6, 8}
	for i, size := range sizes {
		bits := uint(8 * size)
		if size == 8 || (value >= -1<<(bits-1) && value < 1<<(bits-1)) {
			serialTypes = appendVarint(serialTypes, int64(i+1))
			for shift := int(bits) - 8; shift >= 0; shift -= 8 {
				body = append(body, byte(value>>uint(shift)))
			}
			break
		}
	}
	return serialTypes, body
}

// encodeTableLeafCell encodes a cell of a table leaf page that stores a row
// with the given rowid and column values.
func encodeTableLeafCell(rowID int64, values ...any) []byte {
	record := encodeRecord(values...)

	cell := appendVarint(nil, int64(len(record)))
	cell = appendVarint(cell, rowID)
	return append(cell, record...)
}

// encodeTableInteriorCell encodes a cell of a table interior page that points
// to the child page with the given number, and has the given rowid as its
// key.
func encodeTableInteriorCell(leftChild uint32, rowID int64) []byte {
	cell := binary.BigEndian.AppendUint32(nil, leftChild)
	return appendVarint(cell, rowID)
}
