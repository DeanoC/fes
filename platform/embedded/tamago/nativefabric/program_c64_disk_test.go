// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/DeanoC/FogCast/targetclient"
	"github.com/DeanoC/fes/platform/embedded/tamago/agentapi"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/ui/tenfoot"
	"github.com/DeanoC/fes/platform/embedded/tamago/block"
	cg "github.com/DeanoC/fes/platform/embedded/tamago/computer/generated"
	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
	"github.com/DeanoC/fes/platform/embedded/tamago/fat"
	"github.com/DeanoC/fes/platform/embedded/tamago/maccontrol"
	"github.com/DeanoC/fes/platform/embedded/tamago/network"
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
)

func TestTenfootNativeC64DiskSetSwap(t *testing.T) {
	testTenfootNativeC64DiskSwap(t, false, false)
}

func TestTenfootNativeC64DiskSwapFailureRetainsOwnership(t *testing.T) {
	testTenfootNativeC64DiskSwap(t, true, false)
}

func TestTenfootNativeC64DiskSwapWithScopedMAC(t *testing.T) {
	testTenfootNativeC64DiskSwap(t, false, true)
}

func testTenfootNativeC64DiskSwap(t *testing.T, failSwap, scoped bool) {
	ctx := t.Context()
	image := filepath.Join(t.TempDir(), "disk-swap.img")
	file, err := os.Create(image)
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(file.Truncate(64<<20), file.Close()); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("mformat", "-i", image, "-F", "::").CombinedOutput(); err != nil {
		t.Fatalf("mformat: %v: %s", err, output)
	}
	disk, err := block.OpenFile(image, true)
	if err != nil {
		t.Fatal(err)
	}
	defer disk.Close()

	pickerPort := &fixtureC64Picker{length: -1}
	var startup *ProgramStartup
	var diskSet *ProgramC64DiskSet
	var port *spectrumFirmwareCPU
	var scopeCPU *c64CoexistingCPU
	var c64ROM []byte
	clock := time.Unix(1, 0)
	d64 := bytes.Repeat([]byte{0x42}, int(cg.FesComputerC64DiskBytes))
	nextDisk := bytes.Repeat([]byte{0x81}, len(d64))
	nextID := fmt.Sprintf("%x", sha256.Sum256(nextDisk))
	f := newProgramMenuFixtureContext(t, ctx, "", func(f *programMenuFixture) {
		f.networkInMenuOnly = true
		l, err := f.r.f.loader.WithSharedCoreShell()
		if err != nil {
			t.Fatal(err)
		}
		f.r.f.loader = l
		c64, c64Files := c64PackageFixture(t, l, c64Profile, nil)
		c64ROM = c64Files[c64.Sources[0].Path]
		c64.Tape = packagestore.TapeRequest{Path: "FES/C64.D64", Format: packagestore.C64DiskFormat,
			Size: int64(len(d64)), SHA256: fmt.Sprintf("%x", sha256.Sum256(d64))}
		c64Files[c64.Tape.Path] = d64
		var record uploadShellRecord
		if err := json.Unmarshal(c64Files[c64.MetadataPath], &record); err != nil {
			t.Fatal(err)
		}
		buildID, err := hex.DecodeString(record.BuildID)
		if err != nil || len(buildID) != 16 {
			t.Fatal("invalid fixture build identity", record.BuildID, err)
		}

		payload := programUpload(f.r.f.files, f.request)
		maps.Copy(payload, c64Files)
		maps.Copy(payload, programExport(t, "c64", c64, nil))
		payload["FES/MENUSEL.JSN"] = programExport(t, "menu", f.request, nil)["FES/CORESEL.JSN"]
		payload["FES/UI.JSON"] = programJSON(t, map[string]any{
			"format": 1, "scope": "nano-tenfoot-catalog-v1",
			"entries": []map[string]any{{"id": "c64", "program": 0, "title": "C64"}},
		})
		payload[programC64DiskSetPath] = programJSON(t, map[string]any{
			"game_id": "c64",
			"disks":   []map[string]string{{"name": "SIDE2.D64", "sha256": nextID}},
		})
		payload["FES/SIDE2.D64"] = nextDisk
		f.cpu.configuration.sharedShell = true
		port = &spectrumFirmwareCPU{CPUTransport: f.cpu, base: f.r.f.cpu.computerCPU,
			romClock: &clock, diskBatchCost: 5500 * time.Microsecond, wantTape: true}
		picker := c64PickerFixtureCPU{batchedSpectrumCPU{port}, pickerPort}
		l.boot.cpu = picker
		if scoped {
			scopeCPU = &c64CoexistingCPU{c64PickerFixtureCPU: picker, system: f.cpu, owner: f.cpu.state.Owner, mac: 2, active: true}
			l.boot.cpu = scopeCPU
			f.configureC64 = func(options *ProgramMenuOptions) {
				options.C64NetworkPackageID = c64.PackageID
				options.C64MACReceipt = func() (maccontrol.Receipt, bool) {
					return maccontrol.Receipt{Owner: scopeCPU.owner, Generation: scopeCPU.mac, Active: scopeCPU.active, Confirmed: scopeCPU.active}, true
				}
			}
		}
		activate := f.cpu.activate
		f.cpu.activate = func(role programRole) {
			activate(role)
			port.active = role == programRoleUploadShell
			port.ready, port.held, port.image, port.header = false, false, nil, [4]uint16{}
			if port.active {
				f.r.cpu.expectedROM = nil
				raw := f.cpu.configuration.memory[len(f.cpu.configuration.memory)-ProgramMetadataBytes:]
				id := raw[104:120]
				if !bytes.Equal(id, buildID) {
					t.Fatal("unexpected upload shell build in firmware publication")
				}
				port.media = tapeCPU{base: port.base, disk: true, allowLiveDisk: true}
				port.firmwareBytes = len(c64ROM)
				for i := range 8 {
					port.base.words[8+i] = binary.LittleEndian.Uint16(id[2*i:])
				}
				port.base.words[7] = uint16(c64Capabilities)
				port.base.gpo, port.base.gpi, port.base.fetches = 0, cg.FesComputerSignature, 0
			}
		}
		volume, err := fat.Mount(&ownedInstallDisk{disk, f.r.f.lease})
		if err != nil {
			t.Fatal(err)
		}
		writeLauncherFAT(t, volume, payload)
		f.r.f.store, err = packagestore.New(volume, f.r.f.lease)
		if err != nil {
			t.Fatal(err)
		}
		startup, err = ReadProgramStartup(ctx, f.r.f.store, false, true)
		if err != nil {
			t.Fatal(err)
		}
		diskSet, err = ReadProgramC64DiskSet(ctx, f.r.f.store, startup.Entries)
		if err != nil {
			t.Fatal(err)
		}
		f.request = startup.Menu
	})

	lan, control := newProgramNetwork(t)
	if err := f.s.AttachNetwork(control); err != nil {
		t.Fatal(err)
	}
	var authenticated, anonymous *targetclient.Client
	if scoped {
		ep := &c64ScopeEndpoint{Endpoint: agentapi.NewLinkEndpoint(lan.link, "02:00:00:00:00:02")}
		const bearer = "test-c64-scoped-observation-only"
		cfg := agentapi.Provisioning{TargetID: "ae31209a-c49c-4add-a5b1-287b7ea4caf0", Token: bearer, Port: 8181}
		if _, err := f.s.NewTargetService(ep, cfg, f.s.Status().Owner, testProgramBootID, nil); err != nil {
			t.Fatal(err)
		}
		base := &url.URL{Scheme: "http", Host: "native-fixture.invalid:8181"}
		wire := &http.Client{Transport: ep}
		authenticated = targetclient.NewClient(base, bearer, wire).WithKitLease(targetclient.NewKitLease(base, bearer, wire, "host", "test"))
		anonymous = targetclient.NewClient(base, "", wire)
		lan.stop = func() { scopeCPU.active = false; scopeCPU.mac++ }
	}
	lan.start = func() {
		if scoped {
			scopeCPU.mac += 2
			scopeCPU.active = true
			if f.s.Status().State == ProgramRunning {
				return
			}
		}
		if f.s.Status().State != ProgramReady || f.cpu.kind != programRoleMenu || f.menu.cpu.displayed == 0 {
			t.Error("regranted MAC before presenting the returned MENU frame")
		}
	}
	f.s.state.limits.Now = func() time.Time { return clock }
	b, err := NewProgramBackend(f.s, startup.Entries)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.AttachC64DiskSet(diskSet); err != nil {
		t.Fatal(err)
	}
	client := tenfoot.NewClient("http://native.invalid", &http.Client{Transport: b})
	app := tenfoot.NewApp(client, 1280, 720, 100)
	app.Start(ctx)
	defer app.Stop()
	wait := func(phase string, done func(tenfoot.Snapshot) bool) {
		t.Helper()
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) && ctx.Err() == nil {
			app.Tick(time.Now())
			if done(app.Snapshot()) {
				return
			}
			time.Sleep(time.Millisecond)
		}
		snapshot := app.Snapshot()
		t.Fatalf("%s: session=%+v launch=%+v native=%+v", phase, snapshot.Session, snapshot.Launch, f.s.Status())
	}
	wait("initial C64 catalogue", func(snapshot tenfoot.Snapshot) bool {
		return !snapshot.Loading && len(snapshot.Games) == 1 && snapshot.Games[0].ID == "c64"
	})
	source := &programKeyboardReports{}
	keyboard, err := NewProgramInputPlatform(&programInputBase{}, b, source)
	if err != nil {
		t.Fatal(err)
	}
	pressKey := func(command tenfoot.Command, report []byte) {
		t.Helper()
		source.reports = [][]byte{make([]byte, 8), report, make([]byte, 8)}
		frame, err := keyboard.Next(ctx)
		if err != nil || len(frame.Commands) != 1 || frame.Commands[0] != command {
			t.Fatal("unexpected keyboard command", command, frame, err)
		}
		app.Press(frame.Commands[0], frame.At)
	}
	presenter := ProgramMenuPlatform{service: f.s, inner: keyboard}
	presenter.EnableC64Picker(nil)
	presentPicker := func() {
		t.Helper()
		if err := presenter.presentC64Picker(ctx, app.Snapshot().TapePicker); err != nil {
			t.Fatal(err)
		}
	}
	owner := f.s.Status().Owner
	generation := uint64(1)
	pressKey(tenfoot.CmdSelect, []byte{0, 0, 0x28, 0, 0, 0, 0, 0})
	wait("C64 launch", func(snapshot tenfoot.Snapshot) bool { return snapshot.Session.State == "active" })
	status := f.s.Status()
	if scoped {
		before := scopeCPU.checks
		got, err := authenticated.KitLeaseStatus(ctx)
		if err != nil || got.State != "held" || !got.TargetReady || got.Owner != "native-tenfoot" || scopeCPU.checks != before+1 {
			t.Fatal("authenticated lease did not require a fresh C64 proof", got, err, scopeCPU.checks, before)
		}
		if _, err := anonymous.KitLeaseStatus(ctx); err == nil || scopeCPU.checks != before+1 {
			t.Fatal("unauthenticated lease queried C64 scope", err)
		}
		info, err := authenticated.Status(ctx)
		if err != nil || info.ObservedCore == nil || *info.ObservedCore != "fes.c64" || info.CorePackage == nil || info.CorePackage.PackageID != status.Identity.PackageID {
			t.Fatal("ordinary target status lost running C64", info, err)
		}
		if err := lan.link.SendRawFrame(make([]byte, 60)); err != nil {
			t.Fatal(err)
		}
	} else if err := lan.link.SendRawFrame(make([]byte, 60)); !errors.Is(err, network.ErrPaused) {
		t.Fatal("network accepted traffic while C64 held the control-only snapshot", err)
	}
	if status.State != ProgramRunning || status.Owner != owner ||
		status.Generation != generation || f.r.f.lease.r.Active {
		t.Fatal("unexpected C64 service status", status)
	}
	fetch, observed := f.s.FetchObservation()
	if !observed || !bytes.Equal(port.image, c64ROM) || !bytes.Equal(port.media.image, d64) ||
		fetch.ElapsedNS >= int64(5*time.Second) {
		t.Fatal("C64 did not commit its retained ROM and D64 within the launch deadline", fetch)
	}
	prior, err := client.Session(ctx)
	if err != nil || prior.MediaName != "C64.D64" {
		t.Fatal("launcher did not publish the mounted disk", prior)
	}
	if _, err := client.ReplaceRemovableMediaForSession(ctx, prior, nextID, "WRONG.D64"); err == nil {
		t.Fatal("unlisted media name accepted")
	}
	stale := prior
	stale.ID += "-stale"
	if _, err := client.ReplaceRemovableMediaForSession(ctx, stale, nextID, "SIDE2.D64"); err == nil || !bytes.Equal(port.media.image, d64) {
		t.Fatal("stale media binding changed the drive", err)
	}
	pressKey(tenfoot.CmdDetails, []byte{0x05, 0, 0x10, 0, 0, 0, 0, 0})
	wait("native C64 in-play selector", func(snapshot tenfoot.Snapshot) bool {
		picker := snapshot.TapePicker
		return picker.Open && picker.Index >= 0 && picker.Index < len(picker.Rows) &&
			picker.Rows[picker.Index].SetMediaID == nextID
	})
	presentPicker()
	pressKey(tenfoot.CmdSelect, []byte{0, 0, 0x28, 0, 0, 0, 0, 0})
	row := pickerPort.frame[4*pickerWidth : 14*pickerWidth]
	for i := range row {
		row[i] &= 0x7f
	}
	if !bytes.Contains(row, []byte("SIDE2.D64")) {
		t.Fatal("HDMI picker frame did not show alternate D64")
	}
	wait("native C64 swapped disk", func(snapshot tenfoot.Snapshot) bool {
		return !snapshot.TapePicker.Open && snapshot.Session.State == "active" && f.s.Status().DiskSHA256 == nextID
	})
	presentPicker()
	current, err := client.Session(ctx)
	if err != nil || current.MediaName != "SIDE2.D64" {
		t.Fatal("session did not publish replacement disk", current, err)
	}
	after := f.s.Status()
	if !bytes.Equal(port.media.image, nextDisk) || after.State != ProgramRunning ||
		after.Generation != status.Generation || after.DiskSHA256 != nextID ||
		after.Owner != status.Owner || f.r.f.lease.r.Active || current.ID != prior.ID ||
		current.CorePackage.Generation != prior.CorePackage.Generation || port.media.held {
		t.Fatal("hot swap changed the play or failed to commit the selected D64", after)
	}
	if failSwap {
		port.failLiveBatch = true
		if _, err := client.ReplaceRemovableMediaForSession(ctx, current, status.DiskSHA256, "C64.D64"); err == nil {
			t.Fatal("interrupted live transfer reported success")
		}
		if state := f.s.Status(); state.State != ProgramRebootRequired || state.Generation != generation ||
			state.Owner != owner || f.r.f.lease.r.Active || port.media.ready || port.media.commits != 2 {
			t.Fatal("interrupted swap released storage or made a partial disk visible", state)
		}
		return
	}
	pressKey(tenfoot.CmdDetails, []byte{0x05, 0, 0x10, 0, 0, 0, 0, 0})
	wait("native C64 original disk in selector", func(snapshot tenfoot.Snapshot) bool {
		picker := snapshot.TapePicker
		return picker.Open && picker.Index >= 0 && picker.Index < len(picker.Rows) &&
			picker.Rows[picker.Index].SetMediaID == status.DiskSHA256
	})
	presentPicker()
	pressKey(tenfoot.CmdSelect, []byte{0, 0, 0x28, 0, 0, 0, 0, 0})
	wait("native C64 original disk restored", func(snapshot tenfoot.Snapshot) bool {
		return !snapshot.TapePicker.Open && f.s.Status().DiskSHA256 == status.DiskSHA256
	})
	presentPicker()
	if !slices.Equal(pickerPort.commits, []bool{true, false, true, false}) {
		t.Fatal("picker did not publish and hide on both mounted disks", pickerPort.commits)
	}
	pickerPort.commits = nil
	if !bytes.Equal(port.media.image, d64) || f.s.Status().Generation != generation || f.r.f.lease.r.Active {
		t.Fatal("restoring the original disk changed ownership or committed different bytes")
	}

	pressKey(tenfoot.CmdStop, []byte{0x05, 0, 0x29, 0, 0, 0, 0, 0})
	wait("C64 return", func(snapshot tenfoot.Snapshot) bool {
		return snapshot.Session.State == "idle" && !snapshot.GPUParked && f.s.Status().State == ProgramReady
	})
	generation++
	grants := 1
	if scoped {
		grants = 2
	}
	if err := lan.link.SendRawFrame(make([]byte, 60)); err != nil || lan.stops != grants || lan.starts != grants {
		t.Fatal("return failed to restore a single fresh MAC grant", err, lan.stops, lan.starts)
	}
	if status := f.s.Status(); status.Owner != owner || status.Generation != generation ||
		status.State != ProgramReady || f.cpu.kind != programRoleMenu || f.r.f.lease.r.Active {
		t.Fatal("unexpected C64 service status", status)
	}
	pressKey(tenfoot.CmdQuit, []byte{0, 0, 0x14, 0, 0, 0, 0, 0})
	if err := b.ParkMenu(ctx); err != nil || f.s.Status().State != ProgramParked || f.s.Status().Generation != generation+1 {
		t.Fatal("C64 catalogue did not Park", err, f.s.Status())
	}
	if err := lan.link.Available(); !errors.Is(err, network.ErrPaused) || lan.stops != grants+1 || lan.starts != grants {
		t.Fatal("Park left networking active", err, lan.stops, lan.starts)
	}
}

// The ordinary fixture publishes and runs the synthetic C64. This extra model
// separates the active MAC observation from legacy bridge snapshots so a
// fallback to whole-system containment fails immediately.
type c64CoexistingCPU struct {
	c64PickerFixtureCPU
	system     *programMenuCPU
	owner, mac uint64
	active     bool
	checks     int
}

func (c *c64CoexistingCPU) Invoke(a [7]uint64) ([4]uint64, error) {
	if a[0] == uint64(g.FesEmbeddedC64ScopeCall) {
		if !c.active && c.system.kind == programRoleUploadShell && c.system.state.ResetMask == 13 &&
			a == [7]uint64{uint64(g.FesEmbeddedC64ScopeCall), 0, c.owner, uint64(g.FesEmbeddedC64ScopeCookie), c.system.state.Generation} {
			return [4]uint64{0, uint64(g.FesEmbeddedC64ScopeTag) << 32, c.system.state.Generation, c.mac}, nil
		}
		if !c.active || c.system.kind != programRoleUploadShell || c.system.state.ResetMask != 13 ||
			a != [7]uint64{uint64(g.FesEmbeddedC64ScopeCall), c.mac, c.owner, uint64(g.FesEmbeddedC64ScopeCookie), c.system.state.Generation} {
			return [4]uint64{uint64(g.FesEmbeddedC64ScopeRejected), uint64(g.FesEmbeddedC64ScopeTag) << 32}, nil
		}
		c.checks++
		return [4]uint64{0, uint64(g.FesEmbeddedC64ScopeTag) << 32, c.system.state.Generation, c.mac}, nil
	}
	if c.active && c.system.kind == programRoleUploadShell && a[0] == uint64(g.FesEmbeddedBridgeCall) {
		return [4]uint64{}, errors.New("C64 queried all-master bridge service while MAC active")
	}
	return c.c64PickerFixtureCPU.Invoke(a)
}

type c64ScopeEndpoint struct {
	agentapi.Endpoint
	handler http.Handler
}

func (e *c64ScopeEndpoint) NewHTTPServer(cfg network.ServerConfig) (*network.HTTPServer, error) {
	e.handler = cfg.Handler
	return e.Endpoint.NewHTTPServer(cfg)
}
func (e *c64ScopeEndpoint) RoundTrip(r *http.Request) (*http.Response, error) {
	recorder := httptest.NewRecorder()
	e.handler.ServeHTTP(recorder, r)
	return recorder.Result(), nil
}
