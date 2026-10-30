// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol"
	"github.com/DeanoC/fes/platform/embedded/tamago/fabricmodel"
)

type rpcHooks struct {
	t                      *testing.T
	cmd                    *exec.Cmd
	input                  io.WriteCloser
	output                 *bufio.Reader
	region                 *Region
	releases               int
	wrongIdentity, foreign bool
}

var rtlIdentity = embeddedcontrol.Identity{ABI: 1, Build: [4]uint32{0xccddeeff, 0x8899aabb, 0x44556677, 0x00112233}}

func newRPC(t *testing.T) *rpcHooks {
	t.Helper()
	path := os.Getenv("FES_EMBEDDED_RPC")
	if path == "" {
		t.Skip("run nativefabric/verify.py for actual firmware/RTL integration")
	}
	r := &rpcHooks{t: t, cmd: exec.Command(path)}
	var err error
	if r.input, err = r.cmd.StdinPipe(); err != nil {
		t.Fatal(err)
	}
	out, err := r.cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	r.output = bufio.NewReader(out)
	r.cmd.Stderr = os.Stderr
	if err = r.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		r.input.Close()
		if err := r.cmd.Wait(); err != nil {
			t.Errorf("test endpoint: %v", err)
		}
	})
	return r
}
func (r *rpcHooks) call(format string, args ...any) ([]uint64, error) {
	if _, err := fmt.Fprintf(r.input, format+"\n", args...); err != nil {
		return nil, err
	}
	line, err := r.output.ReadString('\n')
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(line)
	if len(fields) < 2 || fields[0] != "R" || fields[1] != "1" {
		return nil, fmt.Errorf("endpoint returned %q", line)
	}
	var result []uint64
	for _, v := range fields[2:] {
		x, err := strconv.ParseUint(v, 16, 64)
		if err != nil {
			return nil, err
		}
		result = append(result, x)
	}
	return result, nil
}
func (r *rpcHooks) Read32(o uint32) (uint32, error) {
	v, err := r.call("read %x", o)
	if err != nil {
		return 0, err
	}
	return uint32(v[0]), nil
}
func (r *rpcHooks) Write32(o, v uint32) error { _, err := r.call("write %x %x", o, v); return err }
func (r *rpcHooks) Invoke(req [7]uint64) ([4]uint64, error) {
	v, err := r.call("smc %x %x %x", req[0], req[1], req[2])
	if err != nil {
		return [4]uint64{}, err
	}
	var out [4]uint64
	copy(out[:], v)
	if r.foreign && req[0] == completed && out[1] != 0 {
		out[1]++
	}
	return out, nil
}
func (r *rpcHooks) Resolve(i fabricmodel.Identity) (embeddedcontrol.Identity, error) {
	v := rtlIdentity
	if r.wrongIdentity && i.Build == 2 {
		v.Build[3] ^= 1
	}
	return v, nil
}
func (r *rpcHooks) Reserve(b []byte) (*Region, error) {
	if r.region != nil || len(b) != 128 {
		return nil, errors.New("expected one 128-byte reserved test region")
	}
	region := &Region{Address: 0x96000000, Bytes: bytes.Clone(b)}
	region.Release = func() error { r.releases++; r.region = nil; return nil }
	r.region = region
	return region, nil
}
func (r *rpcHooks) Save(fabricmodel.Identity) (bool, error) { return true, nil }
func (r *rpcHooks) Contain() (bool, error)                  { _, err := r.call("contain"); return err == nil, err }
func (r *rpcHooks) ControlOnly() (bool, error)              { _, err := r.call("control"); return err == nil, err }
func (r *rpcHooks) ReleaseData() (bool, error)              { _, err := r.call("release"); return err == nil, err }
func setupRPC(t *testing.T) (*fabricmodel.Controller, *Endpoint, *rpcHooks, *time.Duration) {
	t.Helper()
	r := newRPC(t)
	c, err := fabricmodel.New(image(1, "model.idle", true), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	e, err := New(r, Options{DRAMBase: 0x80000000, ChunkBytes: 32})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Duration(0)
	if err = c.Boot(now, time.Second); err != nil {
		t.Fatal(err)
	}
	if err = drive(c, e, &now, 500); err != nil {
		t.Fatalf("boot: %v", err)
	}
	return c, e, r, &now
}
func TestActualFirmwareRTLLaunchReturnAndFailures(t *testing.T) {
	t.Run("launch-return-drains-accepted-read", func(t *testing.T) {
		c, e, r, now := setupRPC(t)
		if _, err := r.call("offer"); err != nil {
			t.Fatal(err)
		}
		if err := c.Load(image(2, "model.game", false), *now, *now+time.Second); err != nil {
			t.Fatal(err)
		}
		for range 5 {
			*now += time.Millisecond
			if err := e.Step(c, *now); err != nil {
				t.Fatal(err)
			}
		}
		stats, err := r.call("stats")
		if err != nil {
			t.Fatal(err)
		}
		if stats[0] != 1 || c.Status().State != fabricmodel.Changing {
			t.Fatalf("programmed before read drain: %v %+v", stats, c.Status())
		}
		if _, err = r.call("beat 0"); err != nil {
			t.Fatal(err)
		}
		*now += time.Millisecond
		if err = e.Step(c, *now); err != nil {
			t.Fatal(err)
		}
		stats, err = r.call("stats")
		if err != nil || stats[0] != 1 {
			t.Fatalf("partial read drained: %v %v", stats, err)
		}
		if _, err = r.call("beat 1"); err != nil {
			t.Fatal(err)
		}
		if err = drive(c, e, now, 500); err != nil {
			t.Fatal(err)
		}
		if c.Status().State != fabricmodel.Running {
			t.Fatalf("launch: %+v", c.Status())
		}
		if err = c.Stop(*now, *now+time.Second); err != nil {
			t.Fatal(err)
		}
		if err = drive(c, e, now, 500); err != nil {
			t.Fatal(err)
		}
		stats, err = r.call("stats")
		if err != nil {
			t.Fatal(err)
		}
		if c.Status().State != fabricmodel.Idle || stats[0] != 3 || stats[1] != 3 || stats[2] != 128 || r.releases != 3 {
			t.Fatalf("return: %+v stats=%v released=%d", c.Status(), stats, r.releases)
		}
	})
	t.Run("unknown-firmware-before-disable", func(t *testing.T) {
		c, e, r, now := setupRPC(t)
		if _, err := r.call("badfirmware"); err != nil {
			t.Fatal(err)
		}
		if err := c.Load(image(2, "model.game", false), *now, *now+time.Second); err != nil {
			t.Fatal(err)
		}
		if err := drive(c, e, now, 500); err == nil || c.Status().State != fabricmodel.RebootRequired {
			t.Fatalf("unknown firmware: %v %+v", err, c.Status())
		}
		s, err := e.ctrl.Snapshot()
		if err != nil || s.Status != 1 || s.Generation != 1 {
			t.Fatalf("old reader changed: %+v %v", s, err)
		}
	})
	t.Run("foreign-completion-retains-memory", func(t *testing.T) {
		c, e, r, now := setupRPC(t)
		r.foreign = true
		if err := c.Load(image(2, "model.game", false), *now, *now+time.Second); err != nil {
			t.Fatal(err)
		}
		if err := drive(c, e, now, 500); err == nil || c.Status().State != fabricmodel.RebootRequired || r.region == nil || r.releases != 1 {
			t.Fatalf("foreign completion: %v %+v", err, c.Status())
		}
		stats, err := r.call("stats")
		if err != nil || stats[1] != 1 {
			t.Fatalf("released target: %v %v", stats, err)
		}
	})
	t.Run("wrong-identity-recovers-idle", func(t *testing.T) {
		c, e, r, now := setupRPC(t)
		r.wrongIdentity = true
		if err := c.Load(image(2, "model.game", false), *now, *now+time.Second); err != nil {
			t.Fatal(err)
		}
		if err := drive(c, e, now, 500); !errors.Is(err, embeddedcontrol.ErrIdentity) || c.Status().State != fabricmodel.Idle {
			t.Fatalf("identity recovery: %v %+v", err, c.Status())
		}
		stats, err := r.call("stats")
		if err != nil || stats[0] != 3 || stats[1] != 2 {
			t.Fatalf("identity released game: %v %v", stats, err)
		}
	})
}
