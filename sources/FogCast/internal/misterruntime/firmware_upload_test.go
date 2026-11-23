package misterruntime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
)

// de25SpectrumResponse rewrites the DE25 standard ZX81 status into a running
// Spectrum whose firmware the runtime uploaded: a format-3 package, the
// firmware-upload interface in the computer ABI, the receipt, and no
// rom_linking capability. upload=false leaves the interface out of the ABI.
func de25SpectrumResponse(t *testing.T, upload bool) (string, corepackage.ROMLinkIdentity) {
	t.Helper()
	var root map[string]any
	if err := json.Unmarshal([]byte(currentDE25Fixture(t, fixtureLines(t, "protocol-v2-de25-standard-computer.jsonl")[0])), &root); err != nil {
		t.Fatal(err)
	}
	iface := func(id string, major, minor int) map[string]any {
		return map[string]any{"id": id, "major": major, "minor": minor}
	}
	// Ascending identities, as the runtime reports them.
	live := []any{iface("fes.audio.pcm-s16-stereo-48k", 1, 0), iface("fes.fabric.de25-scanout", 1, 3),
		iface("fes.fabric.firmware-upload", 1, 0), iface("fes.fabric.video.display-plane", 1, 0),
		iface("fes.gamepad.ports", 1, 0), iface("fes.keyboard.hid", 1, 0), iface("fes.media.spectrum-tape", 2, 0),
		iface("fes.video.fixed-720p60", 1, 0), iface("fes.video.session-display", 1, 0)}
	supported := live
	if !upload {
		supported = append(append([]any{}, live[:2]...), live[3:]...)
	}
	capabilities := root["capabilities"].(map[string]any)
	capabilities["abis"].([]any)[1].(map[string]any)["interfaces"] = supported
	capabilities["active_interfaces"] = live
	capabilities["media_units"] = []any{map[string]any{"unit": 0, "interface": iface("fes.media.spectrum-tape", 2, 0),
		"min_bytes": protocol.SpectrumTapeMinBytes, "max_bytes": protocol.SpectrumTapeMaxBytes, "chunk_bytes": 512, "state": "empty"}}
	root["core"] = "fes.spectrum"
	active := root["active_package"].(map[string]any)
	descriptor := active["descriptor"].(map[string]any)
	descriptor["format"] = 3
	descriptor["core"].(map[string]any)["id"] = "fes.spectrum"
	var declared []any
	for _, id := range []string{"fes.video.fixed-720p60", "fes.keyboard.hid", "fes.gamepad.ports", "fes.audio.pcm-s16-stereo-48k",
		"fes.video.session-display", "fes.fabric.video.display-plane"} {
		declared = append(declared, map[string]any{"id": id, "major": 1, "minor": 0, "required": true})
	}
	declared = append(declared, map[string]any{"id": "fes.fabric.de25-scanout", "major": 1, "minor": 3, "required": true},
		map[string]any{"id": "fes.media.spectrum-tape", "major": 2, "minor": 0, "required": true},
		map[string]any{"id": "fes.fabric.firmware-upload", "major": 1, "minor": 0, "required": true})
	descriptor["interfaces"] = declared
	payload := descriptor["payload"].(map[string]any)
	link := corepackage.ROMLinkIdentity{ROMID: "machine-rom", Role: "firmware", MapSHA256: strings.Repeat("a", 64),
		SourceSHA256: strings.Repeat("b", 64), SourceSize: 16384,
		ProgrammedSHA256: payload["sha256"].(string), ProgrammedSize: int64(payload["size"].(float64))}
	descriptor["rom"] = map[string]any{"id": link.ROMID, "role": link.Role, "source_size": link.SourceSize,
		"file": "rom-map.json", "size": 178, "sha256": link.MapSHA256}
	active["rom_link"] = map[string]any{"rom_id": link.ROMID, "map_sha256": link.MapSHA256, "source_sha256": link.SourceSHA256,
		"source_size": link.SourceSize, "programmed_sha256": link.ProgrammedSHA256, "programmed_size": link.ProgrammedSize}
	encoded, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded), link
}

// A DE25 runtime reports no linker. An active firmware-upload package is
// valid there because its computer ABI lists the upload interface, and only
// with a receipt naming the unpatched payload.
func TestFirmwareUploadStatusNeedsTheInterfaceNotALinker(t *testing.T) {
	line, link := de25SpectrumResponse(t, true)
	response, err := decodeProtocol2Response([]byte(line))
	if err != nil || response.ActivePackage == nil || response.ActivePackage.ROMLink == nil ||
		*response.ActivePackage.ROMLink != link || response.Capabilities.ROMLinking != 0 ||
		!corepackage.FirmwareUpload(response.ActivePackage.Descriptor) {
		t.Fatalf("DE25 firmware-upload status refused: %v", err)
	}
	status := computerMediaStatus(response)
	binding := protocol.MediaUnitBinding{PackageID: response.ActivePackage.PackageID, Generation: *response.Generation, Unit: protocol.SpectrumTapeUnit}
	if !protocol.SessionDisplayCapable(status.CorePackage) || !binding.AcceptsSize(status, protocol.SpectrumTapeMaxBytes) ||
		binding.AcceptsSize(status, protocol.SpectrumTapeMaxBytes+1) {
		t.Fatal("DE25 Spectrum tape or session display differs from the shared computer contract")
	}
	without, _ := de25SpectrumResponse(t, false)
	var unbound map[string]any
	if err := json.Unmarshal([]byte(line), &unbound); err != nil {
		t.Fatal(err)
	}
	delete(unbound["active_package"].(map[string]any), "rom_link")
	noReceipt, err := json.Marshal(unbound)
	if err != nil {
		t.Fatal(err)
	}
	for name, bad := range map[string]string{
		"interface absent from the ABI": without,
		"patched programmed digest":     strings.Replace(line, `"programmed_sha256":"`+link.ProgrammedSHA256, `"programmed_sha256":"`+strings.Repeat("c", 64), 1),
		"patched programmed size":       strings.Replace(line, `"programmed_size":12`, `"programmed_size":13`, 1),
		"no receipt":                    string(noReceipt),
	} {
		if bad == line {
			t.Fatal("fixture edit did not apply:", name)
		}
		if _, err := decodeProtocol2Response([]byte(bad)); !errors.Is(err, errInvalidRuntimeResponse) {
			t.Errorf("%s accepted: %v", name, err)
		}
	}
}

func TestFirmwareUploadClientSendsTheSourcePath(t *testing.T) {
	line, link := de25SpectrumResponse(t, true)
	active, err := decodeProtocol2Response([]byte(line))
	if err != nil {
		t.Fatal(err)
	}
	id := active.ActivePackage.PackageID
	for _, root := range []string{"", "/tmp/data"} {
		fixture := newSequenceSocketFixture(t, []string{line + "\n", line + "\n"})
		response, err := NewClient(fixture.path).LoadFirmwareCore(context.Background(), "/tmp/package", id, root, "/tmp/firmware.rom", link, nil)
		if err != nil || response.ActivePackage == nil || *response.ActivePackage.ROMLink != link {
			t.Fatalf("round trip: %+v %v", response, err)
		}
		requests := fixture.wait(t)
		var request map[string]json.RawMessage
		if err := json.Unmarshal([]byte(requests[1]), &request); err != nil {
			t.Fatal(err)
		}
		want := map[string]string{"protocol": "2", "operation": `"load_rom_core"`, "package_path": `"/tmp/package"`,
			"package_id": `"` + id + `"`, "firmware_path": `"/tmp/firmware.rom"`}
		if root != "" {
			want["operation"], want["data_root"] = `"load_rom_library_core"`, `"/tmp/data"`
		}
		var sent protocol2ROMLink
		if err := json.Unmarshal(request["rom_link"], &sent); err != nil || sent != protocol2ROMLinkFromIdentity(link) || len(request) != len(want)+1 {
			t.Fatalf("request shape: %s %v", requests[1], err)
		}
		for key, value := range want {
			if string(request[key]) != value {
				t.Fatalf("%s = %s, want %s", key, request[key], value)
			}
		}
	}
	// Another source in the reply is not this launch.
	wrong := strings.Replace(line, link.SourceSHA256, strings.Repeat("f", 64), 1)
	fixture := newSequenceSocketFixture(t, []string{line + "\n", wrong + "\n"})
	if _, err = NewClient(fixture.path).LoadFirmwareCore(context.Background(), "/tmp/package", id, "", "/tmp/firmware.rom", link, nil); err == nil {
		t.Fatal("accepted a reply naming another ROM")
	}
	fixture.wait(t)
}

func TestFirmwareUploadClientRefusesBeforeMutation(t *testing.T) {
	line, link := de25SpectrumResponse(t, true)
	without, _ := de25SpectrumResponse(t, false)
	active, err := decodeProtocol2Response([]byte(line))
	if err != nil {
		t.Fatal(err)
	}
	id := active.ActivePackage.PackageID
	// The ZX81-only DE25 runtime and a Cyclone V linker both lack the interface.
	for name, status := range map[string]string{
		"DE25 without upload": currentDE25Fixture(t, fixtureLines(t, "protocol-v2-de25-standard-computer.jsonl")[0]),
		"linker":              fixtureLines(t, "protocol-v2-rom-package-responses.jsonl")[1],
	} {
		fixture := newSequenceSocketFixture(t, []string{status + "\n"})
		if _, err := NewClient(fixture.path).LoadFirmwareCore(context.Background(), "/tmp/package", id, "", "/tmp/firmware.rom", link, nil); err == nil {
			t.Fatal("firmware load sent to a runtime without the interface:", name)
		}
		if requests := fixture.wait(t); len(requests) != 1 || requests[0] != `{"protocol":2,"operation":"status"}` {
			t.Fatalf("%s mutated without capability: %v", name, requests)
		}
	}
	// A linked bitstream is not accepted where only upload is offered.
	fixture := newSequenceSocketFixture(t, []string{without + "\n"})
	if _, err := NewClient(fixture.path).LoadROMLinkedCore(context.Background(), "/tmp/package", id, "", "", "", nil, "/tmp/programmed.rbf", link); err == nil {
		t.Fatal("linked load sent to a runtime with no linker")
	}
	if requests := fixture.wait(t); len(requests) != 1 {
		t.Fatalf("mutated without a linker: %v", requests)
	}
	cartridge := link
	cartridge.Role = "cartridge"
	for name, call := range map[string]func(*Client) error{
		"relative source path": func(c *Client) error {
			_, err := c.LoadFirmwareCore(context.Background(), "/tmp/package", id, "", "firmware.rom", link, nil)
			return err
		},
		"no source path": func(c *Client) error {
			_, err := c.LoadFirmwareCore(context.Background(), "/tmp/package", id, "", "", link, nil)
			return err
		},
		"cartridge role": func(c *Client) error {
			_, err := c.LoadFirmwareCore(context.Background(), "/tmp/package", id, "", "/tmp/firmware.rom", cartridge, nil)
			return err
		},
	} {
		if err := call(NewClient("/nonexistent/fes-runtime.sock")); !errors.Is(err, errInvalidRuntimeRequest) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// de25PCXTResponse is the DE25 PC/XT running after a firmware upload, as the
// runtime reports it: a fes.computer 1.1 package (the PC/XT floppy is a 1.1
// interface), the 64 KiB system ROM plus 4 KiB CGA font as one image, and the
// empty writable 360 KiB floppy unit. abiMinor sets the declared and observed
// computer minor.
func de25PCXTResponse(t *testing.T, abiMinor int) (string, corepackage.ROMLinkIdentity) {
	t.Helper()
	var root map[string]any
	if err := json.Unmarshal([]byte(fixtureLines(t, "protocol-v2-de25-standard-computer.jsonl")[0]), &root); err != nil {
		t.Fatal(err)
	}
	iface := func(id string, major, minor int) map[string]any {
		return map[string]any{"id": id, "major": major, "minor": minor}
	}
	// Ascending identities, as the runtime reports them.
	live := []any{iface("fes.audio.pcm-s16-stereo-48k", 1, 0), iface("fes.fabric.de25-scanout", 1, 3),
		iface("fes.fabric.firmware-upload", 1, 0), iface("fes.fabric.video.display-plane", 1, 0),
		iface("fes.gamepad.ports", 1, 0), iface("fes.keyboard.hid", 1, 0),
		iface("fes.media.pcxt-floppy", 1, 0), iface("fes.media.pcxt-floppy-write", 1, 0),
		iface("fes.video.fixed-720p60", 1, 0), iface("fes.video.session-display", 1, 0)}
	capabilities := root["capabilities"].(map[string]any)
	computer := capabilities["abis"].([]any)[1].(map[string]any)
	computer["minor"], computer["interfaces"] = 1, live
	capabilities["active_interfaces"] = live
	capabilities["media_units"] = []any{map[string]any{"unit": 0, "interface": iface("fes.media.pcxt-floppy", 1, 0),
		"min_bytes": protocol.PcxtFloppyBytes, "max_bytes": protocol.PcxtFloppyBytes, "chunk_bytes": 512, "state": "empty"}}
	root["core"] = "fes.pcxt-dos"
	active := root["active_package"].(map[string]any)
	active["observed"].(map[string]any)["abi"].(map[string]any)["minor"] = abiMinor
	descriptor := active["descriptor"].(map[string]any)
	descriptor["format"] = 3
	descriptor["abi"].(map[string]any)["minor"] = abiMinor
	descriptor["core"].(map[string]any)["id"] = "fes.pcxt-dos"
	var declared []any
	for _, id := range []string{"fes.video.fixed-720p60", "fes.keyboard.hid", "fes.gamepad.ports", "fes.audio.pcm-s16-stereo-48k",
		"fes.video.session-display", "fes.fabric.video.display-plane", "fes.media.pcxt-floppy", "fes.media.pcxt-floppy-write",
		"fes.fabric.firmware-upload"} {
		declared = append(declared, map[string]any{"id": id, "major": 1, "minor": 0, "required": true})
	}
	declared = append(declared, map[string]any{"id": "fes.fabric.de25-scanout", "major": 1, "minor": 3, "required": true})
	descriptor["interfaces"] = declared
	payload := descriptor["payload"].(map[string]any)
	link := corepackage.ROMLinkIdentity{ROMID: "pcxt-firmware", Role: "firmware", MapSHA256: strings.Repeat("a", 64),
		SourceSHA256: strings.Repeat("b", 64), SourceSize: 69632,
		ProgrammedSHA256: payload["sha256"].(string), ProgrammedSize: int64(payload["size"].(float64))}
	descriptor["rom"] = map[string]any{"id": link.ROMID, "role": link.Role, "source_size": link.SourceSize,
		"file": "rom-map.json", "size": 177, "sha256": link.MapSHA256}
	active["rom_link"] = map[string]any{"rom_id": link.ROMID, "map_sha256": link.MapSHA256, "source_sha256": link.SourceSHA256,
		"source_size": link.SourceSize, "programmed_sha256": link.ProgrammedSHA256, "programmed_size": link.ProgrammedSize}
	encoded, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded), link
}

// The PC/XT runs fes.computer 1.1 on the DE25 computer profile. Its launch
// reply is accepted with exactly that contract, the one package admission
// requires, and refused at 1.0, as a 1.0 machine is refused at 1.1.
func TestFirmwareUploadPCXTRunsItsComputerMinor(t *testing.T) {
	line, link := de25PCXTResponse(t, 1)
	active, err := decodeProtocol2Response([]byte(line))
	if err != nil {
		t.Fatalf("DE25 PC/XT status refused: %v", err)
	}
	floppy := protocol.MediaUnitBinding{PackageID: active.ActivePackage.PackageID, Generation: *active.Generation, Unit: protocol.PcxtFloppyUnit}
	if status := computerMediaStatus(active); !floppy.AcceptsSize(status, protocol.PcxtFloppyBytes) || floppy.AcceptsSize(status, protocol.PcxtFloppyBytes-512) {
		t.Fatal("PC/XT floppy unit differs from its exact 360 KiB contract")
	}
	fixture := newSequenceSocketFixture(t, []string{line + "\n", line + "\n"})
	response, err := NewClient(fixture.path).LoadFirmwareCore(context.Background(), "/tmp/package", active.ActivePackage.PackageID, "", "/tmp/firmware.rom", link, nil)
	if err != nil || response.ActivePackage == nil || *response.ActivePackage.ROMLink != link {
		t.Fatalf("PC/XT firmware launch reply refused: %+v %v", response, err)
	}
	fixture.wait(t)
	if old, _ := de25PCXTResponse(t, 0); decodeOK(old) {
		t.Fatal("PC/XT accepted at fes.computer 1.0")
	}
	spectrum, _ := de25SpectrumResponse(t, true)
	var root map[string]any
	if err := json.Unmarshal([]byte(spectrum), &root); err != nil {
		t.Fatal(err)
	}
	active1 := root["active_package"].(map[string]any)
	active1["descriptor"].(map[string]any)["abi"].(map[string]any)["minor"] = 1
	active1["observed"].(map[string]any)["abi"].(map[string]any)["minor"] = 1
	root["capabilities"].(map[string]any)["abis"].([]any)[1].(map[string]any)["minor"] = 1
	raised, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	if decodeOK(string(raised)) {
		t.Fatal("Spectrum accepted at fes.computer 1.1")
	}
}

func decodeOK(line string) bool {
	_, err := decodeProtocol2Response([]byte(line))
	return err == nil
}
