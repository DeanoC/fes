// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash/crc32"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The same C decoder used by the firmware consumes actual arena publications.
// Its context, owner, generation, chunk geometry and role opt-in arrive through
// independent fixture arguments, rather than being adopted from the request.
func decodeRolePublication(t *testing.T, decoder string, raw []byte, source HPSBootObservation, chunk int) ([]byte, error) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "role-publication.bin")
	if err := os.WriteFile(p, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return exec.Command(decoder, p, "owned-role", fmt.Sprint(source.Bridges.Generation), hex.EncodeToString(source.Digest[:]), fmt.Sprint(chunk)).CombinedOutput()
}

func TestProgramRoleEncodingAndArenaJoinTheActualSecureDecoder(t *testing.T) {
	decoder := buildProgramDecoder(t)
	source, m := bindingFixture(t)
	m.CoreSize = 132
	memory := make([]byte, ProgramMetadataBytes+(16<<10))
	arena, err := NewProgramArena(0x94800000, memory, func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	var previous *Region
	for cycle, role := range []programRole{programRoleMenu, programRoleComputer, programRoleMenu, programRoleComputer, programRoleMenu} {
		var rom []byte
		if role == programRoleComputer {
			romBytes := 4096
			if cycle == 3 {
				romBytes = 8192
			}
			rom = bytes.Repeat([]byte{byte(cycle)}, romBytes)
		}
		source.Bridges.Generation = uint64(4 + cycle*6)
		m.PackageID = strings.Repeat(fmt.Sprint(cycle+1), 64)
		header, offset, err := encodeRoleProgramBinding(source, m, role, rom)
		if err != nil || len(header) != 768 {
			t.Fatal(err, len(header))
		}
		r, err := arena.Reserve(bytes.Repeat([]byte{byte(cycle + 1)}, m.CoreSize))
		if err != nil {
			t.Fatal(err)
		}
		if len(rom) != 0 {
			if err := r.StageROM(rom); err != nil {
				t.Fatal(err)
			}
		}
		if err := r.PublishConfiguration(header); err != nil {
			t.Fatal(err)
		}
		publication := bytes.Clone(memory[16<<10 : (16<<10)+len(header)])
		clear(header)
		if previous != nil && (previous.Release() == nil || previous.PublishConfiguration(publication) == nil) {
			t.Fatal("previous personality could alter the current lease")
		}
		out, err := decodeRolePublication(t, decoder, publication, source, m.ChunkBytes)
		want := fmt.Sprintf(" %d %d %08x role=%d abi=%08x\n", offset, len(rom), crc32.ChecksumIEEE(rom), role, role.controlABI())
		if err != nil || !strings.HasPrefix(string(out), "132 1 1 ") || !strings.Contains(string(out), m.PackageID) || !strings.HasSuffix(string(out), want) {
			t.Fatalf("native/secure role publication disagreed: %v %s; expected suffix %q", err, out, want)
		}
		if err := r.Release(); err != nil {
			t.Fatal(err)
		}
		previous = r
	}
}

func TestProgramRoleDoesNotOverlapTheNinthChunkDigest(t *testing.T) {
	decoder := buildProgramDecoder(t)
	source, m := bindingFixture(t)
	m.CoreSize, m.ChunkBytes = 9*64, 64
	m.ChunkSHA384 = make([]string, 9)
	for i := range m.ChunkSHA384 {
		m.ChunkSHA384[i] = strings.Repeat(fmt.Sprintf("%02x", i+1), 48)
	}
	raw, _, err := encodeRoleProgramBinding(source, m, programRoleMenu, nil)
	if err != nil {
		t.Fatal(err)
	}
	last, _ := hex.DecodeString(m.ChunkSHA384[8])
	if !bytes.Equal(raw[584:632], last) {
		t.Fatal("role overwrote final digest")
	}
	if out, err := decodeRolePublication(t, decoder, raw, source, m.ChunkBytes); err != nil || !strings.HasPrefix(string(out), "576 9 1 ") {
		t.Fatalf("nine chunks rejected: %v %s", err, out)
	}
}

func TestProgramRoleRejectsUnstagedOrCrossedResources(t *testing.T) {
	for _, fault := range []string{"menu-rom", "computer-no-rom", "abi", "unknown", "extension", "extension-tail", "size"} {
		t.Run(fault, func(t *testing.T) {
			source, m := bindingFixture(t)
			role, image := programRoleMenu, []byte(nil)
			if fault == "computer-no-rom" {
				role, image = programRoleComputer, make([]byte, 4096)
			}
			header, _, err := encodeRoleProgramBinding(source, m, role, image)
			if err != nil {
				t.Fatal(err)
			}
			arena, _ := NewProgramArena(0x94800000, make([]byte, ProgramMetadataBytes+(16<<10)), func() error { return nil })
			r, err := arena.Reserve(make([]byte, m.CoreSize))
			if err != nil {
				t.Fatal(err)
			}
			switch fault {
			case "menu-rom":
				if err := r.StageROM(make([]byte, 4096)); err != nil {
					t.Fatal(err)
				}
			case "abi":
				binary.LittleEndian.PutUint32(header[636:], programRoleComputer.controlABI())
			case "unknown":
				binary.LittleEndian.PutUint32(header[632:], 3)
			case "extension":
				header[640] = 1
			case "extension-tail":
				header[767] = 1
			case "size":
				header[12] ^= 4
			}
			if err := r.PublishConfiguration(header); err == nil || r.Release() == nil {
				t.Fatal("crossed role resources were published or released")
			}
			if _, err := arena.Reserve([]byte{1}); err == nil {
				t.Fatal("failed role publication released the arena")
			}
		})
	}
	for _, role := range []programRole{0, 3, ^programRole(0)} {
		source, m := bindingFixture(t)
		if raw, _, err := encodeRoleProgramBinding(source, m, role, nil); err == nil || raw != nil {
			t.Fatal("unknown role encoded", role)
		}
	}
	source, m := bindingFixture(t)
	for _, role := range []programRole{programRoleMenu, programRoleComputer} {
		image := make([]byte, 4096)
		if role == programRoleComputer {
			image = nil
		}
		if raw, _, err := encodeRoleProgramBinding(source, m, role, image); err == nil || raw != nil {
			t.Fatal("role encoded with invalid ROM resources", role)
		}
	}
}
