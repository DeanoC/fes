package fogcast

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/protocol"
)

func TestLaunchFloppyDriveAndStaging(t *testing.T) {
	ctx := context.Background()
	store, err := catalog.Open(filepath.Join(t.TempDir(), "catalog.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	s := &Service{catalog: store}
	pkg := strings.Repeat("a", 64)
	entryFor := func(title string, size int, fill byte) catalog.CoreEntry {
		media, _, err := store.ImportCoreMediaStream(ctx, int64(size), bytes.NewReader(bytes.Repeat([]byte{fill}, size)))
		if err != nil {
			t.Fatal(err)
		}
		entry, err := store.CreateCoreMediaEntry(ctx, title, "fes.pc386", pkg, protocol.FloppyRole, media.MediaID)
		if err != nil {
			t.Fatal(err)
		}
		return entry
	}
	// The boot disk goes in A: and the kit is sent it once.
	entry := entryFor("Setup", 1474560, 1)
	source, err := s.readLaunchFloppy(ctx, entry)
	if err != nil || source.floppy.Unit != protocol.PcFloppyUnitA || source.floppy.Size != 1474560 || source.floppy.SHA256 != entry.MediaID || source.floppy.WriteProtected {
		t.Fatalf("read %+v %v", source, err)
	}
	missing := &stagingClient{held: protocol.DiskImageStatus{SHA256: entry.MediaID}}
	if err := stageLaunchFloppy(ctx, missing, source); err != nil || missing.uploaded != 1474560 {
		t.Fatalf("upload %v %d", err, missing.uploaded)
	}
	held := &stagingClient{held: protocol.DiskImageStatus{SHA256: entry.MediaID, Size: 1474560, Staged: true}}
	if err := stageLaunchFloppy(ctx, held, &floppySource{floppy: source.floppy}); err != nil || held.uploaded != 0 {
		t.Fatalf("reuse %v %d", err, held.uploaded)
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.readLaunchFloppy(ctx, entryFor("ED", 2949120, 3)); err == nil || !strings.Contains(err.Error(), "2.88M") {
		t.Fatalf("2.88M %v", err)
	}
}

func TestLaunchFloppiesInsertedNeedsEachDiskInItsDrive(t *testing.T) {
	sha := strings.Repeat("c", 64)
	no := false
	unit := protocol.MediaUnitStatus{Unit: protocol.PcFloppyUnitA, Interface: protocol.PcFloppyInterface(), MinBytes: uint32(protocol.PcFloppyMinBytes),
		MaxBytes: uint32(protocol.PcFloppyMaxBytes), ChunkBytes: 512, State: protocol.MediaUnitReady, Saveable: &no,
		Persistence: &protocol.MediaDataStatus{Mode: "persistent", GameID: "freedos", BaseMediaID: sha, Revision: "absent"}}
	status := func(units ...protocol.MediaUnitStatus) protocol.Status {
		return protocol.Status{CorePackage: &protocol.CorePackageStatus{ABI: protocol.RuntimeContract{ID: protocol.ComputerABIID, Major: 1, Minor: 1},
			ActiveInterfaces: []protocol.RuntimeInterface{{ID: protocol.PcFloppyInterface().ID, Major: 1}}, MediaUnits: units}}
	}
	machine := protocol.LibraryMachineBinding{GameID: "freedos", Floppies: []protocol.StagedFloppy{{Unit: protocol.PcFloppyUnitA, SHA256: sha, Size: 1474560}}}
	if !launchFloppiesInserted(status(unit), machine) {
		t.Fatal("the disk the host sent was refused")
	}
	empty := unit
	empty.State, empty.Persistence = protocol.MediaUnitEmpty, nil
	other := unit
	other.Persistence = &protocol.MediaDataStatus{Mode: "persistent", GameID: "freedos", BaseMediaID: strings.Repeat("d", 64), Revision: "absent"}
	unbound := unit
	unbound.Persistence = nil
	// An older kit drops the header: the drive is empty or absent.
	for name, s := range map[string]protocol.Status{"empty": status(empty), "absent": status(), "other disk": status(other), "unbound": status(unbound)} {
		if launchFloppiesInserted(s, machine) {
			t.Errorf("%s accepted", name)
		}
	}
	// A write-protected disk binds no record.
	machine.Floppies[0].WriteProtected = true
	if !launchFloppiesInserted(status(unbound), machine) || launchFloppiesInserted(status(unit), machine) {
		t.Fatal("write-protected binding")
	}
	if !launchFloppiesInserted(status(), protocol.LibraryMachineBinding{GameID: "freedos"}) {
		t.Fatal("a launch without floppies needs none")
	}
}
