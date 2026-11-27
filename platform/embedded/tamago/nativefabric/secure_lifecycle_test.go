// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha512"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/computer"
	cg "github.com/DeanoC/fes/platform/embedded/tamago/computer/generated"
	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
)

// The real C source supplier replaces the previously fabricated release reply.
// Activation, master-stop/close and return callbacks are real C code. MMIO,
// hashes, ROM device work and tape completion remain injected. These tests
// join the retained native runtime boundary, not target execution or DMA proof.
type secureLifecycleCPU struct {
	*programMenuCPU
	arena           *ConfigurationArena
	t               *testing.T
	input           *bufio.Writer
	output          *bufio.Reader
	fault, releases int
	diagnostics     []string
	tape            *InitialTapeArena
	tapeWire        *tapeCPU
}

func (c *secureLifecycleCPU) WritePhysical32(address uint64, value uint32) error {
	if c.tapeWire != nil {
		return c.tapeWire.WritePhysical32(address, value)
	}
	return c.programMenuCPU.WritePhysical32(address, value)
}

func (c *secureLifecycleCPU) LoadInitialTape(ctx context.Context, scope BridgeSnapshot, request computer.InitialTapeRequest) (computer.InitialTapeReceipt, error) {
	if c.tape == nil || c.tapeWire == nil || !c.tapeWire.held {
		return computer.InitialTapeReceipt{}, fmt.Errorf("test tape lacks held independent arena")
	}
	before := bytes.Clone(c.configuration.memory)
	caller := transferHashCaller(func(a [7]uint64) ([4]uint64, error) {
		if a[2] != scope.Owner || a[4] != scope.Generation || scope != c.state {
			c.t.Fatal("tape lost live scope")
		}
		return tapeStepReply(request, scope.Generation, a[1]), nil
	})
	receipt, err := c.tape.PublishInitialTape(ctx, caller, scope, request)
	if !bytes.Equal(before, c.configuration.memory) {
		c.t.Fatal("tape changed the retained configuration/ROM")
	}
	if err == nil {
		// Tape firmware completion and external CSR effects are injected here;
		// the actual C tape engine has a separate joined publication test.
		p := c.tapeWire
		p.ready = true
		p.starts++
		p.commits++
		p.image = bytes.Clone(request.Image)
		p.base.gpo = receipt.GPO
		p.base.gpi = cg.FesComputerSignature | uint32(p.base.words[15])
		if receipt.GPO&cg.FesComputerRequestMask != 0 {
			p.base.gpi |= cg.FesComputerAckMask
		}
	}
	return receipt, err
}

func (c *secureLifecycleCPU) call(command string) string {
	c.t.Helper()
	if _, err := c.input.WriteString(command + "\n"); err != nil {
		c.t.Fatal(err)
	}
	if err := c.input.Flush(); err != nil {
		c.t.Fatal(err)
	}
	for {
		line, err := c.output.ReadString('\n')
		if err != nil {
			c.t.Fatalf("secure fixture %q: %v", command, err)
		}
		if strings.HasPrefix(line, "R ") {
			return line
		}
		if !strings.HasPrefix(line, "FES_") {
			c.t.Fatalf("unexpected secure fixture output: %q", line)
		}
		if strings.HasPrefix(line, "FES_NANO_SOURCE_RELEASE_FAIL ") {
			c.diagnostics = append(c.diagnostics, line)
		}
		c.t.Log(strings.TrimSpace(line))
	}
}

func (c *secureLifecycleCPU) Invoke(a [7]uint64) ([4]uint64, error) {
	if a[0] == uint64(g.FesEmbeddedBridgeCall) && a[1] == uint64(g.FesEmbeddedBridgeRearmConfiguration) {
		// Retained reuse deliberately bypasses the external allocator. Point the
		// injected transport at the region the real arena just published.
		c.configuration.wire = &transferWire{t: c.t, source: bytes.Clone(c.arena.active.Bytes), region: c.arena.active}
	}
	if a[0] == uint64(g.FesEmbeddedBridgeCall) && a[1] == uint64(g.FesEmbeddedBridgeReleaseSource) {
		c.releases++
		fault := 0
		if c.releases == 2 {
			fault = c.fault
		}
		line := c.call(fmt.Sprintf("release %x %d %d", a[2], a[4], fault))
		var r [4]uint64
		var reason, releases int
		if n, err := fmt.Sscanf(line, "R %d %x %x %d %d %d", &r[0], &r[1], &r[2], &r[3], &reason, &releases); n != 6 || err != nil {
			c.t.Fatalf("bad release response: %q %v", line, err)
		}
		c.t.Logf("actual secure release source=%d target=%d status=%d scope_reason=%d", c.configuration.generation, a[4], r[0], reason)
		return r, nil
	}
	r, err := c.programMenuCPU.Invoke(a)
	if a[0] == isdone && r == ([4]uint64{}) && err == nil {
		header := c.configuration.memory[len(c.configuration.memory)-ProgramMetadataBytes:]
		source := c.configuration.wire.source
		digest := sha512.Sum384(source)
		role := binary.LittleEndian.Uint32(header[programRoleOffset:])
		romBytes := len(c.configuration.expectedROM)
		if role == uint32(programRoleMenu) {
			romBytes = 0
		}
		line := c.call(fmt.Sprintf("configure %x %d %d %d %d %d %x", c.state.Owner, c.state.Generation,
			role, len(source), c.configuration.chunkBytes, romBytes, digest))
		if !strings.HasPrefix(line, fmt.Sprintf("R 0 %d ", c.state.Generation)) {
			c.t.Fatal(line)
		}
	}
	return r, err
}

func secureLifecycleFixture(t *testing.T, fault int) (*programMenuFixture, *secureLifecycleCPU) {
	t.Helper()
	path := os.Getenv("FES_NANO_LIFECYCLE_RPC")
	if path == "" {
		t.Skip("test_nano_source_retirement builds the actual C supplier")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, path)
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		in.Close()
		if err := cmd.Wait(); err != nil {
			t.Errorf("secure fixture: %v", err)
		}
	})
	var cpu *secureLifecycleCPU
	f := newProgramMenuFixtureContext(t, context.Background(), "", func(f *programMenuFixture) {
		memory := make([]byte, 8<<20)
		arena, err := NewRetainedProgramArena(0x94800000, memory, func() error { return nil })
		if err != nil {
			t.Fatal(err)
		}
		f.r.f.arena = arena
		f.r.cpu.memory = memory
		cpu = &secureLifecycleCPU{programMenuCPU: f.cpu, arena: f.r.f.arena, t: t, input: bufio.NewWriter(in), output: bufio.NewReader(out), fault: fault}
		f.r.f.loader.boot.cpu = cpu
	})
	return f, cpu
}

func TestSecureLifecycleRetainedMenuComputerAndPark(t *testing.T) {
	for _, fault := range []int{0, 5, 100, 101} {
		t.Run(fmt.Sprint(fault), func(t *testing.T) {
			f, cpu := secureLifecycleFixture(t, fault)
			cpu.tapeWire = &tapeCPU{CPUTransport: cpu.programMenuCPU, base: f.r.f.cpu.computerCPU, files: f.r.f.files}
			var err error
			cpu.tape, err = NewInitialTapeArena(InitialTapeArenaBase, make([]byte, InitialTapeArenaBytes), cpu.arena, func() error { return nil })
			if err != nil {
				t.Fatal(err)
			}
			for i, size := range []int{8192, 4096, 8192, 4096} {
				request, _, _ := addROMProgram(t, f.r, i, size)
				tapeSize := []int{917, 0, 1, 16375}[i]
				if tapeSize != 0 {
					addGameTape(t, f.r.f, &request, bytes.Repeat([]byte{byte(i + 1)}, tapeSize))
				}
				session, err := f.s.Launch(context.Background(), f.s.Status().Generation, request)
				if err != nil {
					t.Fatal(err)
				}
				if f.s.Status().TapeBytes != tapeSize {
					t.Fatal("lost tape selection", f.s.Status())
				}
				if _, err := f.s.Return(context.Background(), session); err != nil {
					t.Fatal(err)
				}
				if f.s.Status().State != ProgramReady {
					t.Fatal(f.s.Status())
				}
			}
			if cpu.releases != 8 {
				t.Fatal("missing retained source release", cpu.releases)
			}
			if cpu.tapeWire.commits != 3 {
				t.Fatal("missing tape completion", cpu.tapeWire.commits)
			}
			if err := f.s.ParkMenu(context.Background()); err != nil {
				t.Fatal(err)
			}
			if f.s.Status().State != ProgramParked {
				t.Fatal(f.s.Status())
			}
		})
	}
}

func TestSecureLifecycleRejectedReturnRetainsSourceAndCannotRetry(t *testing.T) {
	for _, fault := range []int{1, 2, 4, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28} {
		t.Run(fmt.Sprint(fault), func(t *testing.T) {
			f, cpu := secureLifecycleFixture(t, fault)
			request, _, _ := addROMProgram(t, f.r, 0, 8192)
			session, err := f.s.Launch(context.Background(), f.s.Status().Generation, request)
			if err != nil {
				t.Fatal(err)
			}
			before := bytes.Clone(f.r.cpu.memory)
			if _, err := f.s.Return(context.Background(), session); err == nil || !strings.Contains(err.Error(), "secure source release") {
				t.Fatal(err)
			}
			if cpu.releases != 2 || f.s.Status().State != ProgramRebootRequired || !bytes.Equal(before, f.r.cpu.memory) {
				t.Fatal("failed release lost retained source", f.s.Status())
			}
			if fault != 2 {
				stage := map[int]int{1: 2, 3: 2, 4: 3, 6: 1, 7: 0, 8: 0}[fault]
				if fault >= 9 {
					stage = 2
				}
				clock := map[int]int{7: -110, 8: -34}[fault]
				if len(cpu.diagnostics) != 1 || !strings.Contains(cpu.diagnostics[0], fmt.Sprintf("stage=%d clock_error=%d ", stage, clock)) {
					t.Fatal("failure lost its original boundary/clock diagnostic", cpu.diagnostics)
				}
				if fault == 3 && !strings.Contains(cpu.diagnostics[0], "bad_address=9400b000 bad_value=1 ") {
					t.Fatal(cpu.diagnostics)
				}
			}
			if _, err := f.s.Return(context.Background(), session); err == nil {
				t.Fatal("replayed return")
			}
			if cpu.releases != 2 {
				t.Fatal("retried secure release")
			}
		})
	}
}
