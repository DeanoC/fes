package fogcast

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/protocol"
)

// floppyMachineClient is a kit that stages images by digest and starts a
// pc386 with the launch floppies it was sent, or, as an older agent does,
// drops them.
type floppyMachineClient struct {
	*defaultMediaPackageClient
	staged  map[string][]byte
	binding protocol.LibraryMachineBinding
	older   bool
	loads   int
}

func (c *floppyMachineClient) DiskImage(_ context.Context, sha string) (protocol.DiskImageStatus, error) {
	data, ok := c.staged[sha]
	return protocol.DiskImageStatus{SHA256: sha, Size: int64(len(data)), Staged: ok}, nil
}

func (c *floppyMachineClient) StageDiskImage(_ context.Context, sha string, _ int64, r io.Reader) (protocol.DiskImageStatus, error) {
	data, _ := io.ReadAll(r)
	c.staged[sha] = data
	return protocol.DiskImageStatus{SHA256: sha, Size: int64(len(data)), Staged: true}, nil
}

func (c *floppyMachineClient) LoadLibraryMachineCore(_ context.Context, _ int64, _ io.Reader, id string, b protocol.LibraryMachineBinding) (protocol.Status, error) {
	c.loads++
	c.binding = b
	no := false
	floppy := func(unit uint8) protocol.MediaUnitStatus {
		return protocol.MediaUnitStatus{Unit: unit, Interface: protocol.PcFloppyInterface(), MinBytes: uint32(protocol.PcFloppyMinBytes),
			MaxBytes: uint32(protocol.PcFloppyMaxBytes), ChunkBytes: 512, State: protocol.MediaUnitEmpty, Saveable: &no}
	}
	units := []protocol.MediaUnitStatus{
		{Unit: 0, Interface: protocol.AtaDiskInterface(), MinBytes: uint32(protocol.AtaDiskMinBytes), MaxBytes: uint32(protocol.AtaDiskMaxBytes),
			ChunkBytes: protocol.AtaDiskChunkBytes, State: protocol.MediaUnitEmpty, Saveable: &no},
		floppy(protocol.PcFloppyUnitA), floppy(protocol.PcFloppyUnitB)}
	for _, f := range b.Floppies {
		if c.older {
			break
		}
		u := &units[f.Unit]
		u.State = protocol.MediaUnitReady
		if !f.WriteProtected {
			u.Persistence = &protocol.MediaDataStatus{Mode: "persistent", GameID: b.GameID, BaseMediaID: f.SHA256, Revision: "absent"}
		}
	}
	interfaces := []protocol.RuntimeInterface{}
	for _, i := range []protocol.RuntimeContract{protocol.AtaDiskInterface(), protocol.PcFloppyInterface(),
		protocol.PersistenceWordsInterface(), protocol.Pc386MachineInterface()} {
		interfaces = append(interfaces, protocol.RuntimeInterface{ID: i.ID, Major: i.Major, Minor: i.Minor})
	}
	status := protocol.Status{State: protocol.StateActive, Development: true, CorePackage: &protocol.CorePackageStatus{
		PackageID: id, Generation: 7, CoreID: "fes.pc386", BuildID: "0123456789abcdef0123456789abcdef", ABI: protocol.RuntimeContract{ID: protocol.ComputerABIID, Major: 1, Minor: 1},
		PersistenceMode: "persistent", ActiveInterfaces: interfaces, MediaUnits: units}}
	c.statusResult = status
	return status, nil
}

func pc386FloppyPackage(t *testing.T) []byte {
	t.Helper()
	manifest, err := os.ReadFile("../corepackage/testdata/core-bundle-v2/manifests/valid-basic.toml")
	if err != nil {
		t.Fatal(err)
	}
	m := strings.ReplaceAll(string(manifest), "fes.pong", "fes.pc386")
	m = strings.ReplaceAll(m, "id = \"fes.simple-game\"\nmajor = 1\nminor = 0", "id = \"fes.computer\"\nmajor = 1\nminor = 1")
	m = strings.ReplaceAll(m, "id = \"fes.gamepad\"", "id = \"fes.keyboard.hid\"")
	for _, id := range []string{"fes.audio.pcm-s16-stereo-48k", "fes.mouse.relative", "fes.persistence.words", "fes.pc386.machine",
		"fes.rtc.wall-clock", "fes.memory.hps-ddr", "fes.media.ata-disk", "fes.media.pc-floppy"} {
		m += "\n[[interfaces]]\nid = \"" + id + "\"\nmajor = 1\nminor = 0\nrequired = true\n"
	}
	return corePackageArchiveFixture(t, []byte(m))
}

// A floppy entry launches with disk 1 staged and in its drive, the hard disk
// empty. A kit that starts the machine without it is stopped and reported.
func TestPc386FloppyEntryLaunch(t *testing.T) {
	for _, older := range []bool{false, true} {
		ctx := context.Background()
		s, base, entry, _ := newCoreEntryLaunchFixture(t, pc386FloppyPackage(t), "MS-DOS 6.22 setup", time.Minute)
		client := &floppyMachineClient{defaultMediaPackageClient: base, staged: map[string][]byte{}, older: older}
		s.targetClients[s.selectedTarget] = client
		disk := bytes.Repeat([]byte{0xf6}, 1474560)
		media, _, err := s.ImportCoreMedia(ctx, int64(len(disk)), bytes.NewReader(disk))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.SelectCoreEntryMedia(ctx, entry.GameID, entry.PackageID, "", protocol.FloppyRole, media.MediaID); err != nil {
			var apiErr *protocol.APIError
			if errors.As(err, &apiErr) {
				t.Fatalf("%v cause=%v", err, apiErr.Cause)
			}
			t.Fatal(err)
		}
		_, err = s.Launch(ctx, entry.GameID, nil)
		if client.loads != 1 || len(client.binding.Floppies) != 1 || client.binding.Disk != nil ||
			client.binding.Floppies[0] != (protocol.StagedFloppy{Unit: protocol.PcFloppyUnitA, SHA256: media.MediaID, Size: 1474560}) ||
			!bytes.Equal(client.staged[media.MediaID], disk) {
			t.Fatalf("older=%v binding %+v staged %d", older, client.binding, len(client.staged[media.MediaID]))
		}
		if !older && err != nil {
			t.Fatal(err)
		}
		if older && (err == nil || !strings.Contains(err.Error(), "update the kit") || client.stopCalls == 0) {
			t.Fatalf("older kit: err=%v stops=%d", err, client.stopCalls)
		}
	}
}
