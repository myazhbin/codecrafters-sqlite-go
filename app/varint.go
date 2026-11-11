package main

import "errors"

// maxVarintSize is the maximum number of bytes a varint occupies.
const maxVarintSize = 9

var errTruncatedVarint = errors.New("truncated varint")

// readVarint decodes the variable-length integer ("varint") at the start of
// data. It returns the integer and the number of bytes it occupies.
//
// A varint is a big-endian encoding of a 64-bit twos-complement integer that
// takes between 1 and 9 bytes. Each of the first 8 bytes holds 7 bits of the
// integer in its lower bits, and has its high bit set if more bytes follow.
// The 9th byte, if there is one, holds 8 bits of the integer.
func readVarint(data []byte) (value int64, size int, err error) {
	var v uint64
	for i, b := range data {
		if i == maxVarintSize-1 {
			return int64(v<<8 | uint64(b)), maxVarintSize, nil
		}
		v = v<<7 | uint64(b&0x7f)
		if b&0x80 == 0 {
			return int64(v), i + 1, nil
		}
	}
	return 0, 0, errTruncatedVarint
}
