package main

import (
	"bytes"
	"math"
	"testing"
)

func TestReadVarint(t *testing.T) {
	tests := []struct {
		name      string
		data      []byte
		wantValue int64
		wantSize  int
	}{
		{"zero", []byte{0x00}, 0, 1},
		{"one byte", []byte{0x78}, 120, 1},
		{"largest one-byte value", []byte{0x7f}, 127, 1},
		{"smallest two-byte value", []byte{0x81, 0x00}, 128, 2},
		{"two bytes", []byte{0x81, 0x47}, 199, 2},
		{"largest two-byte value", []byte{0xff, 0x7f}, 16383, 2},
		{"smallest three-byte value", []byte{0x81, 0x80, 0x00}, 16384, 3},
		{
			"largest eight-byte value",
			[]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x7f},
			1<<56 - 1, 8,
		},
		{
			"smallest nine-byte value",
			[]byte{0x80, 0xc0, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x00},
			1 << 56, 9,
		},
		{
			"largest integer",
			[]byte{0xbf, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
			math.MaxInt64, 9,
		},
		{
			"minus one",
			[]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
			-1, 9,
		},
		{
			"smallest integer",
			[]byte{0xc0, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x00},
			math.MinInt64, 9,
		},
		{
			// All 8 bits of the ninth byte are data: its high bit is not a
			// continuation flag.
			"ninth byte holds 8 bits",
			[]byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0xff},
			255, 9,
		},
		{"stops at the first byte without the high bit", []byte{0x05, 0xff, 0xff}, 5, 1},
		{
			"stops after nine bytes",
			[]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
			-1, 9,
		},
		{"padded with leading zero bits", []byte{0x80, 0x01}, 1, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value, size, err := readVarint(tt.data)
			if err != nil {
				t.Fatalf("readVarint(% x) returned an error: %v", tt.data, err)
			}
			if value != tt.wantValue || size != tt.wantSize {
				t.Errorf("readVarint(% x) = (%d, %d), want (%d, %d)",
					tt.data, value, size, tt.wantValue, tt.wantSize)
			}
		})
	}
}

func TestReadVarintRejectsTruncatedInput(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"one continuation byte", []byte{0x81}},
		{"two continuation bytes", []byte{0xff, 0xff}},
		{"eight continuation bytes", []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := readVarint(tt.data); err == nil {
				t.Errorf("readVarint(% x) returned no error", tt.data)
			}
		})
	}
}

// TestReadVarintRoundTrip checks readVarint against the encoder that the
// tests use to build database pages.
func TestReadVarintRoundTrip(t *testing.T) {
	values := []int64{0, 1, 127, 128, 255, 16383, 16384, 1<<21 - 1, 1 << 21, 1<<28 - 1, 1 << 28,
		1<<35 - 1, 1 << 35, 1<<42 - 1, 1 << 42, 1<<49 - 1, 1 << 49, 1<<56 - 1, 1 << 56,
		math.MaxInt64, -1, -2, math.MinInt64}

	for _, want := range values {
		encoded := appendVarint(nil, want)

		got, size, err := readVarint(encoded)
		if err != nil {
			t.Errorf("readVarint(% x) returned an error: %v", encoded, err)
			continue
		}
		if got != want || size != len(encoded) {
			t.Errorf("readVarint(% x) = (%d, %d), want (%d, %d)", encoded, got, size, want, len(encoded))
		}

		// A varint is self-delimiting: bytes that follow it don't matter.
		got, size, err = readVarint(append(bytes.Clone(encoded), 0xff, 0x00))
		if err != nil || got != want || size != len(encoded) {
			t.Errorf("readVarint(% x + trailing bytes) = (%d, %d, %v), want (%d, %d, nil)",
				encoded, got, size, err, want, len(encoded))
		}
	}
}
