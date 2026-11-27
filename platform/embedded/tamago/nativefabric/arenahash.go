// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"crypto/sha256"
	"crypto/sha512"
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

// sum384 reads all source bytes afresh for each whole-image or chunk digest.
func (h *arenaHash) sum384(source []byte) (result [48]byte) {
	digest := sha512.New384()
	h.write(digest, source)
	digest.Sum(result[:0])
	return result
}

// sum256And384WithChunks reads all source bytes afresh once and computes the
// whole-image SHA-256, whole-image SHA-384, and per-chunk SHA-384 digests.
func (h *arenaHash) sum256And384WithChunks(source []byte, chunk int) (sum256 [32]byte, sum384 [48]byte, chunks [][48]byte) {
	h256 := sha256.New()
	h384 := sha512.New384()
	if len(source) == 0 || chunk <= 0 {
		for len(source) != 0 {
			n := min(len(source), len(h.scratch))
			copy(h.scratch[:n], source[:n])
			s := h.scratch[:n]
			h256.Write(s)
			h384.Write(s)
			source = source[n:]
		}
		h256.Sum(sum256[:0])
		h384.Sum(sum384[:0])
		return sum256, sum384, nil
	}
	chunks = make([][48]byte, 0, (len(source)+chunk-1)/chunk)
	chunkHash := sha512.New384()
	inChunk := 0
	for len(source) != 0 {
		n := min(len(source), len(h.scratch), chunk-inChunk)
		copy(h.scratch[:n], source[:n])
		s := h.scratch[:n]
		h256.Write(s)
		h384.Write(s)
		chunkHash.Write(s)
		source = source[n:]
		inChunk += n
		if inChunk == chunk || len(source) == 0 {
			var d [48]byte
			chunkHash.Sum(d[:0])
			chunks = append(chunks, d)
			if len(source) != 0 {
				chunkHash.Reset()
				inChunk = 0
			}
		}
	}
	h256.Sum(sum256[:0])
	h384.Sum(sum384[:0])
	return sum256, sum384, chunks
}
