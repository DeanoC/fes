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
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
	"github.com/DeanoC/fes/platform/embedded/tamago/runtimeadmission"
)

func TestProgramLoaderUsesCompiledDriverInventoryBeforeContainment(t *testing.T) {
	for _, test := range []struct {
		name, iface string
		required    bool
		minor       int
		code        string
	}{
		{"control", "fes.embedded.control", true, 0, "none"},
		{"optional-video", "fes.video.fixed-720p60", false, 0, "none"},
		{"required-video", "fes.video.fixed-720p60", true, 0, "unsupported_interface"},
		{"required-ROM-on-non-ROM-loader", "fes.embedded.rom", true, 0, "unsupported_interface"},
		{"new-control-version", "fes.embedded.control", true, 1, "unsupported_interface"},
	} {
		t.Run(test.name, func(t *testing.T) {
			loader, cpu, lease, files, store := programLoaderFixture(t)
			req, _, _ := addProgram(t, loader, files, 0, func(s string) string {
				return strings.Replace(s, "interfaces = []\n", "", 1) + fmt.Sprintf("\n[[interfaces]]\nid = %q\nmajor = 1\nminor = %d\nrequired = %t\n", test.iface, test.minor, test.required)
			})
			selection, err := store.Retain(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			calls, before := cpu.calls, lease.r
			plan, err := loader.Preflight(selection)
			if got := runtimeadmission.ErrorCode(err); got != test.code {
				t.Fatalf("got %s want %s: %v", got, test.code, err)
			}
			if (plan != nil) != (test.code == "none") || cpu.calls != calls || lease.r != before || !lease.r.Active {
				t.Fatal("preflight touched firmware/storage containment or returned a partial plan")
			}
		})
	}
}

func programJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var nested any
	if err = json.Unmarshal(raw, &nested); err != nil {
		t.Fatal(err)
	}
	raw, err = json.Marshal(nested)
	if err != nil {
		t.Fatal(err)
	}
	return append(raw, '\n')
}

type programTestFiles struct {
	files fstest.MapFS
	lease packagestore.Lease
	opens int
}

func (f *programTestFiles) Open(name string) (fs.File, error) {
	f.opens++
	r, ok := f.lease.LastReceipt()
	if !ok || !r.Active || !r.Prepared {
		return nil, fmt.Errorf("file access after SD containment")
	}
	return f.files.Open(name)
}

func programLoaderFixture(t *testing.T) (*ProgramLoader, *reloadCPU, *computerStorageLease, *programTestFiles, *packagestore.Store) {
	t.Helper()
	v, cpu, lease, _, _, control, config, comp, _ := storedComputer(t)
	c := v.context
	p := programPlatform{Format: 2, Scope: "nano-contained-program-platform-v2", Platform: c.Platform, Device: c.Device,
		ProgrammingProfile: c.ProgrammingProfile, ABI: c.ABI, Major: c.Major, Minor: c.Minor, ControlABI: c.ControlABI,
		HPSIOHash: c.HPSIOHash, PFGIdentity: c.PFGIdentity, HPSParametersSHA256: strings.Repeat("1", 64),
		HPSConfigurationSHA256: strings.Repeat("2", 64), MasterInventorySHA256: strings.Repeat("3", 64)}
	compiled := programJSON(t, programPlatformSource{1, "nano-program-platform-source-v1", hex.EncodeToString(v.digest[:]), p})
	l, err := NewProgramLoader(v, compiled, control, config, comp)
	if err != nil {
		t.Fatal(err)
	}
	files := &programTestFiles{files: fstest.MapFS{}, lease: lease}
	store, err := packagestore.New(files, lease)
	if err != nil {
		t.Fatal(err)
	}
	return l, cpu, lease, files, store
}

func addProgram(t *testing.T, l *ProgramLoader, files *programTestFiles, index int, edit ...func(string) string) (packagestore.Request, selectedProgramRecord, []byte) {
	t.Helper()
	// Actual canonical archives and validation, with synthetic bytes/identities.
	// These fixtures are never physical FPGA packages.
	bytesCount := []int{128, 192, 64}[index%3]
	data := bytes.Repeat([]byte{byte(index + 1)}, bytesCount)
	sum := sha256.Sum256(data)
	build := strings.Repeat(fmt.Sprint(index+1), 32)
	p := l.platform
	manifest := fmt.Sprintf(`format = 2
interfaces = []
[core]
id = "fes.selected-fixture-%d"
name = "Selected native fixture"
description = "Synthetic software fixture; never deploy."
version = "0.1.0"
[target]
platform = %q
device = %q
programming_profile = %q
[payload]
file = "core.rbf"
size = %d
sha256 = %q
[abi]
id = %q
major = %d
minor = %d
[build]
id = %q
repository = "https://example.invalid/selected-program"
revision = "1111111111111111111111111111111111111111"
recipe_sha256 = "2222222222222222222222222222222222222222222222222222222222222222"
toolchain = "software fixture; no FPGA build"
`, index, p.Platform, p.Device, p.ProgrammingProfile, len(data), hex.EncodeToString(sum[:]), p.ABI, p.Major, p.Minor, build)
	for _, change := range edit {
		manifest = change(manifest)
	}
	dir := t.TempDir()
	for name, raw := range map[string][]byte{"manifest.toml": []byte(manifest), "core.rbf": data} {
		if err := os.WriteFile(filepath.Join(dir, name), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	archive, err := corepackage.CanonicalArchive(dir)
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := corepackage.InspectPackage(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctrl, comp := l.control, l.computer
	ctrl.PackageID, ctrl.BuildID, ctrl.HandoffSHA256 = inspection.PackageID, build, strings.Repeat(fmt.Sprint(index+4), 64)
	ctrlRaw := programJSON(t, ctrl)
	controlHash := sha256.Sum256(ctrlRaw)
	comp.PackageID, comp.BuildID, comp.HandoffSHA256 = inspection.PackageID, build, ctrl.HandoffSHA256
	comp.ControlMapSHA256 = hex.EncodeToString(controlHash[:])
	record := selectedProgramRecord{1, "nano-selected-program-v1", inspection.PackageID, build, p, ctrl, comp}
	meta := programJSON(t, record)
	metaSum := sha256.Sum256(meta)
	request := packagestore.Request{Path: fmt.Sprintf("FES/P%d.BIN", index), PackageID: inspection.PackageID,
		MetadataPath: fmt.Sprintf("FES/P%d.JSN", index), MetadataSHA256: hex.EncodeToString(metaSum[:])}
	files.files[request.Path] = &fstest.MapFile{Data: archive}
	files.files[request.MetadataPath] = &fstest.MapFile{Data: meta}
	return request, record, data
}

func retainProgram(t *testing.T, store *packagestore.Store, files *programTestFiles, request packagestore.Request, record selectedProgramRecord) *packagestore.Selection {
	t.Helper()
	meta := programJSON(t, record)
	sum := sha256.Sum256(meta)
	files.files[request.MetadataPath] = &fstest.MapFile{Data: meta}
	request.MetadataSHA256 = hex.EncodeToString(sum[:])
	selected, err := store.Retain(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	return selected
}

func TestSelectedProgramsChangePackageBuildPayloadAndCoreAcrossOwnedReturns(t *testing.T) {
	runSelectedPrograms(t, "pass")
}

func TestSelectedProgramFailuresRetainOwnershipAndCannotReplay(t *testing.T) {
	for _, fault := range []string{"source", "publication", "write", "release", "open", "identity", "hold", "close"} {
		t.Run(fault, func(t *testing.T) { runSelectedPrograms(t, fault) })
	}
}

func runSelectedPrograms(t *testing.T, fault string) {
	t.Helper()
	l, cpu, lease, files, store := programLoaderFixture(t)
	anchor := l.boot.Identity()
	bootDigest := l.boot.digest
	arena, err := NewProgramArena(0x94800000, make([]byte, ProgramMetadataBytes+256), func() error {
		if fault == "release" && cpu.configurations == 2 {
			return fmt.Errorf("lost completion ordering")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var old *ComputerSession
	for cycle := 0; cycle < 3; cycle++ {
		request, record, data := addProgram(t, l, files, cycle)
		selected := retainProgram(t, store, files, request, record)
		calls, opens := cpu.calls, files.opens
		plan, err := l.Preflight(selected)
		if err != nil {
			t.Fatal(cycle, err)
		}
		if cpu.calls != calls || files.opens != opens || plan.Identity().PackageID != request.PackageID || plan.Identity().CoreID == anchor.CoreID {
			t.Fatal("preflight performed I/O or retained the boot core identity")
		}
		// Corrupt card/caller bytes after retention; the plan has no filesystem dependency.
		clear(files.files[request.Path].Data)
		clear(files.files[request.MetadataPath].Data)
		copyMeta := selected.Metadata()
		clear(copyMeta)
		handoff, err := store.Contain(selected)
		if err != nil {
			t.Fatal(err)
		}
		var transfer *ConfigurationTransfer
		if cycle == 1 && fault == "source" {
			cpu.digest[0] ^= 1
		}
		reserve := func(data []byte) (*Region, error) {
			r, err := arena.Reserve(data)
			if err == nil && cycle == 1 && fault == "publication" {
				publish := r.PublishConfiguration
				r.PublishConfiguration = func(data []byte) error {
					if err := publish(data); err != nil {
						return err
					}
					return fmt.Errorf("lost publication ordering")
				}
			}
			return r, err
		}
		if cycle == 0 {
			transfer, _, err = plan.NewTransfer(handoff, reserve)
		} else {
			transfer, _, err = plan.NewReconfigurationTransfer(handoff, reserve)
		}
		if cycle == 1 && fault == "source" {
			if err == nil || transfer != nil || plan.state.region != nil {
				t.Fatal("changed boot source reached reservation")
			}
			calls := cpu.calls
			if _, _, err = plan.NewReconfigurationTransfer(handoff, reserve); err == nil || cpu.calls != calls {
				t.Fatal("failed selected plan replayed")
			}
			return
		}
		if err != nil {
			t.Fatal(cycle, err)
		}
		cpu.wire = &transferWire{t: t, source: bytes.Clone(data), region: transfer.region}
		if cycle == 1 && fault == "write" {
			cpu.wire.fault = "write"
		}
		for step := 0; step < 30 && !transfer.Status().Done; step++ {
			if _, _, err = transfer.Advance(); err != nil {
				break
			}
		}
		if cycle == 1 && (fault == "write" || fault == "publication" || fault == "release") {
			if err == nil || !transfer.Status().Failed || transfer.Status().RetainedBytes != len(data) {
				t.Fatal("uncertain selected transfer released bytes", transfer.Status(), err)
			}
			calls, wireCalls := cpu.calls, len(cpu.wire.calls)
			if _, _, err = transfer.Advance(); err == nil || cpu.calls != calls || len(cpu.wire.calls) != wireCalls {
				t.Fatal("failed selected transfer replayed")
			}
			if _, err = l.OpenComputer(context.Background(), transfer, computerLimits()); err == nil || cpu.calls != calls {
				t.Fatal("failed selected transfer granted control")
			}
			if l.boot.ResumeStorageAfterReturn(store) == nil || lease.r.Active {
				t.Fatal("failed selected transfer granted SD")
			}
			return
		}
		if err != nil {
			t.Fatal(cycle, err)
		}
		if !transfer.Status().Done {
			t.Fatal("selected transfer incomplete")
		}
		header := arena.metadata[:ProgramHeaderBytes]
		sum384 := sha512.Sum384(data)
		if hex.EncodeToString(header[72:104]) != request.PackageID || hex.EncodeToString(header[104:120]) != record.BuildID ||
			binary.LittleEndian.Uint32(header[12:16]) != uint32(len(data)) ||
			binary.LittleEndian.Uint64(header[24:32]) != 17 || binary.LittleEndian.Uint64(header[32:40]) != uint64(4+5*cycle) ||
			!bytes.Equal(header[40:72], bootDigest[:]) || !bytes.Equal(header[152:200], sum384[:]) {
			t.Fatal("published descriptor did not bind the selected program to unchanged boot ownership")
		}
		id, ok := transfer.ProgramIdentity()
		if !ok || id != plan.Identity() {
			t.Fatal(id)
		}
		build, _ := hex.DecodeString(record.BuildID)
		for i := range 8 {
			cpu.words[8+i] = uint16(build[2*i]) | uint16(build[2*i+1])<<8
		}
		if old != nil && old.Computer().Run(context.Background()) == nil {
			t.Fatal("old program retained control")
		}
		if cycle == 1 {
			if fault == "identity" {
				cpu.words[8] ^= 1
			}
			if fault == "open" {
				cpu.loseOpen = true
			}
		}
		otherLoader := *l
		callsBeforeWrongLoader := cpu.calls
		if _, err := otherLoader.OpenComputer(context.Background(), transfer, computerLimits()); err == nil || cpu.calls != callsBeforeWrongLoader {
			t.Fatal("copied loader opened another loader's transfer")
		}
		session, err := l.OpenComputer(context.Background(), transfer, computerLimits())
		if cycle == 1 && (fault == "identity" || fault == "open") {
			if err == nil || session != nil {
				t.Fatal("failed selected opening accepted")
			}
			calls := cpu.calls
			if _, err = l.OpenComputer(context.Background(), transfer, computerLimits()); err == nil || cpu.calls != calls {
				t.Fatal("failed selected opening replayed")
			}
			if l.boot.ResumeStorageAfterReturn(store) == nil || lease.r.Active {
				t.Fatal("failed selected opening granted SD")
			}
			return
		}
		if err != nil {
			t.Fatal(cycle, err)
		}
		if session.Computer().Run(context.Background()) != nil {
			t.Fatal("selected Run rejected")
		}
		if cycle == 1 {
			if fault == "hold" {
				cpu.stall = true
			}
			if fault == "close" {
				cpu.loseClose = true
			}
		}
		closed, err := session.Close(context.Background())
		if cycle == 1 && (fault == "hold" || fault == "close") {
			if err == nil {
				t.Fatal("failed selected return accepted")
			}
			calls, grants := cpu.calls, lease.returns
			if _, err = session.Close(context.Background()); err == nil || cpu.calls != calls {
				t.Fatal("failed selected return replayed")
			}
			if l.boot.ResumeStorageAfterReturn(store) == nil || cpu.calls != calls || lease.returns != grants {
				t.Fatal("failed selected return granted SD")
			}
			return
		}
		if err != nil {
			t.Fatal(cycle, err)
		}
		if closed.After.Generation != uint64(6+5*cycle) || l.boot.Identity() != anchor || l.boot.digest != bootDigest || cpu.digest != bootDigest {
			t.Fatal("selected program replaced boot identity or lost owned return", closed)
		}
		if _, err = l.OpenComputer(context.Background(), transfer, computerLimits()); err == nil {
			t.Fatal("program completion replayed")
		}
		old = session
		if cycle < 2 && l.boot.ResumeStorageAfterReturn(store) != nil {
			t.Fatal("selected return did not resume SD")
		}
	}
	if cpu.configurations != 3 || cpu.opens != 3 || cpu.closes != 3 || lease.returns != 2 {
		t.Fatal(cpu.configurations, cpu.opens, cpu.closes, lease.returns)
	}
}

func TestSelectedProgramIncompatibilityLeavesStorageActiveBeforeFirmwareOrReservation(t *testing.T) {
	for _, fault := range []string{"missing", "package", "build", "device", "io", "pfg", "memory", "handoff", "inventory", "abi", "control-address", "control-stop", "computer-address", "computer-build", "computer-master", "computer-handoff", "control-digest", "capabilities", "unknown-field", "duplicate-field", "missing-false", "oversized"} {
		t.Run(fault, func(t *testing.T) {
			l, cpu, lease, files, store := programLoaderFixture(t)
			request, r, _ := addProgram(t, l, files, 0)
			switch fault {
			case "package":
				r.PackageID = strings.Repeat("a", 64)
			case "build":
				r.BuildID = strings.Repeat("b", 32)
			case "device":
				r.Platform.Device = "other"
			case "io":
				r.Platform.HPSIOHash = strings.Repeat("a", 64)
			case "pfg":
				r.Platform.PFGIdentity = "other"
			case "memory":
				r.Platform.HPSParametersSHA256 = strings.Repeat("a", 64)
			case "handoff":
				r.Platform.HPSConfigurationSHA256 = strings.Repeat("a", 64)
			case "inventory":
				r.Platform.MasterInventorySHA256 = strings.Repeat("a", 64)
			case "abi":
				r.Platform.Major++
			case "control-address":
				r.Control.CPUBase += 4
			case "control-stop":
				r.Control.FabricStop = &hpsFabricStopMap{"unknown", 0, 0, "unknown"}
			case "computer-address":
				r.Computer.CPUBase += 4
			case "computer-build":
				r.Computer.BuildID = strings.Repeat("b", 32)
			case "computer-master":
				r.Computer.HPSMaster = true
			case "computer-handoff":
				r.Computer.HandoffSHA256 = strings.Repeat("a", 64)
			case "control-digest":
				r.Computer.ControlMapSHA256 = strings.Repeat("a", 64)
			case "capabilities":
				r.Computer.Capabilities++
			}
			meta := programJSON(t, r)
			switch fault {
			case "unknown-field":
				meta = bytes.Replace(meta, []byte(`"format":1`), []byte(`"extra":0,"format":1`), 1)
			case "duplicate-field":
				meta = bytes.Replace(meta, []byte(`"format":1`), []byte(`"format":1,"format":1`), 1)
			case "missing-false":
				meta = bytes.Replace(meta, []byte(`"hps_master":false,`), nil, 1)
			case "oversized":
				meta = bytes.Repeat([]byte{' '}, 16<<10+1)
			}
			sum := sha256.Sum256(meta)
			request.MetadataSHA256 = hex.EncodeToString(sum[:])
			files.files[request.MetadataPath] = &fstest.MapFile{Data: meta}
			if fault == "missing" {
				request.MetadataPath = ""
				request.MetadataSHA256 = ""
			}
			selected, err := store.Retain(context.Background(), request)
			if fault == "oversized" {
				if err == nil {
					t.Fatal("oversized metadata retained")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			calls, opens := cpu.calls, files.opens
			if plan, err := l.Preflight(selected); err == nil || plan != nil {
				t.Fatal("incompatible selection accepted")
			}
			if cpu.calls != calls || files.opens != opens || !lease.r.Active || !lease.r.Prepared || lease.r.Generation != 3 {
				t.Fatal("rejection mutated storage or contacted firmware")
			}
		})
	}
}

func TestCompiledProgramPlatformCannotComeFromDifferentBoot(t *testing.T) {
	l, cpu, _, _, _ := programLoaderFixture(t)
	source := programPlatformSource{1, "nano-program-platform-source-v1", hex.EncodeToString(l.boot.digest[:]), l.platform}
	control := programJSON(t, l.control)
	config := programJSON(t, l.configuration)
	comp := programJSON(t, l.computer)
	for _, fault := range []string{"boot", "parameters", "io", "abi", "legacy-handoff", "configuration", "unknown", "missing"} {
		t.Run(fault, func(t *testing.T) {
			r := source
			switch fault {
			case "boot":
				r.BootContextSHA256 = strings.Repeat("a", 64)
			case "parameters":
				r.Platform.HPSParametersSHA256 = ""
			case "io":
				r.Platform.HPSIOHash = strings.Repeat("b", 64)
			case "abi":
				r.Platform.ControlABI++
			case "legacy-handoff":
				r.Platform.Format, r.Platform.Scope = 1, "nano-contained-program-platform-v1"
			case "configuration":
				r.Platform.HPSConfigurationSHA256 = ""
			}
			raw := programJSON(t, r)
			if fault == "unknown" {
				raw = bytes.Replace(raw, []byte(`"format":1`), []byte(`"extra":0,"format":1`), 1)
			}
			if fault == "missing" {
				var fields map[string]any
				_ = json.Unmarshal(raw, &fields)
				delete(fields, "boot_context_sha256")
				raw = programJSON(t, fields)
			}
			calls := cpu.calls
			if next, err := NewProgramLoader(l.boot, raw, control, config, comp); err == nil || next != nil || cpu.calls != calls {
				t.Fatal("invalid compiled platform accepted or performed firmware I/O")
			}
		})
	}
}

func TestSelectedProgramUsesSealedProducerExport(t *testing.T) {
	export, anchor := os.Getenv("FES_NANO_SELECTED_EXPORT"), os.Getenv("FES_NANO_SELECTED_ANCHOR")
	if export == "" || anchor == "" {
		t.Skip("provide an explicit sealed-project export and its matched native SDK anchor")
	}
	read := func(base, name string) []byte {
		raw, err := os.ReadFile(filepath.Join(base, name))
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	contextRaw := read(anchor, "hps-context.json")
	cpu := &computerCPU{digest: sha256.Sum256(contextRaw)}
	v, err := NewHPSContextVerifier(cpu, contextRaw)
	if err != nil {
		t.Fatal(err)
	}
	l, err := NewProgramLoader(v, read(export, "program-platform.json"), read(anchor, "hps-control.json"),
		read(anchor, "hps-configuration.json"), read(anchor, "hps-computer.json"))
	if err != nil {
		t.Fatal(err)
	}
	metadata := read(export, "program.json")
	sum := sha256.Sum256(metadata)
	lease := &storedLease{}
	lease.receipt.Owner, lease.receipt.Generation, lease.receipt.Active, lease.receipt.Prepared = 17, 3, true, true
	store, err := packagestore.New(fstest.MapFS{
		"FES/CORE.BIN":    &fstest.MapFile{Data: read(anchor, "package.fcore")},
		"FES/PROGRAM.JSN": &fstest.MapFile{Data: metadata},
	}, lease)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := store.Retain(context.Background(), packagestore.Request{Path: "FES/CORE.BIN", PackageID: v.context.PackageID,
		MetadataPath: "FES/PROGRAM.JSN", MetadataSHA256: hex.EncodeToString(sum[:])})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := l.Preflight(selection)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Identity() != v.Identity() || cpu.calls != 0 || !lease.receipt.Active || plan.state.program.configuration.CoreSHA256 != v.context.CoreSHA256 {
		t.Fatal("sealed exporter changed identity, touched firmware or lost actual archive binding")
	}
	t.Logf("PASS actual sealed FPGA package through native selected preflight: package=%s bytes=%d; source compatibility only, no hardware admission", plan.Identity().PackageID, len(plan.state.payload))
}

func TestSelectedCatalogueUsesDistinctSealedPackagesWithOneBootAnchor(t *testing.T) {
	catalogue, anchor := os.Getenv("FES_NANO_SELECTED_CATALOGUE"), os.Getenv("FES_NANO_SELECTED_ANCHOR")
	if catalogue == "" || anchor == "" {
		t.Skip("provide a two-package sealed catalogue and its matched native SDK anchor")
	}
	readAnchor := func(name string) []byte {
		raw, err := os.ReadFile(filepath.Join(anchor, name))
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	contextRaw := readAnchor("hps-context.json")
	cpu := &computerCPU{digest: sha256.Sum256(contextRaw)}
	v, err := NewHPSContextVerifier(cpu, contextRaw)
	if err != nil {
		t.Fatal(err)
	}
	l, err := NewProgramLoader(v, readAnchor("program-platform.json"), readAnchor("hps-control.json"),
		readAnchor("hps-configuration.json"), readAnchor("hps-computer.json"))
	if err != nil {
		t.Fatal(err)
	}
	files := os.DirFS(catalogue)
	choices, err := ReadProgramSelections(context.Background(), files, "FES/CORESEL.JSN")
	if err != nil || len(choices) != 2 || choices[0].PackageID == choices[1].PackageID {
		t.Fatalf("two distinct sealed selections required: %v %v", choices, err)
	}
	lease := &storedLease{}
	lease.receipt.Owner, lease.receipt.Generation, lease.receipt.Active, lease.receipt.Prepared = 17, 3, true, true
	store, err := packagestore.New(files, lease)
	if err != nil {
		t.Fatal(err)
	}
	var previous *StoredConfigurationPlan
	for _, index := range []int{0, 1, 0} {
		selection, err := store.Retain(context.Background(), choices[index])
		if err != nil {
			t.Fatal(err)
		}
		plan, err := l.Preflight(selection)
		if err != nil {
			t.Fatal(err)
		}
		i := plan.Identity()
		payloadHash := sha256.Sum256(plan.state.payload)
		if i.PackageID != choices[index].PackageID || cpu.calls != 0 || !lease.receipt.Active ||
			hex.EncodeToString(payloadHash[:]) != i.ProgrammedSHA256 {
			t.Fatal("sealed catalogue lost retained identity, touched firmware or changed SD ownership")
		}
		if previous != nil && (i.BuildID == previous.Identity().BuildID ||
			i.ProgrammedSHA256 == previous.Identity().ProgrammedSHA256) {
			t.Fatal("catalogue entries relabel identical builds or FPGA payloads")
		}
		t.Logf("PASS real sealed catalogue index=%d package=%s build=%s payload=%s; preflight only", index, i.PackageID, i.BuildID, i.ProgrammedSHA256)
		previous = plan
	}
}

func TestSelectedArchiveCannotHideForeignRequirementsBehindMatchingMetadata(t *testing.T) {
	for _, fault := range []string{"abi", "version", "profile", "required-interface"} {
		t.Run(fault, func(t *testing.T) {
			l, cpu, lease, files, store := programLoaderFixture(t)
			edit := func(manifest string) string {
				switch fault {
				case "abi":
					return strings.Replace(manifest, `id = "`+l.platform.ABI+`"`, `id = "fes.other-abi"`, 1)
				case "version":
					return strings.Replace(manifest, "major = 1", "major = 2", 1)
				case "profile":
					return strings.Replace(manifest, l.platform.ProgrammingProfile, "de25-foreign-profile-v1", 1)
				default:
					return strings.Replace(manifest, "interfaces = []", "[[interfaces]]\nid = \"fes.unsupported\"\nmajor = 1\nminor = 0\nrequired = true", 1)
				}
			}
			request, record, _ := addProgram(t, l, files, 0, edit)
			selection := retainProgram(t, store, files, request, record)
			calls := cpu.calls
			if plan, err := l.Preflight(selection); err == nil || plan != nil || cpu.calls != calls || !lease.r.Active {
				t.Fatal("foreign package header bypassed native preflight")
			}
		})
	}
}
