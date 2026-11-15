// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"crypto/sha256"
	"hash"
)

// arenaHash reads each byte of the uncached arena afresh. Copying bounded
// chunks to private heap storage keeps the hash's many message-schedule loads
// off the external mapping. It changes neither the covered bytes nor the hash,
// and copies the entire source again on every check. The owning transfer or
// session serializes use; this scratch memory is never exposed to the FPGA.
type arenaHash struct {
	scratch [32 << 10]byte
}

func (h *arenaHash) write(digest hash.Hash, source []byte) {
	for len(source) != 0 {
		n := min(len(source), len(h.scratch))
		copy(h.scratch[:n], source[:n])
		digest.Write(h.scratch[:n])
		source = source[n:]
	}
}

func (h *arenaHash) sum(source []byte) (result [32]byte) {
	digest := sha256.New()
	h.write(digest, source)
	digest.Sum(result[:0])
	return result
}
