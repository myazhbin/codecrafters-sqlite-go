package main

import (
	"bytes"
	"encoding/binary"
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

// newHeader builds a database header whose page size field holds the given
// raw (on-disk) value.
func newHeader(rawPageSize uint16) []byte {
	header := make([]byte, headerSize)
	copy(header, headerMagic)
	binary.BigEndian.PutUint16(header[16:18], rawPageSize)
	return header
}

// testPage describes a b-tree page of a database built by buildDatabase.
type testPage struct {
	pageType  byte
	cellCount uint16
}

// buildDatabase returns the contents of a database file with the given page
// size that is made up of the given b-tree pages, the first of which is
// page 1. Only the database header and the b-tree page headers are filled in.
func buildDatabase(pageSize int, pages ...testPage) []byte {
	rawPageSize := uint16(pageSize)
	if pageSize == 65536 {
		rawPageSize = 1
	}

	data := make([]byte, pageSize*len(pages))
	copy(data, newHeader(rawPageSize))
	for i, p := range pages {
		headerOffset := i * pageSize
		if i == 0 {
			// The database header comes first on page 1.
			headerOffset += headerSize
		}
		data[headerOffset] = p.pageType
		binary.BigEndian.PutUint16(data[headerOffset+3:], p.cellCount)
	}
	return data
}
