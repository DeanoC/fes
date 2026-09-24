package fogcast

import (
	"strings"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/internal/meshcontent"
	"github.com/DeanoC/FogCast/protocol"
)

// MeshLibrary is the host catalog view Slice 2 can already see.
// Firmware.MediaID is the household BIOS core-media id when that slot
// is filled. Digests are stored SHA-256 strings. This view has no paths
// and no file bytes.
type MeshLibrary struct {
	Firmware catalog.CoreFirmware
	Titles   []MeshTitle
}

// MeshTitle is one library row. Game is the catalog identity. Core is
// set for a package-backed title. ABI is that package's described
// contract; ABI.Major is the package ABI major, not the mesh protocol
// major. Execute is today's session label (fpga_native or host_only).
// Expansions carry the name and digest the catalog already stored.
type MeshTitle struct {
	Game       catalog.Game
	Launchable bool
	Execute    string
	Core       *catalog.CoreEntry
	ABI        corepackage.Contract
	Expansions []MeshExpansion
}

// MeshExpansion is one named expansion slot.
// Digest is the slot-bytes digest: SHA-256 of that slot's own bytes,
// the cart payload (expansion.Manifest.CartSHA256). It is not
// expansion.Asset.ID, not the archive media_id of the stored tar, and
// not a post-link ProgrammedSHA256. Ensure asks the bound executor to
// link these bytes. The host does not pre-link them into primary media.
type MeshExpansion struct {
	Name   string
	Digest string
}

// MeshSkip is why one title was left out of the projection. The title
// is not replaced with a guessed entry.
type MeshSkip struct {
	TitleID string
	Reason  string
}

// ProjectMeshLibrary fills mesh catalog entries from the host library.
// Stored digests go through meshcontent.FromSHA256. Primary media uses
// PrimarySourceID (the format-3 source MediaID / SourceSHA256, never
// ProgrammedSHA256). Each expansion uses ExpansionSlotBytesID
// (MeshExpansion.Digest, the slot-bytes digest). The function does
// not open files and does not hash bytes. A title that cannot be named
// is omitted; the skip result carries the reason. ReadyHere is not called.
// There is no cross-node pull and no host route.
func ProjectMeshLibrary(lib MeshLibrary) (entries []meshcontent.Entry, skipped []MeshSkip) {
	for _, title := range lib.Titles {
		entry, skip, ok := projectMeshTitle(lib.Firmware.MediaID, title)
		if !ok {
			skipped = append(skipped, skip)
			continue
		}
		entries = append(entries, entry)
	}
	return entries, skipped
}

func projectMeshTitle(firmwareDigest string, title MeshTitle) (meshcontent.Entry, MeshSkip, bool) {
	id := title.Game.ID
	if err := protocol.ValidateGameID(id); err != nil {
		return meshcontent.Entry{}, MeshSkip{TitleID: id, Reason: "title id is not a catalog game id"}, false
	}
	if title.Core != nil && title.Core.GameID != "" && title.Core.GameID != id {
		return meshcontent.Entry{}, MeshSkip{TitleID: id, Reason: "core entry is for a different title"}, false
	}
	kind, packageBacked, ok := meshExecuteKind(title.Execute, title.Launchable)
	if !ok {
		return meshcontent.Entry{}, MeshSkip{TitleID: id, Reason: "execution is not a mesh catalog kind"}, false
	}
	system := meshBrowseSystem(title)
	slots, reason, ok := meshSlots(firmwareDigest, title, packageBacked)
	if !ok {
		return meshcontent.Entry{}, MeshSkip{TitleID: id, Reason: reason}, false
	}
	entry := meshcontent.Entry{
		TitleID:    id,
		System:     system,
		Slots:      slots,
		Launchable: title.Launchable,
	}
	if kind != "" {
		entry.Execute = []meshcontent.Execute{{Kind: kind}}
	}
	if err := entry.Validate(); err != nil {
		return meshcontent.Entry{}, MeshSkip{TitleID: id, Reason: err.Error()}, false
	}
	return entry, MeshSkip{}, true
}

func meshExecuteKind(execute string, launchable bool) (kind string, packageBacked bool, ok bool) {
	switch strings.TrimSpace(execute) {
	case ExecutionFPGANative:
		return meshcontent.ExecuteFPGANative, true, true
	case ExecutionHostOnly, meshcontent.ExecuteNativeEmu:
		return meshcontent.ExecuteNativeEmu, false, true
	case "":
		if launchable {
			return "", false, false
		}
		return "", false, true
	default:
		return "", false, false
	}
}

// meshBrowseSystem uses the machine name the closed package cores already
// have. Catalog rows for those cores are stored on the fpga platform.
// A described Core.System on the title's ABI is not available here; the
// core id is the name the library stores.
func meshBrowseSystem(title MeshTitle) string {
	if title.Core != nil {
		switch title.Core.CoreID {
		case "fes.coleco", "fes.zx81", "fes.pong", "fes.sms", "fes.sg1000":
			return strings.TrimPrefix(title.Core.CoreID, "fes.")
		}
	}
	return string(title.Game.System)
}

func meshSlots(firmwareDigest string, title MeshTitle, packageBacked bool) ([]meshcontent.Slot, string, bool) {
	var slots []meshcontent.Slot
	if packageBacked {
		pkg, reason, ok := meshPackage(title)
		if !ok {
			return nil, reason, false
		}
		slots = append(slots, meshcontent.PackageSlot(pkg))
	}
	firmwareRequired := title.Core != nil && title.Core.FirmwareRequired
	if firmwareRequired {
		next, reason, ok := appendStoredDigest(slots, firmwareDigest, true, "household firmware", meshcontent.FromSHA256, meshcontent.BIOSSlot)
		if !ok {
			return nil, reason, false
		}
		slots = next
	}
	primaryRequired := !packageBacked && title.Launchable
	next, reason, ok := appendStoredDigest(slots, storedPrimaryDigest(title), primaryRequired, "primary media", PrimarySourceID, meshcontent.PrimaryMediaSlot)
	if !ok {
		return nil, reason, false
	}
	slots = next
	for _, expansion := range title.Expansions {
		if strings.TrimSpace(expansion.Name) == "" {
			return nil, "expansion name is missing", false
		}
		var built []meshcontent.Slot
		built, reason, ok = appendStoredDigest(nil, expansion.Digest, true, "expansion "+expansion.Name, ExpansionSlotBytesID, func(id meshcontent.ContentID) meshcontent.Slot {
			return meshcontent.ExpansionSlot(expansion.Name, id)
		})
		if !ok {
			return nil, reason, false
		}
		slots = append(slots, built...)
	}
	return slots, "", true
}

// PrimarySourceID names primary media by the format-3 source digest.
// That digest is the catalog MediaID, which the executor records as
// ROMLink.SourceSHA256. ProgrammedSHA256 is the post-link image and
// is not this id.
func PrimarySourceID(sourceSHA256 string) (meshcontent.ContentID, error) {
	return meshcontent.FromSHA256(sourceSHA256)
}

// ExpansionSlotBytesID names an expansion by MeshExpansion.Digest, the
// slot-bytes digest (SHA-256 of the cart payload). Asset.ID, the
// archive media_id, and ProgrammedSHA256 are not this id. The host
// does not link the bytes; the bound executor does.
func ExpansionSlotBytesID(slotBytesDigest string) (meshcontent.ContentID, error) {
	return meshcontent.FromSHA256(slotBytesDigest)
}

func meshPackage(title MeshTitle) (meshcontent.PackageABI, string, bool) {
	if title.Core == nil || title.ABI.Major < 1 || title.ABI.Major > 65535 {
		return meshcontent.PackageABI{}, "package abi is required", false
	}
	pkg := meshcontent.PackageABI{
		PackageID: title.Core.PackageID,
		ABI:       title.ABI.ID,
		Major:     int(title.ABI.Major),
	}
	if err := pkg.Validate(); err != nil {
		return meshcontent.PackageABI{}, "package abi is required", false
	}
	return pkg, "", true
}

// storedPrimaryDigest is the format-3 source identity. Core.MediaID is
// the source SHA-256 (ROMLink.SourceSHA256 on the executor). A native
// title uses the catalog content SHA-256 in that same role. Neither
// value is ROMLink.ProgrammedSHA256.
func storedPrimaryDigest(title MeshTitle) string {
	if title.Core != nil && title.Core.MediaID != "" {
		return title.Core.MediaID
	}
	if title.Game.Content != nil {
		return title.Game.Content.SHA256
	}
	return ""
}

func appendStoredDigest(slots []meshcontent.Slot, digest string, required bool, slot string, parse func(string) (meshcontent.ContentID, error), build func(meshcontent.ContentID) meshcontent.Slot) ([]meshcontent.Slot, string, bool) {
	if digest == "" {
		if required {
			return nil, slot + " digest is required", false
		}
		return slots, "", true
	}
	id, err := parse(digest)
	if err != nil {
		return nil, slot + " digest is not a stored sha256", false
	}
	return append(slots, build(id)), "", true
}
