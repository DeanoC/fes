// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
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

func TestWindowArenaCorruptionCannotSupplyPresentationOrReturn(t *testing.T) {
	for _, phase := range []string{"between-frames", "publication", "readback", "return"} {
		for _, at := range []int{32 << 10, (4 << 20) - 1} {
			t.Run(fmt.Sprintf("%s/%d", phase, at), func(t *testing.T) {
				f := newPresentationFixture(t)
				s := f.open(t, context.Background())
				mutated := false
				corrupt := func() {
					if !mutated {
						s.bytes[at] ^= 1
						mutated = true
					}
				}
				if phase == "between-frames" || phase == "return" {
					if _, err := s.PresentFrame(context.Background(), f.frame); err != nil {
						t.Fatal(err)
					}
				}
				switch phase {
				case "between-frames":
					corrupt()
				case "publication":
					f.barrier = func() error { corrupt(); return nil }
				case "readback":
					f.cpu.after = func(event string) {
						if event == "read" && f.cpu.starts != 0 {
							corrupt()
						}
					}
				case "return":
					f.cpu.after = func(event string) {
						if event == "closed" {
							corrupt()
						}
					}
				}
				if phase != "return" {
					got, err := s.PresentFrame(context.Background(), f.frame)
					if err == nil || got != (WindowPresentation{}) {
						t.Fatal("corrupt arena supplied a presentation", got, err)
					}
					if phase == "between-frames" && (f.publications != 1 || f.cpu.starts != 1) || phase == "publication" && f.cpu.starts != 0 {
						t.Fatal("corrupt publication reached new reader START")
					}
				}
				got, err := s.Close()
				if !mutated || err == nil || got != (WindowSessionReturn{}) || f.cpu.releases != 0 {
					t.Fatal("corrupt arena supplied return or released memory", got, err)
				}
				before := f.cpu.calls + f.cpu.reads + f.cpu.writes
				if again, againErr := s.Close(); again != got || againErr != err || f.cpu.calls+f.cpu.reads+f.cpu.writes != before {
					t.Fatal("failed return replayed hardware work", again, againErr)
				}
			})
		}
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
