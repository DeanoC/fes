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
