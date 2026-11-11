package main

import (
	"math"
	"reflect"
	"testing"
)

// orangesSchemaSQL is the CREATE TABLE statement of the oranges table of the
// sample database.
const orangesSchemaSQL = "CREATE TABLE oranges\n(\n\tid integer primary key autoincrement,\n\tname text,\n\tdescription text\n)"

// orangesSchemaRecord returns the record of the sqlite_schema row that
// describes the oranges table of the sample database, byte for byte as it is
// stored in sample.db.
func orangesSchemaRecord() []byte {
	record := []byte{
		0x07,       // Size of the record header: 7 bytes.
		0x17,       // type: text of (23-13)/2 = 5 bytes.
		0x1b,       // name: text of (27-13)/2 = 7 bytes.
		0x1b,       // tbl_name: text of (27-13)/2 = 7 bytes.
		0x01,       // rootpage: 8-bit integer.
		0x81, 0x47, // sql: text of (199-13)/2 = 93 bytes.
	}
	record = append(record, "table"...)
	record = append(record, "oranges"...)
	record = append(record, "oranges"...)
	record = append(record, 0x04)
	return append(record, orangesSchemaSQL...)
}

func TestDecodeRecord(t *testing.T) {
	got, err := decodeRecord(orangesSchemaRecord())
	if err != nil {
		t.Fatalf("decodeRecord returned an error: %v", err)
	}

	want := []any{"table", "oranges", "oranges", int64(4), orangesSchemaSQL}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("decodeRecord = %#v, want %#v", got, want)
	}
}

func TestDecodeRecordSerialTypes(t *testing.T) {
	tests := []struct {
		name       string
		serialType byte
		body       []byte
		want       any
	}{
		{"NULL", 0, nil, nil},
		{"8-bit integer", 1, []byte{0x7f}, int64(127)},
		{"negative 8-bit integer", 1, []byte{0xff}, int64(-1)},
		{"16-bit integer", 2, []byte{0x01, 0x00}, int64(256)},
		{"negative 16-bit integer", 2, []byte{0x80, 0x00}, int64(-32768)},
		{"24-bit integer", 3, []byte{0x01, 0x00, 0x00}, int64(65536)},
		{"negative 24-bit integer", 3, []byte{0xff, 0xff, 0xfe}, int64(-2)},
		{"32-bit integer", 4, []byte{0x7f, 0xff, 0xff, 0xff}, int64(math.MaxInt32)},
		{"negative 32-bit integer", 4, []byte{0x80, 0x00, 0x00, 0x00}, int64(math.MinInt32)},
		{"48-bit integer", 5, []byte{0x01, 0x00, 0x00, 0x00, 0x00, 0x00}, int64(1 << 40)},
		{"negative 48-bit integer", 5, []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}, int64(-1)},
		{
			"64-bit integer", 6,
			[]byte{0x7f, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
			int64(math.MaxInt64),
		},
		{
			"negative 64-bit integer", 6,
			[]byte{0x80, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
			int64(math.MinInt64),
		},
		{
			"float", 7,
			[]byte{0x40, 0x09, 0x21, 0xfb, 0x54, 0x44, 0x2d, 0x18},
			math.Pi,
		},
		{"integer constant 0", 8, nil, int64(0)},
		{"integer constant 1", 9, nil, int64(1)},
		{"empty blob", 12, nil, []byte{}},
		{"blob", 16, []byte{0xde, 0xad}, []byte{0xde, 0xad}},
		{"empty text", 13, nil, ""},
		{"text", 23, []byte("hello"), "hello"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Surround the value under test with two other columns to check
			// that the right number of bytes is consumed for it.
			record := []byte{0x04, 0x01, tt.serialType, 0x01, 0x2a}
			record = append(record, tt.body...)
			record = append(record, 0x2b)

			got, err := decodeRecord(record)
			if err != nil {
				t.Fatalf("decodeRecord(% x) returned an error: %v", record, err)
			}

			want := []any{int64(42), tt.want, int64(43)}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("decodeRecord(% x) = %#v, want %#v", record, got, want)
			}
		})
	}
}

func TestDecodeRecordWithoutColumns(t *testing.T) {
	// The record header holds nothing but its own size.
	got, err := decodeRecord([]byte{0x01})
	if err != nil {
		t.Fatalf("decodeRecord returned an error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("decodeRecord = %#v, want no values", got)
	}
}

func TestDecodeRecordDoesNotAliasBlobs(t *testing.T) {
	record := []byte{0x02, 0x10, 0xde, 0xad}

	values, err := decodeRecord(record)
	if err != nil {
		t.Fatalf("decodeRecord returned an error: %v", err)
	}
	record[2] = 0x00

	if want := []any{[]byte{0xde, 0xad}}; !reflect.DeepEqual(values, want) {
		t.Errorf("blob changed along with the record: got %#v, want %#v", values, want)
	}
}

func TestDecodeRecordRejectsInvalidRecords(t *testing.T) {
	tests := []struct {
		name   string
		record []byte
	}{
		{"empty record", nil},
		{"header size of zero", []byte{0x00}},
		{"truncated header size", []byte{0x81}},
		{"header larger than the record", []byte{0x03, 0x01}},
		{"truncated serial type", []byte{0x02, 0x81}},
		{"reserved serial type 10", []byte{0x02, 0x0a}},
		{"reserved serial type 11", []byte{0x02, 0x0b}},
		{"negative serial type", []byte{0x0a, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}},
		{"missing integer", []byte{0x02, 0x04, 0x00, 0x00, 0x00}},
		{"missing text", []byte{0x02, 0x17, 'h', 'e', 'l', 'l'}},
		{"missing second value", []byte{0x03, 0x01, 0x01, 0x2a}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if values, err := decodeRecord(tt.record); err == nil {
				t.Errorf("decodeRecord(% x) = %#v, want an error", tt.record, values)
			}
		})
	}
}

// TestDecodeRecordRoundTrip checks decodeRecord against the encoder that the
// tests use to build database pages.
func TestDecodeRecordRoundTrip(t *testing.T) {
	want := []any{
		nil,
		int64(0), int64(1), int64(-1), int64(2),
		int64(127), int64(128), int64(-128), int64(-129),
		int64(32767), int64(32768), int64(-32768), int64(-32769),
		int64(8388607), int64(8388608), int64(-8388608), int64(-8388609),
		int64(math.MaxInt32), int64(math.MaxInt32 + 1), int64(math.MinInt32), int64(math.MinInt32 - 1),
		int64(1<<47 - 1), int64(1 << 47), int64(-1 << 47), int64(-1<<47 - 1),
		int64(math.MaxInt64), int64(math.MinInt64),
		0.0, -1.5, math.MaxFloat64, math.SmallestNonzeroFloat64, math.Inf(1), math.Inf(-1),
		"", "a", "Granny Smith", "naïve ☃",
		[]byte{}, []byte{0x00}, []byte{0x01, 0x02, 0x03},
	}

	got, err := decodeRecord(encodeRecord(want...))
	if err != nil {
		t.Fatalf("decodeRecord returned an error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("decodeRecord = %#v, want %#v", got, want)
	}
}

func TestDecodeRecordWithLargeHeader(t *testing.T) {
	// With 200 columns, the header size takes a 2-byte varint.
	want := make([]any, 200)
	for i := range want {
		want[i] = int64(i + 100)
	}

	got, err := decodeRecord(encodeRecord(want...))
	if err != nil {
		t.Fatalf("decodeRecord returned an error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("decodeRecord = %#v, want %#v", got, want)
	}
}
