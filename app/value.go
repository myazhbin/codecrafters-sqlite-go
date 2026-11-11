package main

import (
	"bytes"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// The values of columns are represented by these Go types:
//
//	nil      NULL
//	int64    INTEGER
//	float64  REAL
//	string   TEXT
//	[]byte   BLOB

// affinity is the type that a column prefers to store its values as. SQLite
// derives it from the declared type of the column.
type affinity int

const (
	// affinityBlob is the affinity of columns that store values as they are.
	affinityBlob affinity = iota
	affinityText
	affinityNumeric
	affinityInteger
	affinityReal
)

// typeAffinity returns the affinity of a column that is declared with the
// given type, which must be in upper case.
func typeAffinity(declaredType string) affinity {
	contains := func(substrings ...string) bool {
		for _, substring := range substrings {
			if strings.Contains(declaredType, substring) {
				return true
			}
		}
		return false
	}

	// SQLite applies these rules in this order.
	switch {
	case contains("INT"):
		return affinityInteger
	case contains("CHAR", "CLOB", "TEXT"):
		return affinityText
	case declaredType == "", contains("BLOB"):
		return affinityBlob
	case contains("REAL", "FLOA", "DOUB"):
		return affinityReal
	default:
		return affinityNumeric
	}
}

// applyAffinity converts a value the way SQLite does before it stores the
// value in a column with the given affinity.
func applyAffinity(value any, columnAffinity affinity) any {
	switch columnAffinity {
	case affinityText:
		// Numbers are stored as text.
		switch value.(type) {
		case int64, float64:
			return formatValue(value)
		}

	case affinityInteger, affinityNumeric, affinityReal:
		// Text that spells out a number is stored as that number.
		if text, ok := value.(string); ok {
			number, ok := parseNumber(text)
			if !ok {
				return value
			}
			value = number
		}
		// A floating point number is stored as an integer if that doesn't
		// change its value. Columns with REAL affinity do the same, but their
		// integers are read back as floating point numbers.
		if float, ok := value.(float64); ok && float > -(1<<63) && float < 1<<63 {
			if integer := int64(float); float64(integer) == float {
				return integer
			}
		}
	}
	return value
}

// convertForComparison converts a literal value for a comparison with the
// values of a column that has the given affinity. SQLite applies these
// conversions so that, for example, the text '3' equals the number 3 in a
// column of integers.
func convertForComparison(literal any, columnAffinity affinity) any {
	switch columnAffinity {
	case affinityInteger, affinityReal:
		// The values of all numeric columns are compared as numbers.
		columnAffinity = affinityNumeric
	}
	return applyAffinity(literal, columnAffinity)
}

// The collating sequences, which are the ways in which SQLite can compare
// two TEXT values.
const (
	// collationBinary compares the bytes of the values. It is the default.
	collationBinary = "BINARY"
	// collationNoCase is like collationBinary, but ignores the case of the
	// 26 letters of ASCII.
	collationNoCase = "NOCASE"
	// collationRTrim is like collationBinary, but ignores trailing spaces.
	collationRTrim = "RTRIM"
)

// compareValues compares two values the way SQLite orders them: NULL comes
// first, then INTEGER and REAL values in numeric order, then TEXT values in
// the order of the given collating sequence, and then BLOB values in the
// order of their bytes. It returns a negative number if a sorts before b, a
// positive number if a sorts after b, and 0 if they are equal.
func compareValues(a, b any, collation string) int {
	if classA, classB := storageClass(a), storageClass(b); classA != classB {
		return classA - classB
	}

	switch a := a.(type) {
	case int64:
		switch b := b.(type) {
		case int64:
			return compareOrdered(a, b)
		case float64:
			return compareIntegerToFloat(a, b)
		}
	case float64:
		switch b := b.(type) {
		case int64:
			return -compareIntegerToFloat(b, a)
		case float64:
			return compareOrdered(a, b)
		}
	case string:
		return compareText(a, b.(string), collation)
	case []byte:
		return bytes.Compare(a, b.([]byte))
	}
	// Both values are NULL.
	return 0
}

// storageClass returns the rank of the type of a value in SQLite's ordering
// of values of different types.
func storageClass(value any) int {
	switch value.(type) {
	case nil:
		return 0
	case int64, float64:
		return 1
	case string:
		return 2
	case []byte:
		return 3
	default:
		panic(fmt.Sprintf("unsupported value type %T", value))
	}
}

func compareOrdered[T int64 | float64](a, b T) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// compareIntegerToFloat compares an integer with a floating point number
// without losing precision: not every 64-bit integer can be converted to a
// floating point number exactly.
func compareIntegerToFloat(i int64, f float64) int {
	// Deal with the numbers that are outside of the range of 64-bit integers.
	if f < -(1 << 63) {
		return 1
	}
	if f >= 1<<63 {
		return -1
	}

	// Compare the integer parts first. If they are equal, the number is small
	// enough for the integer to convert to floating point exactly, or has no
	// fractional part to compare.
	if truncated := int64(f); i != truncated {
		return compareOrdered(i, truncated)
	}
	return compareOrdered(float64(i), f)
}

// compareText compares two TEXT values with the given collating sequence.
func compareText(a, b string, collation string) int {
	switch collation {
	case collationNoCase:
		return strings.Compare(lowerASCII(a), lowerASCII(b))
	case collationRTrim:
		return strings.Compare(strings.TrimRight(a, " "), strings.TrimRight(b, " "))
	default:
		return strings.Compare(a, b)
	}
}

// lowerASCII returns s with the upper case letters of ASCII converted to
// lower case. Unlike strings.ToLower, it leaves all other characters alone.
func lowerASCII(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return r
	}, s)
}

// numericLiteral matches the text of the decimal integers and floating point
// numbers that SQLite converts to numbers.
var numericLiteral = regexp.MustCompile(`^[+-]?([0-9]+\.?[0-9]*|\.[0-9]+)([eE][+-]?[0-9]+)?$`)

// parseNumber converts text to the number that it spells out: an int64 if it
// is an integer that fits in 64 bits, and a float64 otherwise. Whitespace
// around the number is ignored. ok is false if text doesn't hold a number.
func parseNumber(text string) (number any, ok bool) {
	text = strings.Trim(text, " \t\n\v\f\r")
	if !numericLiteral.MatchString(text) {
		return nil, false
	}

	if integer, err := strconv.ParseInt(text, 10, 64); err == nil {
		return integer, true
	}
	// ParseFloat returns the nearest floating point number, or an infinity,
	// for numbers that are out of range.
	float, _ := strconv.ParseFloat(text, 64)
	return float, true
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
