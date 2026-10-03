package misterruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
)

// computerResponse is a synthetic protocol-2 status for an active
// fes.computer 1.0 Apple II package, shaped as the runtime spec describes.
func computerResponse(t *testing.T) Protocol2Response {
	t.Helper()
	var r Protocol2Response
	if err := json.Unmarshal([]byte(fixtureLines(t, "protocol-v2.jsonl")[5]), &r); err != nil {
		t.Fatal(err)
	}
	active := r.ActivePackage
	active.Descriptor.Core.ID = "fes.apple2"
	active.Descriptor.ABI.ID = "fes.computer"
	active.Observed.ABI.ID = "fes.computer"
	active.Descriptor.Interfaces = []corepackage.Interface{
		{ID: "fes.video.fixed-720p60", Major: 1, Required: true}, {ID: "fes.keyboard.hid", Major: 1, Required: true},
		{ID: "fes.gamepad.ports", Major: 1, Required: true}, {ID: "fes.audio.pcm-s16-stereo-48k", Major: 1, Required: true},
		{ID: "fes.media.apple2-floppy", Major: 1, Required: true}, {ID: "fes.expansion.apple2-bus", Major: 1},
	}
	supported := []Protocol2Interface{{ID: "fes.audio.pcm-s16-stereo-48k", Major: 1}, {ID: "fes.expansion.apple2-bus", Major: 1},
		{ID: "fes.gamepad.ports", Major: 1}, {ID: "fes.keyboard.hid", Major: 1}, {ID: "fes.media.apple2-floppy", Major: 1},
		{ID: "fes.video.fixed-720p60", Major: 1}}
	r.Capabilities.ABIs = []Protocol2ABI{{ID: "fes.computer", Major: 1, Interfaces: supported}}
	r.Capabilities.ActiveInterfaces = append([]Protocol2Interface(nil), supported...)
	r.Capabilities.MediaUnits = []protocol.MediaUnitStatus{{Unit: 0, Interface: protocol.Apple2FloppyInterface(),
		MinBytes: 143360, MaxBytes: 143360, ChunkBytes: 512, State: "empty"}}
	generation := uint64(7)
	r.Generation = &generation
	core := "fes.apple2"
	r.Core = &core
	return r
}

func atariStComputerResponse(t *testing.T) Protocol2Response {
	t.Helper()
	r := computerResponse(t)
	r.ActivePackage.Descriptor.Core.ID = "fes.atari-st"
	for i := range r.ActivePackage.Descriptor.Interfaces {
		switch r.ActivePackage.Descriptor.Interfaces[i].ID {
		case "fes.media.apple2-floppy":
			r.ActivePackage.Descriptor.Interfaces[i].ID = protocol.AtariStFloppyInterface().ID
		case "fes.expansion.apple2-bus":
			r.ActivePackage.Descriptor.Interfaces[i].ID = "fes.expansion.atari-st-bus"
		}
	}
	for _, interfaces := range [][]Protocol2Interface{r.Capabilities.ActiveInterfaces, r.Capabilities.ABIs[0].Interfaces} {
		for i := range interfaces {
			switch interfaces[i].ID {
			case "fes.media.apple2-floppy":
				interfaces[i].ID = protocol.AtariStFloppyInterface().ID
			case "fes.expansion.apple2-bus":
				interfaces[i].ID = "fes.expansion.atari-st-bus"
			}
		}
	}
	r.Capabilities.MediaUnits[0] = protocol.MediaUnitStatus{Interface: protocol.AtariStFloppyInterface(),
		MinBytes: uint32(protocol.AtariStFloppyBytes), MaxBytes: uint32(protocol.AtariStFloppyBytes), ChunkBytes: 512, State: "empty"}
	*r.Core = "fes.atari-st"
	return r
}

func TestAtariStStatusRequiresExactUniqueActiveMediaUnit(t *testing.T) {
	r := atariStComputerResponse(t)
	if _, err := decodeProtocol2Response([]byte(responseLine(t, r))); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Protocol2Response){
		func(r *Protocol2Response) { r.Capabilities.MediaUnits[0].MinBytes-- },
		func(r *Protocol2Response) { r.Capabilities.MediaUnits[0].MaxBytes++ },
		func(r *Protocol2Response) {
			r.Capabilities.MediaUnits = append(r.Capabilities.MediaUnits,
				protocol.MediaUnitStatus{Interface: protocol.C64DiskInterface(), MinBytes: uint32(protocol.C64DiskBytes), MaxBytes: uint32(protocol.C64DiskBytes), ChunkBytes: 512, State: "empty"})
		},
		func(r *Protocol2Response) { r.Capabilities.ActiveInterfaces[4].ID = "fes.media.c64-disk" },
	} {
		bad := atariStComputerResponse(t)
		mutate(&bad)
		if _, err := decodeProtocol2Response([]byte(responseLine(t, bad))); err == nil {
			t.Fatal("invalid Atari ST media status accepted")
		}
	}
}

func responseLine(t *testing.T, r Protocol2Response) string {
	t.Helper()
	line, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(line)
}

func TestComputerStatusReportsMediaUnits(t *testing.T) {
	r := computerResponse(t)
	decoded, err := decodeProtocol2Response([]byte(responseLine(t, r)))
	if err != nil {
		t.Fatal(err)
	}
	status := corePackageStatus(activationFromProtocol2(decoded.ActivePackage.PackageID, decoded.ActivePackage.Descriptor, decoded))
	unit, ok := protocol.MediaUnit(status, 0)
	if !ok || unit.State != "empty" || !status.Gamepad || !protocol.KeyboardHIDCapable(status) {
		t.Fatalf("status %+v", status)
	}
	line := responseLine(t, r)
	for name, bad := range map[string]string{
		"unknown unit field": strings.Replace(line, `"state":"empty"`, `"state":"empty","extra":1`, 1),
		"missing state":      strings.Replace(line, `,"state":"empty"`, "", 1),
		"string unit":        strings.Replace(line, `"unit":0`, `"unit":"0"`, 1),
		"bogus state":        strings.Replace(line, `"state":"empty"`, `"state":"spinning"`, 1),
		"wrong floppy size":  strings.Replace(line, `"min_bytes":143360`, `"min_bytes":1`, 1),
		"wrong chunk":        strings.Replace(line, `"chunk_bytes":512`, `"chunk_bytes":256`, 1),
		"wrong floppy unit":  strings.Replace(line, `"unit":0`, `"unit":1`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeProtocol2Response([]byte(bad)); err == nil {
				t.Fatal("invalid media unit accepted")
			}
		})
	}
	inactive := computerResponse(t)
	var active []Protocol2Interface
	for _, i := range inactive.Capabilities.ActiveInterfaces {
		if i.ID != "fes.media.apple2-floppy" {
			active = append(active, i)
		}
	}
	inactive.Capabilities.ActiveInterfaces = active
	inactive.ActivePackage.Descriptor.Interfaces = append(inactive.ActivePackage.Descriptor.Interfaces[:4:4], inactive.ActivePackage.Descriptor.Interfaces[5])
	if !validActivePackage(*inactive.ActivePackage, inactive.Capabilities) || validProtocol2Response(inactive) {
		t.Fatal("unit accepted without its active interface")
	}
	other, err := decodeProtocol2Response([]byte(fixtureLines(t, "protocol-v2.jsonl")[5]))
	if err != nil {
		t.Fatal(err)
	}
	other.Capabilities.MediaUnits = r.Capabilities.MediaUnits
	if validProtocol2Response(other) {
		t.Fatal("media units accepted for a non-computer package")
	}
}

func TestSetKeyboardHIDRequestAndResponse(t *testing.T) {
	r := computerResponse(t)
	id := r.ActivePackage.PackageID
	fixture := newSequenceSocketFixture(t, []string{responseLine(t, r) + "\n"})
	rows := KeyboardHIDRows{0x0010, 0, 0, 0x0100, 0, 0, 0, 0, 0x0002}
	if _, err := NewClient(fixture.path).SetKeyboardHID(context.Background(), id, 7, rows); err != nil {
		t.Fatal(err)
	}
	requests := fixture.wait(t)
	want := `{"protocol":2,"operation":"set_keyboard_hid","package_id":"` + id + `","expected_generation":7,"rows":[16,0,0,256,0,0,0,0,2]}`
	if len(requests) != 1 || requests[0] != want {
		t.Fatalf("request %q", requests)
	}
	client := NewClient("/not/opened")
	for _, bad := range []KeyboardHIDRows{{1}, {8}, {0, 0, 0, 0, 0, 0, 0, 0, 0x0100}} {
		if _, err := client.SetKeyboardHID(context.Background(), id, 7, bad); err != errInvalidRuntimeRequest {
			t.Fatalf("invalid rows %v: %v", bad, err)
		}
	}
	if _, err := client.SetKeyboardHID(context.Background(), id, 0, rows); err != errInvalidRuntimeRequest {
		t.Fatal("zero generation accepted")
	}
	stale := newSequenceSocketFixture(t, []string{responseLine(t, r) + "\n"})
	if _, err := NewClient(stale.path).SetKeyboardHID(context.Background(), id, 8, rows); err != errInvalidRuntimeResponse {
		t.Fatalf("other generation reply: %v", err)
	}
}

func TestComputerMediaRequestShapes(t *testing.T) {
	r := computerResponse(t)
	id := r.ActivePackage.PackageID
	ready := computerResponse(t)
	ready.Capabilities.MediaUnits[0].State = "ready"
	fixture := newSequenceSocketFixture(t, []string{responseLine(t, ready) + "\n", responseLine(t, r) + "\n"})
	client := NewClient(fixture.path)
	if _, err := client.InsertMedia(context.Background(), "/tmp/fogcast-media-unit-1/media.bin", id, 7, 0, 143360); err != nil {
		t.Fatal(err)
	}
	if _, err := client.EjectMedia(context.Background(), id, 7, 0); err != nil {
		t.Fatal(err)
	}
	requests := fixture.wait(t)
	wantInsert := `{"protocol":2,"operation":"insert_media","path":"/tmp/fogcast-media-unit-1/media.bin","expected_package_id":"` + id + `","expected_generation":7,"unit":0,"size":143360}`
	wantEject := `{"protocol":2,"operation":"eject_media","expected_package_id":"` + id + `","expected_generation":7,"unit":0}`
	if len(requests) != 2 || requests[0] != wantInsert || requests[1] != wantEject {
		t.Fatalf("requests %q", requests)
	}
	bad := NewClient("/not/opened")
	for _, call := range []func() error{
		func() error { _, err := bad.InsertMedia(context.Background(), "relative", id, 7, 0, 1); return err },
		func() error { _, err := bad.InsertMedia(context.Background(), "/tmp/x", id, 7, 8, 1); return err },
		func() error { _, err := bad.InsertMedia(context.Background(), "/tmp/x", id, 7, 0, 0); return err },
		func() error {
			_, err := bad.InsertMedia(context.Background(), "/tmp/x", id, 7, 0, 33554433)
			return err
		},
		func() error { _, err := bad.EjectMedia(context.Background(), "BAD", 7, 0); return err },
	} {
		if err := call(); err != errInvalidRuntimeRequest {
			t.Fatalf("invalid media request: %v", err)
		}
	}
}

func TestSetControllerAcceptsComputerPorts(t *testing.T) {
	r := computerResponse(t)
	fixture := newSequenceSocketFixture(t, []string{responseLine(t, r) + "\n"})
	request := ControllerRequest{PackageID: r.ActivePackage.PackageID, Generation: 7, Port: 1, Buttons: 0x11}
	if _, err := NewClient(fixture.path).SetController(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(fixture.wait(t)[0]), &got); err != nil || got["operation"] != "set_controller" || got["keypad"] != float64(0) || got["port"] != float64(1) {
		t.Fatalf("request %v %v", got, err)
	}
}

type computerMediaControl struct {
	Control
	statuses []Protocol2Response
	reads    int
	insert   func(context.Context, string, string, uint64, uint8, uint32) (Protocol2Response, error)
	eject    func(context.Context, string, uint64, uint8) (Protocol2Response, error)
}

func (c *computerMediaControl) Protocol2Status(context.Context) (Protocol2Response, error) {
	c.reads++
	return c.statuses[min(c.reads-1, len(c.statuses)-1)], nil
}
func (c *computerMediaControl) InsertMedia(ctx context.Context, path, id string, generation uint64, unit uint8, size uint32) (Protocol2Response, error) {
	return c.insert(ctx, path, id, generation, unit, size)
}
func (c *computerMediaControl) EjectMedia(ctx context.Context, id string, generation uint64, unit uint8) (Protocol2Response, error) {
	return c.eject(ctx, id, generation, unit)
}

func TestRuntimeInsertMediaStagesOnceAndRequiresReady(t *testing.T) {
	for _, media := range []struct {
		iface protocol.RuntimeContract
		bytes int64
	}{{protocol.Apple2FloppyInterface(), protocol.Apple2FloppyBytes}, {protocol.AtariStFloppyInterface(), protocol.AtariStFloppyBytes}} {
		t.Run(media.iface.ID, func(t *testing.T) { testRuntimeInsertMediaStagesOnceAndRequiresReady(t, media.iface, media.bytes) })
	}
}

func testRuntimeInsertMediaStagesOnceAndRequiresReady(t *testing.T, iface protocol.RuntimeContract, sizeBytes int64) {
	t.Helper()
	disk := bytes.Repeat([]byte{0xa5}, int(sizeBytes))
	for _, name := range []string{"success", "lost reply", "not ready", "runtime error", "short", "wrong size", "stale generation", "absent unit"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("TMPDIR", root)
			before, after := computerResponse(t), computerResponse(t)
			if iface == protocol.AtariStFloppyInterface() {
				before, after = atariStComputerResponse(t), atariStComputerResponse(t)
			}
			after.Capabilities.MediaUnits[0].State = "ready"
			size := int64(len(disk))
			var body io.Reader = bytes.NewReader(disk)
			binding := protocol.MediaUnitBinding{PackageID: before.ActivePackage.PackageID, Generation: 7}
			switch name {
			case "not ready":
				after.Capabilities.MediaUnits[0].State = "empty"
			case "short":
				body = bytes.NewReader(disk[1:])
			case "wrong size":
				size, body = 1024, bytes.NewReader(disk[:1024])
			case "stale generation":
				binding.Generation = 6
			case "absent unit":
				before.Capabilities.MediaUnits = nil
			}
			calls := 0
			control := &computerMediaControl{statuses: []Protocol2Response{before}}
			control.insert = func(op context.Context, path, id string, generation uint64, unit uint8, count uint32) (Protocol2Response, error) {
				calls++
				if _, ok := op.Deadline(); !ok {
					t.Fatal("insert has no operation deadline")
				}
				got, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(got, disk) || id != binding.PackageID || generation != 7 || unit != 0 || count != uint32(sizeBytes) {
					t.Fatalf("staged insert %v", err)
				}
				switch name {
				case "lost reply":
					return Protocol2Response{}, io.EOF
				case "runtime error":
					failed := before
					failed.OK = false
					failed.Error = &Protocol2Error{Code: "io_failed", Message: "media transfer failed", Phase: "transport"}
					return failed, nil
				}
				return after, nil
			}
			units, apiErr := NewRuntime(control, "", 0, 0).InsertMedia(context.Background(), context.Background(), size, body, binding)
			wantCalls := 0
			switch name {
			case "success", "lost reply", "not ready", "runtime error":
				wantCalls = 1
			}
			if calls != wantCalls || (apiErr == nil) != (name == "success") {
				t.Fatalf("calls=%d err=%v", calls, apiErr)
			}
			if name == "success" && (len(units) != 1 || units[0].State != "ready") {
				t.Fatalf("units %+v", units)
			}
			if name == "lost reply" && (apiErr.Code != protocol.CodeMiSTerUnavailable || apiErr.Phase != "transfer") {
				t.Fatalf("lost reply %+v", apiErr)
			}
			if entries, _ := os.ReadDir(root); len(entries) != 0 {
				t.Fatalf("staging retained %v", entries)
			}
		})
	}
}

func TestRuntimeEjectMediaRequiresEmpty(t *testing.T) {
	loaded := computerResponse(t)
	loaded.Capabilities.MediaUnits[0].State = "ready"
	empty := computerResponse(t)
	control := &computerMediaControl{statuses: []Protocol2Response{loaded}}
	control.eject = func(_ context.Context, id string, generation uint64, unit uint8) (Protocol2Response, error) {
		return empty, nil
	}
	runtime := NewRuntime(control, "", 0, 0)
	binding := protocol.MediaUnitBinding{PackageID: loaded.ActivePackage.PackageID, Generation: 7}
	units, apiErr := runtime.EjectMedia(context.Background(), binding)
	if apiErr != nil || !reflect.DeepEqual(units, empty.Capabilities.MediaUnits) {
		t.Fatalf("eject %+v %v", units, apiErr)
	}
	control.eject = func(context.Context, string, uint64, uint8) (Protocol2Response, error) { return loaded, nil }
	if _, apiErr := runtime.EjectMedia(context.Background(), binding); apiErr == nil {
		t.Fatal("eject accepted a unit that stayed ready")
	}
	control.eject = func(context.Context, string, uint64, uint8) (Protocol2Response, error) {
		return Protocol2Response{}, errors.New("lost")
	}
	if _, apiErr := runtime.EjectMedia(context.Background(), binding); apiErr == nil || apiErr.Phase != "transfer" {
		t.Fatalf("lost eject %v", apiErr)
	}
}

type keyboardHIDControl struct {
	Control
	rows []KeyboardHIDRows
	resp Protocol2Response
}

func (c *keyboardHIDControl) SetKeyboardHID(_ context.Context, id string, generation uint64, rows KeyboardHIDRows) (Protocol2Response, error) {
	c.rows = append(c.rows, rows)
	return c.resp, nil
}

func TestRuntimeKeyboardHIDMapsRejection(t *testing.T) {
	control := &keyboardHIDControl{resp: computerResponse(t)}
	runtime := NewRuntime(control, "", 0, 0)
	if err := runtime.SetKeyboardHID(context.Background(), control.resp.ActivePackage.PackageID, 7, KeyboardHIDRows{0x0010}); err != nil || len(control.rows) != 1 {
		t.Fatalf("post %v %d", err, len(control.rows))
	}
	control.resp.OK = false
	control.resp.Error = &Protocol2Error{Code: "busy", Message: "busy", Phase: "input"}
	var apiErr *protocol.APIError
	if err := runtime.SetKeyboardHID(context.Background(), control.resp.ActivePackage.PackageID, 7, KeyboardHIDRows{}); !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeBusy {
		t.Fatalf("rejection %v", err)
	}
}
