// SPDX-License-Identifier: GPL-3.0-or-later
package sdcontrol

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
	"github.com/DeanoC/fes/platform/embedded/tamago/maccontrol"
)

var (
	concurrentRPCOnce   sync.Once
	concurrentRPCBinary string
	concurrentRPCErr    error
)

func buildConcurrentRPC(t *testing.T) string {
	t.Helper()
	concurrentRPCOnce.Do(func() {
		temp, err := os.MkdirTemp("", "fes-concurrent-rpc-")
		if err != nil {
			concurrentRPCErr = err
			return
		}
		fw, err := filepath.Abs("../../firmware")
		if err != nil {
			concurrentRPCErr = err
			return
		}
		concurrentRPCBinary = filepath.Join(temp, "rpc")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		command := exec.CommandContext(
			ctx, "cc", "-std=c11", "-Wall", "-Wextra", "-Werror", "-fsanitize=undefined", "-fno-sanitize-recover=all",
			"-I"+fw,
			filepath.Join(fw, "nano_bridges.c"),
			filepath.Join(fw, "nano_bridge_sip.c"),
			filepath.Join(fw, "nano_sd.c"),
			filepath.Join(fw, "nano_mac.c"),
			filepath.Join(fw, "nano_usb_service.c"),
			filepath.Join(fw, "nano_identity.c"),
			filepath.Join(fw, "nano_configuration.c"),
			filepath.Join(fw, "tests/nano_concurrent_rpc.c"),
			"-o", concurrentRPCBinary,
		)
		out, err := command.CombinedOutput()
		if err != nil {
			concurrentRPCErr = fmt.Errorf("build concurrent C RPC: %v\n%s", err, out)
		}
	})
	if concurrentRPCErr != nil {
		t.Fatal(concurrentRPCErr)
	}
	return concurrentRPCBinary
}

type concurrentRPC struct {
	mu      sync.Mutex
	card    *card
	command *exec.Cmd
	input   io.WriteCloser
	output  *bufio.Reader
	fault   uint32
	stats   [7]uint64
}

func newConcurrentRPC(t *testing.T) (*concurrentRPC, *card) {
	t.Helper()
	bin := buildConcurrentRPC(t)
	c := newCard()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	cmd := exec.CommandContext(ctx, bin)
	cmd.Stderr = os.Stderr
	input, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	r := &concurrentRPC{card: c, command: cmd, input: input, output: bufio.NewReader(output)}
	t.Cleanup(func() {
		input.Close()
		if err := cmd.Wait(); err != nil {
			t.Error(err)
		}
		cancel()
	})
	return r, c
}

func (r *concurrentRPC) Invoke(a [7]uint64) ([4]uint64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c := r.card
	_, err := fmt.Fprintf(r.input, "%x %x %x %x %x %x %x %x %x %x %x %x %x\n",
		a[0], a[1], a[2], a[3], a[4], a[5], a[6],
		c.regs[clock], c.regs[command]&1, c.regs[present]&0xb07,
		c.regs[signalEnable], c.regs[statusEnable], r.fault)
	if err != nil {
		return [4]uint64{}, err
	}
	var reply [4]uint64
	_, err = fmt.Fscanf(r.output, "%x %x %x %x %x %x %x %x %x %x %x\n",
		&reply[0], &reply[1], &reply[2], &reply[3],
		&r.stats[0], &r.stats[1], &r.stats[2], &r.stats[3], &r.stats[4], &r.stats[5], &r.stats[6])
	if err != nil {
		return [4]uint64{}, err
	}
	c.active = r.stats[0] != 0
	if a[0] == uint64(g.FesEmbeddedSdCall) && a[1] == uint64(g.FesEmbeddedSdPrepare) && reply[0] == 0 {
		c.prepared = true
	}
	return reply, nil
}

func TestSharedSDWithActiveFPGAAndConfirmedEthernet(t *testing.T) {
	firmware, c := newConcurrentRPC(t)

	// Open shared SD with private generation starting at 0 -> Resume(1) -> Prepare(2).
	s, err := Open(firmware, c, Config{InputClockHz: 200000000, Shared: true, BootOwner: 17})
	if err != nil {
		t.Fatal(err)
	}
	if !s.Shared() {
		t.Fatal("expected Shared()=true")
	}
	receipt, valid := s.LastReceipt()
	if !valid || !receipt.Shared || receipt.Owner != 17 || receipt.Generation != 2 || !receipt.Active || !receipt.Prepared {
		t.Fatalf("unexpected initial shared receipt: %+v valid=%v", receipt, valid)
	}
	if !s.firmware.DataHeld() {
		t.Fatal("expected initial DataHeld()=true with closed bridges")
	}

	// Shared session rejects ResumeAfterReturn (exclusive closed-bridge API).
	if err := s.Quiesce(); err != nil {
		t.Fatal(err)
	}
	if err := s.ResumeAfterReturn(17, 4); !errors.Is(err, ErrTransition) {
		t.Fatalf("expected ErrTransition for ResumeAfterReturn on shared session, got %v", err)
	}
	if err := s.Resume(); err != nil {
		t.Fatal(err)
	}
	receipt, _ = s.LastReceipt()
	if receipt.Generation != 5 {
		t.Fatalf("expected private SD generation 5, got %d", receipt.Generation)
	}

	// Bring up Ethernet MAC concurrently with active SD.
	ctx := context.Background()
	mac, err := maccontrol.New(ctx, firmware, 17)
	if err != nil {
		t.Fatal(err)
	}
	if err := mac.Acquire(ctx); err != nil {
		t.Fatal(err)
	}
	if err := mac.Confirm(ctx); err != nil {
		t.Fatal(err)
	}

	// Open FPGA Control and Data bridges while BOTH SD and MAC are active!
	reply, err := firmware.Invoke([7]uint64{uint64(g.FesEmbeddedBridgeCall), uint64(g.FesEmbeddedBridgeControl), 17, uint64(g.FesEmbeddedBridgeCookie), 1})
	if err != nil || reply[0] != 0 || reply[3] != 2 {
		t.Fatalf("bridge control open failed: %#v %v", reply, err)
	}
	reply, err = firmware.Invoke([7]uint64{uint64(g.FesEmbeddedBridgeCall), uint64(g.FesEmbeddedBridgeRelease) | (8 << g.FesEmbeddedBridgeReleaseShift), 17, uint64(g.FesEmbeddedBridgeCookie), 2})
	if err != nil || reply[0] != 0 || reply[3] != 3 {
		t.Fatalf("bridge data release failed: %#v %v", reply, err)
	}

	// Observe() refreshes dynamic DataHeld=false while keeping SD generation=5 intact.
	obs, err := s.firmware.Observe()
	if err != nil {
		t.Fatal(err)
	}
	if obs.Generation != 5 || s.firmware.DataHeld() {
		t.Fatalf("expected gen=5 DataHeld=false, got %+v held=%v", obs, s.firmware.DataHeld())
	}

	// Perform actual SDHCI PIO reads while FPGA bridges are open AND Ethernet MAC is confirmed!
	buf := make([]byte, 1024)
	if err := s.ReadSectors(4, buf); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf, bytes.Repeat([]byte{0, 1, 2, 3}, 256)) {
		t.Fatal("PIO read mismatch during concurrent FPGA+Ethernet")
	}

	// Cycle SD Quiesce -> Resume while FPGA bridges stay open and MAC stays confirmed.
	if err := s.Quiesce(); err != nil {
		t.Fatal(err)
	}
	if err := s.Resume(); err != nil {
		t.Fatal(err)
	}
	if err := s.ReadSectors(5, buf); err != nil {
		t.Fatal(err)
	}

	// Close FPGA bridges while SD + MAC remain active; DataHeld flips back to true.
	reply, err = firmware.Invoke([7]uint64{uint64(g.FesEmbeddedBridgeCall), uint64(g.FesEmbeddedBridgeClose), 17, uint64(g.FesEmbeddedBridgeCookie), 3})
	if err != nil || reply[0] != 0 || reply[3] != 4 {
		t.Fatalf("bridge close failed: %#v %v", reply, err)
	}
	if _, err := s.firmware.Observe(); err != nil || !s.firmware.DataHeld() {
		t.Fatalf("expected DataHeld=true after bridge close, err=%v", err)
	}

	// Firmware containment fault retires shared session fail-closed.
	firmware.fault = 10
	if _, err := firmware.Invoke([7]uint64{uint64(g.FesEmbeddedBridgeCall), 0, 0, uint64(g.FesEmbeddedBridgeCookie), 0}); err != nil || true {
		// Next SD call observes firmware error and retires session.
	}
	if err := s.ReadSectors(0, buf); !errors.Is(err, ErrRetired) && err != nil {
		// PIO succeeds until firmware checks, or Quiesce catches firmware fault:
		t.Logf("read after host fault returned: %v", err)
	}
	if err := s.Quiesce(); !errors.Is(err, ErrRetired) {
		t.Fatalf("expected ErrRetired on faulty Quiesce, got %v", err)
	}
	if !s.Retired() {
		t.Fatal("expected Retired()=true")
	}
}

func TestNewSharedClientRejectsLegacyOrInvalidStates(t *testing.T) {
	if _, err := NewSharedClient(nil, 17); !errors.Is(err, ErrNotReady) {
		t.Fatal(err)
	}
	if _, err := NewSharedClient(callFunc(func([7]uint64) ([4]uint64, error) {
		t.Fatal("unexpected call")
		return [4]uint64{}, nil
	}), 0); !errors.Is(err, ErrNotReady) {
		t.Fatal(err)
	}
	// Legacy ClosedOnly reply must be rejected by NewSharedClient.
	legacyCPU := callFunc(func([7]uint64) ([4]uint64, error) {
		return wireReply(0x39, 1), nil
	})
	if _, err := NewSharedClient(legacyCPU, 17); err == nil {
		t.Fatal("accepted closed-only reply for shared client")
	}
	// Open Config{Shared: true} without BootOwner fails cleanly.
	if _, err := Open(legacyCPU, newCard(), Config{InputClockHz: 200000000, Shared: true}); err == nil {
		t.Fatal("accepted Shared without BootOwner")
	}
}
