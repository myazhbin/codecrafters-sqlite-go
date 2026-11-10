package main

import (
	"fmt"
	"io"
	"os"
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
	switch command {
	case ".dbinfo":
		databaseFile, err := os.Open(databaseFilePath)
		if err != nil {
			return err
		}
		defer databaseFile.Close()

		db, err := newDatabase(databaseFile)
		if err != nil {
			return err
		}
		return printDBInfo(db, stdout)
	default:
		return fmt.Errorf("Unknown command %s", command)
	}
}

// printDBInfo writes metadata about db to stdout.
func printDBInfo(db *database, stdout io.Writer) error {
	// Every table has a row in the sqlite_schema table, and every row is
	// stored in a cell. The sqlite_schema table is assumed to fit on a single
	// page and to describe nothing but tables, so the number of cells on its
	// page is the number of tables in the database.
	schemaPage, err := db.readPage(schemaPageNumber)
	if err != nil {
		return err
	}

	if _, err := fmt.Fprintf(stdout, "database page size: %v\n", db.pageSize); err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "number of tables: %v\n", schemaPage.cellCount)
	return err
}
