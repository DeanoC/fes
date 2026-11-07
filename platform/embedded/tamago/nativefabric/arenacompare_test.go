// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"testing"
)

func TestMenuArenaBufferedEqualityCoversLengthsAlignmentAndFreshBytes(t *testing.T) {
	var comparison arenaComparison
	actual := make([]byte, (8<<20)+64)
	for i := range actual {
		actual[i] = byte(i ^ i>>7 ^ i>>19)
	}
	expected := bytes.Clone(actual)
	for _, offset := range []int{0, 1, 7, 31} {
		for _, size := range []int{0, 1, 63, 64, 65, (32 << 10) - 1, 32 << 10, (32 << 10) + 1, 3686400, 4 << 20, 8 << 20} {
			if !comparison.equal(actual[offset:offset+size], expected[offset:offset+size]) {
				t.Fatal("incomplete or unaligned comparison", offset, size)
			}
		}
	}
	for _, at := range []int{0, (32 << 10) - 1, 32 << 10, 3686399, 3686400, (4 << 20) - 1, 4 << 20, (8 << 20) - 1} {
		actual[at] ^= 1
		if comparison.equal(actual, expected) {
			t.Fatal("missed a fresh actual-byte change", at)
		}
		expected[at] ^= 1
		if !comparison.equal(actual, expected) {
			t.Fatal("reused an earlier scratch image", at)
		}
		expected[at] ^= 2
		if comparison.equal(actual, expected) {
			t.Fatal("missed a private expected-byte change", at)
		}
		expected[at] ^= 2
	}
	if comparison.equal(actual, expected[:len(expected)-1]) || comparison.equal(actual[:len(actual)-1], expected) {
		t.Fatal("prefix equality hid a length mismatch")
	}
	if !comparison.equal(nil, nil) || (*arenaComparison)(nil).equal(nil, nil) {
		t.Fatal("invalid empty/nil receiver handling")
	}
}
