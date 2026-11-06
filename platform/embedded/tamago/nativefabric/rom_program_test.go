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
	"hash/crc32"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/hostclient"
	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
)

// The real Go store/loader/arena/transfer/computer/service run against modeled
// secure control. The actual C descriptor decoder checks every published v2
// header. Actual FIFO/provider RTL execution remains a separate model suite.
type romProgramCPU struct {
	*serviceLeaseCPU
	t                         *testing.T
	decoder                   string
	memory                    []byte
	expectedROM               []byte
	gateFailure               bool
	preparedHeaders, romOpens int
	chunkBytes                int
}

func (c *romProgramCPU) Invoke(a [7]uint64) ([4]uint64, error) {
	if a[0] == uint64(g.FesEmbeddedBridgeCall) {
		if a[1] == uint64(g.FesEmbeddedBridgePrepareConfiguration) || a[1] == uint64(g.FesEmbeddedBridgeRearmConfiguration) {
			header := c.memory[len(c.memory)-ProgramMetadataBytes:][:ProgramHeaderBytes+programROMHeaderBytes]
			path := filepath.Join(c.t.TempDir(), "published.bin")
			if err := os.WriteFile(path, header, 0600); err != nil {
				c.t.Fatal(err)
			}
			out, err := exec.Command(c.decoder, path, "owned-rom", fmt.Sprint(c.state.Generation), hex.EncodeToString(c.digest[:]), fmt.Sprint(c.chunkBytes)).CombinedOutput()
			offset := (len(c.wire.source) + 63) &^ 63
			if err != nil || !strings.HasPrefix(string(out), fmt.Sprintf("%d %d 1 ", len(c.wire.source), (len(c.wire.source)-1)/c.chunkBytes+1)) ||
				!strings.HasSuffix(string(out), fmt.Sprintf(" %d %d %08x\n", offset, len(c.expectedROM), crc32.ChecksumIEEE(c.expectedROM))) {
				c.t.Fatalf("real C decoder rejected actual arena publication: %v %s", err, out)
			}
			if binary.LittleEndian.Uint64(header[24:]) != c.state.Owner || binary.LittleEndian.Uint64(header[32:]) != c.state.Generation ||
				!bytes.Equal(c.memory[offset:offset+len(c.expectedROM)], c.expectedROM) {
				c.t.Fatal("publication lost ownership or retained ROM")
			}
			c.preparedHeaders++
		}
		if a[1] == uint64(g.FesEmbeddedBridgeControl) {
			offset := (len(c.wire.source) + 63) &^ 63
			if c.gateFailure || !bytes.Equal(c.memory[offset:offset+len(c.expectedROM)], c.expectedROM) {
				return [4]uint64{2}, nil
			}
			c.romOpens++
		}
	}
	return c.serviceLeaseCPU.Invoke(a)
}

type romProgramFixture struct {
	f                                 *serviceFixture
	cpu                               *romProgramCPU
	control, computer, source, romRaw []byte
}

func romProgramTest(t *testing.T) *romProgramFixture {
	return romProgramTestFor(t, HPSFirstROMDiagnosticProfile)
}

func romProgramTestFor(t *testing.T, profile string) *romProgramFixture {
	t.Helper()
	old, cpu, lease, _, _, control, configuration, computer, _ := storedComputer(t)
	context := old.context
	context.ProgrammingProfile = profile
	if profile == NativeComputerProfile {
		context.CoreID, context.ABI = "fes.zx81", "fes.computer"
	}
	context.ControlABI = 0x10203041
	var ctrl hpsControlMap
	var comp computerMap
	if err := json.Unmarshal(control, &ctrl); err != nil {
		t.Fatal(err)
	}
	ctrl.FabricStop = &hpsFabricStopMap{"sealed-ddr-master-stop-v1", 0x20010100, 128, "held-reset-from-configuration"}
	control = programJSON(t, ctrl)
	if err := json.Unmarshal(computer, &comp); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(control)
	comp.ControlMapSHA256 = hex.EncodeToString(sum[:])
	computer = programJSON(t, comp)
	computerSHA := sha256.Sum256(computer)
	rom := romControlMap{Format: 1, Scope: "sealed-cold-held-rom-v1", PackageID: context.PackageID, BuildID: context.BuildID,
		ControlMapSHA256: comp.ControlMapSHA256, ComputerMapSHA256: hex.EncodeToString(computerSHA[:]), HandoffSHA256: ctrl.HandoffSHA256,
		CPUBase: 0x20010500, Span: 128, Bytes: 8192, ClockHz: 100000000, Transport: "cpu-csr-v1", Role: "firmware",
		HeldConduit: "fes_computer.rom/fes_rom.rom", SourceSHA256: map[string]string{}}
	for _, n := range []string{"fes_de25_rom.sv", "fes_de25_rom_hw.tcl", "fes_embedded_rom.sv", "fes_embedded_rom_csr.sv", "generated/fes_embedded_rom.vh"} {
		rom.SourceSHA256[n] = strings.Repeat("1", 64)
	}
	raw, romRaw := programJSON(t, context), programJSON(t, rom)
	cpu.digest = sha256.Sum256(raw)
	port := &romProgramCPU{serviceLeaseCPU: &serviceLeaseCPU{cpu, lease}, t: t, decoder: buildProgramDecoder(t), memory: make([]byte, ProgramMetadataBytes+(16<<10))}
	var config coldConfigurationMap
	if err := json.Unmarshal(configuration, &config); err != nil {
		t.Fatal(err)
	}
	port.chunkBytes = config.ChunkBytes
	constructor := NewROMHPSContextVerifier
	if profile == NativeComputerProfile {
		constructor = NewComputerHPSContextVerifier
	}
	v, err := constructor(port, raw, control, computer, romRaw)
	if err != nil {
		t.Fatal(err)
	}
	p := programPlatform{Format: 1, Scope: "nano-contained-program-platform-v1", Platform: context.Platform, Device: context.Device,
		ProgrammingProfile: context.ProgrammingProfile, ABI: context.ABI, Major: context.Major, Minor: context.Minor, ControlABI: context.ControlABI,
		HPSIOHash: context.HPSIOHash, PFGIdentity: context.PFGIdentity, HPSParametersSHA256: strings.Repeat("1", 64), HPSHandoffSHA256: strings.Repeat("2", 64), MasterInventorySHA256: strings.Repeat("3", 64)}
	compiled := programJSON(t, programPlatformSource{1, "nano-program-platform-source-v1", hex.EncodeToString(v.digest[:]), p})
	l, err := NewProgramLoader(v, compiled, control, configuration, computer)
	if err != nil {
		t.Fatal(err)
	}
	files := &programTestFiles{files: fstest.MapFS{}, lease: lease}
	store, err := packagestore.New(files, lease)
	if err != nil {
		t.Fatal(err)
	}
	f := &serviceFixture{loader: l, cpu: cpu, lease: lease, files: files, store: store}
	arena, err := NewProgramArena(0x94800000, port.memory, func() error { return nil })
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
		for i := range 8 {
			word := uint16(data[0]) * 0x11
			cpu.words[8+i] = word | word<<8
		}
		if f.fault == "stage" {
			stage := r.StageROM
			r.StageROM = func(b []byte) error {
				if err := stage(b); err != nil {
					return err
				}
				return errors.New("uncertain ROM stage")
			}
		}
		if f.fault == "missing-stage" {
			r.StageROM = nil
		}
		if f.fault == "publication" {
			publish := r.PublishConfiguration
			r.PublishConfiguration = func(b []byte) error {
				if err := publish(b); err != nil {
					return err
				}
				return errors.New("uncertain descriptor publication")
			}
		}
		if f.fault == "write" {
			cpu.wire.fault = "write"
		}
		if f.fault == "rom-after-config" {
			release := r.Release
			r.Release = func() error {
				if err := release(); err != nil {
					return err
				}
				offset := (len(data) + 63) &^ 63
				port.memory[offset] ^= 1
				return nil
			}
		}
		return r, nil
	}
	f.s, err = NewProgramService(l, store, reserve, computerLimits())
	if err != nil {
		t.Fatal(err)
	}
	return &romProgramFixture{f, port, control, computer, raw, romRaw}
}

func addROMProgram(t *testing.T, r *romProgramFixture, index, size int) (packagestore.Request, selectedROMProgramRecord, []byte) {
	return addROMProgramChanged(t, r, index, size, nil)
}

func addROMProgramChanged(t *testing.T, r *romProgramFixture, index, size int, change func(string) string) (packagestore.Request, selectedROMProgramRecord, []byte) {
	t.Helper()
	l := r.f.loader
	p := l.platform
	data := bytes.Repeat([]byte{byte(index + 1)}, []int{128, 192, 64}[index%3])
	sum := sha256.Sum256(data)
	build := strings.Repeat(fmt.Sprint(index+1), 32)
	mapping := []byte(fmt.Sprintf("{\"format\":1,\"device\":\"A5EB013BB23BE4SCS\",\"encoding\":\"cpu-csr-v1\",\"base_sha256\":\"%x\",\"source_size\":%d}\n", sum, size))
	mapSum := sha256.Sum256(mapping)
	manifest := fmt.Sprintf(`format = 3
[[interfaces]]
id = "fes.embedded.rom"
major = 1
minor = 0
required = true
[core]
id = "fes.selected-rom-fixture-%d"
name = "Retained ROM fixture"
description = "Synthetic software fixture; never deploy."
version = "0.1.0"
[target]
platform = %q
device = %q
programming_profile = %q
[payload]
file = "core.rbf"
size = %d
sha256 = "%x"
[abi]
id = %q
major = %d
minor = %d
[build]
id = %q
repository = "https://example.invalid/rom-program"
revision = "1111111111111111111111111111111111111111"
recipe_sha256 = "2222222222222222222222222222222222222222222222222222222222222222"
toolchain = "software fixture; no FPGA build"
[rom]
id = "firmware"
role = "firmware"
source_size = %d
file = "rom-map.json"
size = %d
sha256 = "%x"
`, index, p.Platform, p.Device, p.ProgrammingProfile, len(data), sum, p.ABI, p.Major, p.Minor, build, size, len(mapping), mapSum)
	if p.ProgrammingProfile == NativeComputerProfile {
		manifest = strings.Replace(manifest, fmt.Sprintf("fes.selected-rom-fixture-%d", index), "fes.zx81", 1)
		manifest += `
[[interfaces]]
id = "fes.video.fixed-720p60"
major = 1
minor = 0
required = true
[[interfaces]]
id = "fes.keyboard.hid"
major = 1
minor = 0
required = true
[[interfaces]]
id = "fes.media.zx81-tape"
major = 1
minor = 0
required = true
`
	}
	if change != nil {
		manifest = change(manifest)
	}
	dir := t.TempDir()
	for name, b := range map[string][]byte{"manifest.toml": []byte(manifest), "core.rbf": data, "rom-map.json": mapping} {
		if err := os.WriteFile(filepath.Join(dir, name), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	body, err := corepackage.CanonicalArchive(dir)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := corepackage.RetainArchive(context.Background(), int64(len(body)), bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	i := archive.Inspection()
	image := bytes.Repeat([]byte{byte(index + 0x51)}, size)
	linked, err := archive.LinkROMs(context.Background(), []corepackage.ROMSource{{ID: "firmware", Role: "firmware", Data: image}})
	if err != nil {
		t.Fatal(err)
	}
	ctrl, comp := l.control, l.computer
	ctrl.PackageID, ctrl.BuildID, ctrl.HandoffSHA256 = i.PackageID, build, strings.Repeat(fmt.Sprint(index+4), 64)
	control := programJSON(t, ctrl)
	csha := sha256.Sum256(control)
	comp.PackageID, comp.BuildID, comp.HandoffSHA256, comp.ControlMapSHA256 = i.PackageID, build, ctrl.HandoffSHA256, hex.EncodeToString(csha[:])
	computer := programJSON(t, comp)
	psha := sha256.Sum256(computer)
	rom := *l.boot.rom
	rom.PackageID, rom.BuildID, rom.ControlMapSHA256, rom.ComputerMapSHA256, rom.HandoffSHA256, rom.Bytes = i.PackageID, build, comp.ControlMapSHA256, hex.EncodeToString(psha[:]), ctrl.HandoffSHA256, size
	record := selectedROMProgramRecord{selectedProgramRecord{1, "nano-selected-rom-program-v1", i.PackageID, build, p, ctrl, comp}, rom, linked.ReceiptSHA256()}
	meta := programJSON(t, record)
	msha := sha256.Sum256(meta)
	rsha := sha256.Sum256(image)
	request := packagestore.Request{Path: fmt.Sprintf("FES/ROM%d.FCP", index), PackageID: i.PackageID, MetadataPath: fmt.Sprintf("FES/ROM%d.JSN", index), MetadataSHA256: hex.EncodeToString(msha[:]), ReceiptSHA256: linked.ReceiptSHA256(), Sources: []packagestore.Source{{ID: "firmware", Role: "firmware", Path: fmt.Sprintf("FES/ROM%d.BIN", index), SHA256: hex.EncodeToString(rsha[:])}}}
	r.f.files.files[request.Path] = &fstest.MapFile{Data: body}
	r.f.files.files[request.MetadataPath] = &fstest.MapFile{Data: meta}
	r.f.files.files[request.Sources[0].Path] = &fstest.MapFile{Data: image}
	r.cpu.expectedROM = bytes.Clone(image)
	return request, record, data
}

func TestROMProgramServicePublishesRetainedImagesAndReturns(t *testing.T) {
	for _, profile := range []string{HPSFirstROMDiagnosticProfile, NativeComputerProfile} {
		t.Run(profile, func(t *testing.T) { testROMProgramServicePublishesRetainedImagesAndReturns(t, profile) })
	}
}

func testROMProgramServicePublishesRetainedImagesAndReturns(t *testing.T, profile string) {
	r := romProgramTestFor(t, profile)
	var old ProgramSession
	for cycle, size := range []int{8192, 4096, 8192} {
		req, _, data := addROMProgram(t, r, cycle, size)
		session, err := r.f.s.Launch(context.Background(), r.f.s.Status().Generation, req)
		if err != nil {
			t.Fatal(cycle, err)
		}
		status := r.f.s.Status()
		if status.State != ProgramRunning || status.ROMReceiptSHA256 != req.ReceiptSHA256 || status.Identity.ActivationReceiptSHA256 != req.ReceiptSHA256 || r.f.s.state.transfer.Status().CompletedBytes != len(data) {
			t.Fatal("ROM launch lost receipt or extended SDM bytes", status)
		}
		if cycle > 0 {
			calls := r.cpu.calls
			if _, err = r.f.s.Return(context.Background(), old); err == nil || r.cpu.calls != calls {
				t.Fatal("stale ROM session performed I/O")
			}
		}
		if _, err = r.f.s.Return(context.Background(), session); err != nil {
			t.Fatal(err)
		}
		if r.f.s.Status().State != ProgramReady || r.f.s.Status().ROMReceiptSHA256 != "" || !r.f.lease.r.Active {
			t.Fatal("return did not retire ROM receipt and resume storage")
		}
		old = session
	}
	if r.cpu.preparedHeaders != 3 || r.cpu.romOpens != 3 || r.cpu.configurations != 3 || r.f.lease.returns != 3 {
		t.Fatal("missing paired C publication or owned cycle")
	}
}

func TestROMBackendDistinguishesTwoFirmwareImagesForOneFPGAPackage(t *testing.T) {
	for _, profile := range []string{HPSFirstROMDiagnosticProfile, NativeComputerProfile} {
		t.Run(profile, func(t *testing.T) { testROMBackendDistinguishesTwoFirmwareImagesForOneFPGAPackage(t, profile) })
	}
}

func testROMBackendDistinguishesTwoFirmwareImagesForOneFPGAPackage(t *testing.T, profile string) {
	r := romProgramTestFor(t, profile)
	first, record, _ := addROMProgram(t, r, 0, 8192)
	firstImage := bytes.Clone(r.cpu.expectedROM)
	secondImage := bytes.Clone(firstImage)
	secondImage[0] ^= 1
	body := r.f.files.files[first.Path].Data
	a, err := corepackage.RetainArchive(context.Background(), int64(len(body)), bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	linked, err := a.LinkROMs(context.Background(), []corepackage.ROMSource{{ID: "firmware", Role: "firmware", Data: secondImage}})
	if err != nil {
		t.Fatal(err)
	}
	second := first
	second.ReceiptSHA256 = linked.ReceiptSHA256()
	second.MetadataPath = "FES/ROMALT.JSN"
	second.Sources = append([]packagestore.Source(nil), first.Sources...)
	second.Sources[0].Path = "FES/ROMALT.BIN"
	sum := sha256.Sum256(secondImage)
	second.Sources[0].SHA256 = hex.EncodeToString(sum[:])
	record.ROMReceiptSHA256 = second.ReceiptSHA256
	metadata := programJSON(t, record)
	sum = sha256.Sum256(metadata)
	second.MetadataSHA256 = hex.EncodeToString(sum[:])
	r.f.files.files[second.MetadataPath] = &fstest.MapFile{Data: metadata}
	r.f.files.files[second.Sources[0].Path] = &fstest.MapFile{Data: secondImage}
	if first.ReceiptSHA256 == second.ReceiptSHA256 || first.PackageID != second.PackageID {
		t.Fatal("fixture lost the same-package/different-firmware boundary")
	}
	b, err := NewProgramBackend(r.f.s, []ProgramEntry{{ID: "rom-a", Title: "ROM A", Request: first}, {ID: "rom-b", Title: "ROM B", Request: second}})
	if err != nil {
		t.Fatal(err)
	}
	client := hostclient.NewClient("http://native.invalid", &http.Client{Transport: b})
	for _, row := range []struct {
		id, receipt string
		image       []byte
	}{{"rom-a", first.ReceiptSHA256, firstImage}, {"rom-b", second.ReceiptSHA256, secondImage}, {"rom-a", first.ReceiptSHA256, firstImage}} {
		r.cpu.expectedROM = row.image
		play, err := client.Launch(context.Background(), row.id)
		if err != nil || play.State != "active" {
			t.Fatal("launch", err, play)
		}
		view := r.f.s.Status()
		if view.Identity.PackageID != first.PackageID || view.Identity.ActivationReceiptSHA256 != row.receipt || view.ROMReceiptSHA256 != row.receipt {
			t.Fatal("backend lost the selected firmware identity", view)
		}
		sum := sha256.Sum256(row.image)
		if play.CorePackage == nil || play.CorePackage.ROMLink == nil || play.CorePackage.ROMLink.ROMID != "firmware" ||
			play.CorePackage.ROMLink.Role != "firmware" || play.CorePackage.ROMLink.SourceSize != int64(len(row.image)) ||
			play.CorePackage.ROMLink.SourceSHA256 != hex.EncodeToString(sum[:]) {
			t.Fatal("session API lost the immutable firmware source", play.CorePackage)
		}
		play.CorePackage.ROMLink.SourceSHA256 = "caller mutation"
		observed, err := client.Session(context.Background())
		if err != nil || observed.CorePackage == nil || observed.CorePackage.ROMLink == nil || observed.CorePackage.ROMLink.SourceSHA256 != hex.EncodeToString(sum[:]) {
			t.Fatal("caller changed the retained session ROM metadata", err)
		}
		returned, err := client.StopExpectedStamped(context.Background(), play, hostclient.ClientStamp{})
		if err != nil || returned.CorePackage != nil {
			t.Fatal(err)
		}
		if view = r.f.s.Status(); view.Identity.ActivationReceiptSHA256 != "" || view.ROMReceiptSHA256 != "" || view.State != ProgramReady {
			t.Fatal("returned firmware identity remained published", view)
		}
	}
}

func TestROMConsolePublishesOnlyTheLiveRetainedReceipt(t *testing.T) {
	for _, profile := range []string{HPSFirstROMDiagnosticProfile, NativeComputerProfile} {
		t.Run(profile, func(t *testing.T) { testROMConsolePublishesOnlyTheLiveRetainedReceipt(t, profile) })
	}
}

func testROMConsolePublishesOnlyTheLiveRetainedReceipt(t *testing.T, profile string) {
	r := romProgramTestFor(t, profile)
	request, _, _ := addROMProgram(t, r, 0, 8192)
	c, err := NewProgramConsole(r.f.s, []packagestore.Request{request})
	if err != nil {
		t.Fatal(err)
	}
	status := c.Handle(context.Background(), []byte(`{"op":"status"}`))
	if status.ROMReceiptSHA256 != "" {
		t.Fatal("ready status invented a ROM receipt")
	}
	launch := fmt.Sprintf(`{"op":"launch","owner":%q,"generation":%d,"program":0}`, status.Owner, status.Generation)
	status = c.Handle(context.Background(), []byte(launch))
	if !status.OK || status.ROMReceiptSHA256 != request.ReceiptSHA256 {
		t.Fatal("console lost the actual retained ROM receipt", status)
	}
	stop := fmt.Sprintf(`{"op":"return","owner":%q,"generation":%d}`, status.Owner, status.Generation)
	status = c.Handle(context.Background(), []byte(stop))
	if !status.OK || status.State != ProgramReady || status.ROMReceiptSHA256 != "" {
		t.Fatal("console retained a returned ROM receipt", status)
	}
}

func TestROMProgramServiceRetiresUncertainStageAndFirmwareFailures(t *testing.T) {
	for _, profile := range []string{HPSFirstROMDiagnosticProfile, NativeComputerProfile} {
		t.Run(profile, func(t *testing.T) { testROMProgramServiceRetiresUncertainStageAndFirmwareFailures(t, profile) })
	}
}

func testROMProgramServiceRetiresUncertainStageAndFirmwareFailures(t *testing.T, profile string) {
	for _, fault := range []string{"stage", "missing-stage", "publication", "write", "rom-after-config", "firmware-rom"} {
		t.Run(fault, func(t *testing.T) {
			r := romProgramTestFor(t, profile)
			req, _, _ := addROMProgram(t, r, 0, 8192)
			r.f.fault = fault
			r.cpu.gateFailure = fault == "firmware-rom"
			if _, err := r.f.s.Launch(context.Background(), 0, req); err == nil {
				t.Fatal("failed ROM activation succeeded")
			}
			if r.f.s.Status().State != ProgramRebootRequired || r.f.lease.r.Active || r.f.lease.returns != 0 || r.f.s.state.plan == nil || r.f.arena.active == nil {
				t.Fatal("failed ROM activation released its retained resources")
			}
			calls, opens := r.cpu.calls, r.f.files.opens
			if _, err := r.f.s.Launch(context.Background(), r.f.s.Status().Generation, req); err == nil || r.cpu.calls != calls || r.f.files.opens != opens {
				t.Fatal("failed ROM activation was replayed")
			}
		})
	}
}

func TestROMCompiledResourcesRejectMismatchWithoutIO(t *testing.T) {
	r := romProgramTest(t)
	if _, err := NewHPSContextVerifier(r.cpu, r.source); err == nil {
		t.Fatal("ordinary constructor enabled a ROM profile")
	}
	for _, change := range []func(*romControlMap){
		func(m *romControlMap) { m.CPUBase += 128 }, func(m *romControlMap) { m.Bytes = 16384 }, func(m *romControlMap) { m.HPSMaster = true },
		func(m *romControlMap) { m.Span = 256 }, func(m *romControlMap) { m.HeldConduit = "other" }, func(m *romControlMap) { m.ClockHz = 74250000 },
		func(m *romControlMap) { m.ControlMapSHA256 = strings.Repeat("2", 64) }, func(m *romControlMap) { m.ComputerMapSHA256 = strings.Repeat("2", 64) },
		func(m *romControlMap) { m.SourceSHA256 = nil }, func(m *romControlMap) { m.Role = "cartridge" },
	} {
		var m romControlMap
		if err := json.Unmarshal(r.romRaw, &m); err != nil {
			t.Fatal(err)
		}
		change(&m)
		calls := r.cpu.calls
		if v, err := NewROMHPSContextVerifier(r.cpu, r.source, r.control, r.computer, programJSON(t, m)); err == nil || v != nil || r.cpu.calls != calls {
			t.Fatal("bad compiled ROM resources performed I/O or constructed a verifier")
		}
	}
	if _, err := r.f.loader.boot.NewColdConfigurationTransfer(r.control, programJSON(t, r.f.loader.configuration), &Region{}, HPSBootObservation{}); err == nil {
		t.Fatal("ROM source used fixed legacy publication")
	}
}

func TestROMPreflightRejectsForeignReceiptAndResourcesBeforeContainment(t *testing.T) {
	for _, profile := range []string{HPSFirstROMDiagnosticProfile, NativeComputerProfile} {
		t.Run(profile, func(t *testing.T) { testROMPreflightRejectsForeignReceiptAndResourcesBeforeContainment(t, profile) })
	}
}

func testROMPreflightRejectsForeignReceiptAndResourcesBeforeContainment(t *testing.T, profile string) {
	for _, fault := range []string{"receipt", "size", "address", "scope", "computer", "inventory"} {
		t.Run(fault, func(t *testing.T) {
			r := romProgramTestFor(t, profile)
			req, record, _ := addROMProgram(t, r, 0, 4096)
			switch fault {
			case "receipt":
				record.ROMReceiptSHA256 = strings.Repeat("2", 64)
			case "size":
				record.ROM.Bytes = 8192
			case "address":
				record.ROM.CPUBase = 0x20010400
			case "scope":
				record.Scope = "nano-selected-program-v1"
			case "computer":
				record.ROM.ComputerMapSHA256 = strings.Repeat("2", 64)
			case "inventory":
				record.ROM.SourceSHA256 = nil
			}
			meta := programJSON(t, record)
			sum := sha256.Sum256(meta)
			r.f.files.files[req.MetadataPath] = &fstest.MapFile{Data: meta}
			req.MetadataSHA256 = hex.EncodeToString(sum[:])
			calls := r.cpu.calls
			if _, err := r.f.s.Launch(context.Background(), 0, req); err == nil {
				t.Fatal("invalid retained ROM launch accepted")
			}
			if r.cpu.calls != calls || !r.f.lease.r.Active || r.f.arena.active != nil || r.f.s.Status().State != ProgramReady {
				t.Fatal("content rejection contained SD, reserved memory or performed firmware I/O")
			}
		})
	}
}

func TestOwnedROMDecoderChecksIndependentContextAndGeneration(t *testing.T) {
	decoder := buildProgramDecoder(t)
	source, m := bindingFixture(t)
	source.Bridges.Generation = 4
	source.Digest = sha256.Sum256([]byte("independent trusted boot fixture"))
	header, _, err := encodeROMProgramBinding(source, m, make([]byte, 4096))
	if err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"pass", "owner", "generation", "context", "chunk"} {
		b := bytes.Clone(header)
		switch fault {
		case "owner":
			b[24] ^= 1
		case "generation":
			b[32] ^= 1
		case "context":
			b[40] ^= 1
		case "chunk":
			b[16] ^= 0x40
		}
		path := filepath.Join(t.TempDir(), "published.bin")
		if err := os.WriteFile(path, b, 0600); err != nil {
			t.Fatal(err)
		}
		out, err := exec.Command(decoder, path, "owned-rom", fmt.Sprint(source.Bridges.Generation), hex.EncodeToString(source.Digest[:]), fmt.Sprint(m.ChunkBytes)).CombinedOutput()
		if fault == "pass" {
			if err != nil {
				t.Fatalf("valid independent scope: %v %s", err, out)
			}
		} else {
			var failure *exec.ExitError
			if !errors.As(err, &failure) || failure.ExitCode() != 2 {
				t.Fatalf("wrong scope did not produce a C decoder rejection: %s %v %s", fault, err, out)
			}
		}
	}
}

func TestROMCatalogCarriesOneCopiedSourceAndReceipt(t *testing.T) {
	r := romProgramTest(t)
	req, _, _ := addROMProgram(t, r, 0, 8192)
	choice := programChoice{Path: req.Path, PackageID: req.PackageID, MetadataPath: req.MetadataPath, MetadataSHA256: req.MetadataSHA256,
		ROMReceiptSHA256: req.ReceiptSHA256, Sources: []programROMSource{{req.Sources[0].ID, req.Sources[0].Role, req.Sources[0].Path, req.Sources[0].SHA256}}}
	r.f.files.files["FES/CORESEL.JSN"] = &fstest.MapFile{Data: programJSON(t, programChoices{1, "nano-program-selection-v1", []programChoice{choice, choice}})}
	requests, err := ReadProgramSelections(context.Background(), r.f.files, "FES/CORESEL.JSN")
	if err != nil || len(requests) != 2 {
		t.Fatal(err)
	}
	requests[0].Sources[0].Path = "changed"
	if requests[1].Sources[0].Path != req.Sources[0].Path || requests[1].ReceiptSHA256 != req.ReceiptSHA256 {
		t.Fatal("ROM choices escaped copied ownership")
	}
	for _, bad := range []programChoice{func() programChoice { p := choice; p.ROMReceiptSHA256 = ""; return p }(), func() programChoice { p := choice; p.Sources = nil; return p }(), func() programChoice { p := choice; p.Sources = append(p.Sources, p.Sources[0]); return p }()} {
		r.f.files.files["FES/CORESEL.JSN"] = &fstest.MapFile{Data: programJSON(t, programChoices{1, "nano-program-selection-v1", []programChoice{bad}})}
		if result, err := ReadProgramSelections(context.Background(), r.f.files, "FES/CORESEL.JSN"); err == nil || result != nil {
			t.Fatal("partial/multiple ROM choice accepted")
		}
	}
}
