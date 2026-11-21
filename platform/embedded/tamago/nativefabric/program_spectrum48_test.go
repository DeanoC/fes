// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/crc32"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/fes/platform/embedded/tamago/block"
	"github.com/DeanoC/fes/platform/embedded/tamago/computer"
	cg "github.com/DeanoC/fes/platform/embedded/tamago/computer/generated"
	"github.com/DeanoC/fes/platform/embedded/tamago/fat"
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
)

// The package store, guarded computer port and firmware upload driver are
// real. This models the FPGA's mailbox replies, with an independent check on
// every submitted word and on the state required for an execution release.
type spectrumFirmwareCPU struct {
	CPUTransport
	base           *computerCPU
	active         bool
	held           bool
	ready          bool
	image          []byte
	header         [4]uint16
	begins         int
	commits        int
	releases       int
	failCommit     bool
	fetchWhileHeld bool
	tapeCalls      int
}

func (c *spectrumFirmwareCPU) LoadInitialTape(context.Context, BridgeSnapshot, computer.InitialTapeRequest) (computer.InitialTapeReceipt, error) {
	c.tapeCalls++
	return computer.InitialTapeReceipt{}, errors.New("Spectrum attempted to use the native ZX81 tape service")
}

func (c *spectrumFirmwareCPU) WritePhysical32(address uint64, word uint32) error {
	if err := c.CPUTransport.WritePhysical32(address, word); err != nil || !c.active {
		return err
	}
	op, index, value := word>>24&127, word>>16&255, uint16(word)
	if op == cg.FesComputerOpcodeExecution {
		c.held = value == uint16(cg.FesComputerExecutionHoldReset)
		if !c.held {
			c.releases++
			if !c.ready {
				return errors.New("Spectrum ran before its ROM was ready")
			}
		}
	}
	if op < cg.FesFirmwareUploadOpcodeInfo || op > cg.FesFirmwareUploadOpcodeCommit {
		return nil
	}
	if !c.held {
		return errors.New("Spectrum ROM command without execution Hold")
	}
	switch op {
	case cg.FesFirmwareUploadOpcodeInfo:
		var state uint16
		if c.ready {
			state = uint16(cg.FesFirmwareUploadStateReady)
		}
		words := [...]uint16{spectrum48ROMBytes, 0, state, uint16(len(c.image)), 0}
		if index >= uint32(len(words)) {
			return errors.New("invalid Spectrum firmware information query")
		}
		c.base.gpi |= uint32(words[index])
	case cg.FesFirmwareUploadOpcodeBegin:
		if index >= 4 || c.ready {
			return errors.New("invalid Spectrum firmware begin")
		}
		c.header[index] = value
		c.begins++
	case cg.FesFirmwareUploadOpcodeData:
		if index != uint32(len(c.image)/2)&255 || c.ready || len(c.image) >= spectrum48ROMBytes {
			return errors.New("invalid Spectrum firmware data ordinal")
		}
		c.image = append(c.image, byte(value), byte(value>>8))
	case cg.FesFirmwareUploadOpcodeCommit:
		c.commits++
		wantSize := uint32(c.header[0]) | uint32(c.header[1])<<16
		wantCRC := uint32(c.header[2]) | uint32(c.header[3])<<16
		if wantSize != spectrum48ROMBytes || len(c.image) != spectrum48ROMBytes || crc32.ChecksumIEEE(c.image) != wantCRC {
			return errors.New("Spectrum firmware commit disagrees with the accepted bytes")
		}
		if c.failCommit {
			return errors.New("Spectrum firmware commit lost acknowledgement")
		}
		c.ready = true
		if c.fetchWhileHeld {
			c.base.fetches++
		}
	}
	return nil
}

func packageSpectrumFixture(t *testing.T, l *ProgramLoader) (packagestore.Request, map[string][]byte, []byte, []byte) {
	t.Helper()
	dir := t.TempDir()
	base := "../../../../sources/FogCast/corepackage/testdata/core-bundle-v3"
	for name, source := range map[string]string{"manifest.toml": "manifests/valid-nano-upload-16384.toml",
		"core.rbf": "payloads/fes-fixture.rbf", "rom-map.json": "maps/valid-nano-upload-16384.json"} {
		data, err := os.ReadFile(filepath.Join(base, source))
		if err != nil {
			t.Fatal(err)
		}
		if name == "manifest.toml" {
			data = []byte(strings.NewReplacer("fes.pong", "fes.spectrum", "fes-gp-v1", spectrum48Profile,
				"fes.simple-game", "fes.computer").Replace(string(data)))
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := corepackage.CanonicalArchive(dir)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := corepackage.RetainArchive(t.Context(), int64(len(raw)), bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	i := archive.Inspection()
	rom := make([]byte, spectrum48ROMBytes)
	for offset := range rom {
		rom[offset] = byte(offset ^ offset>>8)
	}
	d := i.Descriptor
	linked, err := archive.LinkROMs(t.Context(), []corepackage.ROMSource{{ID: d.ROM.ID, Role: "firmware", Data: rom}})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(filepath.Join(dir, "core.rbf"))
	if err != nil {
		t.Fatal(err)
	}
	receipt := linked.Receipt().ROMLink
	record := spectrum48Record{Format: 1, Scope: "nano-compiled-spectrum48-program-v1",
		BootContextSHA256: hex.EncodeToString(l.boot.digest[:]), HandoffSHA256: strings.Repeat("4", 64),
		PackageID: i.PackageID, BuildID: d.Build.ID, CoreSize: len(payload), CoreSHA256: d.Payload.SHA256,
		CoreSHA384: fmt.Sprintf("%x", sha512.Sum384(payload)), ROMSourceSHA256: receipt.SourceSHA256,
		ROMReceiptSHA256: linked.ReceiptSHA256(), ProbeCPUBase: 0x20010100,
		ScanoutCPUBase: 0x20010200, ComputerCPUBase: 0x20010300}
	for offset := 0; offset < len(payload); offset += l.configuration.ChunkBytes {
		record.ChunkSHA384 = append(record.ChunkSHA384,
			fmt.Sprintf("%x", sha512.Sum384(payload[offset:min(offset+l.configuration.ChunkBytes, len(payload))])))
	}
	recordRaw := programJSON(t, record)
	policy := make([]byte, 4, 4+32+16+32+48+48*maxProgramChunks)
	binary.LittleEndian.PutUint32(policy, uint32(record.CoreSize))
	for _, s := range append([]string{record.PackageID, record.BuildID, record.CoreSHA256, record.CoreSHA384}, record.ChunkSHA384...) {
		field, err := hex.DecodeString(s)
		if err != nil {
			t.Fatal(err)
		}
		policy = append(policy, field...)
	}
	policy = policy[:cap(policy)]
	request := packagestore.Request{Path: "FES/SPEC48.FCP", PackageID: i.PackageID,
		MetadataPath: "FES/SPEC48.JSN", MetadataSHA256: fmt.Sprintf("%x", sha256.Sum256(recordRaw)),
		ReceiptSHA256: linked.ReceiptSHA256(), Sources: []packagestore.Source{
			{ID: d.ROM.ID, Role: "firmware", Path: "FES/SPEC48.ROM", SHA256: receipt.SourceSHA256},
		}}
	return request, map[string][]byte{request.Path: raw, request.MetadataPath: recordRaw, request.Sources[0].Path: rom}, recordRaw, policy
}

func TestSpectrum48ProgramServiceFATLaunchReturnAndPark(t *testing.T) {
	for _, fault := range []string{"", "disabled", "rom", "companion", "commit", "held-fetch", "drain"} {
		t.Run("fault="+fault, func(t *testing.T) {
			testSpectrum48FAT(t, fault)
		})
	}
}

func testSpectrum48FAT(t *testing.T, fault string) {
	ctx := t.Context()
	image := filepath.Join(t.TempDir(), "spectrum.img")
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
	var expected []byte
	f := newProgramMenuFixtureContext(t, ctx, "", func(f *programMenuFixture) {
		l := f.r.f.loader
		request, files, compiled, policy := packageSpectrumFixture(t, l)
		expected = bytes.Clone(files[request.Sources[0].Path])
		if fault == "rom" {
			files[request.Sources[0].Path][0] ^= 1
		}
		if fault == "companion" {
			raw := bytes.Replace(files[request.MetadataPath], []byte(strings.Repeat("4", 64)), []byte(strings.Repeat("5", 64)), 1)
			files[request.MetadataPath] = raw
			request.MetadataSHA256 = fmt.Sprintf("%x", sha256.Sum256(raw))
		}
		paired, err := l.WithSpectrum48(compiled)
		if err != nil {
			t.Fatal(err)
		}
		if fault != "disabled" {
			f.r.f.loader = paired
		}
		// The C decoder gets the trusted policy independently of the published
		// header. Opening Spectrum never consults the staged ZX81 ROM slot.
		f.cpu.configuration.spectrumPolicy = filepath.Join(t.TempDir(), "policy.bin")
		if err := os.WriteFile(f.cpu.configuration.spectrumPolicy, policy, 0600); err != nil {
			t.Fatal(err)
		}
		port = &spectrumFirmwareCPU{CPUTransport: f.cpu, base: f.r.f.cpu.computerCPU,
			failCommit: fault == "commit", fetchWhileHeld: fault == "held-fetch"}
		l.boot.cpu = port
		activate := f.cpu.activate
		f.cpu.activate = func(role programRole) {
			activate(role)
			port.active = role == programRoleSpectrum48
			port.ready, port.held, port.image, port.header = false, false, nil, [4]uint16{}
			if port.active {
				f.r.cpu.expectedROM = nil
				raw, err := hex.DecodeString(paired.spectrum48.record.BuildID)
				if err != nil {
					t.Fatal(err)
				}
				for i := range 8 {
					port.base.words[8+i] = binary.LittleEndian.Uint16(raw[2*i:])
				}
				port.base.words[7] = uint16(spectrum48Capabilities)
				port.base.gpo, port.base.gpi, port.base.fetches = 0, cg.FesComputerSignature, 0
			}
		}
		payload := programExport(t, "spectrum48", request, files)
		for name, data := range programUpload(f.r.f.files, f.request) {
			payload[name] = data
		}
		payload["FES/MENUSEL.JSN"] = programExport(t, "menu", f.request, nil)["FES/CORESEL.JSN"]
		payload[packagestore.PreferencesPath] = []byte("{}")
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
	backend, err := NewProgramBackend(f.s, startup.Entries)
	if err != nil {
		t.Fatal(err)
	}
	client := hostclient.NewClient("http://native.invalid", &http.Client{Transport: backend})
	for attempt := 1; attempt <= 2; attempt++ {
		play, err := client.Launch(ctx, "spectrum48")
		switch fault {
		case "disabled", "rom", "companion":
			if err != nil || play.ErrorCode != "NATIVE_LAUNCH_FAILED" || play.ID != "" || f.s.Status().State != ProgramReady || port.begins != 0 || port.releases != 0 ||
				f.r.f.cpu.configurations != 2 || f.r.f.lease.r.Active || f.cpu.kind != programRoleMenu {
				t.Fatal("invalid Spectrum content entered configuration or lost the menu", err, f.s.Status())
			}
			return
		case "commit", "held-fetch":
			if err != nil || play.ErrorCode != "NATIVE_LAUNCH_FAILED" || play.ID != "" || port.commits != 1 || port.releases != 0 ||
				f.s.Status().State != ProgramRebootRequired || f.r.f.lease.r.Active {
				t.Fatal("uncertain firmware upload released execution or storage", err, f.s.Status())
			}
			return
		}
		fetch, observed := f.s.FetchObservation()
		if err != nil || play.State != "active" || !bytes.Equal(port.image, expected) ||
			port.begins != 4*attempt || port.commits != attempt || port.releases != attempt ||
			port.tapeCalls != 0 || !observed || fetch.Scope != "nano-spectrum48-fetch-v1" || f.r.f.lease.r.Active {
			t.Fatal("Spectrum skipped retained upload, fetch or containment", err, play.State, fetch)
		}
		port.base.badClosure = fault == "drain"
		stopped, err := client.StopExpectedStamped(ctx, play, hostclient.ClientStamp{})
		if fault == "drain" {
			if err != nil || stopped.ErrorCode != "NATIVE_RETURN_FAILED" || stopped.ID != "" || f.r.f.lease.r.Active || f.s.Status().State != ProgramRebootRequired {
				t.Fatal("uncertain drain released storage", err, f.s.Status())
			}
			return
		}
		if err != nil || f.s.Status().State != ProgramReady || f.cpu.kind != programRoleMenu || f.r.f.lease.r.Active {
			t.Fatal("Spectrum return did not restore the owned menu", err, f.s.Status())
		}
	}
	if err := backend.ParkMenu(ctx); err != nil || f.s.Status().State != ProgramParked || f.r.f.lease.r.Active {
		t.Fatal("Spectrum run did not Park", err, f.s.Status())
	}
}
