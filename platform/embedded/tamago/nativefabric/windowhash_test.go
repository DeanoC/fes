// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"testing"
)

func TestWindowArenaHashCoversUnalignedTailsAndFreshContents(t *testing.T) {
	var h arenaHash
	data := make([]byte, (4<<20)+31)
	for i := range data {
		data[i] = byte(i ^ i>>7 ^ i>>19)
	}
	for _, offset := range []int{0, 1, 7, 31} {
		for _, size := range []int{0, 1, 63, 64, 65, (32 << 10) - 1, 32 << 10, (32 << 10) + 1, 3686400, 4 << 20} {
			source := data[offset : offset+size]
			if got, want := h.sum(source), sha256.Sum256(source); got != want {
				t.Fatal("incomplete or misaligned arena hash", offset, size, got, want)
			}
			// Padding extends the hash of submitted pixels, so cover streaming
			// into a nonempty digest as well as the complete-arena operation.
			digest := sha256.New()
			digest.Write([]byte("submitted frame"))
			h.write(digest, source)
			want := sha256.Sum256(append([]byte("submitted frame"), source...))
			if !bytes.Equal(digest.Sum(nil), want[:]) {
				t.Fatal("streamed padding differs", offset, size)
			}
		}
	}
	prior := h.sum(data)
	for _, at := range []int{0, (32 << 10) - 1, 32 << 10, 3686399, 3686400, len(data) - 1} {
		data[at] ^= 1
		got := h.sum(data)
		if got == prior || got != sha256.Sum256(data) {
			t.Fatal("arena hash reused earlier contents", at)
		}
		prior = got
	}
}

func TestConfigurationPrivateSHA384SnapshotsKeepEveryDigestAndFreshTail(t *testing.T) {
	var h arenaHash
	data := make([]byte, (1<<20)+35)
	for i := range data {
		data[i] = byte(i ^ i>>7)
	}
	for _, offset := range []int{0, 1, 7, 31} {
		for _, size := range []int{0, 1, 127, 128, 129, 32767, 32768, 32769, 1 << 20} {
			source := data[offset : offset+size]
			if got, want := h.sum384(source), sha512.Sum384(source); got != want {
				t.Fatal(offset, size, got, want)
			}
		}
	}
	for _, at := range []int{0, 32767, 32768, len(data) - 1} {
		before := h.sum384(data)
		data[at] ^= 1
		if got := h.sum384(data); got == before || got != sha512.Sum384(data) {
			t.Fatal("stale or omitted source byte", at)
		}
	}
	payload := data[:32772]
	s256, s384 := sha256.Sum256(payload), sha512.Sum384(payload)
	m := coldConfigurationMap{CoreSize: len(payload), CoreSHA256: hex.EncodeToString(s256[:]), CoreSHA384: hex.EncodeToString(s384[:]), ChunkBytes: 32768}
	for at := 0; at < len(payload); at += m.ChunkBytes {
		s := sha512.Sum384(payload[at:min(at+m.ChunkBytes, len(payload))])
		m.ChunkSHA384 = append(m.ChunkSHA384, hex.EncodeToString(s[:]))
	}
	if err := checkConfigurationBytes(m, payload); err != nil {
		t.Fatal(err)
	}
	for _, which := range []string{"whole256", "whole384", "chunk0", "chunk-tail", "source-tail"} {
		bad := m
		bad.ChunkSHA384 = append([]string(nil), m.ChunkSHA384...)
		source := append([]byte(nil), payload...)
		switch which {
		case "whole256":
			bad.CoreSHA256 = "bad"
		case "whole384":
			bad.CoreSHA384 = "bad"
		case "chunk0":
			bad.ChunkSHA384[0] = "bad"
		case "chunk-tail":
			bad.ChunkSHA384[1] = "bad"
		case "source-tail":
			source[len(source)-1] ^= 1
		}
		if err := checkConfigurationBytes(bad, source); err == nil {
			t.Fatal("lost independent digest/freshness", which)
		}
	}
}

func TestArenaHashSinglePassMatchesMultiPassAcrossBoundaries(t *testing.T) {
	var h arenaHash
	data := make([]byte, (256<<10)+73)
	for i := range data {
		data[i] = byte(i*37 ^ i>>9)
	}
	for _, offset := range []int{0, 1, 7, 31} {
		for _, chunk := range []int{64, 32768, 65536, 131072} {
			for _, size := range []int{0, 1, chunk - 1, chunk, chunk + 1, 2*chunk + 17} {
				if offset+size > len(data) {
					continue
				}
				src := data[offset : offset+size]
				g256, g384, c384 := h.sum256And384WithChunks(src, chunk)
				if g256 != sha256.Sum256(src) || g384 != sha512.Sum384(src) {
					t.Fatalf("sum256And384WithChunks whole mismatch offset=%d chunk=%d size=%d", offset, chunk, size)
				}
				var wantC384 [][48]byte
				for at := 0; at < len(src); at += chunk {
					wantC384 = append(wantC384, sha512.Sum384(src[at:min(at+chunk, len(src))]))
				}
				if len(c384) != len(wantC384) {
					t.Fatalf("sha384 chunk count mismatch got=%d want=%d", len(c384), len(wantC384))
				}
				for i := range wantC384 {
					if c384[i] != wantC384[i] {
						t.Fatalf("chunk384[%d] mismatch offset=%d chunk=%d size=%d", i, offset, chunk, size)
					}
				}
			}
		}
	}
}

func BenchmarkCheckConfigurationBytes(b *testing.B) {
	const chunk = 0xffff0
	data := make([]byte, 1683456)
	for i := range data {
		data[i] = byte(i * 17)
	}
	s256, s384 := sha256.Sum256(data), sha512.Sum384(data)
	m := coldConfigurationMap{
		CoreSize:   len(data),
		CoreSHA256: hex.EncodeToString(s256[:]),
		CoreSHA384: hex.EncodeToString(s384[:]),
		ChunkBytes: chunk,
	}
	for at := 0; at < len(data); at += chunk {
		s := sha512.Sum384(data[at:min(at+chunk, len(data))])
		m.ChunkSHA384 = append(m.ChunkSHA384, hex.EncodeToString(s[:]))
	}
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for range b.N {
		if err := checkConfigurationBytes(m, data); err != nil {
			b.Fatal(err)
		}
	}
}
