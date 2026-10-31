// SPDX-License-Identifier: GPL-3.0-or-later
package modelbackend

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/tenfoot"
	"github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol"
	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
	"github.com/DeanoC/fes/platform/embedded/tamago/fabricmodel"
	"github.com/DeanoC/fes/platform/embedded/tamago/nativefabric"
)

type actorRPC struct {
	t             *testing.T
	cmd           *exec.Cmd
	input         io.WriteCloser
	output        *bufio.Reader
	region        *nativefabric.Region
	releases      int
	writes, polls int
	programs      int
	wrongIdentity bool
	loseWrites    bool
	reserved      chan struct{}
}

func actorOptions() nativefabric.Options {
	return nativefabric.Options{DRAMBase: 0x80000000, ChunkBytes: 4096,
		IdentityProfiles: []nativefabric.IdentityProfile{{Platform: "de10_nano", Device: "5CSEBA6U23I7", ProgrammingProfile: "fes-gp-v1", ABI: "fes.simple-game", Major: 1, Minor: 0, ControlABI: 1}}}
}

func TestInjectedEndpointDirect(t *testing.T) {
	policy, config, read := fixtures(t)
	config.Entries = config.Entries[:1]
	r := newActorRPC(t)
	endpoint, err := nativefabric.New(r, actorOptions())
	if err != nil {
		t.Fatal(err)
	}
	backend, err := NewWithEndpoint(policy, config, read, nil, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer closeBackend(t, backend)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := hostclient.NewClient("http://model.invalid", &http.Client{Transport: backend})
	_, err = client.Launch(ctx, "model-normal")
	if err != nil {
		stats, _ := r.call("stats")
		t.Fatalf("launch: %v %+v stats=%v writes=%d polls=%d", err, backend.View(), stats, r.writes, r.polls)
	}
	if backend.View().Status.State != fabricmodel.Running {
		stats, _ := r.call("stats")
		t.Fatalf("launch status: %+v stats=%v writes=%d polls=%d", backend.View(), stats, r.writes, r.polls)
	}
	prior, err := client.Session(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.StopExpectedStamped(ctx, prior, hostclient.ClientStamp{}); err != nil {
		t.Fatal(err)
	}
}

func newActorRPC(t *testing.T) *actorRPC {
	t.Helper()
	path := os.Getenv("FES_EMBEDDED_RPC")
	if path == "" {
		t.Skip("actual RTL integration needs FES_EMBEDDED_RPC")
	}
	r := &actorRPC{t: t, cmd: exec.Command(path)}
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
func (r *actorRPC) call(format string, args ...any) ([]uint64, error) {
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
	var values []uint64
	for _, s := range fields[2:] {
		v, err := strconv.ParseUint(s, 16, 64)
		if err != nil {
			return nil, err
		}
		values = append(values, v)
	}
	return values, nil
}
func (r *actorRPC) Read32(o uint32) (uint32, error) {
	v, err := r.call("read %x", o)
	if err != nil {
		return 0, err
	}
	value := uint32(v[0])
	if r.wrongIdentity && r.programs == 2 && o == g.FesEmbeddedRegBuild3 {
		value ^= 1
	}
	return value, nil
}
func (r *actorRPC) Write32(o, v uint32) error { _, err := r.call("write %x %x", o, v); return err }
func (r *actorRPC) Invoke(q [7]uint64) ([4]uint64, error) {
	if q[0] == 0x42000002 {
		r.writes++
	} else if q[0] == 0xc2000003 {
		r.polls++
	} else if q[0] == 0xc2000001 {
		r.programs++
	}
	v, err := r.call("smc %x %x %x", q[0], q[1], q[2])
	var out [4]uint64
	copy(out[:], v)
	return out, err
}
func (r *actorRPC) Resolve(i fabricmodel.Identity) (embeddedcontrol.Identity, error) {
	return embeddedcontrol.Identity{ABI: 1, Build: [4]uint32{0x89abcdef, 0x01234567, 0x89abcdef, 0x01234567}}, nil
}
func (r *actorRPC) Reserve(data []byte) (*nativefabric.Region, error) {
	if r.region != nil {
		return nil, errors.New("test region already reserved")
	}
	region := &nativefabric.Region{Address: 0x96000000, Bytes: bytes.Clone(data)}
	region.Release = func() error { r.releases++; r.region = nil; return nil }
	r.region = region
	if r.loseWrites && len(data) > 128 {
		if _, err := r.call("lose"); err != nil {
			return region, err
		}
		close(r.reserved)
	}
	return region, nil
}
func (r *actorRPC) Save(fabricmodel.Identity) (bool, error) { return true, nil }
func (r *actorRPC) Contain() (bool, error)                  { _, err := r.call("contain"); return err == nil, err }
func (r *actorRPC) ControlOnly() (bool, error)              { _, err := r.call("control"); return err == nil, err }
func (r *actorRPC) ReleaseData() (bool, error)              { _, err := r.call("release"); return err == nil, err }

func TestTenfootInjectedEndpointLaunchReturnAndIdentityFailure(t *testing.T) {
	for _, wrong := range []bool{false, true} {
		name := "launch-return"
		if wrong {
			name = "identity-failure"
		}
		t.Run(name, func(t *testing.T) {
			policy, config, read := fixtures(t)
			config.Entries = config.Entries[:1]
			if wrong {
				config.Entries[0].Check = "identity"
			} else {
				config.Entries[0].Check = "return"
			}
			r := newActorRPC(t)
			r.wrongIdentity = wrong
			identity, err := r.Resolve(fabricmodel.Identity{})
			if err != nil {
				t.Fatal(err)
			}
			cpu := &sealedActorCPU{actorRPC: r, base: 0x40000000, identity: identity, bridges: actorBridgeState()}
			hooks, err := nativefabric.NewCPUHooks(cpu, cpu, cpu.base, 256)
			if err != nil {
				t.Fatal(err)
			}
			endpoint, err := nativefabric.New(hooks, actorOptions())
			if err != nil {
				t.Fatal(err)
			}
			backend, err := NewWithEndpoint(policy, config, read, nil, endpoint)
			if err != nil {
				t.Fatal(err)
			}
			defer closeBackend(t, backend)
			device, err := gfx.NewSoftware(640, 480)
			if err != nil {
				t.Fatal(err)
			}
			defer device.Close()
			app := tenfoot.NewApp(tenfoot.NewClient("http://model.invalid", &http.Client{Transport: backend, Timeout: 10 * time.Second}), 640, 480, 100)
			if err = app.SetPreferenceStore(preferences{}); err != nil {
				t.Fatal(err)
			}
			p := &scriptPlatform{app: app, backend: backend, entries: config.Entries, device: device}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err = tenfoot.RunPlatform(ctx, app, device, p); err != nil {
				stats, _ := r.call("stats")
				t.Fatalf("%v stage=%d snapshot=%+v backend=%+v stats=%v writes=%d polls=%d", err, p.stage, app.Snapshot().Launch, backend.View(), stats, r.writes, r.polls)
			}
			if p.index != 1 || !p.sawPixels || backend.View().Status.State != fabricmodel.Idle {
				t.Fatalf("case incomplete: %d %+v", p.index, backend.View())
			}
			stats, err := r.call("stats")
			if err != nil {
				t.Fatal(err)
			}
			expectedReleases := uint64(3)
			if wrong {
				expectedReleases = 2
			}
			if stats[0] != 3 || stats[1] != expectedReleases || r.releases != 3 {
				t.Fatalf("program/release stats=%v buffer releases=%d", stats, r.releases)
			}
		})
	}
}

func TestCPUBridgeFailureStopsActorBeforeProgramming(t *testing.T) {
	for _, fault := range []string{"hardware", "fabric", "owner"} {
		t.Run(fault, func(t *testing.T) {
			policy, config, read := fixtures(t)
			config.Entries = config.Entries[:1]
			r := newActorRPC(t)
			identity, err := r.Resolve(fabricmodel.Identity{})
			if err != nil {
				t.Fatal(err)
			}
			cpu := &sealedActorCPU{actorRPC: r, base: 0x40000000, identity: identity, bridges: actorBridgeState(), bridgeFault: fault}
			hooks, err := nativefabric.NewCPUHooks(cpu, cpu, cpu.base, 256)
			if err != nil {
				t.Fatal(err)
			}
			endpoint, err := nativefabric.New(hooks, actorOptions())
			if err != nil {
				t.Fatal(err)
			}
			backend, err := NewWithEndpoint(policy, config, read, nil, endpoint)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			client := hostclient.NewClient("http://model.invalid", &http.Client{Transport: backend})
			_, _ = client.Launch(ctx, "model-normal")
			if backend.View().Status.State != fabricmodel.RebootRequired {
				t.Fatal("failed closure was accepted", backend.View())
			}
			if err = backend.Close(ctx); !errors.Is(err, fabricmodel.ErrReboot) {
				t.Fatal("lost uncertain bridge ownership", err)
			}
			stats, err := r.call("stats")
			if err != nil || stats[0] != 1 || stats[1] != 1 || r.releases != 1 {
				t.Fatal("programmed or released failed target", stats, err)
			}
		})
	}
}

func TestInjectedEndpointCancellationRetainsTransfer(t *testing.T) {
	policy, config, read := fixtures(t)
	config.Entries = config.Entries[:1]
	r := newActorRPC(t)
	r.loseWrites, r.reserved = true, make(chan struct{})
	endpoint, err := nativefabric.New(r, actorOptions())
	if err != nil {
		t.Fatal(err)
	}
	backend, err := NewWithEndpoint(policy, config, read, nil, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		client := hostclient.NewClient("http://model.invalid", &http.Client{Transport: backend})
		_, _ = client.Launch(ctx, "model-normal")
		close(done)
	}()
	select {
	case <-r.reserved:
	case <-time.After(5 * time.Second):
		t.Fatal("transfer did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("canceled launch did not return")
	}
	wait(t, func() bool { return backend.View().Status.State == fabricmodel.RebootRequired })
	if r.region == nil || r.releases != 1 {
		t.Fatal("canceled transfer released its region")
	}
	stats, err := r.call("stats")
	if err != nil || stats[1] != 1 {
		t.Fatalf("canceled target was released: %v %v", stats, err)
	}
	closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer closeCancel()
	if err = backend.Close(closeCtx); !errors.Is(err, fabricmodel.ErrReboot) {
		t.Fatalf("close discarded uncertain owner: %v", err)
	}
}

func TestInjectedEndpointRejectsUnmappedPackageBeforeRetiringOwner(t *testing.T) {
	policy, config, read := fixtures(t)
	config.Entries = config.Entries[:1]
	r := newActorRPC(t)
	options := actorOptions()
	options.IdentityProfiles[0].Device = "unrelated-device"
	endpoint, err := nativefabric.New(r, options)
	if err != nil {
		t.Fatal(err)
	}
	backend, err := NewWithEndpoint(policy, config, read, nil, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer closeBackend(t, backend)
	before := backend.View().Status
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := hostclient.NewClient("http://model.invalid", &http.Client{Transport: backend})
	_, _ = client.Launch(ctx, "model-normal")
	if got := backend.View().Status; got != before {
		t.Fatalf("unmapped package changed owner: before=%+v after=%+v", before, got)
	}
	stats, err := r.call("stats")
	if err != nil || stats[0] != 1 || stats[1] != 1 || r.releases != 1 {
		t.Fatalf("unmapped package touched hardware: %v %v", stats, err)
	}
	s, err := endpointSnapshot(r)
	if err != nil || s.Status != 1 || s.Generation != before.DisplayGeneration {
		t.Fatalf("outgoing reader changed: %+v %v", s, err)
	}
}

func endpointSnapshot(r *actorRPC) (embeddedcontrol.Snapshot, error) {
	return (embeddedcontrol.Control{IO: r}).Snapshot()
}
