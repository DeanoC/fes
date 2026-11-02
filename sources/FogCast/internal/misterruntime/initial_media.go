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
	return media == nil || (validRuntimePath(media.Path) && protocol.AdmitLibraryFloppySize(int64(media.Size)) && media.Unit == protocol.AtariStFloppyUnit && media.DataRoot == MediaDataRoot && protocol.ValidMediaGameID(media.GameID) && protocol.ValidateDigest(media.BaseMediaID) == nil)
}

type protocol2InitialCoreControl interface {
	LoadCoreWithInitialMedia(context.Context, string, string, *InitialMediaRequest) (Protocol2Response, error)
}

func (r *Runtime) loadInitialCore(ctx context.Context, staged corepackage.Staged) (Protocol2Response, error) {
	m := staged.InitialMedia
	initial := &InitialMediaRequest{Path: m.Path, Size: uint32(m.Size), Unit: m.Unit, DataRoot: MediaDataRoot, GameID: m.GameID, BaseMediaID: m.BaseMediaID}
	response, err := r.control.(protocol2InitialCoreControl).LoadCoreWithInitialMedia(ctx, staged.Directory, staged.PackageID, initial)
	r.noteDispatch("load_core", err == nil)
	return response, err
}

func (client *Client) LoadCoreWithInitialMedia(ctx context.Context, path, packageID string, initial *InitialMediaRequest) (Protocol2Response, error) {
	if !validRuntimePath(path) || !protocol2Hex64.MatchString(packageID) || initial == nil || !validInitialMediaRequest(initial) || int64(initial.Size) != protocol.PcxtFloppyBytes {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	before, err := client.Protocol2Status(ctx)
	if err != nil {
		return Protocol2Response{}, protocol2MutationError{error: err, attempted: false}
	}
	line, attempted, err := client.callRawTracked(ctx, struct {
		Protocol     int                  `json:"protocol"`
		Operation    string               `json:"operation"`
		PackagePath  string               `json:"package_path"`
		PackageID    string               `json:"package_id"`
		InitialMedia *InitialMediaRequest `json:"initial_media"`
	}{2, "load_core", path, packageID, initial})
	if err != nil {
		return Protocol2Response{}, protocol2MutationError{error: err, attempted: attempted}
	}
	response, err := decodeProtocol2Response(line)
	if err != nil {
		return Protocol2Response{}, protocol2MutationError{error: err, attempted: true}
	}
	if response.InspectedPackage != nil || (response.OK && (response.ActivePackage == nil || response.ActivePackage.PackageID != packageID || !initialPcxtRequestMatches(response, initial) || !initialMediaGenerationMatches(response, &before, &corepackage.StagedInitialMedia{GameID: initial.GameID, BaseMediaID: initial.BaseMediaID, Unit: initial.Unit, Size: int64(initial.Size)}))) {
		return Protocol2Response{}, protocol2MutationError{error: errInvalidRuntimeResponse, attempted: true}
	}
	return response, nil
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
	initial := &InitialMediaRequest{Path: media.Path, Size: uint32(media.Size), Unit: media.Unit, DataRoot: MediaDataRoot, GameID: media.GameID, BaseMediaID: media.BaseMediaID}
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
	expected := protocol.AtariStFloppyInterface()
	core := "fes.atari-st"
	format := int64(3)
	if initial.InterfaceID == protocol.PcxtFloppyInterface().ID {
		expected, core, format = protocol.PcxtFloppyInterface(), "fes.pcxt-dos", 2
	} else if initial.InterfaceID != "" && initial.InterfaceID != expected.ID {
		return false
	}
	d := response.ActivePackage.Descriptor
	return d.Core.ID == core && d.Format == format && descriptorMediaWriteFor(d, expected) && (expected != protocol.AtariStFloppyInterface() || (d.ROM != nil && d.ROM.Role == "firmware")) && (expected != protocol.PcxtFloppyInterface() || (d.ROM == nil && len(d.ROMs) == 0)) && ok && unit.Interface == expected && protocol.PersistentMediaUnitCapable(p, unit) && (initial.Size == 0 || (initial.Size >= int64(unit.MinBytes) && initial.Size <= int64(unit.MaxBytes))) && unit.State == protocol.MediaUnitReady && unit.Persistence != nil && unit.Persistence.Valid() && unit.Persistence.GameID == initial.GameID && unit.Persistence.BaseMediaID == initial.BaseMediaID
}
func initialPcxtRequestMatches(response Protocol2Response, initial *InitialMediaRequest) bool {
	if initial == nil {
		return true
	}
	return initialMediaMatches(response, &corepackage.StagedInitialMedia{InterfaceID: protocol.PcxtFloppyInterface().ID, GameID: initial.GameID, BaseMediaID: initial.BaseMediaID, Unit: initial.Unit, Size: int64(initial.Size)})
}
func initialRequestMatches(response Protocol2Response, initial *InitialMediaRequest) bool {
	if initial == nil {
		return true
	}
	return initialMediaMatches(response, &corepackage.StagedInitialMedia{GameID: initial.GameID, BaseMediaID: initial.BaseMediaID, Unit: initial.Unit, Size: int64(initial.Size)})
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
