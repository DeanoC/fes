// SPDX-License-Identifier: GPL-3.0-or-later
package de25nano

import (
	"context"
	"crypto/sha512"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/DeanoC/fes/platform/embedded/tamago/computer"
	cg "github.com/DeanoC/fes/platform/embedded/tamago/computer/generated"
	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
	"github.com/DeanoC/fes/platform/embedded/tamago/nativefabric"
)

type tapeCPUFixture struct {
	calls, reads, writes int
	invoke               func([7]uint64) ([4]uint64, error)
}

func (c *tapeCPUFixture) ReadPhysical32(a uint64) (uint32, error) { c.reads++; return uint32(a), nil }
func (c *tapeCPUFixture) WritePhysical32(uint64, uint32) error    { c.writes++; return nil }
func (c *tapeCPUFixture) Invoke(r [7]uint64) ([4]uint64, error)   { c.calls++; return c.invoke(r) }

func TestProgramCPUSelectionRequiresCompiledAgreement(t *testing.T) {
	base := &tapeCPUFixture{}
	for _, pair := range [][2]bool{{false, false}, {false, true}, {true, false}, {true, true}} {
		cpu, err := selectProgramCPU(base, func() (*nativefabric.ConfigurationArena, error) { return nil, errors.New("not yet reserved") }, pair[0], pair[1])
		if pair[0] != pair[1] {
			if err == nil || cpu != nil {
				t.Fatal("mismatched selection accepted", pair)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		_, present := cpu.(nativefabric.InitialTapePlatform)
		if present != pair[0] {
			t.Fatal("unexpected supplier advertisement", pair, present)
		}
		if !present && cpu != base {
			t.Fatal("ordinary transport changed")
		}
		if _, err := cpu.ReadPhysical32(0x104); err != nil {
			t.Fatal(err)
		}
		if err := cpu.WritePhysical32(0x104, 1); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := selectProgramCPU(base, nil, true, true); err == nil {
		t.Fatal("missing arena accessor accepted")
	}
	if _, err := selectProgramCPU(nil, nil, false, false); err == nil {
		t.Fatal("missing transport accepted")
	}
}

func TestBoardTapeHookJoinsFixedArenaToActualCService(t *testing.T) {
	binaryPath := filepath.Join(t.TempDir(), "service")
	args := []string{"-std=c11", "-Wall", "-Wextra", "-Werror", "-fsanitize=undefined", "-fno-sanitize-recover=all", "-I../../../firmware", "../../../firmware/nano_tape.c", "../../../firmware/nano_tape_service.c", "../../../firmware/tests/nano_tape_service_test.c", "-o", binaryPath}
	if out, err := exec.Command("cc", args...).CombinedOutput(); err != nil {
		t.Fatalf("C service fixture: %v\n%s", err, out)
	}
	for _, tc := range []struct{ size, mode int }{{917, 0}, {16375, 0}, {917, 104}, {917, 108}, {917, 111}, {917, 114}} {
		t.Run(fmt.Sprintf("%d/%d", tc.size, tc.mode), func(t *testing.T) {
			memory := make([]byte, 8<<20)
			barriers := 0
			arena, err := nativefabric.NewProgramArena(0x94800000, memory, func() error { barriers++; return nil })
			if err != nil {
				t.Fatal(err)
			}
			previous, err := arena.Reserve([]byte{1, 2, 3, 4})
			if err != nil {
				t.Fatal(err)
			}
			// Model a completed program lease; this supplies no firmware authority.
			if err := previous.PublishConfiguration(make([]byte, nativefabric.ProgramHeaderBytes)); err != nil {
				t.Fatal(err)
			}
			if err := previous.Release(); err != nil {
				t.Fatal(err)
			}
			barriers = 0
			image := make([]byte, tc.size)
			for i := range image {
				image[i] = byte(i*37 + 11)
			}
			var build [16]byte
			b, _ := hex.DecodeString("0123456789abcdef1032547698badcfe")
			copy(build[:], b)
			request := computer.InitialTapeRequest{Image: image, GPO: 0x82000000, CRC32: crc32.ChecksumIEEE(image), Identity: computer.Identity{Build: build, Capabilities: cg.FesComputerCapabilityMediaZx81Tape}}
			scope := nativefabric.BridgeSnapshot{Capabilities: nativefabric.BridgeCapabilities{Mask: 15, ControlMask: 2, HardwareDrainMask: 12, FabricHandshake: nativefabric.FabricHandshakeAbsent}, Owner: 17, Generation: 9, ResetMask: 13, MastersContained: true}
			base := &tapeCPUFixture{}
			base.invoke = func(call [7]uint64) ([4]uint64, error) {
				if call != ([7]uint64{uint64(g.FesEmbeddedTapeCall), 0, 17, uint64(g.FesEmbeddedTapeCookie), 9}) || barriers != 2 {
					t.Fatal("wrong publication/call", call, barriers)
				}
				header := memory[len(memory)-nativefabric.ProgramMetadataBytes:][:128]
				if binary.LittleEndian.Uint32(header[12:]) != uint32(tc.size) {
					t.Fatal("descriptor size")
				}
				dir := t.TempDir()
				digest := sha512.Sum384(memory[:tc.size])
				for name, data := range map[string][]byte{"header": header, "image": memory[:tc.size], "digest": digest[:]} {
					if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
						t.Fatal(err)
					}
				}
				output, err := exec.Command(binaryPath, fmt.Sprint(tc.mode), fmt.Sprint(tc.size), filepath.Join(dir, "header"), filepath.Join(dir, "image"), filepath.Join(dir, "digest")).CombinedOutput()
				if err != nil {
					t.Fatalf("service: %v\n%s", err, output)
				}
				var mode, status, tagbytes, generation, gpocrc, writes, reads uint64
				n, err := fmt.Sscanf(string(output), "PASS tape service mode=%d status=%d tagbytes=%d generation=%d gpocrc=%d writes=%d reads=%d", &mode, &status, &tagbytes, &generation, &gpocrc, &writes, &reads)
				if n != 7 || err != nil {
					t.Fatal(string(output), err)
				}
				return [4]uint64{status, tagbytes, generation, gpocrc}, nil
			}
			cpu, err := selectProgramCPU(base, func() (*nativefabric.ConfigurationArena, error) { return arena, nil }, true, true)
			if err != nil {
				t.Fatal(err)
			}
			receipt, err := cpu.(nativefabric.InitialTapePlatform).LoadInitialTape(context.Background(), scope, request)
			if tc.mode == 0 {
				if err != nil || computer.ValidateInitialTapeReceipt(request, receipt) != nil || barriers != 3 {
					t.Fatal(receipt, err, barriers)
				}
			} else {
				if err == nil || receipt != (computer.InitialTapeReceipt{}) {
					t.Fatal("failed C service completed", receipt, err)
				}
				if _, err := arena.Reserve([]byte{42}); err == nil {
					t.Fatal("uncertain lease overwritten")
				}
			}
			if base.calls != 1 || base.reads != 0 || base.writes != 0 {
				t.Fatal("hook replayed or fell back to per-word CPU I/O", base)
			}
			if _, err := cpu.(nativefabric.InitialTapePlatform).LoadInitialTape(context.Background(), scope, request); err == nil || base.calls != 1 {
				t.Fatal("tape publication replayed")
			}
		})
	}
}

func TestBoardTapeHookRequiresReservedArena(t *testing.T) {
	base := &tapeCPUFixture{invoke: func([7]uint64) ([4]uint64, error) { t.Fatal("unexpected firmware call"); return [4]uint64{}, nil }}
	cpu, err := selectProgramCPU(base, func() (*nativefabric.ConfigurationArena, error) { return nil, errors.New("arena not yet reserved") }, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cpu.(nativefabric.InitialTapePlatform).LoadInitialTape(context.Background(), nativefabric.BridgeSnapshot{}, computer.InitialTapeRequest{}); err == nil || base.calls != 0 {
		t.Fatal("unreserved source accepted")
	}
}
