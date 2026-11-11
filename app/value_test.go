package main

import (
	"math"
	"reflect"
	"testing"
)

func TestTypeAffinity(t *testing.T) {
	tests := []struct {
		declaredType string
		want         affinity
	}{
		{"INT", affinityInteger},
		{"INTEGER", affinityInteger},
		{"TINYINT", affinityInteger},
		{"BIGINT", affinityInteger},
		{"UNSIGNED BIG INT", affinityInteger},
		{"INT8", affinityInteger},
		{"CHARACTER", affinityText},
		{"VARCHAR", affinityText},
		{"VARYING CHARACTER", affinityText},
		{"NCHAR", affinityText},
		{"TEXT", affinityText},
		{"CLOB", affinityText},
		{"BLOB", affinityBlob},
		{"", affinityBlob},
		{"REAL", affinityReal},
		{"DOUBLE", affinityReal},
		{"DOUBLE PRECISION", affinityReal},
		{"FLOAT", affinityReal},
		{"NUMERIC", affinityNumeric},
		{"DECIMAL", affinityNumeric},
		{"BOOLEAN", affinityNumeric},
		{"DATE", affinityNumeric},
		{"DATETIME", affinityNumeric},
		// The rules are applied in order, which has surprising results:
		// "FLOATING POINT" and "CHARINT" contain "INT", and "STRING" matches
		// none of the rules.
		{"FLOATING POINT", affinityInteger},
		{"CHARINT", affinityInteger},
		{"STRING", affinityNumeric},
		{"REAL TEXT", affinityText},
		{"BLOB REAL", affinityBlob},
	}

	for _, tt := range tests {
		if got := typeAffinity(tt.declaredType); got != tt.want {
			t.Errorf("typeAffinity(%q) = %d, want %d", tt.declaredType, got, tt.want)
		}
		column := columnDefinition{name: "c", declaredType: tt.declaredType}
		if got := column.affinity(); got != tt.want {
			t.Errorf("column of type %q: affinity() = %d, want %d", tt.declaredType, got, tt.want)
		}
	}
}

// sign returns -1, 0 or 1 depending on the sign of n.
func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	default:
		return 0
	}
}

func TestCompareValuesOrdersValues(t *testing.T) {
	// These values are in the order in which SQLite sorts them: NULL, then
	// numbers, then text, then blobs.
	ordered := []any{
		nil,
		math.Inf(-1),
		int64(math.MinInt64),
		-1e10,
		int64(-5),
		-1.5,
		int64(-1),
		-0.5,
		int64(0),
		0.5,
		int64(1),
		1.5,
		int64(2),
		int64(1 << 53),
		int64(1<<53 + 1),
		int64(math.MaxInt64),
		1e19,
		math.Inf(1),
		"",
		" ",
		"0",
		"10",
		"9",
		"A",
		"Apple",
		"B",
		"a",
		"apple",
		"apples",
		"b",
		"é",
		[]byte{},
		[]byte{0x00},
		[]byte{0x00, 0x01},
		[]byte{0x01},
		[]byte("a"),
		[]byte{0xff},
	}

	for i, a := range ordered {
		for j, b := range ordered {
			got := sign(compareValues(a, b, collationBinary))
			if want := sign(i - j); got != want {
				t.Errorf("compareValues(%#v, %#v) = %d, want %d", a, b, got, want)
			}
		}
	}
}

func TestCompareValuesComparesNumbersByValue(t *testing.T) {
	tests := []struct {
		a, b any
		want int
	}{
		{int64(3), int64(3), 0},
		{int64(3), 3.0, 0},
		{3.0, int64(3), 0},
		{3.0, 3.0, 0},
		{int64(0), math.Copysign(0, -1), 0},
		{int64(3), 3.5, -1},
		{3.5, int64(3), 1},
		{int64(4), 3.5, 1},
		{int64(-3), -3.5, 1},
		{int64(-4), -3.5, -1},
		{int64(0), 0.5, -1},
		{int64(0), -0.5, 1},
		// These integers can't be converted to floating point exactly: the
		// nearest floating point number is the one they're compared with.
		{int64(1<<53 + 1), float64(1 << 53), 1},
		{float64(1 << 53), int64(1<<53 + 1), -1},
		{int64(math.MaxInt64), float64(1 << 63), -1},
		{int64(math.MinInt64), -float64(1 << 63), 0},
		{int64(math.MinInt64 + 1), -float64(1 << 63), 1},
		{int64(math.MaxInt64), 1e300, -1},
		{int64(math.MinInt64), -1e300, 1},
		{int64(math.MaxInt64), math.Inf(1), -1},
		{int64(math.MinInt64), math.Inf(-1), 1},
	}

	for _, tt := range tests {
		if got := sign(compareValues(tt.a, tt.b, collationBinary)); got != tt.want {
			t.Errorf("compareValues(%#v, %#v) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestCompareValuesUsesCollation(t *testing.T) {
	tests := []struct {
		collation string
		a, b      string
		want      int
	}{
		{collationBinary, "apple", "apple", 0},
		{collationBinary, "apple", "Apple", 1},
		{collationBinary, "Zebra", "apple", -1},
		{collationBinary, "apple", "apple ", -1},
		{collationBinary, "ab", "abc", -1},
		// Text is compared byte by byte, so "é" (0xc3 0xa9) sorts after "z".
		{collationBinary, "é", "z", 1},

		{collationNoCase, "apple", "APPLE", 0},
		{collationNoCase, "Apple", "aPPLE", 0},
		{collationNoCase, "apple", "Banana", -1},
		{collationNoCase, "Zebra", "apple", 1},
		{collationNoCase, "apple", "apple ", -1},
		// Only the letters of ASCII are folded.
		{collationNoCase, "É", "é", -1},
		{collationNoCase, "[", "a", -1},
		{collationNoCase, "[", "A", -1},

		{collationRTrim, "apple", "apple   ", 0},
		{collationRTrim, "apple  ", "apple", 0},
		{collationRTrim, "   ", "", 0},
		{collationRTrim, "apple", "Apple", 1},
		{collationRTrim, " apple", "apple", -1},
		{collationRTrim, "apple\t", "apple", 1},
		{collationRTrim, "a b ", "a b", 0},
	}

	for _, tt := range tests {
		if got := sign(compareValues(tt.a, tt.b, tt.collation)); got != tt.want {
			t.Errorf("compareValues(%q, %q, %s) = %d, want %d", tt.a, tt.b, tt.collation, got, tt.want)
		}
		if got := sign(compareValues(tt.b, tt.a, tt.collation)); got != -tt.want {
			t.Errorf("compareValues(%q, %q, %s) = %d, want %d", tt.b, tt.a, tt.collation, got, -tt.want)
		}
	}

	// The collating sequence only applies to text.
	if got := compareValues([]byte("apple"), []byte("APPLE"), collationNoCase); got <= 0 {
		t.Errorf("compareValues of blobs with NOCASE = %d, want a positive number", got)
	}
}

func TestParseNumber(t *testing.T) {
	tests := []struct {
		text string
		want any
	}{
		{"0", int64(0)},
		{"42", int64(42)},
		{"-7", int64(-7)},
		{"+7", int64(7)},
		{"007", int64(7)},
		{"9223372036854775807", int64(math.MaxInt64)},
		{"-9223372036854775808", int64(math.MinInt64)},
		// Integers that don't fit in 64 bits become floating point numbers.
		{"9223372036854775808", 9223372036854775808.0},
		{"-9223372036854775809", -9223372036854775808.0},
		{"3.14", 3.14},
		{"3.0", 3.0},
		{"5.", 5.0},
		{".5", 0.5},
		{"-.5", -0.5},
		{"1e3", 1000.0},
		{"1E3", 1000.0},
		{"1.5e-2", 0.015},
		{"2e+2", 200.0},
		{"1e999", math.Inf(1)},
		// Whitespace around the number is ignored.
		{" 12 ", int64(12)},
		{"\t-1.5\n", -1.5},
	}

	for _, tt := range tests {
		got, ok := parseNumber(tt.text)
		if !ok {
			t.Errorf("parseNumber(%q) didn't find a number, want %#v", tt.text, tt.want)
			continue
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("parseNumber(%q) = %#v, want %#v", tt.text, got, tt.want)
		}
	}
}

func TestParseNumberRejectsOtherText(t *testing.T) {
	for _, text := range []string{
		"", " ", "abc", "12abc", "abc12", "1 2", "--1", "+-1", "+", "-", ".", "e5", "1e", "1e+", "1.2.3",
		"0x1F", "1_000", "1,000", "inf", "Infinity", "NaN", "٣",
	} {
		if number, ok := parseNumber(text); ok {
			t.Errorf("parseNumber(%q) = %#v, want no number", text, number)
		}
	}
}

func TestApplyAffinity(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		affinity affinity
		want     any
	}{
		{"integer in a text column", int64(7), affinityText, "7"},
		{"real in a text column", 2.5, affinityText, "2.5"},
		{"whole real in a text column", 2.0, affinityText, "2.0"},
		{"text in a text column", "7", affinityText, "7"},
		{"blob in a text column", []byte("7"), affinityText, []byte("7")},

		{"integer text in an integer column", "12", affinityInteger, int64(12)},
		{"real text in an integer column", "5.5", affinityInteger, 5.5},
		{"whole real text in an integer column", "5.0", affinityInteger, int64(5)},
		{"other text in an integer column", "five", affinityInteger, "five"},
		{"whole real in an integer column", 3.0, affinityInteger, int64(3)},
		{"negative zero in an integer column", math.Copysign(0, -1), affinityInteger, int64(0)},
		{"real in an integer column", 3.5, affinityInteger, 3.5},
		{"whole real in a numeric column", 3.0, affinityNumeric, int64(3)},
		{"integer text in a numeric column", " 12 ", affinityNumeric, int64(12)},
		// Columns with REAL affinity store whole numbers as integers, and
		// convert them back when they're read.
		{"integer in a real column", int64(5), affinityReal, int64(5)},
		{"whole real in a real column", 5.0, affinityReal, int64(5)},
		{"real in a real column", 5.25, affinityReal, 5.25},
		{"integer text in a real column", "3", affinityReal, int64(3)},
		// Numbers that are too large for an integer stay as they are.
		{"huge real in an integer column", 1e300, affinityInteger, 1e300},
		{"2^63 in an integer column", 9223372036854775808.0, affinityInteger, 9223372036854775808.0},
		{"-2^63 in an integer column", -9223372036854775808.0, affinityInteger, -9223372036854775808.0},
		{"infinity in an integer column", math.Inf(1), affinityInteger, math.Inf(1)},
		{"blob in an integer column", []byte("12"), affinityInteger, []byte("12")},

		{"integer text in an untyped column", "12", affinityBlob, "12"},
		{"whole real in an untyped column", 3.0, affinityBlob, 3.0},
		{"integer in an untyped column", int64(3), affinityBlob, int64(3)},

		{"NULL in a text column", nil, affinityText, nil},
		{"NULL in an integer column", nil, affinityInteger, nil},
		{"NULL in a real column", nil, affinityReal, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := applyAffinity(tt.value, tt.affinity); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("applyAffinity(%#v, %d) = %#v, want %#v", tt.value, tt.affinity, got, tt.want)
			}
		})
	}
}

func TestConvertForComparison(t *testing.T) {
	tests := []struct {
		name     string
		literal  any
		affinity affinity
		want     any
	}{
		// Text that spells out a number is compared as a number with the
		// values of numeric columns.
		{"integer text with an integer column", "3", affinityInteger, int64(3)},
		{"padded integer text with an integer column", " 7 ", affinityInteger, int64(7)},
		{"real text with an integer column", "3.5", affinityInteger, 3.5},
		{"integer text with a real column", "3", affinityReal, int64(3)},
		{"real text with a numeric column", "1e2", affinityNumeric, int64(100)},
		{"whole real with a real column", 2.0, affinityReal, int64(2)},
		{"other text with an integer column", "abc", affinityInteger, "abc"},
		{"empty text with a numeric column", "", affinityNumeric, ""},
		{"integer with an integer column", int64(3), affinityInteger, int64(3)},
		{"real with an integer column", 2.5, affinityInteger, 2.5},

		// Numbers are compared as text with the values of text columns.
		{"integer with a text column", int64(5), affinityText, "5"},
		{"negative integer with a text column", int64(-5), affinityText, "-5"},
		{"real with a text column", 2.5, affinityText, "2.5"},
		{"whole real with a text column", 2.0, affinityText, "2.0"},
		{"text with a text column", "3", affinityText, "3"},

		// The values of columns without a type are compared as they are.
		{"integer text with an untyped column", "3", affinityBlob, "3"},
		{"integer with an untyped column", int64(3), affinityBlob, int64(3)},
		{"real with an untyped column", 2.5, affinityBlob, 2.5},

		{"NULL with an integer column", nil, affinityInteger, nil},
		{"NULL with a text column", nil, affinityText, nil},
		{"NULL with an untyped column", nil, affinityBlob, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := convertForComparison(tt.literal, tt.affinity); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("convertForComparison(%#v, %d) = %#v, want %#v", tt.literal, tt.affinity, got, tt.want)
			}
		})
	}
}

func TestFormatValue(t *testing.T) {
	tests := []struct {
		value any
		want  string
	}{
		{nil, ""},
		{int64(0), "0"},
		{int64(42), "42"},
		{int64(-7), "-7"},
		{int64(math.MaxInt64), "9223372036854775807"},
		{int64(math.MinInt64), "-9223372036854775808"},
		{1.5, "1.5"},
		{2.0, "2.0"},
		{"", ""},
		{"Granny Smith", "Granny Smith"},
		{[]byte("hello"), "hello"},
		{[]byte{}, ""},
	}

	for _, tt := range tests {
		if got := formatValue(tt.value); got != tt.want {
			t.Errorf("formatValue(%#v) = %q, want %q", tt.value, got, tt.want)
		}
	}
}

// TestFormatFloat checks formatFloat against the text that sqlite3 displays
// for the same numbers.
func TestFormatFloat(t *testing.T) {
	tests := []struct {
		value float64
		want  string
	}{
		{1, "1.0"},
		{0.1, "0.1"},
		{-2.5, "-2.5"},
		{0, "0.0"},
		{math.Copysign(0, -1), "0.0"},
		{100, "100.0"},
		{0.1 + 0.2, "0.3"},
		{1.0 / 3, "0.333333333333333"},
		{2.0 / 3, "0.666666666666667"},
		{math.Pi, "3.14159265358979"},
		{123456789.123456789, "123456789.123457"},
		{0.0001, "0.0001"},
		// Exponents are used below 1e-4 and from 1e15 up.
		{0.00001, "1.0e-05"},
		{1.5e-7, "1.5e-07"},
		{1e14, "100000000000000.0"},
		{99999999999999.99, "100000000000000.0"},
		{1e15, "1.0e+15"},
		{999999999999999.9, "1.0e+15"},
		{123456789012345678, "1.23456789012346e+17"},
		{1e100, "1.0e+100"},
		{-1e100, "-1.0e+100"},
		{math.MaxFloat64, "1.79769313486232e+308"},
		{math.SmallestNonzeroFloat64, "4.94065645841247e-324"},
		{math.Inf(1), "Inf"},
		{math.Inf(-1), "-Inf"},
	}

	for _, tt := range tests {
		if got := formatFloat(tt.value); got != tt.want {
			t.Errorf("formatFloat(%v) = %q, want %q", tt.value, got, tt.want)
		}
	}
}
