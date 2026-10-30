// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol"
	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
	"github.com/DeanoC/fes/platform/embedded/tamago/fabricmodel"
	"github.com/DeanoC/fes/platform/embedded/tamago/runtimeadmission"
)

// The RPC executes the prepared TF-A functions and RTL, while the mailbox,
// memory, bridges and absolute CPU aperture remain simulated.
type sealedRPC struct {
	cmd                               *exec.Cmd
	in                                io.WriteCloser
	out                               *bufio.Reader
	base                              uint64
	identity                          embeddedcontrol.Identity
	region                            *Region
	programs, reads, writes, releases int
	wrongIdentity, lose               bool
}

func newSealedRPC(t *testing.T, identity embeddedcontrol.Identity) *sealedRPC {
	t.Helper()
	path := os.Getenv("FES_SEALED_TFA_RPC")
	if path == "" {
		t.Skip("run verify_sealed.py for the sealed Nano integration")
	}
	r := &sealedRPC{cmd: exec.Command(path), base: 0x40000000, identity: identity}
	var err error
	if r.in, err = r.cmd.StdinPipe(); err != nil {
		t.Fatal(err)
	}
	stdout, err := r.cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	r.out = bufio.NewReader(stdout)
	r.cmd.Stderr = os.Stderr
	if err = r.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		r.in.Close()
		if err := r.cmd.Wait(); err != nil {
			t.Errorf("firmware model: %v", err)
		}
	})
	return r
}
func (r *sealedRPC) call(format string, args ...any) ([]uint64, error) {
	if _, err := fmt.Fprintf(r.in, format+"\n", args...); err != nil {
		return nil, err
	}
	line, err := r.out.ReadString('\n')
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(line)
	if len(fields) < 2 || fields[0] != "R" || fields[1] != "1" {
		return nil, fmt.Errorf("firmware model: %q", line)
	}
	result := make([]uint64, 0, len(fields)-2)
	for _, field := range fields[2:] {
		value, err := strconv.ParseUint(field, 16, 64)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, nil
}
func (r *sealedRPC) ReadPhysical32(address uint64) (uint32, error) {
	r.reads++
	if address < r.base || address-r.base > 255 {
		return 0, errors.New("outside synthetic aperture")
	}
	offset := uint32(address - r.base)
	v, err := r.call("read %x", offset)
	if err != nil {
		return 0, err
	}
	if len(v) != 1 {
		return 0, errors.New("bad read response")
	}
	value := uint32(v[0])
	if r.wrongIdentity && r.programs == 2 && offset == g.FesEmbeddedRegBuild3 {
		value ^= 1
	}
	return value, nil
}
func (r *sealedRPC) WritePhysical32(address uint64, value uint32) error {
	if address < r.base || address-r.base > 255 {
		return errors.New("outside synthetic aperture")
	}
	r.writes++
	_, err := r.call("write %x %x", address-r.base, value)
	return err
}
func (r *sealedRPC) Invoke(q [7]uint64) ([4]uint64, error) {
	if q[0] == start {
		r.programs++
	}
	v, err := r.call("smc %x %x %x", q[0], q[1], q[2])
	var result [4]uint64
	if err != nil {
		return result, err
	}
	if len(v) != len(result) {
		return result, errors.New("bad firmware response")
	}
	copy(result[:], v)
	return result, nil
}
func (r *sealedRPC) Resolve(fabricmodel.Identity) (embeddedcontrol.Identity, error) {
	return r.identity, nil
}
func (r *sealedRPC) Reserve(data []byte) (*Region, error) {
	if r.region != nil {
		return nil, errors.New("reservation still held")
	}
	region := &Region{Address: 0x96000000, Bytes: bytes.Clone(data)}
	region.Release = func() error { r.region = nil; r.releases++; return nil }
	r.region = region
	if r.lose && len(data) > 128 {
		if _, err := r.call("lose"); err != nil {
			return region, err
		}
	}
	return region, nil
}
func (r *sealedRPC) Save(fabricmodel.Identity) (bool, error) { return true, nil }
func (r *sealedRPC) Contain() (bool, error)                  { _, err := r.call("contain"); return err == nil, err }
func (r *sealedRPC) ControlOnly() (bool, error)              { _, err := r.call("control"); return err == nil, err }
func (r *sealedRPC) ReleaseData() (bool, error)              { _, err := r.call("release"); return err == nil, err }

func sealedFixture(t *testing.T) (*corepackage.Archive, runtimeadmission.Policy, embeddedcontrol.Identity, []byte) {
	t.Helper()
	path := os.Getenv("FES_DE25_SEALED_PACKAGE")
	if path == "" {
		t.Skip("run verify_sealed.py for the sealed Nano integration")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := corepackage.RetainArchive(context.Background(), int64(len(body)), bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	i := archive.Inspection()
	d := i.Descriptor
	if d.Format != 2 || d.Target.Platform != "de25_nano" || d.Target.Device != "A5EB013BB23BE4SCS" || d.Target.ProgrammingProfile != "de25-embedded-diagnostic-v1" || d.ABI.ID != "fes.de25-embedded-diagnostic" || d.ABI.Major != 1 || d.ABI.Minor != 0 {
		t.Fatalf("unexpected diagnostic package: %+v", d)
	}
	words, err := BuildWords(d.Build.ID)
	if err != nil {
		t.Fatal(err)
	}
	identity := embeddedcontrol.Identity{ABI: 0x10203040, Build: words}
	// This is an explicit simulation decision, not an acceptance oracle.
	policy := runtimeadmission.Policy{Scope: "qemu-model-only", RuntimeID: strings.Repeat("a", 64),
		Target:    runtimeadmission.Target{Platform: d.Target.Platform, Device: d.Target.Device},
		Drivers:   []runtimeadmission.Driver{{Profile: d.Target.ProgrammingProfile, ABI: runtimeadmission.Contract{ID: d.ABI.ID, Major: d.ABI.Major, Minor: d.ABI.Minor}}},
		Decisions: []runtimeadmission.Decision{{PackageID: i.PackageID, Code: "none"}}}
	return archive, policy, identity, body
}

func TestSealedNanoPackageNativeFirmwareCycle(t *testing.T) {
	archive, policy, identity, body := sealedFixture(t)
	gate, err := runtimeadmission.NewModel(policy)
	if err != nil {
		t.Fatal(err)
	}
	packageID := archive.Inspection().PackageID
	image, err := gate.PrepareModel(archive, packageID)
	if err != nil {
		t.Fatal(err)
	}
	// A changed payload must fail retained inspection, before any CPU access.
	payload, err := io.ReadAll(archive.Payload())
	if err != nil {
		t.Fatal(err)
	}
	offset := bytes.Index(body, payload)
	if offset < 0 {
		t.Fatal("payload absent from archive")
	}
	altered := bytes.Clone(body)
	altered[offset] ^= 1
	if _, err = corepackage.RetainArchive(context.Background(), int64(len(altered)), bytes.NewReader(altered)); err == nil {
		t.Fatal("altered payload accepted")
	}
	if _, err = gate.PrepareModel(archive, strings.Repeat("b", 64)); runtimeadmission.ErrorCode(err) != "package_mismatch" {
		t.Fatal(err)
	}

	for _, scenario := range []string{"launch-return", "wrong-live-id", "lost-firmware-transfer"} {
		t.Run(scenario, func(t *testing.T) {
			r := newSealedRPC(t, identity)
			hooks, err := NewCPUHooks(r, r, r.base, 256)
			if err != nil {
				t.Fatal(err)
			}
			d := archive.Inspection().Descriptor
			profile := IdentityProfile{Platform: d.Target.Platform, Device: d.Target.Device, ProgrammingProfile: d.Target.ProgrammingProfile, ABI: d.ABI.ID, Major: 1, ControlABI: identity.ABI}
			endpoint, err := New(hooks, Options{DRAMBase: 0x80000000, ChunkBytes: 65536, IdentityProfiles: []IdentityProfile{profile}})
			if err != nil {
				t.Fatal(err)
			}
			if err = endpoint.Preflight(image); err != nil {
				t.Fatal(err)
			}
			wrong := profile
			wrong.Device += "-other"
			unmapped, err := New(hooks, Options{DRAMBase: 0x80000000, ChunkBytes: 65536, IdentityProfiles: []IdentityProfile{wrong}})
			if err != nil || unmapped.Preflight(image) == nil || r.reads != 0 || r.writes != 0 || r.programs != 0 {
				t.Fatal("unmapped profile reached CPU")
			}
			idleData := []byte("model idle")
			idle, err := fabricmodel.Prepare("model.idle", idleData, sha256.Sum256(idleData), fabricmodel.Identity{ABI: 1, Core: 1, Build: 1}, true)
			if err != nil {
				t.Fatal(err)
			}
			controller, err := fabricmodel.New(idle, 60*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			now := time.Duration(0)
			if err = controller.Boot(now, now+60*time.Second); err != nil {
				t.Fatal(err)
			}
			if err = drive(controller, endpoint, &now, 50000); err != nil || controller.Status().State != fabricmodel.Idle {
				t.Fatalf("boot: %v %+v", err, controller.Status())
			}
			r.wrongIdentity = scenario == "wrong-live-id"
			r.lose = scenario == "lost-firmware-transfer"
			deadline := now + 60*time.Second
			if r.lose {
				deadline = now + time.Second
			}
			if err = controller.Load(image, now, deadline); err != nil {
				t.Fatal(err)
			}
			err = drive(controller, endpoint, &now, 50000)
			switch scenario {
			case "launch-return":
				if err != nil || controller.Status().State != fabricmodel.Running {
					t.Fatalf("launch: %v %+v", err, controller.Status())
				}
				stats, err := r.call("stats")
				if err != nil || len(stats) != 3 || stats[0] != 2 || stats[1] != 2 || stats[2] != uint64(len(payload)) {
					t.Fatalf("package transfer or release differs: %v %v", stats, err)
				}
				if err = controller.Stop(now, now+60*time.Second); err != nil {
					t.Fatal(err)
				}
				if err = drive(controller, endpoint, &now, 50000); err != nil || controller.Status().State != fabricmodel.Idle || r.programs != 3 || r.releases != 3 {
					t.Fatalf("return: %v %+v programs=%d releases=%d", err, controller.Status(), r.programs, r.releases)
				}
			case "wrong-live-id":
				if !errors.Is(err, embeddedcontrol.ErrIdentity) || controller.Status().State != fabricmodel.Idle || r.programs != 3 || r.releases != 3 {
					t.Fatalf("identity recovery: %v %+v programs=%d", err, controller.Status(), r.programs)
				}
				stats, err := r.call("stats")
				if err != nil || len(stats) != 3 || stats[1] != 2 {
					t.Fatalf("mismatched target released data: %v %v", stats, err)
				}
			case "lost-firmware-transfer":
				if err == nil || controller.Status().State != fabricmodel.RebootRequired || r.region == nil || r.releases != 1 {
					t.Fatalf("transfer outcome: %v %+v", err, controller.Status())
				}
				programs := r.programs
				if endpoint.Step(controller, now) == nil || r.programs != programs {
					t.Fatal("uncertain transfer was replayed")
				}
				stats, err := r.call("stats")
				if err != nil || len(stats) != 3 || stats[1] != 1 {
					t.Fatalf("uncertain transfer released data: %v %v", stats, err)
				}
			}
		})
	}
}
