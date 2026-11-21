package fogcast

import (
	"crypto/sha256"
	"fmt"
	"io"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
)

func initialSTDisk(d corepackage.Descriptor, media *coreEntryMedia) bool {
	if d.Format != 3 || d.Core.ID != "fes.atari-st" || media.unit == nil || *media.unit != protocol.AtariStFloppyUnit {
		return false
	}
	for _, iface := range d.Interfaces {
		if iface.ID == protocol.AtariStFloppyWriteInterface().ID && iface.Required && iface.Major == 1 && iface.Minor == 0 {
			return true
		}
	}
	return false
}

func initialPcxtDisk(d corepackage.Descriptor, media *coreEntryMedia) bool {
	if media == nil || media.unit == nil || *media.unit != protocol.PcxtFloppyUnit {
		return false
	}
	return pcxtWritableFloppyPackage(d)
}

// pcxtStartDisk is the disk a PC/XT title starts with: its own, or, while
// it has none, the FreeDOS boot disk this host bundles. With an empty drive
// the BIOS has nothing to start. The default is derived at each launch and
// never saved as the title's choice, like bundled firmware; what the
// computer writes to it is kept for the title against the bundled image,
// and the PC/XT shelf lists it once it has changes (shelfEntry).
func (s *Service) pcxtStartDisk(d corepackage.Descriptor, entry catalog.CoreEntry) catalog.CoreEntry {
	if !pcxtWritableFloppyPackage(d) {
		return entry
	}
	return s.shelfEntry(entry)
}

// pcxtWritableFloppyPackage is a format-2 PC/XT package whose floppy keeps
// what is written to it.
func pcxtWritableFloppyPackage(d corepackage.Descriptor) bool {
	if d.Format != 2 || d.Core.ID != pcxtCoreID {
		return false
	}
	base, write := false, false
	for _, iface := range d.Interfaces {
		if iface.Required && iface.Major == 1 && iface.Minor == 0 {
			base = base || iface.ID == protocol.PcxtFloppyInterface().ID
			write = write || iface.ID == protocol.PcxtFloppyWriteInterface().ID
		}
	}
	return base && write
}

func snapshotInitialPcxtDisk(entry catalog.CoreEntry, media *coreEntryMedia) (*corepackage.InitialMedia, error) {
	if media.size != protocol.PcxtFloppyBytes {
		return nil, protocol.DiskMediaRequestError()
	}
	data, err := io.ReadAll(io.LimitReader(media, media.size+1))
	if err != nil || int64(len(data)) != media.size || fmt.Sprintf("%x", sha256.Sum256(data)) != entry.MediaID {
		return nil, protocol.DiskMediaRequestError()
	}
	return &corepackage.InitialMedia{InterfaceID: protocol.PcxtFloppyInterface().ID, GameID: entry.GameID, BaseMediaID: entry.MediaID, Unit: *media.unit, Bytes: data}, nil
}

// Snapshot the selected immutable disk before any target mutation. The ROM
// envelope carries explicit library context; the runtime restores saved bytes
// and commits drive A before releasing execution.
func snapshotInitialSTDisk(entry catalog.CoreEntry, media *coreEntryMedia) (*corepackage.InitialMedia, error) {
	if !protocol.AdmitAtariStFloppySize(media.size) {
		return nil, protocol.DiskMediaRequestError()
	}
	data, err := io.ReadAll(io.LimitReader(media, media.size+1))
	if err != nil || int64(len(data)) != media.size || fmt.Sprintf("%x", sha256.Sum256(data)) != entry.MediaID {
		return nil, protocol.DiskMediaRequestError()
	}
	return &corepackage.InitialMedia{GameID: entry.GameID, BaseMediaID: entry.MediaID, Unit: *media.unit, Bytes: data}, nil
}
