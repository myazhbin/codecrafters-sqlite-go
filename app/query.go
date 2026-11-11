package main

import (
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

// executeSQL runs a SQL statement against db and writes its result to stdout,
// one row per line.
func executeSQL(db *database, sql string, stdout io.Writer) error {
	statement, err := parseSelect(sql)
	if err != nil {
		return err
	}
	table, err := db.findTable(statement.table)
	if err != nil {
		return err
	}

	if statement.count {
		rowCount, err := db.countRows(table)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(stdout, rowCount)
		return err
	}

	columns := make([]int, len(statement.columns))
	for i, name := range statement.columns {
		if columns[i], err = table.columnIndex(name); err != nil {
			return err
		}
	}

	// Like the sqlite3 command-line shell does by default, separate the
	// values of a row with pipes.
	fields := make([]string, len(columns))
	return db.scanTable(table, func(r row) error {
		for i, column := range columns {
			fields[i] = formatValue(r.value(column))
		}
		_, err := fmt.Fprintln(stdout, strings.Join(fields, "|"))
		return err
	})
}

// formatValue returns the text that the sqlite3 command-line shell displays
// for a value.
func formatValue(value any) string {
	switch v := value.(type) {
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		return formatFloat(v)
	case string:
		return v
	case []byte:
		return string(v)
	default:
		// NULL is displayed as an empty string.
		return ""
	}
}

// formatFloat returns the text that SQLite converts a floating point number
// to, which is the result of formatting it with "%!.15g": at most 15
// significant digits, with a decimal point and at least one digit after it.
func formatFloat(f float64) string {
	switch {
	case math.IsInf(f, 1):
		return "Inf"
	case math.IsInf(f, -1):
		return "-Inf"
	case f == 0:
		// SQLite doesn't display the sign of a negative zero.
		return "0.0"
	}

	text := strconv.FormatFloat(f, 'g', 15, 64)
	if strings.Contains(text, ".") {
		return text
	}
	// Add the missing fractional part, before the exponent if there is one.
	mantissa, exponent, hasExponent := strings.Cut(text, "e")
	if hasExponent {
		return mantissa + ".0e" + exponent
	}
	return mantissa + ".0"
}
