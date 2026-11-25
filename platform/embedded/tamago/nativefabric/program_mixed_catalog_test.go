// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/ui/tenfoot"
	"github.com/DeanoC/fes/platform/embedded/tamago/block"
	"github.com/DeanoC/fes/platform/embedded/tamago/computer"
	cg "github.com/DeanoC/fes/platform/embedded/tamago/computer/generated"
	"github.com/DeanoC/fes/platform/embedded/tamago/fat"
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
)

// Use the secure ZX81 tape supplier only for the legacy computer role.
type mixedCatalogueCPU struct {
	batchedSpectrumCPU
	zx81 *serviceInitialTapeCPU
}

func (c mixedCatalogueCPU) WritePhysical32(address uint64, value uint32) error {
	if c.active {
		return c.batchedSpectrumCPU.WritePhysical32(address, value)
	}
	return c.zx81.WritePhysical32(address, value)
}

func (c mixedCatalogueCPU) LoadInitialTape(ctx context.Context, scope BridgeSnapshot, request computer.InitialTapeRequest) (computer.InitialTapeReceipt, error) {
	if c.active {
		return c.batchedSpectrumCPU.LoadInitialTape(ctx, scope, request)
	}
	return c.zx81.LoadInitialTape(ctx, scope, request)
}

func TestSharedCoreShellMixedCatalogueFATLifecycle(t *testing.T) {
	ctx := t.Context()
	image := filepath.Join(t.TempDir(), "mixed.img")
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

	var startup *ProgramStartup
	var port *spectrumFirmwareCPU
	var zx81Port *serviceInitialTapeCPU
	var zx81ROM, spectrumROM, c64ROM []byte
	clock := time.Unix(1, 0)
	zx81Tape := []byte{1, 2, 3}
	tapInput := []byte{2, 0, 0xff, 0xff}
	spectrumTape, err := protocol.ConvertSpectrumTape(tapInput, int(protocol.SpectrumTapeMaxBytes))
	if err != nil {
		t.Fatal(err)
	}
	d64 := bytes.Repeat([]byte{0x42}, int(cg.FesComputerC64DiskBytes))
	f := newProgramMenuFixtureContext(t, ctx, "", func(f *programMenuFixture) {
		l, err := f.r.f.loader.WithSharedCoreShell()
		if err != nil {
			t.Fatal(err)
		}
		f.r.f.loader = l
		zx81, _, _ := addROMProgram(t, f.r, 0, 8192)
		addGameTape(t, f.r.f, &zx81, zx81Tape)
		zxFiles := programUpload(f.r.f.files, zx81)
		zx81ROM = zxFiles[zx81.Sources[0].Path]

		spectrum, spectrumFiles, _, policy := packageSpectrumFixture(t, l, true)
		spectrumROM = spectrumFiles[spectrum.Sources[0].Path]
		spectrum.Tape = packagestore.TapeRequest{Path: "FES/SPEC.TAP", Format: packagestore.SpectrumTapeFormat,
			Size: int64(len(tapInput)), SHA256: fmt.Sprintf("%x", sha256.Sum256(tapInput))}
		spectrumFiles[spectrum.Tape.Path] = tapInput

		c64, c64Files := c64PackageFixture(t, l, c64Profile, nil)
		c64ROM = c64Files[c64.Sources[0].Path]
		c64.Tape = packagestore.TapeRequest{Path: "FES/C64.D64", Format: packagestore.C64DiskFormat,
			Size: int64(len(d64)), SHA256: fmt.Sprintf("%x", sha256.Sum256(d64))}
		c64Files[c64.Tape.Path] = d64
		bad := c64
		bad.Tape.SHA256 = fmt.Sprintf("%x", sha256.Sum256([]byte("wrong D64")))

		payload := programUpload(f.r.f.files, f.request)
		payload["FES/MENUSEL.JSN"] = programExport(t, "menu", f.request, nil)["FES/CORESEL.JSN"]
		choices := programChoices{Format: 1, Scope: "nano-program-selection-v1"}
		for _, entry := range []struct {
			id      string
			request packagestore.Request
			files   map[string][]byte
		}{
			{"zx81", zx81, zxFiles},
			{"spectrum48", spectrum, spectrumFiles},
			{"c64", c64, c64Files},
			{"bad-c64", bad, nil},
		} {
			one := programExport(t, entry.id, entry.request, nil)
			var selection programChoices
			if err := json.Unmarshal(one["FES/CORESEL.JSN"], &selection); err != nil {
				t.Fatal(err)
			}
			choices.Programs = append(choices.Programs, selection.Programs[0])
			maps.Copy(payload, entry.files)
		}
		payload["FES/CORESEL.JSN"] = programJSON(t, choices)
		payload["FES/UI.JSON"] = programJSON(t, map[string]any{
			"format": 1, "scope": "nano-tenfoot-catalog-v1",
			"entries": []map[string]any{
				{"id": "zx81", "program": 0, "title": "ZX81"},
				{"id": "spectrum48", "program": 1, "title": "Spectrum 48K"},
				{"id": "c64", "program": 2, "title": "C64"},
				{"id": "bad-c64", "program": 3, "title": "Invalid C64 media"},
			},
		})

		builds := map[string]bool{}
		for i, raw := range [][]byte{spectrumFiles[spectrum.MetadataPath], c64Files[c64.MetadataPath]} {
			var record uploadShellRecord
			if err := json.Unmarshal(raw, &record); err != nil {
				t.Fatal(err)
			}
			builds[record.BuildID] = i == 1
		}
		if len(builds) != 2 {
			t.Fatal("mixed catalogue fixtures must have distinct upload shell identities")
		}
		f.cpu.configuration.sharedShell = true
		f.cpu.configuration.spectrumPolicy = filepath.Join(t.TempDir(), "policy.bin")
		if err := os.WriteFile(f.cpu.configuration.spectrumPolicy, policy, 0600); err != nil {
			t.Fatal(err)
		}
		port = &spectrumFirmwareCPU{CPUTransport: f.cpu, base: f.r.f.cpu.computerCPU,
			romClock: &clock, diskBatchCost: 5500 * time.Microsecond, wantTape: true}
		zx81Port = &serviceInitialTapeCPU{tapeCPU: &tapeCPU{CPUTransport: f.cpu, base: port.base}}
		l.boot.cpu = mixedCatalogueCPU{batchedSpectrumCPU{port}, zx81Port}
		activate := f.cpu.activate
		f.cpu.activate = func(role programRole) {
			activate(role)
			port.active = role == programRoleUploadShell
			port.ready, port.held, port.image, port.header = false, false, nil, [4]uint16{}
			if role == programRoleComputer {
				f.r.cpu.expectedROM = zx81ROM
				port.base.words[7] = uint16(l.computer.Capabilities)
			}
			if port.active {
				f.r.cpu.expectedROM = nil
				raw := f.cpu.configuration.memory[len(f.cpu.configuration.memory)-ProgramMetadataBytes:]
				id := raw[104:120]
				c64, known := builds[hex.EncodeToString(id[:])]
				if !known {
					t.Fatal("unexpected upload shell build in firmware publication")
				}
				port.media = tapeCPU{base: port.base, spectrum: !c64, disk: c64}
				capabilities := spectrum48Capabilities
				port.firmwareBytes = len(spectrumROM)
				if c64 {
					capabilities = c64Capabilities
					port.firmwareBytes = len(c64ROM)
				}
				for i := range 8 {
					port.base.words[8+i] = binary.LittleEndian.Uint16(id[2*i:])
				}
				port.base.words[7] = uint16(capabilities)
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
		f.request = startup.Menu
	})

	f.s.state.limits.Now = func() time.Time { return clock }
	b, err := NewProgramBackend(f.s, startup.Entries)
	if err != nil {
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
	wait("initial mixed catalogue", func(snapshot tenfoot.Snapshot) bool {
		return !snapshot.Loading && len(snapshot.Games) == 4
	})
	ids := make([]string, 0, len(startup.Entries))
	for _, game := range app.Snapshot().Games {
		ids = append(ids, game.ID)
	}
	if !slices.Equal(ids, []string{"zx81", "spectrum48", "c64", "bad-c64"}) {
		t.Fatal("Tenfoot changed the exported catalogue order", ids)
	}
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
	owner := f.s.Status().Owner
	var generation uint64
	for _, id := range []string{"zx81", "spectrum48", "bad-c64", "c64", "c64"} {
		target := -1
		for index, game := range app.Snapshot().Games {
			if game.ID == id {
				target = index
			}
		}
		if target < 0 {
			t.Fatal("title missing from Tenfoot", id)
		}
		for focus := app.Snapshot().Grid.Focus; focus != target; focus = app.Snapshot().Grid.Focus {
			next := focus + 1
			if focus < target {
				pressKey(tenfoot.CmdRight, []byte{0, 0, 0x4f, 0, 0, 0, 0, 0})
			} else {
				next = focus - 1
				pressKey(tenfoot.CmdLeft, []byte{0, 0, 0x50, 0, 0, 0, 0, 0})
			}
			wait(id+" selection", func(snapshot tenfoot.Snapshot) bool { return snapshot.Grid.Focus == next })
		}
		pressKey(tenfoot.CmdSelect, []byte{0, 0, 0x28, 0, 0, 0, 0, 0})
		if id == "bad-c64" {
			wait("rejected D64", func(snapshot tenfoot.Snapshot) bool {
				return snapshot.Launch.Phase == "host" && snapshot.Launch.ErrorMessage != ""
			})
			if status := f.s.Status(); status.State != ProgramReady ||
				status.Generation != generation || f.cpu.kind != programRoleMenu {
				t.Fatal("bad retained disk was admitted", status)
			}
			continue
		}
		wait(id+" launch", func(snapshot tenfoot.Snapshot) bool { return snapshot.Session.State == "active" })
		generation++
		status := f.s.Status()
		if status.State != ProgramRunning || status.Owner != owner ||
			status.Generation != generation || f.r.f.lease.r.Active {
			t.Fatal(id, status)
		}
		switch id {
		case "zx81":
			if zx81Port.calls != 1 || !bytes.Equal(zx81Port.image, zx81Tape) {
				t.Fatal("ZX81 did not use its retained initial tape")
			}
		case "spectrum48":
			if !bytes.Equal(port.image, spectrumROM) || !bytes.Equal(port.media.image, spectrumTape) {
				t.Fatal("Spectrum did not use its retained ROM and tape")
			}
		case "c64":
			fetch, observed := f.s.FetchObservation()
			if !observed || !bytes.Equal(port.image, c64ROM) || !bytes.Equal(port.media.image, d64) ||
				fetch.ElapsedNS >= int64(5*time.Second) {
				t.Fatal("C64 did not commit its retained ROM and D64 within the launch deadline", fetch)
			}
		}
		pressKey(tenfoot.CmdStop, []byte{0x05, 0, 0x29, 0, 0, 0, 0, 0})
		wait(id+" return", func(snapshot tenfoot.Snapshot) bool {
			return snapshot.Session.State == "idle" && !snapshot.GPUParked && f.s.Status().State == ProgramReady
		})
		generation++
		if status := f.s.Status(); status.Owner != owner || status.Generation != generation ||
			status.State != ProgramReady || f.cpu.kind != programRoleMenu || f.r.f.lease.r.Active {
			t.Fatal(id, status)
		}
	}
	pressKey(tenfoot.CmdQuit, []byte{0, 0, 0x14, 0, 0, 0, 0, 0})
	if err := b.ParkMenu(ctx); err != nil || f.s.Status().State != ProgramParked || f.s.Status().Generation != generation+1 {
		t.Fatal("mixed catalogue did not Park", err, f.s.Status())
	}
}
