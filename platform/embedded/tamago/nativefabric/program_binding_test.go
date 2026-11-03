// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func bindingFixture(t *testing.T) (HPSBootObservation, coldConfigurationMap) {
	t.Helper()
	_, raw, _ := configurationSource(t)
	var m coldConfigurationMap
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	m.BuildID = "000102030405060708090a0b0c0d0e0f"
	source := HPSBootObservation{Digest: [32]byte{0x42}, Bridges: BridgeSnapshot{Owner: 17, Generation: 1}}
	return source, m
}

func TestProgramBindingPairsNativeEncodingWithTheActualSecureCDecoder(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "program-decoder")
	argv := []string{"-std=c11", "-Wall", "-Wextra", "-Werror", "-fsanitize=undefined", "-fno-sanitize-recover=all", "-I../../firmware"}
	for _, name := range []string{"nano_program.c", "nano_configuration.c", "nano_bridges.c", "tests/nano_program_test.c"} {
		argv = append(argv, filepath.Join("../../firmware", name))
	}
	argv = append(argv, "-o", binary)
	if output, err := exec.Command("cc", argv...).CombinedOutput(); err != nil {
		t.Fatalf("secure fixture build: %v\n%s", err, output)
	}
	source, m := bindingFixture(t)
	for _, id := range []string{strings.Repeat("1", 64), strings.Repeat("2", 64)} {
		m.PackageID = id
		raw, err := encodeProgramBinding(source, m)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "request.bin")
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		output, err := exec.Command(binary, path).CombinedOutput()
		want := fmt.Sprintf("128 1 1 0c0d0e0f 08090a0b 04050607 00010203 %s\n", id)
		if err != nil || string(output) != want {
			t.Fatalf("native/secure binding differs: %v\nwant %q\ngot %q", err, want, output)
		}
	}
}

func TestProgramBindingRejectsMalformedOrUnboundedRequests(t *testing.T) {
	for _, mode := range []string{"context", "owner", "generation", "empty", "large", "unaligned", "chunk", "chunk-alignment", "count", "many-chunks", "package", "build", "digest", "zero-digest", "zero-chunk"} {
		t.Run(mode, func(t *testing.T) {
			source, m := bindingFixture(t)
			switch mode {
			case "context":
				source.Digest = [32]byte{}
			case "owner":
				source.Bridges.Owner = 0
			case "generation":
				source.Bridges.Generation = 0
			case "empty":
				m.CoreSize = 0
			case "large":
				m.CoreSize = MaxProgramBytes + 4
			case "unaligned":
				m.CoreSize++
			case "chunk":
				m.ChunkBytes = 0
			case "chunk-alignment":
				m.ChunkBytes = 0xffff0
			case "count":
				m.ChunkSHA384 = nil
			case "many-chunks":
				m.CoreSize, m.ChunkBytes = 640, 64
				m.ChunkSHA384 = make([]string, 10)
			case "package":
				m.PackageID = strings.Repeat("A", 64)
			case "build":
				m.BuildID = strings.Repeat("0", 32)
			case "digest":
				m.CoreSHA384 = "not-a-digest"
			case "zero-digest":
				m.CoreSHA256 = strings.Repeat("0", 64)
			case "zero-chunk":
				m.ChunkSHA384[0] = strings.Repeat("0", 96)
			}
			if raw, err := encodeProgramBinding(source, m); err == nil || raw != nil {
				t.Fatal("invalid binding was encoded", mode, err)
			}
		})
	}
}

func TestProgramArenaPublishesOnePrivateHeaderPerLeaseAndRevokesOldCallbacks(t *testing.T) {
	memory := bytes.Repeat([]byte{0xff}, ProgramMetadataBytes+256)
	arena, err := NewProgramArena(0x94800000, memory, func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	region, err := arena.Reserve([]byte{1, 2, 3, 4})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(memory[256:], make([]byte, ProgramMetadataBytes)) || len(region.Bytes) != 4 {
		t.Fatal("reservation exposed metadata or retained a previous header")
	}
	source, m := bindingFixture(t)
	header, err := encodeProgramBinding(source, m)
	if err != nil {
		t.Fatal(err)
	}
	want := bytes.Clone(header)
	if err := region.PublishConfiguration(header); err != nil {
		t.Fatal(err)
	}
	clear(header)
	if !bytes.Equal(memory[256:256+ProgramHeaderBytes], want) || !bytes.Equal(region.Bytes, []byte{1, 2, 3, 4}) {
		t.Fatal("publication retained caller-owned bytes or overwrote the payload")
	}
	if region.PublishConfiguration(want) == nil {
		t.Fatal("duplicate publication succeeded")
	}
	if err := region.Release(); err != nil {
		t.Fatal(err)
	}
	next, err := arena.Reserve([]byte{5, 6, 7, 8})
	if err != nil {
		t.Fatal(err)
	}
	if region.PublishConfiguration(want) == nil || region.Release() == nil || !bytes.Equal(next.Bytes, []byte{5, 6, 7, 8}) {
		t.Fatal("old callbacks altered the new lease")
	}
	if next.Release() == nil || next.PublishConfiguration(want) == nil {
		t.Fatal("unpublished completion or its retired arena was reusable")
	}
}

func TestProgramArenaRetainsChangedOrUncertainMetadata(t *testing.T) {
	for _, mode := range []string{"before-publication", "after-publication", "changed-copy", "changed-completion", "completion-mapping"} {
		t.Run(mode, func(t *testing.T) {
			memory := make([]byte, ProgramMetadataBytes+256)
			calls := 0
			arena, err := NewProgramArena(0x94800000, memory, func() error {
				calls++
				if mode == "before-publication" && calls == 3 || mode == "after-publication" && calls == 4 || mode == "completion-mapping" && calls == 5 {
					return errors.New("mapping or ordering lost")
				}
				if mode == "changed-copy" && calls == 4 {
					memory[256] ^= 1
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			region, err := arena.Reserve([]byte{1, 2, 3, 4})
			if err != nil {
				t.Fatal(err)
			}
			source, m := bindingFixture(t)
			header, err := encodeProgramBinding(source, m)
			if err != nil {
				t.Fatal(err)
			}
			err = region.PublishConfiguration(header)
			if mode == "changed-completion" || mode == "completion-mapping" {
				if err != nil {
					t.Fatal(err)
				}
				if mode == "changed-completion" {
					memory[256] ^= 1
				}
				err = region.Release()
			}
			if err == nil || region.PublishConfiguration(header) == nil || region.Release() == nil {
				t.Fatal("uncertain publication/completion was replayed or released")
			}
			if _, err := arena.Reserve([]byte{9}); err == nil {
				t.Fatal("uncertain arena was overwritten")
			}
		})
	}
}
