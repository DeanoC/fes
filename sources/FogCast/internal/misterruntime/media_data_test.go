package misterruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
	"io"
	"os"
	"sort"
	"strings"
	"testing"
	"time"
)

func writableSTResponse(t *testing.T, bound bool) Protocol2Response {
	r := atariStComputerResponse(t)
	iface := protocol.AtariStFloppyWriteInterface()
	r.ActivePackage.Descriptor.Interfaces = append(r.ActivePackage.Descriptor.Interfaces, corepackage.Interface{ID: iface.ID, Major: 1, Required: true})
	for _, list := range []*[]Protocol2Interface{&r.Capabilities.ActiveInterfaces, &r.Capabilities.ABIs[0].Interfaces} {
		*list = append(*list, Protocol2Interface{ID: iface.ID, Major: 1})
		sort.Slice(*list, func(i, j int) bool { return (*list)[i].ID < (*list)[j].ID })
	}
	if bound {
		r.ActivePackage.PersistenceMode = "persistent"
		r.Capabilities.MediaUnits[0].State = "ready"
		r.Capabilities.MediaUnits[0].Persistence = &protocol.MediaDataStatus{Mode: "persistent", GameID: "st-desktop", BaseMediaID: strings.Repeat("b", 64), Revision: "absent"}
	}
	return r
}
func TestWritableDiskStatusExactShapeAndPairedContract(t *testing.T) {
	r := writableSTResponse(t, true)
	if _, err := decodeProtocol2Response([]byte(responseLine(t, r))); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Protocol2Response){func(r *Protocol2Response) { r.Capabilities.MediaUnits[0].Persistence.BaseMediaID = "bad" }, func(r *Protocol2Response) { r.Capabilities.MediaUnits[0].State = "empty" }, func(r *Protocol2Response) { r.ActivePackage.PersistenceMode = "volatile" }, func(r *Protocol2Response) { r.Capabilities.MediaUnits = nil }, func(r *Protocol2Response) {
		r.Capabilities.ActiveInterfaces = r.Capabilities.ActiveInterfaces[:len(r.Capabilities.ActiveInterfaces)-2]
	}} {
		bad := writableSTResponse(t, true)
		change(&bad)
		if _, err := decodeProtocol2Response([]byte(responseLine(t, bad))); err == nil {
			t.Fatal("invalid bound disk accepted")
		}
	}
	raw := strings.Replace(responseLine(t, r), `"mode":"persistent"`, `"mode":"persistent","extra":1`, 1)
	if _, err := decodeProtocol2Response([]byte(raw)); err == nil {
		t.Fatal("unknown persistence field accepted")
	}
}

type durableMediaControl struct {
	*computerMediaControl
	libraryCalls, saveCalls int
	binding                 protocol.LibraryMediaBinding
	response                Protocol2Response
	lost                    bool
	bytes                   []byte
	remaining               time.Duration
}

func (c *durableMediaControl) InsertLibraryMedia(ctx context.Context, path, root string, b protocol.LibraryMediaBinding, size uint32) (Protocol2Response, error) {
	c.libraryCalls++
	d, _ := ctx.Deadline()
	c.remaining = time.Until(d)
	c.binding = b
	if root != MediaDataRoot {
		panic("wrong fixed root")
	}
	c.bytes, _ = os.ReadFile(path)
	if c.lost {
		return Protocol2Response{}, io.EOF
	}
	return c.response, nil
}
func (c *durableMediaControl) SaveMedia(context.Context, protocol.MediaUnitBinding) (Protocol2Response, error) {
	c.saveCalls++
	return c.response, nil
}
func TestLibraryDiskStagedOnceBindingAndSave(t *testing.T) {
	for _, lost := range []bool{false, true} {
		before, after := writableSTResponse(t, false), writableSTResponse(t, true)
		c := &durableMediaControl{computerMediaControl: &computerMediaControl{statuses: []Protocol2Response{before}}, response: after, lost: lost}
		r := NewRuntime(c, "", 0, 0)
		b := protocol.LibraryMediaBinding{MediaUnitBinding: protocol.MediaUnitBinding{PackageID: before.ActivePackage.PackageID, Generation: 7}, GameID: "st-desktop", BaseMediaID: strings.Repeat("b", 64)}
		disk := bytes.Repeat([]byte{0xa5}, int(protocol.AtariStFloppyBytes))
		units, err := r.InsertLibraryMedia(context.Background(), context.Background(), int64(len(disk)), bytes.NewReader(disk), b)
		if c.libraryCalls != 1 || c.binding != b || !bytes.Equal(c.bytes, disk) {
			t.Fatal("staging/binding/retry")
		}
		if lost {
			if err == nil {
				t.Fatal("lost reply accepted")
			}
			continue
		}
		if err != nil || len(units) != 1 || units[0].Persistence == nil {
			t.Fatal(err, units)
		}
		c.statuses = []Protocol2Response{after}
		if _, err := r.SaveMedia(context.Background(), b.MediaUnitBinding); err != nil || c.saveCalls != 1 {
			t.Fatal(err, c.saveCalls)
		}
	}
}
func TestLibraryDiskLocalRequestFields(t *testing.T) {
	r := writableSTResponse(t, true)
	fixture := newSequenceSocketFixture(t, []string{responseLine(t, r) + "\n"})
	client := NewClient(fixture.path)
	b := protocol.LibraryMediaBinding{MediaUnitBinding: protocol.MediaUnitBinding{PackageID: r.ActivePackage.PackageID, Generation: 7}, GameID: "st-desktop", BaseMediaID: strings.Repeat("b", 64)}
	if _, err := client.InsertLibraryMedia(context.Background(), "/disk.st", MediaDataRoot, b, 737280); err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if json.Unmarshal([]byte(fixture.wait(t)[0]), &fields) != nil || fields["operation"] != "insert_library_media" || fields["game_id"] != b.GameID || fields["data_root"] != MediaDataRoot {
		t.Fatal(fields)
	}
}

func TestBoundLibraryReplacementBudgetIncludesRecoveryCapture(t *testing.T) {
	for _, bound := range []bool{false, true} {
		before, after := writableSTResponse(t, bound), writableSTResponse(t, true)
		c := &durableMediaControl{computerMediaControl: &computerMediaControl{statuses: []Protocol2Response{before}}, response: after}
		r := NewRuntime(c, "", 0, 0)
		b := protocol.LibraryMediaBinding{MediaUnitBinding: protocol.MediaUnitBinding{PackageID: before.ActivePackage.PackageID, Generation: 7}, GameID: "st-desktop", BaseMediaID: strings.Repeat("b", 64)}
		disk := bytes.Repeat([]byte{0xa5}, int(protocol.AtariStFloppyBytes))
		if _, e := r.InsertLibraryMedia(context.Background(), context.Background(), int64(len(disk)), bytes.NewReader(disk), b); e != nil {
			t.Fatal(e)
		}
		want := 270 * time.Second
		if bound {
			want = 405 * time.Second
		}
		if c.remaining < want-time.Second || c.remaining > want {
			t.Fatal(c.remaining, want)
		}
	}
}
