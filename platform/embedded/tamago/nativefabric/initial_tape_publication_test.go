// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
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
)

func tapePublicationRequest(size int) (BridgeSnapshot, computer.InitialTapeRequest) {
	scope := BridgeSnapshot{Capabilities: BridgeCapabilities{15, 2, 12, FabricHandshakeAbsent}, Owner: 17, Generation: 9, ResetMask: 13, MastersContained: true}
	image := make([]byte, size)
	for i := range image {
		image[i] = byte(i*37 + 11)
	}
	var build [16]byte
	b, _ := hex.DecodeString("0123456789abcdef1032547698badcfe")
	copy(build[:], b)
	return scope, computer.InitialTapeRequest{Image: image, Identity: computer.Identity{Build: build, Capabilities: cg.FesComputerCapabilityMediaZx81Tape}, GPO: 0x82000000, CRC32: crc32.ChecksumIEEE(image)}
}

func completedTapeArena(t *testing.T, barrier func() error) (*ConfigurationArena, *Region) {
	t.Helper()
	a, err := NewProgramArena(0x94800000, make([]byte, 8<<20), barrier)
	if err != nil {
		t.Fatal(err)
	}
	r, err := a.Reserve([]byte{1, 2, 3, 4})
	if err != nil {
		t.Fatal(err)
	}
	source, binding := bindingFixture(t)
	header, err := encodeProgramBinding(source, binding)
	if err != nil || r.PublishConfiguration(header) != nil || r.Release() != nil {
		t.Fatal("initial program lease did not complete", err)
	}
	return a, r
}

// Modeled completion only. The joined test below obtains it from the C engine.
func tapePublicationReply(r computer.InitialTapeRequest, generation uint64) [4]uint64 {
	commands := 49 + 3*((len(r.Image)+511)/512) + (len(r.Image)+1)/2
	bit := r.GPO & cg.FesComputerRequestMask
	if commands%2 != 0 {
		bit ^= cg.FesComputerRequestMask
	}
	gpo := bit | cg.FesComputerOpcodeIdentity<<24 | 15<<16
	return [4]uint64{0, uint64(g.FesEmbeddedTapeTag)<<32 | uint64(len(r.Image)), generation, uint64(r.CRC32)<<32 | uint64(gpo)}
}

func TestInitialTapePublicationRetainsAndReusesOnlyCompletedGeneration(t *testing.T) {
	scope, request := tapePublicationRequest(917)
	checks, calls := 0, 0
	a, old := completedTapeArena(t, func() error { checks++; return nil })
	checks = 0
	header, err := initialTapeHeader(scope, request)
	if err != nil {
		t.Fatal(err)
	}
	caller := transferHashCaller(func(args [7]uint64) ([4]uint64, error) {
		calls++
		want := [7]uint64{uint64(g.FesEmbeddedTapeCall), 0, scope.Owner, uint64(g.FesEmbeddedTapeCookie), scope.Generation}
		if args != want || checks != 2 || !bytes.Equal(a.metadata[:128], header) || !bytes.Equal(a.active.Bytes, request.Image) {
			t.Fatal("fixed publication/guard/call differs", args, checks)
		}
		return tapePublicationReply(request, scope.Generation), nil
	})
	receipt, err := a.PublishInitialTape(context.Background(), caller, scope, request)
	if err != nil || computer.ValidateInitialTapeReceipt(request, receipt) != nil || checks != 3 || calls != 1 || !a.ready || a.failed {
		t.Fatal(receipt, err, checks, calls)
	}
	if old.Release() == nil || old.PublishConfiguration(make([]byte, ProgramHeaderBytes)) == nil || !a.ready {
		t.Fatal("old callbacks affected tape lease")
	}
	before := bytes.Clone(a.memory)
	if _, err := a.PublishInitialTape(context.Background(), caller, scope, request); err == nil || calls != 1 || !bytes.Equal(before, a.memory) {
		t.Fatal("same-generation tape replayed")
	}
	foreign := scope
	foreign.Owner++
	foreign.Generation++
	if _, err := a.PublishInitialTape(context.Background(), caller, foreign, request); err == nil || calls != 1 {
		t.Fatal("foreign owner reused publication")
	}
	newer := scope
	newer.Generation++
	if _, err := a.PublishInitialTape(context.Background(), caller, newer, request); err == nil || calls != 1 {
		t.Fatal("generation alone bypassed the next configuration lease")
	}
	// A later configuration must complete before the next program's tape.
	next, err := a.Reserve([]byte{5, 6, 7, 8})
	if err != nil {
		t.Fatal(err)
	}
	scope.Generation++
	if _, err := a.PublishInitialTape(context.Background(), caller, scope, request); err == nil || calls != 1 {
		t.Fatal("outstanding configuration overwritten")
	}
	source, binding := bindingFixture(t)
	raw, err := encodeProgramBinding(source, binding)
	if err != nil || next.PublishConfiguration(raw) != nil || next.Release() != nil {
		t.Fatal("next program did not complete", err)
	}
	header, err = initialTapeHeader(scope, request)
	checks = 0
	if _, err = a.PublishInitialTape(context.Background(), caller, scope, request); err != nil || calls != 2 {
		t.Fatal("new completed generation could not publish", err)
	}
}

func TestInitialTapePublicationFailuresRetainMemoryWithoutRetry(t *testing.T) {
	for _, mode := range []string{"before", "copied", "after", "copy-image", "copy-header", "copy-tail", "call-error", "tag", "status", "limit", "bytes", "generation", "gpo", "crc", "cancel", "reply-image", "reply-header", "reply-tail"} {
		t.Run(mode, func(t *testing.T) {
			scope, request := tapePublicationRequest(917)
			checks, calls := 0, 0
			var arena *ConfigurationArena
			armed := false
			a, old := completedTapeArena(t, func() error {
				if !armed {
					return nil
				}
				checks++
				if mode == "before" && checks == 1 || mode == "copied" && checks == 2 || mode == "after" && checks == 3 {
					return errors.New("mapping lost")
				}
				if checks == 2 {
					switch mode {
					case "copy-image":
						arena.memory[0] ^= 1
					case "copy-header":
						arena.metadata[55] ^= 1
					case "copy-tail":
						arena.metadata[len(arena.metadata)-1] ^= 1
					}
				}
				return nil
			})
			arena, armed = a, true
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			caller := transferHashCaller(func([7]uint64) ([4]uint64, error) {
				calls++
				r := tapePublicationReply(request, scope.Generation)
				switch mode {
				case "call-error":
					return r, errors.New("uncertain SMC")
				case "tag":
					r[1] ^= 1 << 32
				case "status":
					r[0] = uint64(g.FesEmbeddedTapeRejected)
				case "limit":
					r[0] = uint64(g.FesEmbeddedTapeLimit)
				case "bytes":
					r[1]++
				case "generation":
					r[2]++
				case "gpo":
					r[3] ^= 1
				case "crc":
					r[3] ^= 1 << 32
				case "cancel":
					cancel()
				case "reply-image":
					a.memory[0] ^= 1
				case "reply-header":
					a.metadata[55] ^= 1
				case "reply-tail":
					a.metadata[len(a.metadata)-1] ^= 1
				}
				return r, nil
			})
			receipt, err := a.PublishInitialTape(ctx, caller, scope, request)
			if err == nil || receipt != (computer.InitialTapeReceipt{}) || !a.failed {
				t.Fatal("uncertain publication completed", receipt, err)
			}
			if mode == "limit" && !errors.Is(err, computer.ErrLimit) {
				t.Fatal("deadline classification lost", err)
			}
			if mode == "before" || mode == "copied" || mode == "copy-image" || mode == "copy-header" || mode == "copy-tail" {
				if calls != 0 {
					t.Fatal("known failure reached firmware")
				}
			} else if calls != 1 {
				t.Fatal("firmware call count", calls)
			}
			before, oldCalls, oldChecks := bytes.Clone(a.memory), calls, checks
			if _, err := a.PublishInitialTape(context.Background(), caller, scope, request); err == nil || calls != oldCalls || checks != oldChecks {
				t.Fatal("retired publication replayed")
			}
			if _, err := a.Reserve([]byte{42}); err == nil || old.Release() == nil || !bytes.Equal(before, a.memory) {
				t.Fatal("uncertain memory released/overwritten")
			}
		})
	}
}

func TestInitialTapePublicationRejectsInvalidScopeAndSourceBeforeWriting(t *testing.T) {
	for _, mode := range []string{"owner", "generation", "reset", "master", "never", "caps", "build", "minor", "media", "crc", "run", "empty", "large", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			scope, request := tapePublicationRequest(917)
			a, _ := completedTapeArena(t, func() error { return nil })
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch mode {
			case "owner":
				scope.Owner = 0
			case "generation":
				scope.Generation = 0
			case "reset":
				scope.ResetMask = 15
			case "master":
				scope.MastersContained = false
			case "never":
				scope.NeverEnabled = true
			case "caps":
				scope.Capabilities.ControlMask = 1
			case "build":
				request.Identity.Build = [16]byte{}
			case "minor":
				request.Identity.Minor++
			case "media":
				request.Identity.Capabilities = 0
			case "crc":
				request.CRC32++
			case "run":
				request.GPO |= 1
			case "empty":
				request.Image = nil
			case "large":
				request.Image = make([]byte, 16376)
			case "cancel":
				cancel()
			}
			before := bytes.Clone(a.memory)
			calls := 0
			_, err := a.PublishInitialTape(ctx, transferHashCaller(func([7]uint64) ([4]uint64, error) { calls++; return [4]uint64{}, nil }), scope, request)
			if err == nil || calls != 0 || !bytes.Equal(before, a.memory) || a.failed || !a.ready {
				t.Fatal("invalid request changed completed lease", err, calls)
			}
		})
	}
}

func TestInitialTapePublicationJoinsGoDescriptorToSecureCService(t *testing.T) {
	binaryPath := filepath.Join(t.TempDir(), "service")
	args := []string{"-std=c11", "-Wall", "-Wextra", "-Werror", "-fsanitize=undefined", "-fno-sanitize-recover=all", "-I../../firmware", "../../firmware/nano_tape.c", "../../firmware/nano_tape_service.c", "../../firmware/tests/nano_tape_service_test.c", "-o", binaryPath}
	if output, err := exec.Command("cc", args...).CombinedOutput(); err != nil {
		t.Fatalf("C fixture: %v\n%s", err, output)
	}
	for _, size := range []int{1, 512, 917, 16375} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			scope, request := tapePublicationRequest(size)
			a, _ := completedTapeArena(t, func() error { return nil })
			caller := transferHashCaller(func(call [7]uint64) ([4]uint64, error) {
				if call != [7]uint64{uint64(g.FesEmbeddedTapeCall), 0, 17, uint64(g.FesEmbeddedTapeCookie), 9} {
					t.Fatal("unexpected call", call)
				}
				dir := t.TempDir()
				digest := sha512.Sum384(a.active.Bytes)
				for name, data := range map[string][]byte{"header": a.metadata[:128], "image": a.active.Bytes, "digest": digest[:]} {
					if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
						t.Fatal(err)
					}
				}
				output, err := exec.Command(binaryPath, "0", fmt.Sprint(size), filepath.Join(dir, "header"), filepath.Join(dir, "image"), filepath.Join(dir, "digest")).CombinedOutput()
				if err != nil {
					t.Fatalf("service: %v\n%s", err, output)
				}
				var mode, status, tagbytes, generation, gpocrc, writes, reads uint64
				if n, err := fmt.Sscanf(string(output), "PASS tape service mode=%d status=%d tagbytes=%d generation=%d gpocrc=%d writes=%d reads=%d", &mode, &status, &tagbytes, &generation, &gpocrc, &writes, &reads); n != 7 || err != nil || writes == 0 || reads == 0 {
					t.Fatal(string(output), err)
				}
				if binary.LittleEndian.Uint32(a.metadata[12:]) != uint32(size) {
					t.Fatal("descriptor size")
				}
				return [4]uint64{status, tagbytes, generation, gpocrc}, nil
			})
			receipt, err := a.PublishInitialTape(context.Background(), caller, scope, request)
			if err != nil || computer.ValidateInitialTapeReceipt(request, receipt) != nil || !a.ready || a.failed {
				t.Fatal(receipt, err)
			}
		})
	}
}
