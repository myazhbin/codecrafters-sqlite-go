package main

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	// Available if you need it!
	// "github.com/xwb1989/sqlparser"
)

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
	databaseFile, err := os.Open(databaseFilePath)
	if err != nil {
		return err
	}
	defer databaseFile.Close()

	db, err := newDatabase(databaseFile)
	if err != nil {
		return err
	}

	switch command {
	case ".dbinfo":
		return printDBInfo(db, stdout)
	case ".tables":
		return printTables(db, stdout)
	default:
		return fmt.Errorf("Unknown command %s", command)
	}
}

// printDBInfo writes metadata about db to stdout.
func printDBInfo(db *database, stdout io.Writer) error {
	schema, err := db.readSchema()
	if err != nil {
		return err
	}

	tableCount := 0
	for _, object := range schema {
		if object.kind == "table" {
			tableCount++
		}
	}

	if _, err := fmt.Fprintf(stdout, "database page size: %v\n", db.pageSize); err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "number of tables: %v\n", tableCount)
	return err
}

// printTables writes the names of the tables and views of db to stdout, in
// alphabetical order and separated by spaces. The tables that SQLite creates
// for its own use, like sqlite_sequence, are left out.
func printTables(db *database, stdout io.Writer) error {
	schema, err := db.readSchema()
	if err != nil {
		return err
	}

	var names []string
	for _, object := range schema {
		if (object.kind == "table" || object.kind == "view") && !object.isInternal() {
			names = append(names, object.tableName)
		}
	}
	if len(names) == 0 {
		return nil
	}
	sort.Strings(names)

	_, err = fmt.Fprintln(stdout, strings.Join(names, " "))
	return err
}
