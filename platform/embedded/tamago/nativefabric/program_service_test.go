// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/computer"
	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
)

type serviceFixture struct {
	s      *ProgramService
	loader *ProgramLoader
	cpu    *reloadCPU
	lease  *computerStorageLease
	files  *programTestFiles
	store  *packagestore.Store
	arena  *ConfigurationArena
	fault  string
}

// Match nano_bridge_sip.c: context reads cannot run under an SD device lease.
// All transfer/control layers still use the actual Go implementations below.
type serviceLeaseCPU struct {
	*reloadCPU
	lease *computerStorageLease
}

func (c *serviceLeaseCPU) Invoke(args [7]uint64) ([4]uint64, error) {
	if c.lease.r.Active && args[0] == uint64(g.FesEmbeddedBridgeCall) &&
		(args[1] == uint64(g.FesEmbeddedBridgeContextPart0) || args[1] == uint64(g.FesEmbeddedBridgeContextPart1)) {
		return [4]uint64{1}, nil
	}
	return c.reloadCPU.Invoke(args)
}

func serviceTest(t *testing.T) *serviceFixture {
	t.Helper()
	l, cpu, lease, files, store := programLoaderFixture(t)
	f := &serviceFixture{loader: l, cpu: cpu, lease: lease, files: files, store: store}
	l.boot.cpu = &serviceLeaseCPU{cpu, lease}
	arena, err := NewProgramArena(0x94800000, make([]byte, ProgramMetadataBytes+256), func() error {
		if f.fault == "release" && cpu.configurations == 1 {
			return errors.New("completion ordering failed")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	f.arena = arena
	reserve := func(data []byte) (*Region, error) {
		r, err := arena.Reserve(data)
		if err != nil {
			return r, err
		}
		cpu.wire = &transferWire{t: t, source: bytes.Clone(data), region: r}
		if f.fault == "write" {
			cpu.wire.fault = "write"
		}
		if f.fault == "reserve" {
			return r, errors.New("partial reservation retained")
		}
		if f.fault == "publication" {
			publish := r.PublishConfiguration
			r.PublishConfiguration = func(data []byte) error {
				if err := publish(data); err != nil {
					return err
				}
				return errors.New("metadata ordering failed")
			}
		}
		// The synthetic payload's byte identifies its actual synthetic build.
		for i := range 8 {
			word := uint16(data[0]) * 0x11
			cpu.words[8+i] = word | word<<8
		}
		if f.fault == "identity" {
			cpu.words[8] ^= 1
		}
		cpu.loseOpen = f.fault == "open"
		return r, nil
	}
	f.s, err = NewProgramService(l, store, reserve, computerLimits())
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *serviceFixture) launch(t *testing.T, index int) ProgramSession {
	t.Helper()
	request, _, _ := addProgram(t, f.loader, f.files, index)
	session, err := f.s.Launch(context.Background(), f.s.Status().Generation, request)
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func TestProgramServiceRequestsRunThreeDifferentPackagesAndRevokePriorSessions(t *testing.T) {
	f := serviceTest(t)
	var old ProgramSession
	for cycle := 0; cycle < 3; cycle++ {
		request, _, data := addProgram(t, f.loader, f.files, cycle)
		generation := f.s.Status().Generation
		if generation != uint64(cycle*2) {
			t.Fatal(f.s.Status())
		}
		session, err := f.s.Launch(context.Background(), generation, request)
		if err != nil {
			t.Fatal(cycle, err)
		}
		if status := f.s.Status(); status.State != ProgramRunning || status.Held || status.Identity.PackageID != request.PackageID ||
			session.Generation() != generation+1 || status.Identity.CoreID == f.loader.boot.Identity().CoreID {
			t.Fatal(status)
		}
		if f.lease.r.Active || f.s.state.transfer.Status().CompletedBytes != len(data) {
			t.Fatal("published running before transfer/containment")
		}
		opens, calls := f.files.opens, f.cpu.calls
		for _, stale := range []ProgramSession{{}, old} {
			if f.s.Execution(context.Background(), stale, true) == nil {
				t.Fatal("old session controlled a new core")
			}
		}
		if _, err := f.s.Launch(context.Background(), generation, request); err == nil || f.files.opens != opens || f.cpu.calls != calls {
			t.Fatal("running/stale request performed I/O")
		}
		if err := f.s.Execution(context.Background(), session, false); err != nil || !f.s.Status().Held {
			t.Fatal(err)
		}
		if err := f.s.Keyboard(context.Background(), session, [9]uint16{}); err != nil {
			t.Fatal(err)
		}
		if err := f.s.Execution(context.Background(), session, true); err != nil || f.s.Status().Held {
			t.Fatal(err)
		}
		if _, err := f.s.Return(context.Background(), session); err != nil {
			t.Fatal(err)
		}
		if f.s.Status().State != ProgramReady || !f.lease.r.Active || f.s.Status().Generation != generation+2 {
			t.Fatal("return published before SD resumption", f.s.Status(), f.lease.r)
		}
		calls, opens = f.cpu.calls, f.files.opens
		if _, err := f.s.Launch(context.Background(), generation, request); err == nil || f.cpu.calls != calls || f.files.opens != opens {
			t.Fatal("stale launch replayed after return")
		}
		if _, err := f.s.Return(context.Background(), session); err == nil || f.cpu.calls != calls {
			t.Fatal("return was replayed")
		}
		old = session
	}
	if f.cpu.configurations != 3 || f.cpu.opens != 3 || f.cpu.closes != 3 || f.lease.returns != 3 {
		t.Fatal(f.cpu.configurations, f.cpu.opens, f.cpu.closes, f.lease.returns)
	}
}

func TestProgramServiceContentRejectionKeepsReadySDAndDoesNotConsumeRequestGeneration(t *testing.T) {
	for _, fault := range []string{"missing", "metadata", "incompatible", "cancelled"} {
		t.Run(fault, func(t *testing.T) {
			f := serviceTest(t)
			req, _, _ := addProgram(t, f.loader, f.files, 0)
			switch fault {
			case "missing":
				delete(f.files.files, req.Path)
			case "metadata":
				f.files.files[req.MetadataPath].Data[0] ^= 1
			case "incompatible":
				req, _, _ = addProgram(t, f.loader, f.files, 0, func(s string) string {
					return strings.Replace(s, "interfaces = []", "[[interfaces]]\nid=\"fes.unknown\"\nmajor=1\nminor=0\nrequired=true", 1)
				})
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if fault == "cancelled" {
				cancel()
			}
			calls := f.cpu.calls
			if _, err := f.s.Launch(ctx, 0, req); err == nil || f.cpu.calls != calls || f.s.Status().State != ProgramReady || !f.lease.r.Active || f.s.Status().Generation != 0 {
				t.Fatal("content rejection changed ownership", f.s.Status(), err)
			}
			f.launch(t, 1)
		})
	}
}

func TestProgramServiceUncertainLaunchRetainsItsPlanArenaAndRejectsAllReplay(t *testing.T) {
	for _, fault := range []string{"reserve", "publication", "write", "release", "identity", "open"} {
		t.Run(fault, func(t *testing.T) {
			f := serviceTest(t)
			req, _, _ := addProgram(t, f.loader, f.files, 0)
			f.fault = fault
			if _, err := f.s.Launch(context.Background(), 0, req); !errors.Is(err, ErrProgramRetired) || f.s.Status().State != ProgramRebootRequired || f.lease.r.Active {
				t.Fatal("uncertain launch was published or granted SD", f.s.Status(), err)
			}
			if f.s.state.plan == nil || f.s.state.plan.state.region == nil {
				t.Fatal("partial plan/reservation was lost")
			}
			calls, opens, grants := f.cpu.calls, f.files.opens, f.lease.returns
			copy := *f.s
			for _, s := range []*ProgramService{f.s, &copy} {
				if _, err := s.Launch(context.Background(), s.Status().Generation, req); !errors.Is(err, ErrProgramRetired) {
					t.Fatal(err)
				}
				if _, err := s.Return(context.Background(), ProgramSession{}); !errors.Is(err, ErrProgramRetired) {
					t.Fatal(err)
				}
			}
			if f.cpu.calls != calls || f.files.opens != opens || f.lease.returns != grants {
				t.Fatal("retired service replayed I/O")
			}
		})
	}
}

func TestProgramServiceReturnFailureNeverResumesOrRetries(t *testing.T) {
	for _, fault := range []string{"hold", "close", "grant", "source"} {
		t.Run(fault, func(t *testing.T) {
			f := serviceTest(t)
			session := f.launch(t, 0)
			switch fault {
			case "hold":
				f.cpu.stall = true
			case "close":
				f.cpu.loseClose = true
			case "grant":
				f.lease.fault = true
			case "source":
				f.cpu.digest[0] ^= 1
			}
			if _, err := f.s.Return(context.Background(), session); !errors.Is(err, ErrProgramRetired) || f.s.Status().State != ProgramRebootRequired || f.lease.r.Active {
				t.Fatal(err, f.s.Status())
			}
			calls, grants := f.cpu.calls, f.lease.returns
			if _, err := f.s.Return(context.Background(), session); !errors.Is(err, ErrProgramRetired) || f.cpu.calls != calls || f.lease.returns != grants {
				t.Fatal("failed return replayed")
			}
		})
	}
}

func TestProgramServiceOwnsOneVerifierAndCopiesShareRevocation(t *testing.T) {
	f := serviceTest(t)
	loaderCopy := *f.loader
	for _, l := range []*ProgramLoader{f.loader, &loaderCopy} {
		calls := f.cpu.calls
		if s, err := NewProgramService(l, f.store, f.arena.Reserve, computerLimits()); s != nil || err == nil || f.cpu.calls != calls {
			t.Fatal("verifier claimed twice")
		}
	}
	session := f.launch(t, 0)
	copy := *f.s
	if _, err := copy.Park(context.Background(), session); err != nil || f.s.Status().State != ProgramParked || f.lease.r.Active || f.lease.returns != 0 {
		t.Fatal("park reopened storage", err)
	}
	calls := f.cpu.calls
	if _, err := f.s.Return(context.Background(), session); !errors.Is(err, ErrProgramRetired) || f.cpu.calls != calls {
		t.Fatal("parked copy revived service")
	}
}

func TestProgramServiceRejectsBusyStaleAndInvalidInputWithoutHardwareIO(t *testing.T) {
	f := serviceTest(t)
	session := f.launch(t, 0)
	f.s.state.operation.Lock()
	if err := f.s.Execution(context.Background(), session, true); !errors.Is(err, ErrProgramBusy) {
		t.Fatal(err)
	}
	f.s.state.operation.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := f.cpu.calls
	if _, err := f.s.Return(ctx, session); !errors.Is(err, context.Canceled) || f.cpu.calls != calls || f.s.Status().State != ProgramRunning {
		t.Fatal("cancelled return touched hardware", err)
	}
	if err := f.s.Keyboard(context.Background(), session, [9]uint16{1}); !errors.Is(err, computer.ErrArgument) || f.cpu.calls != calls || f.s.Status().State != ProgramRunning {
		t.Fatal("invalid input retired a healthy core", err)
	}
}

func TestProgramServiceClockDeadlineAndLateFirmwareReturnRetainConfiguration(t *testing.T) {
	for _, fault := range []string{"deadline", "backwards", "late", "cancel"} {
		t.Run(fault, func(t *testing.T) {
			f := serviceTest(t)
			req, _, _ := addProgram(t, f.loader, f.files, 0)
			now := time.Unix(100, 0)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f.s.state.limits.Now = func() time.Time { return now }
			f.s.state.limits.Yield = func() {
				switch fault {
				case "deadline":
					now = now.Add(20 * time.Second)
				case "backwards":
					now = now.Add(-time.Second)
				case "cancel":
					cancel()
				}
			}
			if fault == "late" {
				calls := 0
				f.s.state.limits.Now = func() time.Time {
					calls++
					if calls == 3 {
						now = now.Add(20 * time.Second)
					}
					return now
				}
			}
			if _, err := f.s.Launch(ctx, 0, req); !errors.Is(err, ErrProgramRetired) || f.s.state.transfer == nil || !f.s.state.transfer.Status().Failed || f.lease.r.Active || f.cpu.opens != 0 {
				t.Fatal("late or cancelled configuration reached control", err, f.s.Status())
			}
		})
	}
}

func TestProgramServiceChangedLeaseAndSourceRetireBeforeReservation(t *testing.T) {
	for _, fault := range []string{"lease", "source"} {
		t.Run(fault, func(t *testing.T) {
			f := serviceTest(t)
			req, _, _ := addProgram(t, f.loader, f.files, 0)
			if fault == "lease" {
				f.lease.r.Generation++
			} else {
				f.cpu.digest[0] ^= 1
			}
			if _, err := f.s.Launch(context.Background(), 0, req); !errors.Is(err, ErrProgramRetired) || f.s.Status().State != ProgramRebootRequired {
				t.Fatal(err, f.s.Status())
			}
			if f.s.state.plan != nil && f.s.state.plan.state.region != nil || f.s.state.transfer != nil || f.cpu.configurations != 0 || f.cpu.opens != 0 {
				t.Fatal("changed ownership reached allocation or configuration")
			}
		})
	}
}

func TestProgramServiceControlFailureClosesOnceAndCannotReopenSD(t *testing.T) {
	f := serviceTest(t)
	session := f.launch(t, 0)
	f.cpu.stall = true
	if err := f.s.Execution(context.Background(), session, false); !errors.Is(err, ErrProgramRetired) || f.lease.r.Active || f.lease.returns != 0 || f.cpu.closes != 1 {
		t.Fatal("uncertain control did not close once", err)
	}
	calls := f.cpu.calls
	if _, err := f.s.Return(context.Background(), session); !errors.Is(err, ErrProgramRetired) || f.cpu.calls != calls || f.cpu.closes != 1 {
		t.Fatal("failed control replayed cleanup", err)
	}
}

type waitingProgramFiles struct {
	files            fs.FS
	entered, release chan struct{}
	wait             bool
}

func (f *waitingProgramFiles) Open(path string) (fs.File, error) {
	if !f.wait {
		f.wait = true
		close(f.entered)
		<-f.release
	}
	return f.files.Open(path)
}
func TestProgramServiceConcurrentStatusAndRequestsRemainSerialized(t *testing.T) {
	f := serviceTest(t)
	req, _, _ := addProgram(t, f.loader, f.files, 0)
	files := &waitingProgramFiles{files: f.files, entered: make(chan struct{}), release: make(chan struct{})}
	store, err := packagestore.New(files, f.lease)
	if err != nil {
		t.Fatal(err)
	}
	f.s.state.store = store
	done := make(chan error, 1)
	go func() { _, err := f.s.Launch(context.Background(), 0, req); done <- err }()
	<-files.entered
	if f.s.Status().State != ProgramLoading {
		t.Fatal(f.s.Status())
	}
	if _, err := f.s.Launch(context.Background(), 0, req); !errors.Is(err, ErrProgramBusy) {
		t.Fatal(err)
	}
	if _, err := f.s.Return(context.Background(), ProgramSession{}); !errors.Is(err, ErrProgramBusy) {
		t.Fatal(err)
	}
	close(files.release)
	if err := <-done; err != nil || f.s.Status().State != ProgramRunning {
		t.Fatal(err)
	}
}
