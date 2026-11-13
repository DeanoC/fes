// SPDX-License-Identifier: GPL-3.0-or-later
package usbcontrol

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
	"github.com/DeanoC/fes/platform/embedded/tamago/input"
)

var rpcBinary, sharedRPCBinary string

func TestMain(m *testing.M) {
	temp, err := os.MkdirTemp("", "fes-usb-wire-")
	if err != nil {
		panic(err)
	}
	fw, err := filepath.Abs("../../firmware")
	if err != nil {
		panic(err)
	}
	root, err := filepath.Abs("../../../..")
	if err != nil {
		panic(err)
	}
	script := `import sys,json,hashlib
from pathlib import Path
root=Path(sys.argv[1]);temp=Path(sys.argv[2]);sys.path.insert(0,str(root/'platform/embedded/nano'))
import usb_readiness
project=temp/'project';handoff=project/'board/hps/project/output_files/hps_bootloader_handoff.bin'
handoff.parent.mkdir(parents=True);data=(root/'tests/fixtures/nano-usb-board/hps_bootloader_handoff.bin').read_bytes();handoff.write_bytes(data)
(project/'board/hps/build.json').write_text(json.dumps({'generated_handoff_and_sdc_sha256':{'output_files/hps_bootloader_handoff.bin':hashlib.sha256(data).hexdigest()}}))
(temp/'usb_board_policy.h').write_text(usb_readiness.header(project,{'hps_io_hash':'d'*64}))`
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	out, err := exec.CommandContext(ctx, "python3", "-c", script, root, temp).CombinedOutput()
	cancel()
	if err == nil {
		args := []string{"-std=c11", "-Wall", "-Wextra", "-Werror", "-fsanitize=undefined", "-fno-sanitize-recover=all", "-I" + fw, "-I" + temp}
		for _, file := range []string{"nano_cold.c", "nano_bridges.c", "nano_bridge_sip.c", "nano_sd.c", "nano_hps_io.c", "nano_usb_platform.c", "nano_usb_board.c", "nano_mac.c", "nano_usb_service.c", "nano_identity.c", "nano_configuration.c", "tests/nano_usb_rpc.c"} {
			args = append(args, filepath.Join(fw, file))
		}
		rpcBinary = filepath.Join(temp, "rpc")
		args = append(args, "-o", rpcBinary)
		ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
		out, err = exec.CommandContext(ctx, "cc", args...).CombinedOutput()
		cancel()
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "USB C integration: %v\n%s", err, out)
		os.RemoveAll(temp)
		os.Exit(1)
	}
	sharedRPCBinary = filepath.Join(temp, "shared-rpc")
	args := []string{"-std=c11", "-Wall", "-Wextra", "-Werror", "-fsanitize=undefined", "-fno-sanitize-recover=all", "-I" + fw}
	for _, file := range []string{"nano_cold.c", "nano_boot.c", "nano_bridges.c", "nano_bridge_sip.c", "nano_sd.c", "nano_sd_platform.c", "nano_usb_platform.c", "nano_mac.c", "nano_usb_service.c", "nano_usb_composition.c", "nano_identity.c", "nano_configuration.c", "tests/nano_usb_shared_rpc.c"} {
		args = append(args, filepath.Join(fw, file))
	}
	args = append(args, "-o", sharedRPCBinary)
	ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
	out, err = exec.CommandContext(ctx, "cc", args...).CombinedOutput()
	cancel()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Shared USB C integration: %v\n%s", err, out)
		os.RemoveAll(temp)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(temp)
	os.Exit(code)
}

type rpc struct {
	mu      sync.Mutex
	cmd     *exec.Cmd
	input   io.WriteCloser
	scan    *bufio.Scanner
	calls   int
	metrics [9]uint64
}

func newRPC(t *testing.T, args ...string) *rpc {
	return startRPC(t, rpcBinary, args...)
}
func newSharedRPC(t *testing.T, args ...string) *rpc {
	return startRPC(t, sharedRPCBinary, args...)
}
func startRPC(t *testing.T, binary string, args ...string) *rpc {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	p := &rpc{cmd: exec.CommandContext(ctx, binary, args...)}
	var err error
	p.input, err = p.cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := p.cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	p.scan = bufio.NewScanner(output)
	p.cmd.Stderr = os.Stderr
	if err := p.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.input.Close(); p.cmd.Wait(); cancel() })
	return p
}
func (p *rpc) Invoke(a [7]uint64) ([4]uint64, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	_, err := fmt.Fprintf(p.input, "%x %x %x %x %x %x %x\n", a[0], a[1], a[2], a[3], a[4], a[5], a[6])
	if err != nil {
		return [4]uint64{}, err
	}
	if !p.scan.Scan() {
		return [4]uint64{}, errors.New("C USB RPC ended")
	}
	fields := strings.Fields(p.scan.Text())
	if len(fields) != 13 {
		return [4]uint64{}, fmt.Errorf("invalid C USB reply %q", p.scan.Text())
	}
	var r [4]uint64
	for i, s := range fields {
		v, err := strconv.ParseUint(s, 16, 64)
		if err != nil {
			return r, err
		}
		if i < 4 {
			r[i] = v
		} else {
			p.metrics[i-4] = v
		}
	}
	return r, nil
}
func (p *rpc) model(t *testing.T, op uint64) {
	t.Helper()
	if _, err := p.Invoke([7]uint64{0xf025ffff, op}); err != nil {
		t.Fatal(err)
	}
}

type reports struct {
	polls int
	err   error
}

func (r *reports) PollKeyboard() ([]byte, error) {
	r.polls++
	return []byte{0, 0, 4, 0, 0, 0, 0, 0}, r.err
}
func TestActualCFirmwarePrepareDriverConfirmStop(t *testing.T) {
	p := newRPC(t)
	ctx := context.Background()
	c, err := New(ctx, p, 17)
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		driver := &reports{}
		s, err := Open(ctx, c, func() (input.USBReports, error) {
			v, valid := c.LastReceipt()
			if !valid || !v.Active || v.Confirmed {
				t.Fatal("driver opened before prepared lease")
			}
			p.model(t, 0)
			return driver, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		v, valid := c.LastReceipt()
		if !valid || !v.Confirmed || p.metrics[0] != 1 || p.metrics[1] != 1 || p.metrics[2] != 1 {
			t.Fatal("confirmation missing", v, p.metrics)
		}
		report, err := s.PollKeyboard()
		if err != nil || len(report) != 8 || driver.polls != 1 {
			t.Fatal(err)
		}
		for _, call := range [][7]uint64{{uint64(g.FesEmbeddedSdCall), 0, 0, uint64(g.FesEmbeddedSdCookie)}, {uint64(g.FesEmbeddedBridgeCall), 0, 0, uint64(g.FesEmbeddedBridgeCookie)}, {0xc2000001}} {
			r, err := p.Invoke(call)
			if err != nil || r[0] != 1 || p.metrics[6] != 0 {
				t.Fatal("USB lease allowed another device", r, err, p.metrics)
			}
		}
		if err := s.Close(ctx); err != nil {
			t.Fatal(err)
		}
		before := p.calls
		if err := s.Close(ctx); err != nil || p.calls != before {
			t.Fatal("close replayed", err)
		}
		if _, err := s.PollKeyboard(); !errors.Is(err, ErrRetired) || driver.polls != 1 {
			t.Fatal("closed source polled", err)
		}
		v, valid = c.LastReceipt()
		if !valid || v.Active || v.Confirmed || p.metrics[2] != 0 {
			t.Fatal("stop did not restore containment", v, p.metrics)
		}
	}
	if _, err := New(ctx, p, 17); err == nil {
		t.Fatal("new client adopted a prior USB generation")
	}
}
func TestDriverFailureAbortsPreparedLease(t *testing.T) {
	p := newRPC(t)
	c, err := New(context.Background(), p, 17)
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("driver startup failed")
	driver := &reports{}
	s, err := Open(context.Background(), c, func() (input.USBReports, error) { return driver, failure })
	if !errors.Is(err, failure) || s == nil || p.metrics[0] != 0 || p.metrics[2] != 0 {
		t.Fatal("failed startup not contained", err, p.metrics)
	}
	calls := p.calls
	if _, err := s.PollKeyboard(); err == nil || driver.polls != 0 || p.calls != calls {
		t.Fatal("failed source used")
	}
	if err := s.Close(context.Background()); !errors.Is(err, failure) || p.calls != calls {
		t.Fatal("failed startup retried")
	}
}
func TestLiveFailureAndLostReplyKeepLease(t *testing.T) {
	for _, lost := range []bool{false, true} {
		t.Run(fmt.Sprint(lost), func(t *testing.T) {
			p := newRPC(t)
			var cpu Caller = p
			if lost {
				cpu = callerFunc(func(a [7]uint64) ([4]uint64, error) {
					r, e := p.Invoke(a)
					if a[1] == uint64(g.FesEmbeddedUsbConfirm) {
						return r, errors.New("reply lost")
					}
					return r, e
				})
			}
			c, err := New(context.Background(), cpu, 17)
			if err != nil {
				t.Fatal(err)
			}
			driver := &reports{}
			s, err := Open(context.Background(), c, func() (input.USBReports, error) {
				p.model(t, 0)
				if !lost {
					p.model(t, 1)
				}
				return driver, nil
			})
			if err == nil || s == nil || p.metrics[0] != 1 || p.metrics[2] != 1 {
				t.Fatal("uncertainty lost reservation", err, p.metrics)
			}
			calls := p.calls
			if _, err := s.PollKeyboard(); err == nil || driver.polls != 0 {
				t.Fatal("uncertain source polled")
			}
			for _, op := range []func(context.Context) error{c.Acquire, c.Confirm, c.Stop, c.Observe} {
				if err := op(context.Background()); !errors.Is(err, ErrRetired) {
					t.Fatal(err)
				}
			}
			if p.calls != calls {
				t.Fatal("retired client retried")
			}
		})
	}
}

type callerFunc func([7]uint64) ([4]uint64, error)

func (f callerFunc) Invoke(a [7]uint64) ([4]uint64, error) { return f(a) }
func TestUnavailableStaleAndMalformedReplies(t *testing.T) {
	p := newRPC(t, "unready")
	if _, err := New(context.Background(), p, 17); !errors.Is(err, ErrNotReady) {
		t.Fatal(err)
	}
	p = newRPC(t)
	if _, err := New(context.Background(), p, 18); err == nil {
		t.Fatal("wrong boot owner accepted")
	}
	c, err := New(context.Background(), p, 17)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	accesses := p.metrics[4]
	for _, a := range [][7]uint64{{uint64(g.FesEmbeddedUsbCall), 3, 18, uint64(g.FesEmbeddedUsbCookie), 1}, {uint64(g.FesEmbeddedUsbCall), 3, 17, uint64(g.FesEmbeddedUsbCookie), 0}, {uint64(g.FesEmbeddedUsbCall), 9, 17, uint64(g.FesEmbeddedUsbCookie), 1}} {
		r, err := p.Invoke(a)
		if err != nil || r[0] != 2 || p.metrics[4] != accesses {
			t.Fatal("stale/invalid request performed I/O", r, err)
		}
	}
	for _, damage := range []func(*[4]uint64){func(r *[4]uint64) { r[1] ^= 1 << 32 }, func(r *[4]uint64) { r[1] |= 1 << 20 }, func(r *[4]uint64) { r[2]++ }, func(r *[4]uint64) { r[3]++ }} {
		wire := newRPC(t)
		cpu := callerFunc(func(a [7]uint64) ([4]uint64, error) {
			r, e := wire.Invoke(a)
			if a[1] == uint64(g.FesEmbeddedUsbAcquire) {
				damage(&r)
			}
			return r, e
		})
		client, e := New(context.Background(), cpu, 17)
		if e != nil {
			t.Fatal(e)
		}
		if e := client.Acquire(context.Background()); !errors.Is(e, ErrRetired) {
			t.Fatal("malformed transition accepted", e)
		}
	}
}

func TestCanceledAcquisitionCleansOnlyKnownReply(t *testing.T) {
	p := newRPC(t)
	ctx, cancel := context.WithCancel(context.Background())
	cpu := callerFunc(func(a [7]uint64) ([4]uint64, error) {
		r, e := p.Invoke(a)
		if a[1] == uint64(g.FesEmbeddedUsbAcquire) {
			cancel()
		}
		return r, e
	})
	c, err := New(ctx, cpu, 17)
	if err != nil {
		t.Fatal(err)
	}
	factoryCalls := 0
	s, err := Open(ctx, c, func() (input.USBReports, error) { factoryCalls++; return &reports{}, nil })
	if !errors.Is(err, context.Canceled) || s == nil || factoryCalls != 0 || p.metrics[0] != 0 || p.metrics[2] != 0 {
		t.Fatal("known canceled acquisition not contained", err, p.metrics)
	}
}
func TestFailedStopAndStaleSourceCannotReplay(t *testing.T) {
	for _, failedStop := range []bool{false, true} {
		t.Run(fmt.Sprint(failedStop), func(t *testing.T) {
			p := newRPC(t)
			ctx := context.Background()
			c, err := New(ctx, p, 17)
			if err != nil {
				t.Fatal(err)
			}
			open := func() *Source {
				s, e := Open(ctx, c, func() (input.USBReports, error) { p.model(t, 0); return &reports{}, nil })
				if e != nil {
					t.Fatal(e)
				}
				return s
			}
			old := open()
			if failedStop {
				p.model(t, 2)
				if err := old.Close(ctx); !errors.Is(err, ErrRetired) || p.metrics[0] != 1 || p.metrics[2] != 1 {
					t.Fatal("failed stop lost lease", err, p.metrics)
				}
				calls := p.calls
				if err := old.Close(ctx); err == nil || p.calls != calls {
					t.Fatal("failed stop retried")
				}
				return
			}
			if err := c.Stop(ctx); err != nil {
				t.Fatal(err)
			}
			current := open()
			calls := p.calls
			if err := old.Close(ctx); !errors.Is(err, ErrRetired) || p.calls != calls {
				t.Fatal("stale source stopped replacement", err)
			}
			if _, err := old.PollKeyboard(); !errors.Is(err, ErrRetired) || p.calls != calls {
				t.Fatal("stale source polled")
			}
			if err := current.Close(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestDriverPollFailureIsLatchedAndClosesOnce(t *testing.T) {
	p := newRPC(t)
	ctx := context.Background()
	c, err := New(ctx, p, 17)
	if err != nil {
		t.Fatal(err)
	}
	driver := &reports{err: errors.New("USB report failure")}
	s, err := Open(ctx, c, func() (input.USBReports, error) { p.model(t, 0); return driver, nil })
	if err != nil {
		t.Fatal(err)
	}
	if data, err := s.PollKeyboard(); !errors.Is(err, driver.err) || data != nil {
		t.Fatal("partial report escaped", err)
	}
	if _, err := s.PollKeyboard(); !errors.Is(err, driver.err) || driver.polls != 1 {
		t.Fatal("driver retried", err)
	}
	if err := s.Close(ctx); !errors.Is(err, driver.err) || p.metrics[0] != 0 || p.metrics[2] != 0 {
		t.Fatal("failed driver not stopped", err, p.metrics)
	}
	if _, valid := c.LastReceipt(); valid {
		t.Fatal("failed driver client remained reusable")
	}
}

func TestFirmwareRejectsCallbackWithoutTransition(t *testing.T) {
	p := newRPC(t)
	ctx := context.Background()
	c, err := New(ctx, p, 17)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Acquire(ctx); err != nil {
		t.Fatal(err)
	}
	p.model(t, 0)
	p.model(t, 5)
	// The trusted callback claims success but leaves the prepared state unchanged.
	raw, err := p.Invoke([7]uint64{uint64(g.FesEmbeddedUsbCall), uint64(g.FesEmbeddedUsbConfirm), 17, uint64(g.FesEmbeddedUsbCookie), 1})
	if err != nil {
		t.Fatal(err)
	}
	if raw != [4]uint64{uint64(g.FesEmbeddedUsbError), uint64(g.FesEmbeddedUsbTag) << 32, 0, 0} || p.metrics[0] != 1 || p.metrics[2] != 1 || p.metrics[7] != 1 || p.metrics[8] != 1 {
		t.Fatal("firmware accepted a callback without transition or lost containment", raw, p.metrics)
	}
}

func TestInconsistentStopRetainsSharedReservation(t *testing.T) {
	p := newRPC(t)
	ctx := context.Background()
	c, err := New(ctx, p, 17)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Acquire(ctx); err != nil {
		t.Fatal(err)
	}
	p.model(t, 6)
	if err = c.Stop(ctx); !errors.Is(err, ErrRetired) {
		t.Fatal(err)
	}
	if p.metrics[0] != 0 || p.metrics[2] != 1 || p.metrics[7] != 1 {
		t.Fatal("inconsistent stop released reservation", p.metrics)
	}
	raw, err := p.Invoke([7]uint64{uint64(g.FesEmbeddedSdCall), 0, 0, uint64(g.FesEmbeddedSdCookie)})
	if err != nil || raw[0] != 1 {
		t.Fatal("SD was not blocked after inconsistent stop", raw, err)
	}
}
