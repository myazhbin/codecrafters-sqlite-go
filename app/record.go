package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// decodeRecord decodes a record, which is how a row is stored in a b-tree
// cell, into the values of its columns.
//
// A record is made up of a header and a body. The header starts with its own
// size in bytes, followed by one "serial type" for each column, all stored as
// varints. A serial type gives the data type and the size of a column's value.
// The body holds the values of the columns, in the same order.
//
// Each value is returned as nil (for NULL), an int64 (INTEGER), a float64
// (REAL), a string (TEXT) or a []byte (BLOB).
func decodeRecord(data []byte) ([]any, error) {
	headerSize, n, err := readVarint(data)
	if err != nil {
		return nil, fmt.Errorf("invalid record header size: %w", err)
	}
	if headerSize < int64(n) || headerSize > int64(len(data)) {
		return nil, fmt.Errorf("invalid record header size %d", headerSize)
	}
	header, body := data[n:headerSize], data[headerSize:]

	var values []any
	for len(header) > 0 {
		serialType, n, err := readVarint(header)
		if err != nil {
			return nil, fmt.Errorf("invalid serial type: %w", err)
		}
		header = header[n:]

		size, err := serialTypeSize(serialType)
		if err != nil {
			return nil, err
		}
		if size > len(body) {
			return nil, errors.New("record body is truncated")
		}
		values = append(values, decodeValue(serialType, body[:size]))
		body = body[size:]
	}
	return values, nil
}

// serialTypeSize returns the number of bytes that a value with the given
// serial type takes up in the body of a record.
func serialTypeSize(serialType int64) (int, error) {
	switch {
	case serialType < 0, serialType == 10, serialType == 11:
		// Serial types 10 and 11 are reserved for internal use by SQLite and
		// never appear in a well-formed database file.
		return 0, fmt.Errorf("invalid serial type %d", serialType)
	case serialType <= 4:
		// NULL, or a 1, 2, 3 or 4-byte integer.
		return int(serialType), nil
	case serialType == 5:
		return 6, nil
	case serialType == 6, serialType == 7:
		return 8, nil
	case serialType == 8, serialType == 9:
		// The integer constants 0 and 1 take up no space.
		return 0, nil
	}

	// BLOBs have even serial types and TEXT values have odd ones.
	size := (serialType - 12) / 2
	if size > math.MaxInt32 {
		return 0, fmt.Errorf("invalid serial type %d", serialType)
	}
	return int(size), nil
}

// decodeValue decodes a value with the given serial type. data must hold
// exactly the bytes of the value, as sized by serialTypeSize.
func decodeValue(serialType int64, data []byte) any {
	switch {
	case serialType == 0:
		return nil
	case serialType <= 6:
		// A big-endian twos-complement integer. Converting its first byte to
		// an int8 extends the sign bit.
		value := int64(int8(data[0]))
		for _, b := range data[1:] {
			value = value<<8 | int64(b)
		}
		return value
	case serialType == 7:
		return math.Float64frombits(binary.BigEndian.Uint64(data))
	case serialType == 8:
		return int64(0)
	case serialType == 9:
		return int64(1)
	case serialType%2 == 0:
		// Copy the bytes so that the BLOB doesn't alias the page it's read from.
		return append([]byte{}, data...)
	default:
		return string(data)
	}
}
