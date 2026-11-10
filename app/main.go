package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	// Available if you need it!
	// "github.com/xwb1989/sqlparser"
)

// headerSize is the size in bytes of the database header that every SQLite
// database file starts with.
const headerSize = 100

// headerMagic is the string every SQLite database file starts with.
var headerMagic = []byte("SQLite format 3\x00")

// Usage: your_program.sh sample.db .dbinfo
func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "Usage: your_program.sh <database file> <command>")
		os.Exit(1)
	}

	if err := run(os.Args[1], os.Args[2], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run executes command against the database file at databaseFilePath and
// writes the command's output to stdout.
func run(databaseFilePath, command string, stdout io.Writer) error {
	switch command {
	case ".dbinfo":
		databaseFile, err := os.Open(databaseFilePath)
		if err != nil {
			return err
		}
		defer databaseFile.Close()

		pageSize, err := readPageSize(databaseFile)
		if err != nil {
			return err
		}

		_, err = fmt.Fprintf(stdout, "database page size: %v\n", pageSize)
		return err
	default:
		return fmt.Errorf("Unknown command %s", command)
	}
}

// readPageSize reads the database header from r, which must be positioned at
// the start of a database file, and returns the database page size in bytes.
func readPageSize(r io.Reader) (uint32, error) {
	header := make([]byte, headerSize)
	if _, err := io.ReadFull(r, header); err != nil {
		return 0, fmt.Errorf("failed to read database header: %w", err)
	}
	if !bytes.HasPrefix(header, headerMagic) {
		return 0, errors.New("file is not a SQLite database: bad magic string")
	}

	// The page size is a 2-byte big-endian integer stored right after the
	// magic string. A page size of 65536 doesn't fit in 2 bytes, so it is
	// stored as 1.
	pageSize := uint32(binary.BigEndian.Uint16(header[16:18]))
	if pageSize == 1 {
		pageSize = 65536
	}
	return pageSize, nil
}
