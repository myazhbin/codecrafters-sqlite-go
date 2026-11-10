package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func TestDBInfo(t *testing.T) {
	got := runCommand(t, sampleDB, ".dbinfo")

	want := "database page size: 4096\nnumber of tables: 3\n"
	if got != want {
		t.Errorf(".dbinfo output = %q, want %q", got, want)
	}
}

func TestDBInfoCountsTables(t *testing.T) {
	for _, tableCount := range []int{1, 2, 3, 5, 8} {
		t.Run(fmt.Sprintf("%d tables", tableCount), func(t *testing.T) {
			var sql strings.Builder
			for i := 0; i < tableCount; i++ {
				fmt.Fprintf(&sql, "CREATE TABLE table_%d (id integer primary key, name text);\n", i)
			}
			path := createDatabase(t, sql.String())

			got := runCommand(t, path, ".dbinfo")

			want := fmt.Sprintf("database page size: 4096\nnumber of tables: %d\n", tableCount)
			if got != want {
				t.Errorf(".dbinfo output = %q, want %q", got, want)
			}
		})
	}
}

func TestDBInfoSupportsEveryPageSize(t *testing.T) {
	for _, pageSize := range []int{512, 1024, 2048, 4096, 8192, 16384, 32768, 65536} {
		t.Run(fmt.Sprint(pageSize), func(t *testing.T) {
			path := createDatabase(t, fmt.Sprintf(`
				PRAGMA page_size = %d;
				CREATE TABLE apples (id integer primary key, name text);
				CREATE TABLE oranges (id integer primary key, name text);
			`, pageSize))

			got := runCommand(t, path, ".dbinfo")

			want := fmt.Sprintf("database page size: %d\nnumber of tables: 2\n", pageSize)
			if got != want {
				t.Errorf(".dbinfo output = %q, want %q", got, want)
			}
		})
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
