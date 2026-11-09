// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/crc32"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/tenfoot"
	"github.com/DeanoC/fes/platform/embedded/tamago/computer"
	cg "github.com/DeanoC/fes/platform/embedded/tamago/computer/generated"
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
)

// Only the external mailbox responses are modeled. Actual retention, package
// admission, secure C descriptor decoding, transfer, service and tape driver run.
type tapeCPU struct {
	CPUTransport
	base                                    *computerCPU
	files                                   *programTestFiles
	image                                   []byte
	header                                  [4]uint16
	chunk                                   [3]uint16
	used, starts, commits, runs, mediaOpens int
	mediaGeneration                         uint64
	held, ready                             bool
	fault                                   string
	now                                     *time.Time
	cancel                                  context.CancelFunc
}

func (c *tapeCPU) WritePhysical32(address uint64, word uint32) error {
	if err := c.CPUTransport.WritePhysical32(address, word); err != nil {
		return err
	}
	op, index, value := word>>24&127, word>>16&255, uint16(word)
	if op == cg.FesComputerOpcodeExecution {
		c.held = value == uint16(cg.FesComputerExecutionHoldReset)
		if !c.held {
			c.runs++
		}
		return nil
	}
	if op < cg.FesComputerOpcodeMediaInfo || op > cg.FesComputerOpcodeMediaEject {
		return nil
	}
	if !c.held {
		return errors.New("media operation outside held execution")
	}
	if c.mediaOpens == 0 || c.mediaGeneration != c.base.state.Generation {
		c.mediaOpens = c.files.opens
		c.mediaGeneration = c.base.state.Generation
	}
	if c.files.opens != c.mediaOpens {
		return errors.New("media accessed filesystem after containment")
	}
	switch op {
	case cg.FesComputerOpcodeMediaInfo:
		state := uint16(cg.FesComputerMediaStateEmpty)
		if c.ready {
			state = uint16(cg.FesComputerMediaStateReady)
		}
		values := [6]uint16{uint16(cg.FesComputerZx81TapeMinBytes), 0, uint16(cg.FesDe25ComputerZx81TapeMaxBytes), 0, uint16(cg.FesComputerMediaChunkMaxBytes), state}
		if index >= 6 {
			return errors.New("invalid media info")
		}
		c.base.gpi |= uint32(values[index])
	case cg.FesComputerOpcodeMediaBegin:
		if index >= 4 {
			return errors.New("invalid tape begin")
		}
		if index == 0 {
			c.starts++
			c.image = nil
			c.ready = false
		}
		c.header[index] = value
	case cg.FesComputerOpcodeMediaChunk:
		if index >= 3 {
			return errors.New("invalid tape chunk")
		}
		c.chunk[index] = value
		if index == 2 {
			if int(c.chunk[0])|int(c.chunk[1])<<16 != len(c.image) || value == 0 || uint32(value) > cg.FesComputerMediaChunkMaxBytes {
				return errors.New("tape chunk position/length differs")
			}
			c.used = 0
		}
	case cg.FesComputerOpcodeMediaData:
		if int(index)*2 != c.used || c.used >= int(c.chunk[2]) {
			return errors.New("tape data ordinal differs")
		}
		c.image = append(c.image, byte(value))
		c.used++
		if c.used < int(c.chunk[2]) {
			c.image = append(c.image, byte(value>>8))
			c.used++
		} else if value>>8 != 0 {
			return errors.New("odd tape byte not padded")
		}
		switch c.fault {
		case "write":
			return errors.New("lost tape data acknowledgement")
		case "owner":
			c.base.state.Generation++
		case "late":
			budget, _ := computer.TapeTransferBudget(len(c.image), time.Second)
			*c.now = c.now.Add(budget)
		case "cancel":
			c.cancel()
		}
	case cg.FesComputerOpcodeMediaCommit:
		c.commits++
		if index != 0 || len(c.image) != int(c.header[0])|int(c.header[1])<<16 || crc32.ChecksumIEEE(c.image) != uint32(c.header[2])|uint32(c.header[3])<<16 {
			return errors.New("tape commit size/CRC differs")
		}
		if c.fault == "commit" {
			c.base.gpi |= 1
			return nil
		}
		c.ready = true
		if c.fault == "held-change" {
			c.base.fetches++
		}
	}
	return nil
}

func addGameTape(t *testing.T, f *serviceFixture, request *packagestore.Request, image []byte) {
	t.Helper()
	sum := sha256.Sum256(image)
	request.Tape = packagestore.TapeRequest{Path: "GAMES/TEST.P", SHA256: hex.EncodeToString(sum[:]), Size: int64(len(image))}
	f.files.files[request.Tape.Path] = &fstest.MapFile{Data: image}
}

func TestProgramTapeLaunchRetainsExactMediaBeforeRunAndClearsReturnedStatus(t *testing.T) {
	for _, initial := range []bool{false, true} {
		t.Run(fmt.Sprint("platform=", initial), func(t *testing.T) { testProgramTapeLaunch(t, initial) })
	}
}

func testProgramTapeLaunch(t *testing.T, initial bool) {
	r := romProgramTestFor(t, NativeComputerProfile)
	f := r.f
	port := &tapeCPU{CPUTransport: f.loader.boot.cpu.(CPUTransport), base: f.cpu.computerCPU, files: f.files}
	f.loader.boot.cpu = port
	var platform *serviceInitialTapeCPU
	if initial {
		now := time.Unix(100, 0)
		port.now = &now
		f.s.state.limits.Now = func() time.Time { return now }
		platform = &serviceInitialTapeCPU{tapeCPU: port, hookFault: "slow-valid"}
		f.loader.boot.cpu = platform
	}
	for cycle, size := range []int{1025, 1, int(cg.FesDe25ComputerZx81TapeMaxBytes)} {
		request, _, _ := addROMProgram(t, r, cycle, 8192)
		image := bytes.Repeat([]byte{byte(7 + cycle)}, size)
		addGameTape(t, f, &request, image)
		want := bytes.Clone(image)
		// Mutating both card and exported copy after preflight must not alter
		// the retained tape consumed after configuration and SD containment.
		reserve := f.s.state.reserve
		f.s.state.reserve = func(data []byte) (*Region, error) {
			clear(image)
			delete(f.files.files, request.Tape.Path)
			clear(f.s.state.plan.state.selection.Tape())
			return reserve(data)
		}
		session, err := f.s.Launch(context.Background(), f.s.Status().Generation, request)
		f.s.state.reserve = reserve
		if err != nil || !bytes.Equal(port.image, want) || !port.ready || port.runs != cycle+1 || port.starts != cycle+1 || port.commits != cycle+1 {
			t.Fatal("tape/Run ownership", err, port.starts, port.commits, port.runs)
		}
		if initial {
			observation, ok := f.s.FetchObservation()
			if !ok || observation.TapeElapsedNS != int64(2*time.Second) || observation.ElapsedNS < int64(2*time.Second) || observation.ControlElapsedNS >= int64(time.Second) || observation.TapeBytes != uint32(size) {
				t.Fatal("separate control/media budgets lost", observation, ok)
			}
		}
		view := f.s.Status()
		if view.TapeSHA256 != request.Tape.SHA256 || view.TapeBytes != size || f.lease.r.Active {
			t.Fatal("running tape receipt lost", view)
		}
		if _, err = f.s.Return(context.Background(), session); err != nil {
			t.Fatal(err)
		}
		if view = f.s.Status(); view.TapeSHA256 != "" || view.TapeBytes != 0 || !f.lease.r.Active {
			t.Fatal("returned tape remained active", view)
		}
		port.mediaOpens = 0
	}
	if platform != nil && platform.calls != 3 {
		t.Fatal("platform initial tape was bypassed", platform.calls)
	}
}

func TestProgramTapeFailuresKeepExecutionHeldAndCannotReplay(t *testing.T) {
	for _, fault := range []string{"write", "commit", "owner", "late", "cancel", "held-change"} {
		t.Run(fault, func(t *testing.T) {
			r := romProgramTestFor(t, NativeComputerProfile)
			f := r.f
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			now := time.Unix(100, 0)
			f.s.state.limits.Now = func() time.Time { return now }
			port := &tapeCPU{CPUTransport: f.loader.boot.cpu.(CPUTransport), base: f.cpu.computerCPU, files: f.files, fault: fault, now: &now, cancel: cancel}
			f.loader.boot.cpu = port
			request, _, _ := addROMProgram(t, r, 0, 8192)
			addGameTape(t, f, &request, []byte{1, 2, 3})
			_, err := f.s.Launch(ctx, 0, request)
			if !errors.Is(err, ErrProgramRetired) || !errors.Is(err, ErrProgramExecution) || port.runs != 0 || f.s.Status().State != ProgramRebootRequired || f.lease.r.Active || f.s.Status().TapeSHA256 != "" {
				t.Fatal("failed tape released execution or storage", err, f.s.Status(), port.runs)
			}
			calls := f.cpu.calls
			writes := f.cpu.writes
			_, _ = f.s.Launch(context.Background(), f.s.Status().Generation, request)
			if f.cpu.calls != calls || f.cpu.writes != writes || port.starts != 1 {
				t.Fatal("failed media transaction replayed")
			}
		})
	}
}

func TestProgramTapeContentRejectionKeepsReadyGenerationAndStorage(t *testing.T) {
	for _, fault := range []string{"missing", "hash", "oversize", "unsupported-profile"} {
		t.Run(fault, func(t *testing.T) {
			profile := NativeComputerProfile
			if fault == "unsupported-profile" {
				profile = HPSFirstROMDiagnosticProfile
			}
			r := romProgramTestFor(t, profile)
			f := r.f
			request, _, _ := addROMProgram(t, r, 0, 8192)
			image := []byte{1, 2, 3}
			if fault == "oversize" {
				image = make([]byte, int(cg.FesDe25ComputerZx81TapeMaxBytes)+1)
			}
			addGameTape(t, f, &request, image)
			if fault == "missing" {
				delete(f.files.files, request.Tape.Path)
			}
			if fault == "hash" {
				request.Tape.SHA256 = strings.Repeat("a", 64)
			}
			_, err := f.s.Launch(context.Background(), 0, request)
			if err == nil || f.cpu.calls != 0 || !f.lease.r.Active || f.s.Status().Generation != 0 || f.s.Status().State != ProgramReady || f.arena.active != nil {
				t.Fatal("invalid tape reached hardware or consumed generation", err, f.s.Status())
			}
		})
	}
}

func TestProgramTapeSelectionsReachActualTenfootBackendByCatalogID(t *testing.T) {
	for _, initial := range []bool{false, true} {
		t.Run(fmt.Sprint("platform=", initial), func(t *testing.T) { testProgramTapeApp(t, initial) })
	}
}

func testProgramTapeApp(t *testing.T, initial bool) {
	r := romProgramTestFor(t, NativeComputerProfile)
	f := r.f
	request, _, _ := addROMProgram(t, r, 0, 8192)
	addGameTape(t, f, &request, []byte{3, 2, 1})
	choice := programChoice{Path: request.Path, PackageID: request.PackageID, MetadataPath: request.MetadataPath, MetadataSHA256: request.MetadataSHA256, Sources: []programROMSource{{ID: request.Sources[0].ID, Role: request.Sources[0].Role, Path: request.Sources[0].Path, SHA256: request.Sources[0].SHA256}}, ROMReceiptSHA256: request.ReceiptSHA256, Tape: &request.Tape}
	raw := programJSON(t, programChoices{Format: 1, Scope: "nano-program-selection-v1", Programs: []programChoice{choice}})
	files := fstest.MapFS{"SELECT.JSN": &fstest.MapFile{Data: raw}, "UI.JSN": &fstest.MapFile{Data: []byte("{\"entries\":[{\"id\":\"game\",\"program\":0,\"title\":\"Tape game\"}],\"format\":1,\"scope\":\"nano-tenfoot-catalog-v1\"}\n")}}
	choices, err := ReadProgramSelections(context.Background(), files, "SELECT.JSN")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := ReadProgramCatalog(context.Background(), files, "UI.JSN", choices)
	if err != nil {
		t.Fatal(err)
	}
	port := &tapeCPU{CPUTransport: f.loader.boot.cpu.(CPUTransport), base: f.cpu.computerCPU, files: f.files}
	f.loader.boot.cpu = port
	var secure *serviceInitialTapeCPU
	if initial {
		secure = &serviceInitialTapeCPU{tapeCPU: port}
		f.loader.boot.cpu = secure
	}
	backend, err := NewProgramBackend(f.s, entries)
	if err != nil {
		t.Fatal(err)
	}
	client := hostclient.NewClient("http://native.invalid", &http.Client{Transport: backend})
	play, err := client.Launch(context.Background(), "game")
	if err != nil || play.State != "active" || !bytes.Equal(port.image, []byte{3, 2, 1}) {
		t.Fatal(play, err)
	}
	if _, err = client.StopExpectedStamped(context.Background(), play, hostclient.ClientStamp{}); err != nil {
		t.Fatal(err)
	}
	device, err := gfx.NewSoftware(640, 480)
	if err != nil {
		t.Fatal(err)
	}
	defer device.Close()
	app := tenfoot.NewApp(tenfoot.NewClient("http://native.invalid", &http.Client{Transport: backend, Timeout: 10 * time.Second}), 640, 480, 100)
	if err = app.SetPreferenceStore(nativePreferences{}); err != nil {
		t.Fatal(err)
	}
	platform := &nativeAppPlatform{app: app, service: f.s, device: device, fixture: f}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err = tenfoot.RunPlatform(ctx, app, device, platform); err != nil {
		t.Fatal(err)
	}
	if platform.cycles != 3 || !platform.pixels || port.starts != 4 || port.commits != 4 || port.runs != 4 || f.s.Status().TapeSHA256 != "" || !f.lease.r.Active {
		t.Fatal("App tape launch/return did not complete", platform.cycles, port.starts, port.runs, f.s.Status())
	}
	if secure != nil && secure.calls != 4 {
		t.Fatal("App bypassed initial tape platform", secure.calls)
	}
}

func TestMenuTapeSelectionRejectsBeforeAnyFirmwareOrContainment(t *testing.T) {
	r, _ := mixedMenuTest(t)
	request, _, _ := addMenuSelection(t, r, 0)
	addGameTape(t, r.f, &request, []byte{1})
	selection, err := r.f.store.Retain(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	calls := r.f.cpu.calls
	if plan, err := r.f.loader.Preflight(selection); err == nil || plan != nil || r.f.cpu.calls != calls || !r.f.lease.r.Active {
		t.Fatal("menu accepted game tape", err)
	}
}

func TestProgramTapeFailureReachesTenfootWithoutReleasingExecution(t *testing.T) {
	for _, initial := range []bool{false, true} {
		t.Run(fmt.Sprint("platform=", initial), func(t *testing.T) { testProgramTapeAppFailure(t, initial) })
	}
}

func testProgramTapeAppFailure(t *testing.T, initial bool) {
	r := romProgramTestFor(t, NativeComputerProfile)
	f := r.f
	request, _, _ := addROMProgram(t, r, 0, 8192)
	addGameTape(t, f, &request, []byte{1, 2, 3})
	port := &tapeCPU{CPUTransport: f.loader.boot.cpu.(CPUTransport), base: f.cpu.computerCPU, files: f.files, fault: "commit"}
	f.loader.boot.cpu = port
	var secure *serviceInitialTapeCPU
	if initial {
		port.fault = ""
		secure = &serviceInitialTapeCPU{tapeCPU: port, hookFault: "error"}
		f.loader.boot.cpu = secure
	}
	backend, err := NewProgramBackend(f.s, []ProgramEntry{{ID: "game", Title: "Tape game", Request: request}})
	if err != nil {
		t.Fatal(err)
	}
	device, err := gfx.NewSoftware(640, 480)
	if err != nil {
		t.Fatal(err)
	}
	defer device.Close()
	app := tenfoot.NewApp(tenfoot.NewClient("http://native.invalid", &http.Client{Transport: backend, Timeout: 10 * time.Second}), 640, 480, 100)
	if err = app.SetPreferenceStore(nativePreferences{}); err != nil {
		t.Fatal(err)
	}
	platform := &nativeAppPlatform{app: app, service: f.s, device: device, fixture: f, failure: "launch"}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err = tenfoot.RunPlatform(ctx, app, device, platform); err != nil {
		t.Fatal(err)
	}
	if !platform.sawFailure || port.runs != 0 || port.commits != 1 || f.lease.r.Active || f.s.Status().State != ProgramRebootRequired {
		t.Fatal("App hid a failed tape launch or retried it", platform.sawFailure, port.runs, port.commits, f.s.Status())
	}
	if secure != nil && secure.calls != 1 {
		t.Fatal("App retried or bypassed failed initial hook", secure.calls)
	}
}

func TestTapeSelectionRejectsPartialDuplicateAndUnknownDataBeforeLaunch(t *testing.T) {
	request := packagestore.TapeRequest{Path: "FES/GAME.P", SHA256: strings.Repeat("a", 64), Size: 3}
	choice := programChoice{Path: "FES/CORE.FCP", PackageID: strings.Repeat("b", 64), MetadataPath: "FES/CORE.JSN", MetadataSHA256: strings.Repeat("c", 64), Tape: &request}
	raw := programJSON(t, programChoices{Format: 1, Scope: "nano-program-selection-v1", Programs: []programChoice{choice}})
	for _, bad := range [][]byte{
		bytes.Replace(raw, []byte(`"size":3`), []byte(`"size":true`), 1),
		bytes.Replace(raw, []byte(`"size":3`), []byte(`"size":0`), 1),
		bytes.Replace(raw, []byte(`"size":3`), []byte(`"size":16385`), 1),
		bytes.Replace(raw, []byte(`"size":3`), []byte(`"size":3,"size":3`), 1),
		bytes.Replace(raw, []byte(`"size":3`), []byte(`"size":3,"unit":1`), 1),
		bytes.Replace(raw, []byte(`"path":"FES/GAME.P"`), []byte(`"path":"../GAME.P"`), 1),
		bytes.Replace(raw, []byte(`"size":3`), []byte(`"unused":3`), 1),
	} {
		files := fstest.MapFS{"SELECT.JSN": &fstest.MapFile{Data: bad}}
		if requests, err := ReadProgramSelections(context.Background(), files, "SELECT.JSN"); err == nil || requests != nil {
			t.Fatal("bad tape selection accepted", string(bad))
		}
	}
}
