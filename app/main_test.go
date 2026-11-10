package main

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

// sampleDB is the sample database that ships with the repository. It uses a
// 4096-byte page size.
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

// newHeader builds a database header whose page size field holds the given
// raw (on-disk) value.
func newHeader(rawPageSize uint16) []byte {
	header := make([]byte, headerSize)
	copy(header, headerMagic)
	binary.BigEndian.PutUint16(header[16:18], rawPageSize)
	return header
}

func TestDBInfoPrintsPageSize(t *testing.T) {
	got := runCommand(t, sampleDB, ".dbinfo")

	if want := "database page size: 4096\n"; got != want {
		t.Errorf(".dbinfo output = %q, want %q", got, want)
	}
}

func TestRunRejectsUnknownCommand(t *testing.T) {
	var stdout bytes.Buffer
	err := run(sampleDB, ".nope", &stdout)

	if err == nil {
		t.Fatal("run with an unknown command returned no error")
	}
	if !strings.Contains(err.Error(), ".nope") {
		t.Errorf("error %q does not mention the unknown command", err)
	}
	if stdout.Len() != 0 {
		t.Errorf("unknown command wrote %q to stdout, want nothing", stdout.String())
	}
}

func TestRunReportsMissingDatabaseFile(t *testing.T) {
	var stdout bytes.Buffer
	if err := run("does-not-exist.db", ".dbinfo", &stdout); err == nil {
		t.Fatal("run with a missing database file returned no error")
	}
}

func TestReadPageSize(t *testing.T) {
	tests := []struct {
		name string
		raw  uint16
		want uint32
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
			got, err := readPageSize(bytes.NewReader(newHeader(tt.raw)))
			if err != nil {
				t.Fatalf("readPageSize returned an error: %v", err)
			}
			if got != tt.want {
				t.Errorf("readPageSize = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestReadPageSizeIsBigEndian(t *testing.T) {
	header := newHeader(0)
	header[16], header[17] = 0x10, 0x00 // 0x1000 = 4096

	got, err := readPageSize(bytes.NewReader(header))
	if err != nil {
		t.Fatalf("readPageSize returned an error: %v", err)
	}
	if got != 4096 {
		t.Errorf("readPageSize = %d, want 4096", got)
	}
}

func TestReadPageSizeRejectsTruncatedHeader(t *testing.T) {
	truncated := newHeader(4096)[:headerSize-1]

	if _, err := readPageSize(bytes.NewReader(truncated)); err == nil {
		t.Fatal("readPageSize with a truncated header returned no error")
	}
}

func TestReadPageSizeRejectsBadMagic(t *testing.T) {
	header := newHeader(4096)
	copy(header, "Not a database!!")

	if _, err := readPageSize(bytes.NewReader(header)); err == nil {
		t.Fatal("readPageSize with a bad magic string returned no error")
	}
}
