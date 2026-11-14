// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"testing"
)

func TestProgramROMArenaStagesSeparatelyAndRevokesOldCallbacks(t *testing.T) {
	for _, size := range []int{4096, 8192} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			source, m := bindingFixture(t)
			payload := bytes.Repeat([]byte{0x73}, m.CoreSize)
			rom := bytes.Repeat([]byte{0x5a}, size)
			wantROM := bytes.Clone(rom)
			header, offset, err := encodeROMProgramBinding(source, m, rom)
			if err != nil {
				t.Fatal(err)
			}
			memory := bytes.Repeat([]byte{0xff}, ProgramMetadataBytes+(16<<10))
			arena, err := NewProgramArena(0x94800000, memory, func() error { return nil })
			if err != nil {
				t.Fatal(err)
			}
			region, err := arena.Reserve(payload)
			if err != nil {
				t.Fatal(err)
			}
			if err = region.StageROM(rom); err != nil {
				t.Fatal(err)
			}
			clear(rom)
			if len(region.Bytes) != len(payload) || cap(region.Bytes) != len(payload) || !bytes.Equal(region.Bytes, payload) ||
				!bytes.Equal(memory[offset:offset+size], wantROM) || !bytes.Equal(memory[len(payload):offset], make([]byte, offset-len(payload))) {
				t.Fatal("ROM staging changed/exposed FPGA, padding or retained source")
			}
			if err = region.StageROM(wantROM); err == nil {
				t.Fatal("duplicate ROM staging succeeded")
			}
			if err = region.PublishConfiguration(header); err != nil {
				t.Fatal(err)
			}
			wantHeader := bytes.Clone(header)
			clear(header)
			if !bytes.Equal(memory[16<<10:(16<<10)+len(wantHeader)], wantHeader) ||
				binary.LittleEndian.Uint32(wantHeader[12:]) != uint32(len(payload)) {
				t.Fatal("metadata did not preserve the separate SDM payload size")
			}
			if err = region.Release(); err != nil {
				t.Fatal(err)
			}
			next, err := arena.Reserve([]byte{1, 2, 3, 4})
			if err != nil {
				t.Fatal(err)
			}
			if region.StageROM(wantROM) == nil || region.PublishConfiguration(wantHeader) == nil || region.Release() == nil {
				t.Fatal("old callbacks authorized a new arena lease")
			}
			if !bytes.Equal(next.Bytes, []byte{1, 2, 3, 4}) {
				t.Fatal("stale ROM callback changed the next FPGA payload")
			}
		})
	}
}

func TestProgramROMArenaRejectsDescriptorAndCopyFailures(t *testing.T) {
	for _, mode := range []string{"stage-before", "stage-after", "publish-before", "publish-after", "release-order",
		"stage-copy", "before-publication-copy", "after-publication-copy", "release-copy", "descriptor-offset", "descriptor-size", "descriptor-crc", "descriptor-sha256", "descriptor-sha384", "descriptor-version"} {
		t.Run(mode, func(t *testing.T) {
			source, m := bindingFixture(t)
			rom := bytes.Repeat([]byte{0x5a}, 4096)
			header, offset, err := encodeROMProgramBinding(source, m, rom)
			if err != nil {
				t.Fatal(err)
			}
			memory := make([]byte, ProgramMetadataBytes+(16<<10))
			calls := 0
			arena, err := NewProgramArena(0x94800000, memory, func() error {
				calls++
				if mode == "stage-before" && calls == 3 || mode == "stage-after" && calls == 4 ||
					mode == "publish-before" && calls == 5 || mode == "publish-after" && calls == 6 || mode == "release-order" && calls == 7 {
					return errors.New("lost mapping/order")
				}
				if mode == "stage-copy" && calls == 4 || mode == "before-publication-copy" && calls == 5 ||
					mode == "after-publication-copy" && calls == 6 || mode == "release-copy" && calls == 7 {
					memory[offset] ^= 1
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			region, err := arena.Reserve(make([]byte, m.CoreSize))
			if err != nil {
				t.Fatal(err)
			}
			err = region.StageROM(rom)
			if err == nil {
				mutations := map[string]int{"descriptor-offset": 12, "descriptor-size": 16, "descriptor-crc": 20, "descriptor-sha256": 24, "descriptor-sha384": 56}
				if at, ok := mutations[mode]; ok {
					header[ProgramHeaderBytes+at] ^= 1
				}
				if mode == "descriptor-version" {
					header[4] = 1
				}
				err = region.PublishConfiguration(header)
			}
			if err == nil {
				err = region.Release()
			}
			if err == nil {
				t.Fatal("uncertain or substituted ROM publication succeeded")
			}
			if region.StageROM(rom) == nil || region.PublishConfiguration(header) == nil || region.Release() == nil {
				t.Fatal("uncertain ROM publication was reusable")
			}
			if next, e := arena.Reserve([]byte{1, 2, 3, 4}); e == nil || next != nil {
				t.Fatal("failed ROM publication released its pinned arena")
			}
		})
	}
}

func TestProgramROMArenaRejectsBoundsBeforeWriting(t *testing.T) {
	for _, size := range []int{0, 4095, 8193, 8192} {
		memory := bytes.Repeat([]byte{0xac}, ProgramMetadataBytes+256)
		calls := 0
		arena, err := NewProgramArena(0x94800000, memory, func() error { calls++; return nil })
		if err != nil {
			t.Fatal(err)
		}
		region, err := arena.Reserve([]byte{1, 2, 3, 4})
		if err != nil {
			t.Fatal(err)
		}
		before := bytes.Clone(memory)
		count := calls
		if region.StageROM(make([]byte, size)) == nil || calls != count || !bytes.Equal(memory, before) {
			t.Fatal("invalid ROM changed the arena", size)
		}
	}
}
