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
	staged   map[string][]byte
	binding  protocol.LibraryMachineBinding
	older    bool
	loads    int
	inserted []protocol.LibraryMediaBinding
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

func (c *floppyMachineClient) InsertLibraryMedia(_ context.Context, size int64, body io.Reader, b protocol.LibraryMediaBinding) (protocol.Status, error) {
	data, _ := io.ReadAll(body)
	if int64(len(data)) != size {
		return protocol.Status{}, errors.New("short body")
	}
	c.inserted = append(c.inserted, b)
	status := c.statusResult
	units := append([]protocol.MediaUnitStatus(nil), status.CorePackage.MediaUnits...)
	u := &units[b.Unit]
	u.State = protocol.MediaUnitReady
	u.Persistence = nil
	if !b.WriteProtected {
		u.Persistence = &protocol.MediaDataStatus{Mode: "persistent", GameID: b.GameID, BaseMediaID: b.BaseMediaID, Revision: "absent"}
	}
	pkg := *status.CorePackage
	pkg.MediaUnits = units
	status.CorePackage = &pkg
	c.statusResult = status
	return status, nil
}

func (c *floppyMachineClient) SaveMedia(context.Context, protocol.MediaUnitBinding) (protocol.Status, error) {
	return c.statusResult, nil
}

// A hard-disk title takes any floppy in the library into A: or B:, so it can
// boot install disks; a disk the drive cannot read never reaches the kit.
func TestPc386DriveTakesAnyLibraryFloppy(t *testing.T) {
	ctx := context.Background()
	s, base, entry, _ := newCoreEntryLaunchFixture(t, pc386FloppyPackage(t), "DOS install", time.Minute)
	client := &floppyMachineClient{defaultMediaPackageClient: base, staged: map[string][]byte{}}
	s.targetClients[s.selectedTarget] = client
	response, err := s.Launch(ctx, entry.GameID, nil)
	if err != nil {
		t.Fatal(err)
	}
	setup := bytes.Repeat([]byte{0x31}, 1474560)
	media, _, err := s.ImportCoreMedia(ctx, int64(len(setup)), bytes.NewReader(setup))
	if err != nil {
		t.Fatal(err)
	}
	b5, _, err := s.ImportCoreMedia(ctx, 1228800, bytes.NewReader(bytes.Repeat([]byte{0x32}, 1228800)))
	if err != nil {
		t.Fatal(err)
	}
	dev := s.libraryDevelopmentMediaBinding(*response.Status.CorePackage)
	binding := func(unit uint8, id string) protocol.LibraryMediaBinding {
		return protocol.LibraryMediaBinding{MediaUnitBinding: protocol.MediaUnitBinding{PackageID: dev.PackageID, Generation: dev.Generation,
			Unit: unit, Target: dev.Target, TargetID: dev.TargetID}, GameID: entry.GameID, BaseMediaID: id}
	}
	// A: cannot read the 1.2M disk: refused before the kit, naming B:.
	if _, err := s.InsertLibraryDisk(ctx, binding(protocol.PcFloppyUnitA, b5.MediaID)); err == nil || !strings.Contains(err.Error(), "put it in B:") || len(client.inserted) != 0 {
		t.Fatalf("1.2M into A: err=%v inserted=%d", err, len(client.inserted))
	}
	if _, err := s.InsertLibraryDisk(ctx, binding(protocol.PcFloppyUnitA, media.MediaID)); err != nil || len(client.inserted) != 1 ||
		client.inserted[0].Unit != protocol.PcFloppyUnitA || client.inserted[0].BaseMediaID != media.MediaID || client.inserted[0].GameID != entry.GameID {
		t.Fatalf("setup disk into A: err=%v inserted=%+v", err, client.inserted)
	}
	// Ejecting A: empties it, so the next reboot boots C:.
	after, err := s.EjectPcFloppy(ctx, binding(protocol.PcFloppyUnitA, media.MediaID).MediaUnitBinding)
	if u, ok := protocol.MediaUnit(after.CorePackage, protocol.PcFloppyUnitA); err != nil || !ok || u.State != protocol.MediaUnitEmpty {
		t.Fatalf("eject A: err=%v unit=%+v", err, u)
	}
	// Another title's binding does not apply to this machine.
	other := binding(protocol.PcFloppyUnitB, b5.MediaID)
	other.GameID = "someone-else"
	if _, err := s.InsertLibraryDisk(ctx, other); err == nil || len(client.inserted) != 1 {
		t.Fatalf("other title err=%v", err)
	}
}

func (c *floppyMachineClient) InsertMedia(context.Context, int64, io.Reader, protocol.MediaUnitBinding) (protocol.Status, error) {
	return protocol.Status{}, errors.New("a pc386 drive takes library disks only")
}

func (c *floppyMachineClient) EjectMedia(_ context.Context, b protocol.MediaUnitBinding) (protocol.Status, error) {
	status := c.statusResult
	units := append([]protocol.MediaUnitStatus(nil), status.CorePackage.MediaUnits...)
	units[b.Unit].State, units[b.Unit].Persistence = protocol.MediaUnitEmpty, nil
	pkg := *status.CorePackage
	pkg.MediaUnits = units
	status.CorePackage = &pkg
	c.statusResult = status
	return status, nil
}
