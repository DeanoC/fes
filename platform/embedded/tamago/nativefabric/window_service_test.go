// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"context"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	stdimage "image"
	"maps"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/tenfoot"
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
)

// Run the actual retained archive, transfer, window session and service against
// modeled secure calls/CSRs. The existing C/RTL tests cover the lower provider.
type serviceWindowCPU struct {
	*windowStorageCPU
	f     *presentationFixture
	reset map[uint32]uint32
}

func (c *serviceWindowCPU) Invoke(a [7]uint64) ([4]uint64, error) {
	r, err := c.windowStorageCPU.Invoke(a)
	if a[0] == isdone && err == nil && r == ([4]uint64{}) {
		c.f.cpu.registers = maps.Clone(c.reset)
		c.f.cpu.lastDrainCookie = 0
	}
	return r, err
}

type windowServiceFixture struct {
	f       *presentationFixture
	s       *WindowService
	h       WindowHandle
	lease   *windowStorageLease
	files   *windowStorageFiles
	request packagestore.Request
	fault   string
}

func windowServiceTest(t *testing.T) *windowServiceFixture {
	t.Helper()
	f, store, _, lease, files, request := storedWindow(t)
	r := &windowServiceFixture{f: f, lease: lease, files: files, request: request}
	reset := maps.Clone(f.cpu.registers)
	initialContext, cancelInitial := context.WithCancel(context.Background())
	initial := f.open(t, initialContext)
	if _, err := initial.PresentFrame(context.Background(), f.frame); err != nil {
		t.Fatal(err)
	}
	data := f.transfer.source
	sum := sha512.Sum384(data)
	m := coldConfigurationMap{Format: 1, Scope: "sealed-cold-configuration-v1", PackageID: f.v.context.PackageID, BuildID: f.v.context.BuildID,
		CoreSHA256: f.v.context.CoreSHA256, CoreSHA384: hex.EncodeToString(sum[:]), CoreSize: len(data), Base: 0x94800000, Size: 8 << 20, DRAMBase: 0x80000000, ChunkBytes: 64}
	for offset := 0; offset < len(data); offset += 64 {
		digest := sha512.Sum384(data[offset:min(offset+64, len(data))])
		m.ChunkSHA384 = append(m.ChunkSHA384, hex.EncodeToString(digest[:]))
	}
	configuration := canonicalConfiguration(t, m)
	cpu := f.v.cpu.(*windowStorageCPU)
	f.v.cpu = &serviceWindowCPU{cpu, f, reset}
	arena, err := NewProgramArena(0x94800000, make([]byte, ProgramMetadataBytes+256), func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	reserve := func(data []byte) (*Region, error) {
		region, err := arena.Reserve(data)
		if err != nil {
			return region, err
		}
		cpu.configuration.wire = &transferWire{t: t, source: bytes.Clone(data), region: region}
		if r.fault == "write" {
			cpu.configuration.wire.fault = "write"
		}
		if r.fault == "reserve" {
			return region, errors.New("uncertain reservation")
		}
		return region, nil
	}
	r.s, r.h, err = NewWindowService(initial, store, f.control, configuration, f.access, reserve, windowReadLimits())
	if err != nil {
		t.Fatal(err)
	}
	cancelInitial() // The service has taken ownership of the boot session.
	return r
}

func TestWindowServiceThreeRequestCyclesAndDisplayHandles(t *testing.T) {
	r := windowServiceTest(t)
	h := r.h
	for cycle := 0; cycle < 3; cycle++ {
		r.f.colour(byte(cycle + 2))
		// This is the same Present boundary consumed by board.Display/Tenfoot.
		var display interface{ Present(*stdimage.RGBA) error } = h
		if err := display.Present(r.f.frame); err != nil {
			t.Fatal(err)
		}
		if r.s.Status().Frames != 2 {
			t.Fatal("missing first or next frame", r.s.Status())
		}
		var returned WindowSessionReturn
		var err error
		if cycle == 2 {
			returned, err = r.s.Park(context.Background(), h)
		} else {
			returned, err = r.s.Return(context.Background(), h)
		}
		if err != nil || returned.After.Generation != uint64(7+cycle*6) || returned.Frames != 2 {
			t.Fatal(returned, err)
		}
		calls, reads, opens := r.f.cpu.calls, r.f.cpu.reads, r.files.opens
		if err := h.Present(r.f.frame); !errors.Is(err, ErrWindowServiceSession) && !errors.Is(err, ErrWindowServiceRetired) {
			t.Fatal("old display replayed", err)
		}
		if r.f.cpu.calls != calls || r.f.cpu.reads != reads || r.files.opens != opens {
			t.Fatal("old display performed I/O")
		}
		if cycle == 2 {
			break
		}
		ctx, cancel := context.WithCancel(context.Background())
		h, err = r.s.Launch(ctx, r.s.Status().Generation, r.request, r.f.frame)
		cancel() // A successful request's cancellation must not kill its handle.
		if err != nil {
			t.Fatal(err)
		}
		if r.files.opens <= opens || r.s.Status().State != ProgramRunning {
			t.Fatal("launch skipped fresh SD or session")
		}
	}
	if r.s.Status().State != ProgramParked || r.lease.returns != 2 || r.f.cpu.closes != 3 || r.f.cpu.releases != 0 {
		t.Fatal("wrong ownership", r.s.Status(), r.lease.returns, r.f.cpu.closes)
	}
	if _, err := r.s.Launch(context.Background(), r.s.Status().Generation, r.request, r.f.frame); !errors.Is(err, ErrWindowServiceRetired) {
		t.Fatal(err)
	}
}

func TestWindowServiceRejectionsKeepStorageUsableAndPerformNoControlIO(t *testing.T) {
	r := windowServiceTest(t)
	if _, err := r.s.Return(context.Background(), r.h); err != nil {
		t.Fatal(err)
	}
	view := r.s.Status()
	calls, reads, files := r.f.cpu.calls, r.f.cpu.reads, r.files.opens
	if _, err := r.s.Launch(context.Background(), view.Generation-1, r.request, r.f.frame); !errors.Is(err, ErrWindowServiceSession) {
		t.Fatal(err)
	}
	if _, err := r.s.Launch(context.Background(), view.Generation, r.request, stdimage.NewRGBA(stdimage.Rect(0, 0, 1, 1))); err == nil {
		t.Fatal("bad geometry")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.s.Launch(ctx, view.Generation, r.request, r.f.frame); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if r.files.opens != files || r.f.cpu.calls != calls || r.f.cpu.reads != reads {
		t.Fatal("invalid request performed I/O")
	}
	bad := r.request
	bad.PackageID = "0000000000000000000000000000000000000000000000000000000000000000"
	if _, err := r.s.Launch(context.Background(), view.Generation, bad, r.f.frame); err == nil {
		t.Fatal("wrong package accepted")
	}
	if r.s.Status().State != ProgramReady || r.s.Status().Generation != view.Generation || r.f.cpu.calls != calls {
		t.Fatal("content rejection retired healthy SD")
	}
	if _, err := r.s.Launch(context.Background(), view.Generation, r.request, r.f.frame); err != nil {
		t.Fatal(err)
	}
}

func TestWindowServiceUncertainLaunchNeverRetriesOrReleasesFrameArena(t *testing.T) {
	for _, fault := range []string{"reserve", "write", "data-open", "crc", "source", "padding"} {
		t.Run(fault, func(t *testing.T) {
			r := windowServiceTest(t)
			if _, err := r.s.Return(context.Background(), r.h); err != nil {
				t.Fatal(err)
			}
			r.fault = fault
			switch fault {
			case "data-open", "crc":
				r.f.cpu.fault = fault
			case "source":
				r.f.cpu.digest[0] ^= 1
			case "padding":
				r.f.region.Bytes[len(r.f.region.Bytes)-1] ^= 1
			}
			if _, err := r.s.Launch(context.Background(), r.s.Status().Generation, r.request, r.f.frame); !errors.Is(err, ErrWindowServiceRetired) {
				t.Fatal(fault, err)
			}
			calls, reads, files, returns := r.f.cpu.calls, r.f.cpu.reads, r.files.opens, r.lease.returns
			if _, err := r.s.Launch(context.Background(), r.s.Status().Generation, r.request, r.f.frame); !errors.Is(err, ErrWindowServiceRetired) {
				t.Fatal(err)
			}
			if _, err := r.s.Return(context.Background(), r.h); !errors.Is(err, ErrWindowServiceRetired) {
				t.Fatal(err)
			}
			if r.f.cpu.calls != calls || r.f.cpu.reads != reads || r.files.opens != files || r.lease.returns != returns || r.f.cpu.releases != 0 {
				t.Fatal("terminal failure replayed or freed frame arena")
			}
		})
	}
}

func TestWindowServiceReturnAndPresentationFailuresKeepOwnership(t *testing.T) {
	for _, fault := range []string{"close", "grant", "receipt", "frame"} {
		t.Run(fault, func(t *testing.T) {
			r := windowServiceTest(t)
			var err error
			if fault == "frame" {
				r.f.cpu.fault = "crc"
				_, err = r.s.Present(context.Background(), r.h, r.f.frame)
			} else {
				if fault == "close" {
					r.f.cpu.fault = fault
				} else {
					r.lease.fault = fault
				}
				_, err = r.s.Return(context.Background(), r.h)
			}
			if !errors.Is(err, ErrWindowServiceRetired) {
				t.Fatal(fault, err)
			}
			closes, returns, calls := r.f.cpu.closes, r.lease.returns, r.f.cpu.calls
			if err = r.h.Present(r.f.frame); !errors.Is(err, ErrWindowServiceRetired) {
				t.Fatal(err)
			}
			if _, err = r.s.Return(context.Background(), r.h); !errors.Is(err, ErrWindowServiceRetired) {
				t.Fatal(err)
			}
			if r.f.cpu.closes != closes || r.lease.returns != returns || r.f.cpu.calls != calls || r.f.cpu.releases != 0 {
				t.Fatal("uncertain return retried")
			}
		})
	}
}

func TestWindowServiceCannotBorrowAnotherOwnerOrDuplicateTheService(t *testing.T) {
	r := windowServiceTest(t)
	other := windowServiceTest(t)
	p := r.s.state
	calls, reads := r.f.cpu.calls, r.f.cpu.reads
	if _, err := r.s.Present(context.Background(), other.h, r.f.frame); !errors.Is(err, ErrWindowServiceSession) {
		t.Fatal(err)
	}
	for _, store := range []*packagestore.Store{nil, {}, other.s.state.store, p.store} {
		if _, _, err := NewWindowService(p.current, store, p.control, p.configuration, p.contract, p.reserve, p.limits); err == nil {
			t.Fatal("foreign or duplicate service")
		}
	}
	copySession := *p.current
	if _, _, err := NewWindowService(&copySession, p.store, p.control, p.configuration, p.contract, p.reserve, p.limits); err == nil {
		t.Fatal("copied session adopted")
	}
	copyService := *r.s
	p.operation.Lock()
	if _, err := copyService.Present(context.Background(), r.h, r.f.frame); !errors.Is(err, ErrWindowServiceBusy) {
		t.Fatal(err)
	}
	p.operation.Unlock()
	if r.f.cpu.calls != calls || r.f.cpu.reads != reads {
		t.Fatal("rejected ownership did I/O")
	}
	if _, err := copyService.Park(context.Background(), r.h); err != nil {
		t.Fatal(err)
	}
}

func TestWindowServiceReturnFinishesAfterRequestCancellation(t *testing.T) {
	r := windowServiceTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.f.cpu.after = func(event string) {
		if event == "closed" {
			cancel()
		}
	}
	if _, err := r.s.Return(ctx, r.h); err != nil {
		t.Fatal(err)
	}
	if ctx.Err() == nil || r.s.Status().State != ProgramReady || r.lease.returns != 1 {
		t.Fatal("accepted return abandoned SD", r.s.Status())
	}
}

func TestWindowServiceConcurrentPresentationDoesNotPermitReturn(t *testing.T) {
	r := windowServiceTest(t)
	entered, proceed := make(chan struct{}), make(chan struct{})
	r.f.barrier = func() error { close(entered); <-proceed; return nil }
	done := make(chan error, 1)
	go func() { _, err := r.s.Present(context.Background(), r.h, r.f.frame); done <- err }()
	<-entered
	if _, err := r.s.Return(context.Background(), r.h); !errors.Is(err, ErrWindowServiceBusy) {
		t.Fatal(err)
	}
	if r.s.Status().State != ProgramRunning || r.s.Status().Frames != 1 {
		t.Fatal("published an in-flight frame")
	}
	close(proceed)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	r.f.barrier = nil
	if _, err := r.s.Park(context.Background(), r.h); err != nil {
		t.Fatal(err)
	}
}

func TestWindowServiceConfigurationDeadlineAndBackwardsClockRetire(t *testing.T) {
	for _, step := range []time.Duration{20 * time.Second, -time.Second} {
		t.Run(step.String(), func(t *testing.T) {
			r := windowServiceTest(t)
			if _, err := r.s.Return(context.Background(), r.h); err != nil {
				t.Fatal(err)
			}
			now := time.Unix(100, 0)
			r.s.state.limits.Now = func() time.Time { result := now; now = now.Add(step); return result }
			if _, err := r.s.Launch(context.Background(), r.s.Status().Generation, r.request, r.f.frame); !errors.Is(err, ErrWindowServiceRetired) {
				t.Fatal(err)
			}
			if r.s.Status().State != ProgramRebootRequired || r.f.cpu.releases != 0 {
				t.Fatal("clock failure lost ownership")
			}
		})
	}
}

func TestWindowServicePresentsRealTenfootFramesAcrossReturnAndReload(t *testing.T) {
	r := windowServiceTest(t)
	device, err := gfx.NewSoftware(1280, 720)
	if err != nil {
		t.Fatal(err)
	}
	defer device.Close()
	presenter, err := tenfoot.NewPresenter(device)
	if err != nil {
		t.Fatal(err)
	}
	defer presenter.Close()
	h := r.h
	seen := map[uint32]bool{}
	for index, message := range []string{"Package selection", "Core returned", "Ready for another load"} {
		if index > 0 {
			if _, err := r.s.Return(context.Background(), h); err != nil {
				t.Fatal(err)
			}
			h, err = r.s.Launch(context.Background(), r.s.Status().Generation, r.request, r.f.frame)
			if err != nil {
				t.Fatal(err)
			}
		}
		var grid tenfoot.Grid
		grid.Layout(1280, 720)
		if err := presenter.Draw(tenfoot.Snapshot{Grid: grid, Status: message, LoadErr: message}); err != nil {
			t.Fatal(err)
		}
		frame := device.Framebuffer()
		// Independently model the FPGA's visible packed B,G,R,ff read, rather
		// than borrowing the driver's expected checksum or publication bytes.
		for y := 0; y < 720; y++ {
			for x := 0; x < 1280; x++ {
				i := frame.PixOffset(x, y)
				o := (y*1280 + x) * 4
				copy(r.f.expected[o:o+4], []byte{frame.Pix[i+2], frame.Pix[i+1], frame.Pix[i], 0xff})
			}
		}
		presented, err := r.s.Present(context.Background(), h, frame)
		if err != nil {
			t.Fatal(err)
		}
		if presented.Frame.DeliveredBytes != 3686400 || seen[presented.Frame.CRC32] {
			t.Fatal("Tenfoot output did not change", presented.Frame)
		}
		seen[presented.Frame.CRC32] = true
		// Restore the fixture's plain first frame for the next request's launch.
		r.f.colour(byte(index + 2))
	}
	if _, err := r.s.Park(context.Background(), h); err != nil {
		t.Fatal(err)
	}
}
