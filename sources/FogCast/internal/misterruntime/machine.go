package misterruntime

import (
	"context"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
)

// ataDiskRequest is load_library_core's optional ata_disk object: a staged
// base image, the media-data root of its delta, and the geometry the core
// reports in IDENTIFY DEVICE.
type ataDiskRequest struct {
	Path            string `json:"path"`
	Size            int64  `json:"size"`
	BaseMediaID     string `json:"base_media_id"`
	DataRoot        string `json:"data_root"`
	Cylinders       uint16 `json:"cylinders"`
	Heads           uint16 `json:"heads"`
	SectorsPerTrack uint16 `json:"sectors_per_track"`
}

func (d ataDiskRequest) valid() bool {
	geometry := protocol.DiskGeometry{Cylinders: d.Cylinders, Heads: d.Heads, SectorsPerTrack: d.SectorsPerTrack}
	return validRuntimePath(d.Path) && validRuntimePath(d.DataRoot) && protocol2Hex64.MatchString(d.BaseMediaID) && geometry.Valid(d.Size)
}

type protocol2MachineLibraryControl interface {
	LoadLibraryMachineCore(context.Context, string, string, string, string, *ataDiskRequest, *InitialMediaRequest) (Protocol2Response, error)
}

type protocol2MachineDataControl interface {
	InspectMachineCoreData(context.Context, string, string, string, string) (Protocol2Response, error)
	UpdateMachineSettings(context.Context, string, string, string, string, string, protocol.MachineSettings) (Protocol2Response, error)
	UpdatePcxtMachineSettings(context.Context, string, string, string, string, string, protocol.PcxtMachineSettings) (Protocol2Response, error)
}

// LoadLibraryMachineCore sends load_library_core with the library game that
// keys the machine record and, when bound, the hard disk or initial floppy.
func (client *Client) LoadLibraryMachineCore(ctx context.Context, path, packageID, root, gameID string, disk *ataDiskRequest, initial *InitialMediaRequest) (Protocol2Response, error) {
	if !validRuntimePath(path) || !validRuntimePath(root) || !protocol2Hex64.MatchString(packageID) || !protocol.ValidMediaGameID(gameID) ||
		(disk != nil && !disk.valid()) || !validInitialMediaRequest(initial) || (disk != nil && initial != nil) ||
		(initial != nil && int64(initial.Size) != protocol.PcxtFloppyBytes) {
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
		DataRoot     string               `json:"data_root"`
		GameID       string               `json:"game_id"`
		ATADisk      *ataDiskRequest      `json:"ata_disk,omitempty"`
		InitialMedia *InitialMediaRequest `json:"initial_media,omitempty"`
	}{2, "load_library_core", path, packageID, root, gameID, disk, initial})
	if err != nil {
		return Protocol2Response{}, protocol2MutationError{error: err, attempted: attempted}
	}
	response, err := decodeProtocol2Response(line)
	if err != nil {
		return Protocol2Response{}, protocol2MutationError{error: err, attempted: true}
	}
	if response.InspectedPackage != nil || (response.OK && (response.State != "running_development" ||
		response.Execution != "development" || response.ActivePackage == nil || response.ActivePackage.PackageID != packageID ||
		response.ActivePackage.Composition != nil || response.ActivePackage.SlotComposition != nil ||
		!initialPcxtRequestMatches(response, initial) ||
		(initial != nil && !initialMediaGenerationMatches(response, &before, &corepackage.StagedInitialMedia{GameID: initial.GameID, BaseMediaID: initial.BaseMediaID, Unit: initial.Unit, Size: int64(initial.Size)})))) {
		return Protocol2Response{}, protocol2MutationError{error: errInvalidRuntimeResponse, attempted: true}
	}
	return response, nil
}

func (client *Client) InspectMachineCoreData(ctx context.Context, path, id, root, gameID string) (Protocol2Response, error) {
	if !protocol.ValidMediaGameID(gameID) {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	return client.coreData(ctx, coreDataRequest{Protocol: 2, Operation: "inspect_core_data", PackagePath: path, PackageID: id, DataRoot: root, GameID: gameID})
}

func (client *Client) UpdateMachineSettings(ctx context.Context, path, id, root, gameID, revision string, settings protocol.MachineSettings) (Protocol2Response, error) {
	if !protocol.ValidMediaGameID(gameID) || !protocol.ValidCoreDataRevision(revision) || !settings.Valid() {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	return client.coreData(ctx, coreDataRequest{Protocol: 2, Operation: "update_core_settings", PackagePath: path, PackageID: id,
		DataRoot: root, GameID: gameID, ExpectedRevision: revision, Machine: &settings})
}

func (client *Client) UpdatePcxtMachineSettings(ctx context.Context, path, id, root, gameID, revision string, settings protocol.PcxtMachineSettings) (Protocol2Response, error) {
	if !protocol.ValidMediaGameID(gameID) || !protocol.ValidCoreDataRevision(revision) || !settings.Valid() {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	return client.coreData(ctx, coreDataRequest{Protocol: 2, Operation: "update_core_settings", PackagePath: path, PackageID: id,
		DataRoot: root, GameID: gameID, ExpectedRevision: revision, PcxtMachine: &settings})
}
