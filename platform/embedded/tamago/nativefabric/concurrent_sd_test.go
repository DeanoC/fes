// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io/fs"
	"maps"
	"sync"
	"testing"
	"time"

	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
	"github.com/DeanoC/fes/platform/embedded/tamago/scanout"
	"github.com/DeanoC/fes/platform/embedded/tamago/sdcontrol"
)

type sharedTestLease struct {
	mu         sync.Mutex
	r          sdcontrol.Receipt
	quiesce    int
	resumes    int
	returns    int
	quiesceErr error
	resumeErr  error
}

func (l *sharedTestLease) LastReceipt() (sdcontrol.Receipt, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.r, true
}

func (l *sharedTestLease) Quiesce() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.r.Active {
		return errors.New("shared lease already quiesced")
	}
	if l.quiesceErr != nil {
		return l.quiesceErr
	}
	l.quiesce++
	l.r.Active, l.r.Prepared = false, false
	l.r.Generation++
	return nil
}

func (l *sharedTestLease) Resume() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.r.Active || l.r.Prepared {
		return errors.New("shared lease already active")
	}
	if l.resumeErr != nil {
		return l.resumeErr
	}
	l.resumes++
	l.r.Active, l.r.Prepared = true, true
	l.r.Generation += 2
	return nil
}

func (l *sharedTestLease) ResumeAfterReturn(uint64, uint64) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.returns++
	return errors.New("shared lease forbids ResumeAfterReturn")
}

func newSharedMenuFixture(t *testing.T) (*programMenuFixture, *sharedTestLease, *programNetwork) {
	t.Helper()
	r := romProgramFixtureFor(t, NativeComputerProfile, false)
	l, err := addCompiledMenu(t, r.f.loader, compiledMenuFor(t, r.f.loader))
	if err != nil {
		t.Fatal(err)
	}
	r.f.loader = l
	f := newMenuFixture(t)
	f.cpu.computerCPU = *r.f.cpu.computerCPU
	r.f.cpu.computerCPU = &f.cpu.computerCPU
	port := &selectedRoleCPU{romProgramCPU: r.cpu}
	cpu := &programMenuCPU{selectedMenuCPU: &selectedMenuCPU{menuCPU: f.cpu, configuration: port}}
	l.boot.cpu = cpu
	fixture := &programMenuFixture{r: r, menu: f, cpu: cpu}

	// Shared SD uses private generation and does not hold bridges->device_lease.
	r.f.lease.r.Active, r.f.lease.r.Prepared = false, false
	sharedLease := &sharedTestLease{
		r: sdcontrol.Receipt{
			Owner:      r.f.cpu.state.Owner,
			Generation: 2,
			Active:     true,
			Prepared:   true,
			Shared:     true,
		},
	}
	r.f.files.lease = sharedLease
	sharedStore, err := packagestore.New(r.f.files, sharedLease)
	if err != nil {
		t.Fatal(err)
	}
	r.f.store = sharedStore

	registers := maps.Clone(f.cpu.registers)
	cpu.activate = func(role programRole) {
		cpu.kind = role
		if role == programRoleMenu {
			f.cpu.registers = maps.Clone(registers)
			f.cpu.accepted, f.cpu.displayed, f.cpu.slot, f.cpu.lastDrainCookie = 0, 0, 0, 0
			raw := port.memory[len(port.memory)-ProgramMetadataBytes:]
			for i := range 4 {
				f.cpu.registers[g.FesEmbeddedRegBuild0+uint32(i)*4] = binary.BigEndian.Uint32(raw[116-i*4:])
			}
		} else {
			for i := range 8 {
				word := uint16(r.f.cpu.wire.source[0]) * 0x11
				r.f.cpu.words[8+i] = word | word<<8
			}
		}
	}
	request, _, _ := addMenuSelection(t, r, 0)
	fixture.request = request
	reserve := func(data []byte) (*Region, error) {
		region, err := r.f.arena.Reserve(data)
		if err == nil {
			r.f.cpu.wire = &transferWire{t: t, source: bytes.Clone(data), region: region}
		}
		return region, err
	}
	netState, netCtrl := newProgramNetwork(t)
	opts := ProgramMenuOptions{
		Request: request,
		Limits:  scanout.MenuLimits{ReaderLimits: windowReadLimits(), PublicationTimeout: 4 * time.Second},
		Reserve: func(data []byte) (*Region, error) {
			fixture.reserves++
			return &Region{
				Address:          0x95000000,
				Bytes:            bytes.Clone(data),
				Release:          func() error { f.cpu.releases++; return nil },
				OrderFrameStores: func() error { return nil },
			}, nil
		},
	}
	fixture.s, err = NewProgramServiceWithMenu(context.Background(), fixture.r.f.loader, sharedStore, reserve, computerLimits(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.s.AttachNetwork(netCtrl); err != nil {
		t.Fatal(err)
	}
	return fixture, sharedLease, netState
}

func TestSharedSDWithMenuLiveInstallComputerAndEthernet(t *testing.T) {
	f, lease, netState := newSharedMenuFixture(t)

	// 1. After MENU Ready: SD is active concurrently with displayed MENU + live Ethernet!
	active, err := f.r.f.store.ActiveReceipt()
	if err != nil || !active.Active || !active.Prepared || !active.Shared {
		t.Fatalf("expected active shared SD while MENU is displayed: %+v err=%v", active, err)
	}
	if netState.paused.Load() {
		t.Fatal("expected Ethernet live while MENU is Ready")
	}
	if err := f.s.PresentMenu(context.Background(), menuFrame(0x42)); err != nil {
		t.Fatal(err)
	}
	f.assertFrame(t, 0x42)

	// 2. Concurrent Read during MENU + Ethernet works cleanly.
	req, _, _ := addROMProgram(t, f.r, 0, 8192)
	if _, err := f.r.f.store.Retain(context.Background(), req); err != nil {
		t.Fatalf("concurrent Retain during active MENU+Ethernet failed: %v", err)
	}

	// 3. ProgramBackend.Install writes directly to active SD WITHOUT closing MENU or pausing Ethernet!
	backend, err := NewProgramBackend(f.s, []ProgramEntry{{ID: "core0", Title: "Core 0", Request: req}})
	if err != nil {
		t.Fatal(err)
	}
	req1, _, _ := addROMProgram(t, f.r, 1, 4096)
	installFiles := programUpload(f.r.f.files, req1)
	plan, err := backend.PrepareInstall(context.Background(), "core1", "Live Installed Core", req1, installFiles)
	if err != nil {
		t.Fatal(err)
	}
	configsBefore := f.r.f.cpu.configurations
	quiescesBefore := lease.quiesce
	stopsBefore := netState.stops
	if err := backend.Install(context.Background(), f.s.Status().Generation, plan); err != nil {
		t.Fatalf("live shared Install failed: %v", err)
	}
	if f.r.f.cpu.configurations != configsBefore {
		t.Fatalf("shared Install closed/reloaded MENU: configs %d -> %d", configsBefore, f.r.f.cpu.configurations)
	}
	if lease.quiesce != quiescesBefore {
		t.Fatalf("shared Install quiesced SD unnecessarily: %d -> %d", quiescesBefore, lease.quiesce)
	}
	if netState.stops != stopsBefore || netState.paused.Load() {
		t.Fatalf("shared Install paused Ethernet unnecessarily: stops %d -> %d paused=%v", stopsBefore, netState.stops, netState.paused.Load())
	}

	// Verify installed entry file exists on active SD while MENU stayed live.
	if file, err := f.r.f.files.Open("FES/UI.JSON"); err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	} else if err == nil {
		file.Close()
	}

	// 4. Rejected Launch restores MENU and keeps shared SD active!
	beforeGen := f.s.Status().Generation
	if _, err := f.s.Launch(context.Background(), beforeGen, packagestore.Request{Path: "missing"}); err == nil {
		t.Fatal("expected rejected launch error")
	}
	if f.s.Status().State != ProgramReady || f.s.Status().Generation != beforeGen {
		t.Fatalf("unexpected state after rejected launch: %+v", f.s.Status())
	}
	if _, err := f.r.f.store.ActiveReceipt(); err != nil {
		t.Fatalf("expected active shared SD restored after rejected launch: %v", err)
	}
	if netState.paused.Load() {
		t.Fatal("expected Ethernet resumed after rejected launch")
	}

	// 5. Launch the newly installed Computer core (req1): pauses SD & Ethernet during config,
	// then resumes BOTH SD and Ethernet concurrently with running Computer core!
	session, err := f.s.Launch(context.Background(), f.s.Status().Generation, req1)
	if err != nil {
		t.Fatalf("Launch failed: %v", err)
	}
	if f.s.Status().State != ProgramRunning || f.cpu.kind != programRoleComputer {
		t.Fatalf("expected ProgramRunning computer, got %+v kind=%v", f.s.Status(), f.cpu.kind)
	}
	if active, err := f.r.f.store.ActiveReceipt(); err != nil || !active.Active || !active.Prepared {
		t.Fatalf("expected SD resumed concurrently with running Computer core: %+v err=%v", active, err)
	}
	if netState.paused.Load() {
		t.Fatal("expected Ethernet resumed concurrently with running Computer core")
	}
	// Exercise Computer control AND concurrent SD Retain while Computer + Ethernet run!
	if err := f.s.Execution(context.Background(), session, true); err != nil {
		t.Fatal(err)
	}
	if _, err := f.r.f.store.Retain(context.Background(), req1); err != nil {
		t.Fatalf("concurrent SD read during active Computer+Ethernet failed: %v", err)
	}
	if err := f.s.Keyboard(context.Background(), session, [9]uint16{}); err != nil {
		t.Fatal(err)
	}
	if err := f.s.Execution(context.Background(), session, false); err != nil {
		t.Fatal(err)
	}

	// 6. Return to MENU restores MENU, SD, and Ethernet without ResumeAfterReturn!
	if _, err := f.s.Return(context.Background(), session); err != nil {
		t.Fatalf("Return failed: %v", err)
	}
	if f.s.Status().State != ProgramReady || f.cpu.kind != programRoleMenu || f.menu.cpu.displayed != 1 {
		t.Fatalf("Return did not restore MENU: %+v", f.s.Status())
	}
	if lease.returns != 0 {
		t.Fatalf("shared mode called ResumeAfterReturn %d times", lease.returns)
	}
	if active, err := f.r.f.store.ActiveReceipt(); err != nil || !active.Active {
		t.Fatalf("expected active SD after Return to MENU: %+v err=%v", active, err)
	}
	if netState.paused.Load() {
		t.Fatal("expected Ethernet live after Return to MENU")
	}

	// 7. ParkMenu quiesces shared SD cleanly and reaches ProgramParked.
	if err := f.s.ParkMenu(context.Background()); err != nil {
		t.Fatalf("ParkMenu failed: %v", err)
	}
	if f.s.Status().State != ProgramParked {
		t.Fatalf("expected ProgramParked, got %+v", f.s.Status())
	}
	if r, ok := lease.LastReceipt(); !ok || r.Active {
		t.Fatalf("expected shared SD quiesced on ParkMenu: %+v ok=%v", r, ok)
	}
}

func TestSharedSDNonMenuLaunchAndPark(t *testing.T) {
	l, cpu, _, files, _ := programLoaderFixture(t)
	lease := &sharedTestLease{
		r: sdcontrol.Receipt{
			Owner:      cpu.state.Owner,
			Generation: 2,
			Active:     true,
			Prepared:   true,
			Shared:     true,
		},
	}
	files.lease = lease
	store, err := packagestore.New(files, lease)
	if err != nil {
		t.Fatal(err)
	}
	arena, err := NewProgramArena(0x94800000, make([]byte, ProgramMetadataBytes+256), func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	reserve := func(data []byte) (*Region, error) {
		r, err := arena.Reserve(data)
		if err == nil {
			cpu.wire = &transferWire{t: t, source: bytes.Clone(data), region: r}
			for i := range 8 {
				word := uint16(data[0]) * 0x11
				cpu.words[8+i] = word | word<<8
			}
		}
		return r, err
	}
	svc, err := NewProgramService(l, store, reserve, computerLimits())
	if err != nil {
		t.Fatal(err)
	}
	netState, netCtrl := newProgramNetwork(t)
	if err := svc.AttachNetwork(netCtrl); err != nil {
		t.Fatal(err)
	}

	req, _, _ := addProgram(t, l, files, 0)
	session, err := svc.Launch(context.Background(), svc.Status().Generation, req)
	if err != nil {
		t.Fatal(err)
	}
	if r, err := store.ActiveReceipt(); err != nil || !r.Active || netState.paused.Load() {
		t.Fatalf("expected SD and Ethernet both active during running non-menu core: %+v err=%v paused=%v", r, err, netState.paused.Load())
	}
	if _, err := svc.Park(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if svc.Status().State != ProgramParked {
		t.Fatalf("expected ProgramParked, got %+v", svc.Status())
	}
	if r, ok := lease.LastReceipt(); !ok || r.Active || lease.returns != 0 {
		t.Fatalf("expected shared SD quiesced cleanly on Park: %+v returns=%d", r, lease.returns)
	}
}

func TestSharedSDConcurrentRetainActiveTransferAdmission(t *testing.T) {
	l, cpu, _, files, _ := programLoaderFixture(t)
	lease := &sharedTestLease{
		r: sdcontrol.Receipt{
			Owner:      cpu.state.Owner,
			Generation: 2,
			Active:     true,
			Prepared:   true,
			Shared:     true,
		},
	}
	files.lease = lease
	store, err := packagestore.New(files, lease)
	if err != nil {
		t.Fatal(err)
	}
	arena, err := NewProgramArena(0x94800000, make([]byte, ProgramMetadataBytes+256), func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	req, _, _ := addProgram(t, l, files, 0)
	sel, err := store.Retain(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	handoff, err := store.RetainActive(sel)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ActiveReceipt(); err != nil || !handoff.Concurrent() {
		t.Fatalf("expected SD active for Concurrent RetainActive handoff: %v", err)
	}
	plan, err := l.Preflight(sel)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := plan.NewTransfer(handoff, func(data []byte) (*Region, error) {
		return arena.Reserve(data)
	}); err != nil {
		t.Fatalf("expected Concurrent RetainActive handoff to pass cold transfer admission: %v", err)
	}
}

func TestProgramBackendRoundTripAndActiveStoreCheckInSharedMode(t *testing.T) {
	f, _, _ := newSharedMenuFixture(t)
	req, _, _ := addROMProgram(t, f.r, 0, 8192)
	if !f.r.f.store.Shared() {
		t.Fatal("expected store.Shared()=true")
	}
	if _, err := f.s.Launch(context.Background(), f.s.Status().Generation, req); err != nil {
		t.Fatal(err)
	}
	if _, err := f.r.f.store.ActiveReceipt(); err != nil {
		t.Fatalf("expected active store while running: %v", err)
	}
}
