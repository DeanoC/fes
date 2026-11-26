// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/sha512"
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
	"time"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/ui/tenfoot"
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
	romClock      *time.Time
	inROMBatch    bool
	romBatches    int
	failROMBatch  bool
	inDiskBatch   bool
	diskBatches   int
	diskBatchCost time.Duration
	dmaStatus     uint32
	CPUTransport
	base           *computerCPU
	active         bool
	held           bool
	ready          bool
	image          []byte
	firmwareBytes  int
	header         [4]uint16
	begins         int
	commits        int
	releases       int
	failCommit     bool
	fetchWhileHeld bool
	tapeCalls      int
	media          tapeCPU
	wantTape       bool
}

func (c *spectrumFirmwareCPU) LoadInitialTape(context.Context, BridgeSnapshot, computer.InitialTapeRequest) (computer.InitialTapeReceipt, error) {
	c.tapeCalls++
	return computer.InitialTapeReceipt{}, errors.New("Spectrum attempted to use the native ZX81 tape service")
}

type batchedSpectrumCPU struct{ *spectrumFirmwareCPU }
type fixtureSpectrumROMRegisters struct{ *spectrumFirmwareCPU }

func (c fixtureSpectrumROMRegisters) Read32(offset uint32) (uint32, error) {
	return c.ReadPhysical32(0x20010300 + uint64(offset))
}
func (c fixtureSpectrumROMRegisters) Write32(offset, value uint32) error {
	return c.WritePhysical32(0x20010300+uint64(offset), value)
}
func (c batchedSpectrumCPU) WithSpectrumROMBatch(base uint64, send func(computer.IO) error) error {
	if base != 0x20010300 || !c.active || !c.held || c.inROMBatch || c.ready {
		return errors.New("ROM batch outside the held Spectrum aperture")
	}
	c.romBatches++
	// Charge the two mapping/ownership boundaries and a cost per raw CSR.
	if c.romClock != nil {
		*c.romClock = c.romClock.Add(850 * time.Microsecond)
	}
	c.inROMBatch = true
	defer func() { c.inROMBatch = false }()
	before := len(c.image)
	if err := send(fixtureSpectrumROMRegisters{c.spectrumFirmwareCPU}); err != nil {
		return err
	}
	batchWords := computer.FirmwareDataBatchWords
	if c.firmwareBytes == c64FirmwareBytes {
		batchWords = computer.C64DataBatchWords
	}
	if len(c.image)-before != 2*batchWords || c.failROMBatch {
		return errors.New("ROM batch completion failed")
	}
	return nil
}
func (c *spectrumFirmwareCPU) WithHeldMediaBatch(base uint64, send func(computer.IO) error) error {
	if base != 0x20010300 || !c.active || !c.held || !c.ready || c.inDiskBatch {
		return errors.New("media batch outside held upload shell")
	}
	c.diskBatches++
	if c.romClock != nil {
		*c.romClock = c.romClock.Add(c.diskBatchCost)
	}
	c.inDiskBatch = true
	defer func() { c.inDiskBatch = false }()
	before := len(c.media.image)
	if err := send(fixtureSpectrumROMRegisters{c}); err != nil {
		return err
	}
	total := int(c.media.header[0]) | int(c.media.header[1])<<16
	if len(c.media.image)-before != min(int(cg.FesComputerMediaChunkMaxBytes), total-before) {
		return errors.New("media batch did not deliver exactly one chunk")
	}
	return nil
}

func (c *spectrumFirmwareCPU) chargeROMAccess() {
	if c.romClock == nil || !c.active || !c.held || c.ready {
		return
	}
	cost := 850 * time.Microsecond
	if c.inROMBatch {
		cost = time.Microsecond
	}
	*c.romClock = c.romClock.Add(cost)
}

func (c *spectrumFirmwareCPU) ReadPhysical32(address uint64) (uint32, error) {
	c.chargeROMAccess()
	if address == uploadShellScanoutCPUBase+uint64(cg.FesDe25ScanoutStatusOffset) {
		return c.dmaStatus, nil
	}
	return c.CPUTransport.ReadPhysical32(address)
}

func (c *spectrumFirmwareCPU) WritePhysical32(address uint64, word uint32) error {
	c.chargeROMAccess()
	if err := c.CPUTransport.WritePhysical32(address, word); err != nil || !c.active {
		return err
	}
	op, index, value := word>>24&127, word>>16&255, uint16(word)
	if op == cg.FesComputerOpcodeExecution {
		c.held = value == uint16(cg.FesComputerExecutionHoldReset)
		if !c.held {
			c.releases++
			if !c.ready || c.wantTape && !c.media.ready {
				return errors.New("Spectrum ran before its selected ROM and tape were ready")
			}
		}
	}
	if op >= cg.FesComputerOpcodeMediaInfo && op <= cg.FesComputerOpcodeMediaEject {
		if c.media.disk && (op == cg.FesComputerOpcodeMediaChunk || op == cg.FesComputerOpcodeMediaData) && !c.inDiskBatch {
			return errors.New("C64 chunk header or data escaped its held batch")
		}
		c.media.held = c.held
		if !c.ready {
			return errors.New("Spectrum tape submitted before its ROM was ready")
		}
		return c.media.writeMedia(op, index, value)
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
		words := [...]uint16{uint16(c.firmwareBytes), 0, state, uint16(len(c.image)), 0}
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
		if index != uint32(len(c.image)/2)&255 || c.ready || len(c.image) >= c.firmwareBytes {
			return errors.New("invalid Spectrum firmware data ordinal")
		}
		c.image = append(c.image, byte(value), byte(value>>8))
	case cg.FesFirmwareUploadOpcodeCommit:
		c.commits++
		wantSize := uint32(c.header[0]) | uint32(c.header[1])<<16
		wantCRC := uint32(c.header[2]) | uint32(c.header[3])<<16
		if wantSize != uint32(c.firmwareBytes) || len(c.image) != c.firmwareBytes || crc32.ChecksumIEEE(c.image) != wantCRC {
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

func packageSpectrumFixture(t *testing.T, l *ProgramLoader, alternate bool) (packagestore.Request, map[string][]byte, []byte, []byte) {
	t.Helper()
	dir := t.TempDir()
	base := "../../../../sources/FogCast/corepackage/testdata/core-bundle-v3"
	payloadFixture, err := os.ReadFile(filepath.Join(base, "payloads/fes-fixture.rbf"))
	if err != nil {
		t.Fatal(err)
	}
	mapFixture, err := os.ReadFile(filepath.Join(base, "maps/valid-nano-upload-16384.json"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(filepath.Join(base, "manifests/valid-nano-upload-16384.toml"))
	if err != nil {
		t.Fatal(err)
	}
	payload := payloadFixture
	romMap := mapFixture
	if alternate {
		payload = append(bytes.Clone(payloadFixture), 0x34, 0x32, 0x34, 0x32)
		oldDigest := fmt.Sprintf("%x", sha256.Sum256(payloadFixture))
		newDigest := fmt.Sprintf("%x", sha256.Sum256(payload))
		romMap = bytes.ReplaceAll(mapFixture, []byte(oldDigest), []byte(newDigest))
		manifest = []byte(strings.NewReplacer(
			"0123456789abcdef0123456789abcdef", "abcdef0123456789abcdef0123456789",
			"size = 12\n", "size = 16\n", oldDigest, newDigest,
			fmt.Sprintf("%x", sha256.Sum256(mapFixture)), fmt.Sprintf("%x", sha256.Sum256(romMap)),
		).Replace(string(manifest)))
	}
	for name, data := range map[string][]byte{"manifest.toml": manifest, "core.rbf": payload, "rom-map.json": romMap} {
		if name == "manifest.toml" {
			data = []byte(strings.NewReplacer("fes.pong", "fes.spectrum", "fes-gp-v1", spectrum48Profile,
				"fes.simple-game", "fes.computer", "fes.gamepad\"", "fes.gamepad.ports\"").Replace(string(data)) + `
[[interfaces]]
id = "fes.media.spectrum-tape"
major = 2
minor = 0
required = true
[[interfaces]]
id = "fes.keyboard.hid"
major = 1
minor = 0
required = true
[[interfaces]]
id = "fes.audio.pcm-s16-stereo-48k"
major = 1
minor = 0
required = true
[[interfaces]]
id = "fes.video.session-display"
major = 1
minor = 0
required = true
[[interfaces]]
id = "fes.fabric.video.display-plane"
major = 1
minor = 0
required = true
[[interfaces]]
id = "fes.fabric.de25-scanout"
major = 1
minor = 3
required = true
`)
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
	receipt := linked.Receipt().ROMLink
	record := uploadShellRecord{Format: 1, Scope: "nano-compiled-spectrum48-program-v1",
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
	stem := "FES/SPEC48"
	if alternate {
		stem = "FES/SPEC48B"
	}
	request := packagestore.Request{Path: stem + ".FCP", PackageID: i.PackageID,
		MetadataPath: stem + ".JSN", MetadataSHA256: fmt.Sprintf("%x", sha256.Sum256(recordRaw)),
		ReceiptSHA256: linked.ReceiptSHA256(), Sources: []packagestore.Source{
			{ID: d.ROM.ID, Role: "firmware", Path: stem + ".ROM", SHA256: receipt.SourceSHA256},
		}}
	return request, map[string][]byte{request.Path: raw, request.MetadataPath: recordRaw, request.Sources[0].Path: rom}, recordRaw, policy
}

func TestSpectrum48ProgramServiceFATLaunchReturnAndPark(t *testing.T) {
	for _, fault := range []string{"", "tape", "tape-format", "tape-content", "tape-output-limit", "tape-digest", "tape-commit", "disabled", "rom", "companion", "commit", "held-fetch", "drain"} {
		t.Run("fault="+fault, func(t *testing.T) {
			testSpectrum48FAT(t, fault)
		})
	}
}

func TestSharedCoreShellFATLifecycle(t *testing.T) {
	for _, fault := range []string{"shared", "shared-bad-map", "shared-bad-dma", "shared-wedged", "shared-fast-rom", "shared-rom-batch-fault", "shared-slow-rom", "shared-c64", "shared-c64-disk-commit", "shared-c64-deadline"} {
		t.Run(fault, func(t *testing.T) { testSpectrum48FAT(t, fault) })
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
	var expected, expectedTape, expectedDisk []byte
	var activeBuild, firstBuild string
	var alternateChoice packagestore.Request
	var alternateBuild string
	shared := strings.HasPrefix(fault, "shared")
	c64 := strings.HasPrefix(fault, "shared-c64")
	multi := shared && !c64
	var romClock = time.Unix(1, 0)
	timedROM := fault == "shared-fast-rom" || fault == "shared-rom-batch-fault" || fault == "shared-slow-rom"
	dmaMode := strings.TrimPrefix(fault, "shared-")
	guardModel := os.Getenv("FES_TEST_DE25_DMA_GUARD")
	if (dmaMode == "bad-dma" || dmaMode == "wedged") && guardModel == "" {
		t.Skip("build the shell Verilator testbench and set FES_TEST_DE25_DMA_GUARD")
	}
	f := newProgramMenuFixtureContext(t, ctx, "", func(f *programMenuFixture) {
		l := f.r.f.loader
		request, files, compiled, policy := packageSpectrumFixture(t, l, false)
		if c64 {
			request, files = c64PackageFixture(t, l, c64Profile, nil)
			compiled = files[request.MetadataPath]
			expectedDisk = bytes.Repeat([]byte{0x12, 0x9a}, int(cg.FesComputerC64DiskBytes)/2)
			request.Tape = packagestore.TapeRequest{Path: "FES/C64.D64", Format: packagestore.C64DiskFormat,
				Size: int64(len(expectedDisk)), SHA256: fmt.Sprintf("%x", sha256.Sum256(expectedDisk))}
			files[request.Tape.Path] = expectedDisk
		}
		var first uploadShellRecord
		if err := json.Unmarshal(compiled, &first); err != nil {
			t.Fatal(err)
		}
		activeBuild, firstBuild = first.BuildID, first.BuildID
		expected = bytes.Clone(files[request.Sources[0].Path])
		if strings.HasPrefix(fault, "tape") {
			// A ROM-loader TAP block larger than the ZX81 file ceiling.
			size := (24 << 10) + 2
			if fault == "tape-output-limit" {
				size = packagestore.MaxRetainedSpectrumTapeBytes
			}
			tap := make([]byte, size)
			binary.LittleEndian.PutUint16(tap, uint16(len(tap)-2))
			tap[2], tap[len(tap)-1] = 0xff, 0xff
			var err error
			expectedTape, err = protocol.ConvertSpectrumTape(tap, int(protocol.SpectrumTapeMaxBytes))
			if err != nil || len(expectedTape) <= 16<<10 {
				t.Fatal("invalid Spectrum TAP fixture", err, len(expectedTape))
			}
			if fault == "tape-output-limit" && len(expectedTape) <= computer.MaxInitialSpectrumTapeBytes {
				t.Fatal("fixture does not exceed the converted stream limit", len(expectedTape))
			}
			if fault == "tape-content" {
				tap = []byte{0xff, 0xff, 0}
			}
			request.Tape = packagestore.TapeRequest{Path: "FES/SPEC48.TAP", Format: packagestore.SpectrumTapeFormat,
				Size: int64(len(tap)), SHA256: fmt.Sprintf("%x", sha256.Sum256(tap))}
			files[request.Tape.Path] = tap
			if fault == "tape-format" {
				request.Tape.Format = ""
				request.Tape.Size = 4
				files[request.Tape.Path] = []byte{2, 0, 0xff, 0xff}
				request.Tape.SHA256 = fmt.Sprintf("%x", sha256.Sum256(files[request.Tape.Path]))
			}
			if fault == "tape-digest" {
				tap[2] ^= 1
			}
		}
		if fault == "rom" {
			files[request.Sources[0].Path][0] ^= 1
		}
		if fault == "companion" {
			raw := bytes.Replace(files[request.MetadataPath], []byte(strings.Repeat("4", 64)), []byte(strings.Repeat("5", 64)), 1)
			files[request.MetadataPath] = raw
			request.MetadataSHA256 = fmt.Sprintf("%x", sha256.Sum256(raw))
		}
		var paired *ProgramLoader
		if shared {
			paired, err = l.WithSharedCoreShell()
		} else {
			paired, err = l.WithSpectrum48(compiled)
		}
		if err != nil {
			t.Fatal(err)
		}
		if fault != "disabled" {
			f.r.f.loader = paired
		}
		// The C decoder gets the trusted policy independently of the published
		// header. Opening Spectrum never consults the staged ZX81 ROM slot.
		f.cpu.configuration.sharedShell = shared
		f.cpu.configuration.spectrumPolicy = filepath.Join(t.TempDir(), "policy.bin")
		if err := os.WriteFile(f.cpu.configuration.spectrumPolicy, policy, 0600); err != nil {
			t.Fatal(err)
		}
		port = &spectrumFirmwareCPU{CPUTransport: f.cpu, base: f.r.f.cpu.computerCPU,
			failCommit: fault == "commit", fetchWhileHeld: fault == "held-fetch", wantTape: len(expectedTape)+len(expectedDisk) != 0, firmwareBytes: len(expected)}
		port.media = tapeCPU{base: port.base, spectrum: !c64, disk: c64}
		if fault == "tape-commit" || fault == "shared-c64-disk-commit" {
			port.media.fault = "commit"
		}
		l.boot.cpu = port
		if c64 {
			l.boot.cpu = batchedSpectrumCPU{port}
			port.romClock = &romClock
			port.diskBatchCost = 5500 * time.Microsecond
			if fault == "shared-c64-deadline" {
				port.diskBatchCost = 25 * time.Millisecond
			}
		}
		if timedROM {
			port.romClock = &romClock
			port.failROMBatch = fault == "shared-rom-batch-fault"
			if fault != "shared-slow-rom" {
				l.boot.cpu = batchedSpectrumCPU{port}
			}
		}
		activate := f.cpu.activate
		f.cpu.activate = func(role programRole) {
			activate(role)
			port.active = role == programRoleUploadShell
			port.ready, port.held, port.image, port.header = false, false, nil, [4]uint16{}
			port.media.ready, port.media.image = false, nil
			if port.active {
				f.r.cpu.expectedROM = nil
				raw, err := hex.DecodeString(activeBuild)
				if err != nil {
					t.Fatal(err)
				}
				for i := range 8 {
					port.base.words[8+i] = binary.LittleEndian.Uint16(raw[2*i:])
				}
				capabilities := spectrum48Capabilities
				if c64 {
					capabilities = c64Capabilities
				}
				port.base.words[7] = uint16(capabilities)
				port.base.gpo, port.base.gpi, port.base.fetches = 0, cg.FesComputerSignature, 0
			}
		}
		payload := programExport(t, "spectrum48", request, files)
		if multi {
			var filesB map[string][]byte
			var recordB []byte
			alternateChoice, filesB, recordB, _ = packageSpectrumFixture(t, l, true)
			var second uploadShellRecord
			if err := json.Unmarshal(recordB, &second); err != nil {
				t.Fatal(err)
			}
			alternateBuild = second.BuildID
			if first.PackageID == second.PackageID || first.BuildID == second.BuildID || first.CoreSHA256 == second.CoreSHA256 {
				t.Fatal("second build must have independent identities")
			}
			if fault == "shared-bad-map" {
				second.ScanoutCPUBase += 256
				recordB = programJSON(t, second)
				filesB[alternateChoice.MetadataPath] = recordB
				alternateChoice.MetadataSHA256 = fmt.Sprintf("%x", sha256.Sum256(recordB))
			}
			export := programExport(t, "spectrum48b", alternateChoice, filesB)
			var a, b programChoices
			if err := errors.Join(json.Unmarshal(payload["FES/CORESEL.JSN"], &a), json.Unmarshal(export["FES/CORESEL.JSN"], &b)); err != nil {
				t.Fatal(err)
			}
			a.Programs = append(a.Programs, b.Programs...)
			for name, data := range filesB {
				payload[name] = data
			}
			payload["FES/CORESEL.JSN"] = programJSON(t, a)
			payload["FES/UI.JSON"] = []byte(`{"entries":[{"id":"spectrum48","program":0,"title":"Spectrum A"},{"id":"spectrum48b","program":1,"title":"Spectrum B"}],"format":1,"scope":"nano-tenfoot-catalog-v1"}` + "\n")
		}
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
	if timedROM || c64 {
		f.s.state.limits.Now = func() time.Time { return romClock }
	}
	backend, err := NewProgramBackend(f.s, startup.Entries)
	if err != nil {
		t.Fatal(err)
	}
	if dmaMode == "bad-dma" || dmaMode == "wedged" {
		testSharedShellApp(t, backend, f, guardModel, dmaMode, func(second bool) {
			activeBuild = firstBuild
			if second {
				activeBuild = alternateBuild
			}
		}, alternateChoice.PackageID, port)
		return
	}
	client := hostclient.NewClient("http://native.invalid", &http.Client{Transport: backend})
	for attempt := 1; attempt <= 2; attempt++ {
		id := "spectrum48"
		activeBuild = firstBuild
		if multi && attempt == 2 {
			id, activeBuild = "spectrum48b", alternateBuild
		}
		play, err := client.Launch(ctx, id)
		if fault == "shared-bad-map" && attempt == 2 {
			if err != nil || play.ErrorCode != "NATIVE_LAUNCH_FAILED" || f.s.Status().State != ProgramReady || port.commits != 1 {
				t.Fatal("invalid second shell map entered configuration", play, err, f.s.Status())
			}
			return
		}
		if multi && attempt == 2 && f.s.Status().Identity.PackageID != alternateChoice.PackageID {
			t.Fatal("second SD build did not become the held service identity", f.s.Status())
		}
		switch fault {
		case "disabled", "rom", "companion", "tape-format", "tape-content", "tape-output-limit", "tape-digest":
			if err != nil || play.ErrorCode != "NATIVE_LAUNCH_FAILED" || play.ID != "" || f.s.Status().State != ProgramReady || port.begins != 0 || port.releases != 0 ||
				f.r.f.cpu.configurations != 2 || f.r.f.lease.r.Active || f.cpu.kind != programRoleMenu {
				t.Fatal("invalid Spectrum content entered configuration or lost the menu", err, f.s.Status())
			}
			return
		case "shared-rom-batch-fault", "shared-slow-rom":
			if err != nil || play.ErrorCode != "NATIVE_LAUNCH_FAILED" || f.s.Status().State != ProgramRebootRequired ||
				port.commits != 0 || port.releases != 0 || f.r.f.lease.r.Active ||
				fault == "shared-rom-batch-fault" && (port.romBatches != 1 || len(port.image) != 2*computer.FirmwareDataBatchWords) ||
				fault == "shared-slow-rom" && (port.romBatches != 0 || len(port.image) == 0 || len(port.image) >= len(expected)) {
				t.Fatal("ROM failure retried, committed or lost containment", err, f.s.Status(), port.romBatches, len(port.image))
			}
			return
		case "shared-c64-deadline":
			if err != nil || play.ErrorCode != "NATIVE_LAUNCH_FAILED" || f.s.Status().State != ProgramRebootRequired ||
				f.r.f.lease.r.Active || port.media.commits != 0 || port.releases != 0 ||
				len(port.media.image) == 0 || len(port.media.image) >= len(expectedDisk) {
				t.Fatal("expired C64 upload committed, ran or lost containment", err, f.s.Status(), len(port.media.image))
			}
			return
		case "commit", "held-fetch", "tape-commit", "shared-c64-disk-commit":
			if err != nil || play.ErrorCode != "NATIVE_LAUNCH_FAILED" || play.ID != "" || port.commits != 1 || port.releases != 0 ||
				f.s.Status().State != ProgramRebootRequired || f.r.f.lease.r.Active {
				t.Fatal("uncertain firmware upload released execution or storage", err, f.s.Status())
			}
			if fault == "shared-c64-disk-commit" && (port.media.commits != 1 || !bytes.Equal(port.media.image, expectedDisk)) {
				t.Fatal("disk commit fault did not exercise a complete transfer")
			}
			if fault == "tape-commit" && (port.media.starts != 1 || port.media.commits != 1 || !bytes.Equal(port.media.image, expectedTape)) {
				t.Fatal("tape commit fault did not exercise a complete transfer")
			}
			return
		}
		fetch, observed := f.s.FetchObservation()
		scope := "nano-spectrum48-fetch-v1"
		if c64 {
			scope = "nano-c64-fetch-d64-v1"
			view := f.s.Status()
			chunkBytes := int(cg.FesComputerMediaChunkMaxBytes)
			chunks := (len(expectedDisk) + chunkBytes - 1) / chunkBytes
			if port.media.commits != attempt || !bytes.Equal(port.media.image, expectedDisk) ||
				int(fetch.DiskBytes) != len(expectedDisk) || view.DiskBytes != len(expectedDisk) ||
				view.DiskSHA256 != fmt.Sprintf("%x", sha256.Sum256(expectedDisk)) ||
				port.diskBatches != attempt*chunks || fetch.ElapsedNS >= int64(5*time.Second) {
				t.Fatal("C64 D64 transfer differed from retained bytes", view, fetch, len(port.media.image), port.media.commits)
			}
		}
		if len(expectedTape) != 0 {
			scope = "nano-spectrum48-fetch-tape-v1"
			status := f.s.Status()
			if port.media.starts != attempt || port.media.commits != attempt || !bytes.Equal(port.media.image, expectedTape) ||
				int(fetch.TapeBytes) != len(expectedTape) || status.TapeBytes != len(expectedTape) ||
				status.TapeSHA256 != fmt.Sprintf("%x", sha256.Sum256(expectedTape)) {
				t.Fatal("Spectrum did not freshly commit the converted SD tape", status, fetch)
			}
		}
		if err != nil || play.State != "active" || !bytes.Equal(port.image, expected) ||
			port.begins != 4*attempt || port.commits != attempt || port.releases != attempt ||
			port.tapeCalls != 0 || !observed || fetch.Scope != scope || f.r.f.lease.r.Active {
			t.Fatal("Spectrum skipped retained upload, fetch or containment", err, play.State, fetch)
		}
		if fault == "shared-fast-rom" && (port.romBatches != attempt*len(expected)/(2*computer.FirmwareDataBatchWords) ||
			fetch.ElapsedNS <= 0 || fetch.ElapsedNS >= int64(time.Second)) {
			t.Fatal("full ROM was not uploaded in bounded batches", port.romBatches, fetch)
		}
		port.base.badClosure = fault == "drain"
		stopped, err := client.StopExpectedStamped(ctx, play, hostclient.ClientStamp{})
		if port.base.badClosure {
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

// The test adapter turns the simulated shell fault into the App's normal
// Stop command. The normal service still owns drain, containment and restore.
func testSharedShellApp(t *testing.T, backend *ProgramBackend, f *programMenuFixture, guard, mode string,
	selectBuild func(bool), secondPackage string, port *spectrumFirmwareCPU) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	app := tenfoot.NewApp(tenfoot.NewClient("http://native.invalid", &http.Client{Transport: backend}), 640, 480, 100)
	app.Start(ctx)
	defer app.Stop()
	wait := func(name string, check func(tenfoot.Snapshot) bool) {
		t.Helper()
		for ctx.Err() == nil {
			app.Tick(time.Now())
			if check(app.Snapshot()) {
				return
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatalf("%s: App=%+v service=%+v", name, app.Snapshot().Session, f.s.Status())
	}
	ready := func(s tenfoot.Snapshot) bool {
		return !s.Loading && len(s.Games) == 2 && s.Session.State == "idle" && f.s.Status().State == ProgramReady
	}
	playing := func(s tenfoot.Snapshot) bool {
		return s.Session.State == "active" && f.s.Status().State == ProgramRunning
	}
	wait("initial library", ready)
	owner := f.s.Status().Owner
	selectBuild(false)
	app.Press(tenfoot.CmdSelect, time.Now())
	wait("first build", playing)
	first := f.s.Status().Identity.PackageID
	app.Press(tenfoot.CmdStop, time.Now())
	wait("first return", ready)
	app.Press(tenfoot.CmdRight, time.Now())
	wait("second selection", func(s tenfoot.Snapshot) bool { return s.Grid.Focus == 1 })
	selectBuild(true)
	app.Press(tenfoot.CmdSelect, time.Now())
	wait("second build", playing)
	if state := f.s.Status(); state.Owner != owner || state.Identity.PackageID != secondPackage || first == secondPackage {
		t.Fatal("wrong second build or boot ownership changed", state)
	}

	output, err := exec.CommandContext(ctx, guard, "+mode="+mode).CombinedOutput()
	expected := "fault=1 drained=1 forwarded=1\n"
	if mode == "wedged" {
		expected = "fault=0 drained=0 forwarded=1\n"
	}
	if err != nil || !bytes.HasPrefix(output, []byte(expected)) {
		t.Fatalf("DMA guard model did not supply the expected outcome: %v: %s", err, output)
	}
	port.base.badClosure = mode == "wedged"
	if mode == "bad-dma" {
		port.dmaStatus = scanoutDMARejected
		platform := &ProgramMenuPlatform{service: f.s, inner: &programInputBase{}, dmaArmed: f.s.Status().Generation}
		frame, err := platform.Next(ctx)
		if err != nil || len(frame.Commands) != 1 || frame.Commands[0] != tenfoot.CmdStop {
			t.Fatal("live fault did not schedule the normal Tenfoot Stop", frame, err)
		}
		platform.dmaPollAt = time.Time{}
		frame, err = platform.Next(ctx)
		if err != nil || len(frame.Commands) != 0 {
			t.Fatal("sticky fault scheduled another Stop", frame, err)
		}
		port.dmaStatus = 0
	}
	app.Press(tenfoot.CmdStop, time.Now())
	if mode == "wedged" {
		wait("terminal return", func(_ tenfoot.Snapshot) bool { return f.s.Status().State == ProgramRebootRequired })
		terminal := f.s.Status()
		before := f.r.f.cpu.configurations
		client := hostclient.NewClient("http://native.invalid", &http.Client{Transport: backend})
		again, err := client.Launch(ctx, "spectrum48")
		if terminal.Owner != owner || f.r.f.lease.r.Active || (err == nil && again.ID != "") ||
			before != f.r.f.cpu.configurations || f.cpu.kind == programRoleMenu {
			t.Fatal("wedged master released a lease, rebuilt the menu or permitted another load", terminal, again, err)
		}
		return
	}
	wait("fault return to library", ready)
	if f.s.Status().Owner != owner || f.cpu.kind != programRoleMenu || f.r.f.lease.r.Active {
		t.Fatal("DMA fault did not restore the same owned menu", f.s.Status())
	}
	app.Press(tenfoot.CmdLeft, time.Now())
	wait("first selection", func(s tenfoot.Snapshot) bool { return s.Grid.Focus == 0 })
	selectBuild(false)
	app.Press(tenfoot.CmdSelect, time.Now())
	wait("post-fault relaunch", playing)
	if state := f.s.Status(); state.Owner != owner || state.Identity.PackageID != first {
		t.Fatal("post-fault launch did not preserve the boot and select the first package", state)
	}
	app.Press(tenfoot.CmdStop, time.Now())
	wait("post-fault return", ready)
	if err := backend.ParkMenu(ctx); err != nil || f.s.Status().State != ProgramParked {
		t.Fatal("post-fault Park failed", f.s.Status(), err)
	}
}
