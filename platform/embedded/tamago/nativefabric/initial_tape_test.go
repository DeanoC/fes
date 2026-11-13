// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/computer"
	cg "github.com/DeanoC/fes/platform/embedded/tamago/computer/generated"
)

// Model only the atomic platform delivery for App/store/lifecycle tests. The
// separate captured-RTL test below executes the actual C engine and CSR path.
type serviceInitialTapeCPU struct {
	*tapeCPU
	calls     int
	hookFault string
}

func (c *serviceInitialTapeCPU) LoadInitialTape(ctx context.Context, scope BridgeSnapshot, r computer.InitialTapeRequest) (computer.InitialTapeReceipt, error) {
	c.calls++
	if err := ctx.Err(); err != nil {
		return computer.InitialTapeReceipt{}, err
	}
	if scope != c.base.state || !c.held {
		return computer.InitialTapeReceipt{}, errors.New("model initial tape lacks owned Hold")
	}
	c.starts++
	c.image = bytes.Clone(r.Image)
	c.ready = true
	c.commits++
	commands := 32 + 12 + 4 + 1 + 3*((len(r.Image)+511)/512) + (len(r.Image)+1)/2
	bit := r.GPO & cg.FesComputerRequestMask
	if commands%2 != 0 {
		bit ^= cg.FesComputerRequestMask
	}
	c.base.gpo = bit | cg.FesComputerOpcodeIdentity<<24 | 15<<16
	c.base.gpi = cg.FesComputerSignature | uint32(c.base.words[15])
	if bit != 0 {
		c.base.gpi |= cg.FesComputerAckMask
	}
	receipt := computer.InitialTapeReceipt{GPO: c.base.gpo, Bytes: uint32(len(r.Image)), CRC32: crc32.ChecksumIEEE(r.Image)}
	switch c.hookFault {
	case "error":
		return computer.InitialTapeReceipt{}, errors.New("uncertain secure completion")
	case "owner":
		c.base.state.Generation++
	case "bytes":
		receipt.Bytes++
	case "crc":
		receipt.CRC32++
	case "gpo":
		receipt.GPO ^= 1
	case "mutation":
		r.Image[0] ^= 1
	case "cancel":
		c.cancel()
	case "slow-valid":
		*c.now = c.now.Add(2 * time.Second)
	case "deadline":
		budget, _ := computer.TapeTransferBudget(len(r.Image), time.Second)
		*c.now = c.now.Add(budget)
	}
	return receipt, nil
}

func TestProgramInitialTapePlatformFailureRetiresWithoutRunOrReplay(t *testing.T) {
	for _, fault := range []string{"error", "owner", "bytes", "crc", "gpo", "mutation", "cancel", "deadline"} {
		t.Run(fault, func(t *testing.T) {
			r := romProgramTestFor(t, NativeComputerProfile)
			f := r.f
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			now := time.Unix(100, 0)
			f.s.state.limits.Now = func() time.Time { return now }
			port := &serviceInitialTapeCPU{tapeCPU: &tapeCPU{CPUTransport: f.loader.boot.cpu.(CPUTransport), base: f.cpu.computerCPU, files: f.files, now: &now, cancel: cancel}, hookFault: fault}
			f.loader.boot.cpu = port
			request, _, _ := addROMProgram(t, r, 0, 8192)
			addGameTape(t, f, &request, []byte{1, 2, 3})
			_, err := f.s.Launch(ctx, 0, request)
			if !errors.Is(err, ErrProgramRetired) || !errors.Is(err, ErrProgramExecution) || port.calls != 1 || port.runs != 0 || f.lease.r.Active || f.s.Status().TapeSHA256 != "" {
				t.Fatal(err, port.calls, port.runs, f.s.Status())
			}
			calls, writes := f.cpu.calls, f.cpu.writes
			_, _ = f.s.Launch(context.Background(), f.s.Status().Generation, request)
			if port.calls != 1 || f.cpu.calls != calls || f.cpu.writes != writes {
				t.Fatal("retired initial tape replayed")
			}
		})
	}
}

type rtlInitialTapeCPU struct {
	*computerCPU
	probe           *producedProbe
	mode, transfers int
	lateOwner       bool
}

func TestInitialTapePlatformRejectsLostScopeBeforeCallingSupplier(t *testing.T) {
	v, base, control, mapping := computerFixture(t, "")
	platform := &serviceInitialTapeCPU{tapeCPU: &tapeCPU{CPUTransport: base, base: base}}
	v.cpu = platform
	s, err := v.OpenComputer(context.Background(), control, mapping, computerCompletionFixture(v, base), computerLimits())
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Computer().Hold(context.Background()); err != nil {
		t.Fatal(err)
	}
	base.state.Generation++
	writes := base.writes
	if err = s.Computer().LoadInitialTape(context.Background(), []byte{1}); err == nil || platform.calls != 0 || base.writes != writes {
		t.Fatal("lost ownership entered supplier", err, platform.calls, base.writes)
	}
	if !errors.Is(s.Computer().Run(context.Background()), computer.ErrRetired) || base.writes != writes {
		t.Fatal("lost initial scope issued Run")
	}
}

func (c *rtlInitialTapeCPU) LoadInitialTape(_ context.Context, scope BridgeSnapshot, r computer.InitialTapeRequest) (computer.InitialTapeReceipt, error) {
	c.transfers++
	if scope != c.state {
		return computer.InitialTapeReceipt{}, errors.New("unowned initial tape")
	}
	v, err := c.probe.call("secure %x %x %x %x %x %x %x %x %x", len(r.Image), r.GPO, r.Identity.Capabilities, r.Identity.Build, r.CRC32, c.mode, r.Image, scope.Owner, scope.Generation)
	if err != nil {
		return computer.InitialTapeReceipt{}, err
	}
	if len(v) != 12 || v[0] != 0 || v[1] != 1 || v[10] != scope.Owner || v[11] != scope.Generation {
		return computer.InitialTapeReceipt{}, errors.New("secure C engine rejected initial tape")
	}
	if c.lateOwner {
		c.state.Generation++
	}
	return computer.InitialTapeReceipt{GPO: uint32(v[2]), Bytes: uint32(v[3]), CRC32: uint32(v[4])}, nil
}

func initialTapeProbe(t *testing.T) *producedProbe {
	t.Helper()
	executable := os.Getenv("FES_COMPUTER_MODEL")
	if executable == "" {
		t.Skip("run computer/verify.py with captured RTL")
	}
	p := &producedProbe{command: exec.Command(executable)}
	p.command.Dir = os.Getenv("FES_COMPUTER_MODEL_DIRECTORY")
	var err error
	p.input, err = p.command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := p.command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	p.output = bufio.NewReader(output)
	p.command.Stderr = os.Stderr
	if err = p.command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		p.input.Close()
		if err := p.command.Wait(); err != nil {
			t.Error(err)
		}
	})
	return p
}

func TestSelectedRTLInitialTapePlatformLaunchReturnAndFailure(t *testing.T) {
	for _, scenario := range []struct {
		size, mode int
		late       bool
	}{{1, 0, false}, {917, 0, false}, {16375, 0, false}, {917, 1, false}, {917, 2, false}, {917, 3, false}, {917, 4, false}, {917, 5, false}, {917, 0, true}} {
		t.Run(fmt.Sprint(scenario), func(t *testing.T) {
			p := initialTapeProbe(t)
			v, base, control, mapping := computerFixture(t, os.Getenv("FES_COMPUTER_BUILD_ID"))
			base.port = p
			cpu := &rtlInitialTapeCPU{computerCPU: base, probe: p, mode: scenario.mode, lateOwner: scenario.late}
			v.cpu = cpu
			s, err := v.OpenComputer(context.Background(), control, mapping, computerCompletionFixture(v, base), computerLimits())
			if err != nil {
				t.Fatal(err)
			}
			c := s.Computer()
			ctx := context.Background()
			if err = c.Hold(ctx); err != nil {
				t.Fatal(err)
			}
			before, err := c.Activity(ctx)
			if err != nil {
				t.Fatal(err)
			}
			image := make([]byte, scenario.size)
			for i := range image {
				image[i] = byte(i*29 + 7)
			}
			err = c.LoadInitialTape(ctx, image)
			if scenario.mode != 0 || scenario.late {
				if err == nil || cpu.transfers != 1 {
					t.Fatal("bad secure scope accepted", err, cpu.transfers)
				}
				writes := cpu.writes
				if !errors.Is(c.Run(ctx), computer.ErrRetired) || !errors.Is(c.LoadInitialTape(ctx, image), computer.ErrRetired) || cpu.writes != writes || cpu.transfers != 1 {
					t.Fatal("failed secure transfer replayed or ran")
				}
				state, err := p.call("inspect")
				if err != nil || len(state) != 5 || state[0] != 1 {
					t.Fatal("failed C transfer released CPU", state, err)
				}
				_, _ = s.Close(ctx)
				return
			}
			if err != nil || cpu.transfers != 1 {
				t.Fatal(err, cpu.transfers)
			}
			after, err := c.Activity(ctx)
			if err != nil || before != after {
				t.Fatal("held execution changed", before, after, err)
			}
			for i, want := range image {
				value, err := p.call("byte %x", i)
				if err != nil || len(value) != 1 || value[0] != uint64(want) {
					t.Fatal("tape byte differs", i, value, err)
				}
			}
			if err = c.Run(ctx); err != nil {
				t.Fatal(err)
			}
			_, err = p.call("advance %x", 10000)
			if err != nil {
				t.Fatal(err)
			}
			active, err := c.Activity(ctx)
			if err != nil || active.Cycles == after.Cycles {
				t.Fatal("native Run missed fetches", active, err)
			}
			closed, err := s.Close(ctx)
			if err != nil || !closed.HoldAcknowledged || closed.After.ResetMask != 15 {
				t.Fatal("owned return failed", closed, err)
			}
		})
	}
}
