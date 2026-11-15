// SPDX-License-Identifier: GPL-3.0-or-later
package maccontrol

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

var rpcBinary string

func TestMain(m *testing.M) {
	temp, err := os.MkdirTemp("", "fes-mac-wire-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fw, err := filepath.Abs("../../firmware")
	if err != nil {
		os.RemoveAll(temp)
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	rpcBinary = filepath.Join(temp, "rpc")
	cmd := exec.CommandContext(ctx, "cc", "-std=c11", "-Wall", "-Wextra", "-Werror", "-fsanitize=undefined", "-fno-sanitize-recover=all", "-I"+fw, filepath.Join(fw, "nano_cold.c"), filepath.Join(fw, "nano_boot.c"), filepath.Join(fw, "nano_bridges.c"), filepath.Join(fw, "nano_bridge_sip.c"), filepath.Join(fw, "nano_sd.c"), filepath.Join(fw, "nano_sd_platform.c"), filepath.Join(fw, "nano_usb_platform.c"), filepath.Join(fw, "nano_usb_service.c"), filepath.Join(fw, "nano_usb_composition.c"), filepath.Join(fw, "nano_identity.c"), filepath.Join(fw, "nano_configuration.c"), filepath.Join(fw, "nano_mac.c"), filepath.Join(fw, "tests/nano_mac_rpc.c"), "-o", rpcBinary)
	output, err := cmd.CombinedOutput()
	cancel()
	if err != nil {
		fmt.Fprintf(os.Stderr, "MAC C wire fixture: %v\n%s", err, output)
		os.RemoveAll(temp)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(temp)
	os.Exit(code)
}

type rpc struct {
	input  io.WriteCloser
	output *bufio.Reader
	fault  uint32
	stats  [6]uint64
	calls  int
	drop   uint32
	mutate func(*[4]uint64)
	cancel context.CancelFunc
}

func newRPC(t *testing.T, unready bool) *rpc {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	args := []string{}
	if unready {
		args = append(args, "unready")
	}
	cmd := exec.CommandContext(ctx, rpcBinary, args...)
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
	return &rpc{input: input, output: bufio.NewReader(output)}
}
func (r *rpc) Invoke(a [7]uint64) ([4]uint64, error) {
	r.calls++
	if _, err := fmt.Fprintf(r.input, "%x %x %x %x %x %x %x %x\n", a[0], a[1], a[2], a[3], a[4], a[5], a[6], r.fault); err != nil {
		return [4]uint64{}, err
	}
	var reply [4]uint64
	_, err := fmt.Fscanf(r.output, "%x %x %x %x %x %x %x %x %x %x\n", &reply[0], &reply[1], &reply[2], &reply[3], &r.stats[0], &r.stats[1], &r.stats[2], &r.stats[3], &r.stats[4], &r.stats[5])
	if err != nil {
		return [4]uint64{}, err
	}
	if r.cancel != nil {
		r.cancel()
	}
	if r.drop != 0 && uint64(r.drop) == a[1] {
		return [4]uint64{}, errors.New("lost reply after C mutation")
	}
	if r.mutate != nil {
		r.mutate(&reply)
	}
	return reply, nil
}
func newClient(t *testing.T, r Caller) *Client {
	t.Helper()
	c, err := New(context.Background(), r, 17)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func TestActualFirmwareCyclesAndExclusiveCopiedPlatformBinding(t *testing.T) {
	ctx := context.Background()
	r := newRPC(t, false)
	c := newClient(t, r)
	copyClient := *c
	if _, err := c.BindPlatform(); !errors.Is(err, ErrTransition) {
		t.Fatal(err)
	}
	if err := c.Acquire(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.BindPlatform(); !errors.Is(err, ErrTransition) {
		t.Fatal(err)
	}
	if err := c.Confirm(ctx); err != nil {
		t.Fatal(err)
	}
	hooks, err := copyClient.BindPlatform()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.BindPlatform(); !errors.Is(err, ErrTransition) {
		t.Fatal(err)
	}
	before := r.calls
	for _, fn := range []func(context.Context) error{c.Acquire, c.Confirm, c.Stop, c.Observe} {
		if !errors.Is(fn(ctx), ErrTransition) {
			t.Fatal("public alias retained access")
		}
	}
	if r.calls != before {
		t.Fatal("public alias entered firmware")
	}
	// Overwriting the original exported handle cannot retarget captured hooks.
	*c = Client{}
	for cycle := 0; cycle < 3; cycle++ {
		if err = hooks.Stop(ctx); err != nil {
			t.Fatal(err)
		}
		receipt, ok := copyClient.LastReceipt()
		if !ok || receipt != (Receipt{Owner: 17, Generation: uint64(3 + cycle*3)}) {
			t.Fatal(receipt, ok)
		}
		stops := r.stats[3]
		if err = hooks.Stop(ctx); err != nil || r.stats[3] != stops {
			t.Fatal("contained stop mutated", err)
		}
		if err = hooks.Start(ctx); err != nil {
			t.Fatal(err)
		}
		if err = hooks.Confirm(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if r.stats[1] != 4 || r.stats[2] != 4 || r.stats[3] != 3 || r.stats[0] != 1 {
		t.Fatal(r.stats)
	}
}
func TestActualFirmwareFaultsAndLostRepliesRetainLastReceipt(t *testing.T) {
	for _, op := range []uint32{g.FesEmbeddedMacAcquire, g.FesEmbeddedMacConfirm, g.FesEmbeddedMacStop} {
		for _, kind := range []string{"before", "callback", "after", "identity", "lost"} {
			t.Run(fmt.Sprintf("%d-%s", op, kind), func(t *testing.T) {
				ctx := context.Background()
				r := newRPC(t, false)
				c := newClient(t, r)
				if op > 1 {
					if err := c.Acquire(ctx); err != nil {
						t.Fatal(err)
					}
				}
				if op > 2 {
					if err := c.Confirm(ctx); err != nil {
						t.Fatal(err)
					}
				}
				before, _ := c.LastReceipt()
				switch kind {
				case "before":
					r.fault = 1
				case "callback":
					r.fault = 1 << op
				case "after":
					r.fault = 16
				case "identity":
					r.fault = 64
				case "lost":
					r.drop = op
				}
				if err := c.transition(ctx, op, false); !errors.Is(err, ErrRetired) {
					t.Fatal(err)
				}
				after, valid := c.LastReceipt()
				if valid || after != before {
					t.Fatal(after, valid, before)
				}
				calls := r.calls
				if !errors.Is(c.Stop(ctx), ErrRetired) || !errors.Is(c.Acquire(ctx), ErrRetired) || r.calls != calls {
					t.Fatal("retired client retried")
				}
				if (kind == "callback" || kind == "after") && r.stats[0] != 1 {
					t.Fatal("failed reservation released", r.stats)
				}
			})
		}
	}
}
func TestRejectsDamagedRepliesWithoutUpdatingOwnership(t *testing.T) {
	for name, mutate := range map[string]func(*[4]uint64){
		"tag": func(r *[4]uint64) { r[1] ^= 1 << 63 }, "reserved": func(r *[4]uint64) { r[1] |= 1 << 30 },
		"owner": func(r *[4]uint64) { r[2]++ }, "stale": func(r *[4]uint64) { r[3]-- }, "skip": func(r *[4]uint64) { r[3]++ },
		"both": func(r *[4]uint64) { r[1] |= uint64(g.FesEmbeddedMacContained) }, "early-confirm": func(r *[4]uint64) { r[1] |= uint64(g.FesEmbeddedMacConfirmed) },
		"error-state": func(r *[4]uint64) { r[0] = 4 }, "unknown-status": func(r *[4]uint64) { *r = [4]uint64{9, uint64(g.FesEmbeddedMacTag) << 32} },
	} {
		t.Run(name, func(t *testing.T) {
			r := newRPC(t, false)
			c := newClient(t, r)
			before, _ := c.LastReceipt()
			r.mutate = mutate
			if !errors.Is(c.Acquire(context.Background()), ErrRetired) {
				t.Fatal("accepted corrupt reply")
			}
			after, ok := c.LastReceipt()
			if ok || after != before {
				t.Fatal(after, ok)
			}
		})
	}
}
func TestInitialQueryRejectsUnreadyForeignAndPreviouslyUsedLeases(t *testing.T) {
	ctx := context.Background()
	r := newRPC(t, true)
	if c, err := New(ctx, r, 17); c != nil || !errors.Is(err, ErrNotReady) {
		t.Fatal(c, err)
	}
	r = newRPC(t, false)
	if c, err := New(ctx, r, 18); c != nil || err == nil {
		t.Fatal(c, err)
	}
	c := newClient(t, r)
	if err := c.Acquire(ctx); err != nil {
		t.Fatal(err)
	}
	if c, err := New(ctx, r, 17); c != nil || err == nil {
		t.Fatal("adopted active lease", c, err)
	}
	if err := c.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if c, err := New(ctx, r, 17); c != nil || err == nil {
		t.Fatal("adopted returned lease", c, err)
	}
}
func TestCancellationBeforeAndAfterKnownFirmwareReply(t *testing.T) {
	r := newRPC(t, false)
	c := newClient(t, r)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := r.calls
	if !errors.Is(c.Acquire(ctx), context.Canceled) || r.calls != calls {
		t.Fatal("pre-cancellation called firmware")
	}
	ctx, cancel = context.WithCancel(context.Background())
	r.cancel = cancel
	if !errors.Is(c.Acquire(ctx), context.Canceled) {
		t.Fatal("late cancellation lost")
	}
	v, ok := c.LastReceipt()
	if !ok || v != (Receipt{17, 1, true, false}) {
		t.Fatal(v, ok)
	}
	r.cancel = nil
	if err := c.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

type callFunc func([7]uint64) ([4]uint64, error)

func (f callFunc) Invoke(a [7]uint64) ([4]uint64, error) { return f(a) }
func TestCopiedHandleSerializesConcurrentGrant(t *testing.T) {
	r := newRPC(t, false)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	c := newClient(t, callFunc(func(a [7]uint64) ([4]uint64, error) {
		if a[1] == 1 {
			once.Do(func() { close(entered); <-release })
		}
		return r.Invoke(a)
	}))
	copied := *c
	done := make(chan error, 1)
	go func() { done <- c.Acquire(context.Background()) }()
	<-entered
	if !errors.Is(copied.Acquire(context.Background()), ErrBusy) {
		t.Fatal("overlapping grant admitted")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if r.stats[1] != 1 {
		t.Fatal("multiple grants", r.stats)
	}
}

func TestAcquisitionReservesConfirmationAndStopGenerationSpace(t *testing.T) {
	c := &Client{state: &client{last: Receipt{Owner: 17, Generation: ^uint64(0) - 2}, cpu: callFunc(func([7]uint64) ([4]uint64, error) {
		t.Fatal("generation exhaustion entered firmware")
		return [4]uint64{}, nil
	})}}
	before, _ := c.LastReceipt()
	if err := c.Acquire(context.Background()); !errors.Is(err, ErrRetired) {
		t.Fatal(err)
	}
	after, valid := c.LastReceipt()
	if valid || after != before {
		t.Fatal(after, valid)
	}
}
