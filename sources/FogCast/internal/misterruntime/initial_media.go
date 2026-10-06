package misterruntime

import (
	"context"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/misteross/expansion"
)

// InitialMediaRequest is private local-socket staging input. Network callers
// provide source bytes and library identity, never target storage paths.
type InitialMediaRequest struct {
	Path        string `json:"path"`
	Size        uint32 `json:"size"`
	Unit        uint8  `json:"unit"`
	DataRoot    string `json:"data_root"`
	GameID      string `json:"game_id"`
	BaseMediaID string `json:"base_media_id"`
}

func validInitialMediaRequest(media *InitialMediaRequest) bool {
	return media == nil || (validRuntimePath(media.Path) && media.Size == uint32(protocol.AtariStFloppyBytes) && media.Unit == protocol.AtariStFloppyUnit && media.DataRoot == MediaDataRoot && protocol.ValidMediaGameID(media.GameID) && protocol.ValidateDigest(media.BaseMediaID) == nil)
}

type protocol2InitialROMControl interface {
	LoadROMLinkedCoreWithInitialMedia(context.Context, string, string, string, string, string, *expansion.Composition, string, corepackage.ROMLinkIdentity, *InitialMediaRequest) (Protocol2Response, error)
}
type protocol2InitialROMSlotControl interface {
	LoadROMSlotComposedCoreWithInitialMedia(context.Context, string, string, []SlotExpansionPath, string, expansion.SlotComposition, string, corepackage.ROMLinkIdentity, *InitialMediaRequest) (Protocol2Response, error)
}
type protocol2InitialROMPartsControl interface {
	LoadROMPartsComposedCoreWithInitialMedia(context.Context, string, string, []PartPath, string, expansion.PartsComposition, string, corepackage.ROMLinkIdentity, *InitialMediaRequest) (Protocol2Response, error)
}

func (r *Runtime) supportsInitialROMCore(staged corepackage.Staged) bool {
	if staged.PartsComposition != nil {
		_, ok := r.control.(protocol2InitialROMPartsControl)
		return ok
	}
	if staged.SlotComposition != nil {
		_, ok := r.control.(protocol2InitialROMSlotControl)
		return ok
	}
	_, ok := r.control.(protocol2InitialROMControl)
	return ok
}
func (r *Runtime) loadInitialROMCore(ctx context.Context, staged corepackage.Staged) (Protocol2Response, error) {
	media := staged.InitialMedia
	initial := &InitialMediaRequest{Path: media.Path, Size: uint32(protocol.AtariStFloppyBytes), Unit: media.Unit, DataRoot: MediaDataRoot, GameID: media.GameID, BaseMediaID: media.BaseMediaID}
	var response Protocol2Response
	var err error
	operation := "load_rom_core"
	switch {
	case staged.PartsComposition != nil:
		operation = "load_rom_composed_core"
		response, err = r.control.(protocol2InitialROMPartsControl).LoadROMPartsComposedCoreWithInitialMedia(ctx, staged.Directory, staged.PackageID, partPaths(staged), staged.PayloadPath, *staged.PartsComposition, staged.ProgrammedPath, *staged.ROMLink, initial)
	case staged.SlotComposition != nil:
		operation = "load_rom_composed_core"
		response, err = r.control.(protocol2InitialROMSlotControl).LoadROMSlotComposedCoreWithInitialMedia(ctx, staged.Directory, staged.PackageID, slotExpansionPaths(staged), staged.PayloadPath, *staged.SlotComposition, staged.ProgrammedPath, *staged.ROMLink, initial)
	default:
		dataRoot := ""
		if staged.Composition == nil {
			dataRoot = CoreDataRoot
		}
		response, err = r.control.(protocol2InitialROMControl).LoadROMLinkedCoreWithInitialMedia(ctx, staged.Directory, staged.PackageID, dataRoot, staged.ExpansionDirectory, staged.PayloadPath, staged.Composition, staged.ProgrammedPath, *staged.ROMLink, initial)
	}
	r.noteDispatch(operation, err == nil)
	return response, err
}

// initialMediaMatches is only a launch-result check. Live media may later eject
// or replace this disk without changing package/ROM identity or restart adoption.
func initialMediaMatches(response Protocol2Response, initial *corepackage.StagedInitialMedia) bool {
	if initial == nil {
		return true
	}
	if !response.OK || response.Error != nil || response.State != "running_development" || response.Execution != "development" || response.ActivePackage == nil || response.ActivePackage.PersistenceMode != "persistent" {
		return false
	}
	p := computerMediaStatus(response).CorePackage
	if !protocol.MediaWriteCapable(p) {
		return false
	}
	unit, ok := protocol.MediaUnit(p, initial.Unit)
	return ok && unit.State == protocol.MediaUnitReady && unit.Persistence != nil && unit.Persistence.Valid() && unit.Persistence.GameID == initial.GameID && unit.Persistence.BaseMediaID == initial.BaseMediaID
}
func initialRequestMatches(response Protocol2Response, initial *InitialMediaRequest) bool {
	if initial == nil {
		return true
	}
	return initialMediaMatches(response, &corepackage.StagedInitialMedia{GameID: initial.GameID, BaseMediaID: initial.BaseMediaID, Unit: initial.Unit})
}

// initialMediaGenerationMatches fences only this atomic launch. The mutable
// disk unit is not part of the identity used for restart adoption.
func initialMediaGenerationMatches(response Protocol2Response, before *Protocol2Response, initial *corepackage.StagedInitialMedia) bool {
	if initial == nil {
		return true
	}
	if response.Generation == nil || *response.Generation == 0 {
		return false
	}
	return before == nil || before.Generation == nil || *response.Generation > *before.Generation
}
