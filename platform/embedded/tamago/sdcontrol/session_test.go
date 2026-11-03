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
	"strings"
	"sync"
	"testing"
	"time"

	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
	"github.com/DeanoC/fes/platform/embedded/tamago/sdhci"
)

var rpcBinary string

func TestMain(m *testing.M) {
	temp, err := os.MkdirTemp("", "fes-sd-wire-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fw, err := filepath.Abs("../../firmware")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.RemoveAll(temp)
		os.Exit(1)
	}
	rpcBinary = filepath.Join(temp, "rpc")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	command := exec.CommandContext(ctx, "cc", "-std=c11", "-Wall", "-Wextra", "-Werror", "-fsanitize=undefined", "-fno-sanitize-recover=all",
		"-I"+fw, "-I"+filepath.Join(fw, "tests"), filepath.Join(fw, "nano_bridges.c"), filepath.Join(fw, "nano_bridge_sip.c"),
		filepath.Join(fw, "nano_sd.c"), filepath.Join(fw, "nano_identity.c"), filepath.Join(fw, "nano_configuration.c"), filepath.Join(fw, "tests/nano_sd_rpc.c"), "-o", rpcBinary)
	output, err := command.CombinedOutput()
	cancel()
	if err != nil {
		fmt.Fprintf(os.Stderr, "C wire fixture: %v\n%s", err, output)
		os.RemoveAll(temp)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(temp)
	os.Exit(code)
}

type rpc struct {
	mu       sync.Mutex
	card     *card
	command  *exec.Cmd
	input    io.WriteCloser
	output   *bufio.Reader
	requests [][7]uint64
	stats    [7]uint64
	fault    uint32
	drop     uint64
}

func newRPC(t *testing.T, mode string) (*rpc, *card) {
	t.Helper()
	c := newCard()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	args := []string{}
	if mode != "" {
		args = append(args, mode)
	}
	command := exec.CommandContext(ctx, rpcBinary, args...)
	command.Stderr = os.Stderr
	input, err := command.StdinPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	output, err := command.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if err = command.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	r := &rpc{card: c, command: command, input: input, output: bufio.NewReader(output)}
	t.Cleanup(func() {
		input.Close()
		if err := command.Wait(); err != nil {
			t.Error(err)
		}
		cancel()
	})
	return r, c
}
func (r *rpc) Invoke(a [7]uint64) ([4]uint64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = append(r.requests, a)
	c := r.card
	_, err := fmt.Fprintf(r.input, "%x %x %x %x %x %x %x %x %x %x %x %x %x\n",
		a[0], a[1], a[2], a[3], a[4], a[5], a[6], c.regs[clock], c.regs[command]&1, c.regs[present]&0xb07,
		c.regs[signalEnable], c.regs[statusEnable], r.fault)
	if err != nil {
		return [4]uint64{}, err
	}
	var reply [4]uint64
	_, err = fmt.Fscanf(r.output, "%x %x %x %x %x %x %x %x %x %x %x\n",
		&reply[0], &reply[1], &reply[2], &reply[3], &r.stats[0], &r.stats[1], &r.stats[2], &r.stats[3], &r.stats[4], &r.stats[5], &r.stats[6])
	if err != nil {
		return [4]uint64{}, err
	}
	c.active = r.stats[0] != 0
	if a[0] == uint64(g.FesEmbeddedSdCall) && a[1] == uint64(g.FesEmbeddedSdPrepare) && reply[0] == 0 {
		c.prepared = true
	}
	if r.drop != 0 && a[1] == r.drop && a[0] == uint64(g.FesEmbeddedSdCall) {
		return [4]uint64{}, errors.New("lost response after actual C transition")
	}
	return reply, nil
}
func TestFirmwareAndPIOReadQuiesceResume(t *testing.T) {
	firmware, c := newRPC(t, "")
	s, err := Open(firmware, c, 200000000)
	if err != nil {
		t.Fatal(err)
	}
	identity := s.Identity()
	if identity.Sectors != 8192 || identity.CID == [15]byte{} {
		t.Fatal(identity)
	}
	read := make([]byte, 1024)
	if err = s.ReadSectors(3, read); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(read, bytes.Repeat([]byte{0, 1, 2, 3}, 256)) {
		t.Fatal("PIO bytes")
	}
	// Exact owned dispatcher blocks bridge snapshots and configuration before
	// consulting the all-master cold callbacks; C assertions enforce this.
	beforeOwns := firmware.stats[5]
	for _, a := range [][7]uint64{
		{uint64(g.FesEmbeddedBridgeCall), 0, 0, uint64(g.FesEmbeddedBridgeCookie)},
		{uint64(g.FesEmbeddedBridgeCall), uint64(g.FesEmbeddedBridgeProbeIdentity), 17, uint64(g.FesEmbeddedBridgeCookie), 1},
		{uint64(g.FesEmbeddedBridgeCall), uint64(g.FesEmbeddedBridgeConfiguration), 17, uint64(g.FesEmbeddedBridgeCookie), 1},
		{0xc2000001},
	} {
		reply, err := firmware.Invoke(a)
		if err != nil || reply[0] != 1 {
			t.Fatalf("busy=%#v %v", reply, err)
		}
		if firmware.stats[5] != beforeOwns || firmware.stats[6] != 0 {
			t.Fatal("lease reached cold/config callbacks")
		}
	}
	if err = s.Quiesce(); err != nil {
		t.Fatal(err)
	}
	receipt, valid := s.LastReceipt()
	if !valid || receipt != (Receipt{Owner: 17, Generation: 4}) {
		t.Fatalf("%+v %v", receipt, valid)
	}
	beforeOwns = firmware.stats[5]
	if reply, err := firmware.Invoke([7]uint64{uint64(g.FesEmbeddedBridgeCall), uint64(g.FesEmbeddedBridgeProbeIdentity), 17, uint64(g.FesEmbeddedBridgeCookie), 1}); err != nil || reply != ([4]uint64{uint64(g.FesEmbeddedBridgeNotReady), uint64(g.FesEmbeddedBridgeIdentityTag) << 32}) || firmware.stats[5] != beforeOwns {
		t.Fatalf("SD firmware granted identity access without a provider: %#v %v", reply, err)
	}
	accesses, calls := c.accesses, len(firmware.requests)
	if !errors.Is(s.ReadSectors(0, read), sdhci.ErrQuiesced) || !errors.Is(s.Flush(), sdhci.ErrQuiesced) || !errors.Is(s.Quiesce(), sdhci.ErrQuiesced) {
		t.Fatal("quiescence gate")
	}
	if accesses != c.accesses || calls != len(firmware.requests) {
		t.Fatal("repeated operation accessed contained controller")
	}
	reply, err := firmware.Invoke([7]uint64{uint64(g.FesEmbeddedBridgeCall), 0, 0, uint64(g.FesEmbeddedBridgeCookie)})
	if err != nil || reply[0] != 0 || reply[3] != 4 || uint32(reply[1]) != 0xdf {
		t.Fatalf("restored bridge receipt=%#v %v", reply, err)
	}
	if err = s.Resume(); err != nil {
		t.Fatal(err)
	}
	if s.Identity() != identity {
		t.Fatal("card identity changed")
	}
	after := make([]byte, len(read))
	if err = s.ReadSectors(3, after); err != nil || !bytes.Equal(after, read) {
		t.Fatalf("resumed read %v", err)
	}
	if err = s.Quiesce(); err != nil {
		t.Fatal(err)
	}
	receipt, valid = s.LastReceipt()
	if !valid || receipt.Generation != 7 || receipt.Active {
		t.Fatal(receipt, valid)
	}
	if firmware.stats[1] != 2 || firmware.stats[2] != 2 || firmware.stats[3] != 2 {
		t.Fatal(firmware.stats)
	}
}

func TestActualFirmwareClosureAllowsPIOReopenWithSameCardAndNoGrantReplay(t *testing.T) {
	firmware, card := newRPC(t, "returned")
	s, err := Open(firmware, card, 200000000)
	if err != nil {
		t.Fatal(err)
	}
	identity := s.Identity()
	if err := s.Quiesce(); err != nil {
		t.Fatal(err)
	}
	for cycle := uint64(0); cycle < 3; cycle++ {
		// Only the host fixture handles this private model instruction. It uses
		// the real C bridge open/stop/close functions and publishes their receipt.
		r, err := firmware.Invoke([7]uint64{0xf0250001})
		if err != nil || r[0] != 0 || r[2] != 17 || r[3] != 6+5*cycle {
			t.Fatal(r, err)
		}
		before := len(firmware.requests)
		if err := s.ResumeAfterReturn(r[2]+1, r[3]); !errors.Is(err, ErrTransition) || len(firmware.requests) != before {
			t.Fatal("wrong owner reached firmware", err)
		}
		if err := s.ResumeAfterReturn(r[2], r[3]); err != nil {
			t.Fatal(err)
		}
		if s.Identity() != identity {
			t.Fatal("changed card adopted")
		}
		data := make([]byte, 512)
		if err := s.ReadSectors(2, data); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(data, bytes.Repeat([]byte{0, 1, 2, 3}, 128)) {
			t.Fatal("wrong PIO contents after return")
		}
		before = len(firmware.requests)
		if err := s.ResumeAfterReturn(r[2], r[3]); !errors.Is(err, ErrTransition) || len(firmware.requests) != before {
			t.Fatal("active lease return replay", err)
		}
		if err := s.Quiesce(); err != nil {
			t.Fatal(err)
		}
		last, valid := s.LastReceipt()
		if !valid || last != (Receipt{Owner: 17, Generation: r[3] + 3}) {
			t.Fatal(last, valid)
		}
	}
}
func assertRetired(t *testing.T, s *Session, firmware *rpc, c *card) {
	t.Helper()
	accesses, calls := c.accesses, len(firmware.requests)
	for _, err := range []error{s.ReadSectors(0, make([]byte, 512)), s.Flush(), s.Quiesce(), s.Resume()} {
		if !errors.Is(err, ErrRetired) {
			t.Fatalf("retired: %v", err)
		}
	}
	if accesses != c.accesses || calls != len(firmware.requests) {
		t.Fatal("retired session accessed hardware/firmware")
	}
	if _, valid := s.LastReceipt(); valid {
		t.Fatal("uncertain receipt claimed current")
	}
}
func TestSessionRetainsFailedGrantsAndStop(t *testing.T) {
	for _, name := range []string{"resume-partial", "prepare", "host-clock", "stop", "driver-stop", "changed-card", "lost-grant-response"} {
		t.Run(name, func(t *testing.T) {
			firmware, c := newRPC(t, "")
			switch name {
			case "resume-partial":
				firmware.fault = 2
			case "prepare":
				firmware.fault = 4
			case "host-clock":
				c.badClock = true
			case "lost-grant-response":
				firmware.drop = 1
			}
			s, err := Open(firmware, c, 200000000)
			switch name {
			case "stop":
				if err != nil {
					t.Fatal(err)
				}
				firmware.fault = 6
				err = s.Quiesce()
			case "driver-stop":
				if err != nil {
					t.Fatal(err)
				}
				c.regs[present] = 1
				err = s.Quiesce()
			case "changed-card":
				if err != nil {
					t.Fatal(err)
				}
				if err = s.Quiesce(); err != nil {
					t.Fatal(err)
				}
				c.cid++
				err = s.Resume()
				if !errors.Is(err, sdhci.ErrCardChanged) {
					t.Fatal(err)
				}
			}
			if s == nil || !errors.Is(err, ErrRetired) {
				t.Fatalf("%v %v", s, err)
			}
			assertRetired(t, s, firmware, c)
			receipt, _ := s.LastReceipt()
			if name != "resume-partial" && name != "lost-grant-response" && !receipt.Active {
				t.Fatal("unresolved active receipt lost")
			}
			if name == "host-clock" || name == "prepare" {
				if c.commands != 0 {
					t.Fatal("commands after preparation failed")
				}
			}
			if name == "resume-partial" || name == "lost-grant-response" {
				if c.accesses != 0 {
					t.Fatal("MMIO after uncertain grant")
				}
			}
			if name != "stop" && name != "changed-card" && firmware.stats[3] != 0 {
				t.Fatal("automatic stop attempted")
			}
		})
	}
}
func TestUnreadyServiceDoesNotAccessController(t *testing.T) {
	firmware, c := newRPC(t, "unready")
	s, err := Open(firmware, c, 200000000)
	if s != nil || !errors.Is(err, ErrNotReady) || c.accesses != 0 {
		t.Fatal(s, err, c.accesses)
	}
	if len(firmware.requests) != 1 || firmware.stats[1] != 0 || firmware.stats[2] != 0 || firmware.stats[3] != 0 {
		t.Fatal(firmware.stats)
	}
}
func TestInvalidConfigDoesNotContactFirmware(t *testing.T) {
	cpu := callFunc(func([7]uint64) ([4]uint64, error) { t.Fatal("unexpected firmware call"); return [4]uint64{}, nil })
	for _, hz := range []uint32{0, 399999, 200000001} {
		if s, err := Open(cpu, newCard(), hz); err == nil || s != nil {
			t.Fatal(hz)
		}
	}
	if _, err := Open(cpu, nil, 200000000); err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatal(err)
	}
}
