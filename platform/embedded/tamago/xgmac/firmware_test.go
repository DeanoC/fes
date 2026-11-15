// SPDX-License-Identifier: GPL-3.0-or-later
package xgmac

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
)

// The actual portable C lease engine is connected through a test-only pipe.
// Its boot, clock, memory exclusion and independent drain callbacks are modeled,
// not physical proof or a replacement for the secure Nano SiP dispatcher.
type firmwareMAC struct {
	h      *hardware
	input  io.WriteCloser
	output *bufio.Reader
	stats  [6]uint64
}

func newFirmwareMAC(t *testing.T, h *hardware) *firmwareMAC {
	t.Helper()
	fw, err := filepath.Abs("../../firmware")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	binary := filepath.Join(t.TempDir(), "mac")
	compile := exec.CommandContext(ctx, "cc", "-std=c11", "-Wall", "-Wextra", "-Werror", "-fsanitize=undefined", "-fno-sanitize-recover=all", "-I"+fw,
		filepath.Join(fw, "nano_cold.c"), filepath.Join(fw, "nano_boot.c"), filepath.Join(fw, "nano_bridges.c"), filepath.Join(fw, "nano_bridge_sip.c"), filepath.Join(fw, "nano_sd.c"), filepath.Join(fw, "nano_sd_platform.c"), filepath.Join(fw, "nano_usb_platform.c"), filepath.Join(fw, "nano_usb_service.c"), filepath.Join(fw, "nano_usb_composition.c"), filepath.Join(fw, "nano_identity.c"), filepath.Join(fw, "nano_configuration.c"), filepath.Join(fw, "nano_mac.c"), filepath.Join(fw, "tests/nano_mac_rpc.c"), "-o", binary)
	if output, err := compile.CombinedOutput(); err != nil {
		cancel()
		t.Fatalf("MAC C engine: %v\n%s", err, output)
	}
	cmd := exec.CommandContext(ctx, binary)
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
	if err = cmd.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		input.Close()
		if err := cmd.Wait(); err != nil {
			t.Error(err)
		}
		cancel()
	})
	return &firmwareMAC{h: h, input: input, output: bufio.NewReader(output)}
}

func (f *firmwareMAC) Invoke(a [7]uint64) ([4]uint64, error) {
	// Match the C fixture's exact synthetic device/window, not a real Nano PHY.
	c := configuration()
	if f.h.mem.Base() != 0x92000000 || f.h.mem.Size() != 0x200000 || c.PHYAddress != 4 || c.PHYID != 0x001cc916 {
		return [4]uint64{}, fmt.Errorf("model differs from the trusted fixture binding")
	}
	if a[1] == uint64(g.FesEmbeddedMacAcquire) || a[1] == uint64(g.FesEmbeddedMacStop) || a[1] == uint64(g.FesEmbeddedMacConfirm) {
		want := uint32(0)
		if a[1] == uint64(g.FesEmbeddedMacConfirm) {
			want = 1
		}
		for _, reg := range []uint32{txConfig, rxConfig, txControl, rxControl} {
			if f.h.Read32(reg)&1 != want {
				return [4]uint64{}, fmt.Errorf("firmware stage %d before driver state at %#x", a[1], reg)
			}
		}
		if want == 1 && f.h.mem.Read32(rxRing+12)&own == 0 {
			return [4]uint64{}, fmt.Errorf("confirmation before descriptor initialization")
		}
	}
	if _, err := fmt.Fprintf(f.input, "%x %x %x %x %x %x %x 0\n", a[0], a[1], a[2], a[3], a[4], a[5], a[6]); err != nil {
		return [4]uint64{}, err
	}
	var r [4]uint64
	_, err := fmt.Fscanf(f.output, "%x %x %x %x %x %x %x %x %x %x\n", &r[0], &r[1], &r[2], &r[3], &f.stats[0], &f.stats[1], &f.stats[2], &f.stats[3], &f.stats[4], &f.stats[5])
	return r, err
}
