// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import "bytes"

// arenaComparison copies fresh uncached bytes into private CPU scratch before
// comparing them. Success covers both complete images; no sampled bytes or
// previous comparison result can stand in for a fresh read. The session's
// existing operation lock serializes this scratch, which is never given to DMA.
type arenaComparison struct {
	scratch [32 << 10]byte
}

func (c *arenaComparison) equal(actual, expected []byte) bool {
	equal, _ := c.compare(actual, expected, nil)
	return equal
}

// compare keeps every fresh-byte check while allowing a synchronous device
// owner to advance between bounded uncached reads. It never publishes scratch.
func (c *arenaComparison) compare(actual, expected []byte, progress func() error) (bool, error) {
	if c == nil || len(actual) != len(expected) {
		return false, nil
	}
	for len(actual) != 0 {
		if progress != nil {
			if err := progress(); err != nil {
				return false, err
			}
		}
		n := min(len(actual), len(c.scratch))
		copy(c.scratch[:n], actual[:n])
		if !bytes.Equal(c.scratch[:n], expected[:n]) {
			return false, nil
		}
		actual, expected = actual[n:], expected[n:]
	}
	return true, nil
}
