package misterruntime

import (
	"context"
	"reflect"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/misteross/expansion"
)

// SlotExpansionPath names the staged directory of the card in one physical
// slot. Each directory holds exactly manifest.json and cart.rbf.
type SlotExpansionPath struct {
	Slot int    `json:"slot"`
	Path string `json:"path"`
}

// validSlotLoad checks the v2 request: 1..7 ascending unique slots whose
// cards match the composition tuple in order, and a recomputed v2 identity.
func validSlotLoad(packageID string, expansions []SlotExpansionPath, payloadPath string, c expansion.SlotComposition) bool {
	if !protocol2Hex64.MatchString(packageID) || !validRuntimePath(payloadPath) || len(expansions) == 0 ||
		len(expansions) > 7 || len(expansions) != len(c.Expansions) {
		return false
	}
	for index, e := range expansions {
		if e.Slot < 1 || e.Slot > 7 || !validRuntimePath(e.Path) || (index > 0 && expansions[index-1].Slot >= e.Slot) ||
			c.Expansions[index].Slot != e.Slot {
			return false
		}
	}
	id, err := expansion.SlotCompositionID(c.PackageID, c.Expansions, c.PayloadSHA256)
	return err == nil && id == c.ID && c.PackageID == packageID && protocol2Hex64.MatchString(c.ShellSHA256) &&
		c.PayloadSize >= 40408 && c.PayloadSize <= corepackage.MaxPayloadSize
}

func slotLoadResponse(line []byte, packageID string, c expansion.SlotComposition, identity *corepackage.ROMLinkIdentity) (Protocol2Response, error) {
	response, err := decodeProtocol2Response(line)
	if err != nil || response.InspectedPackage != nil {
		if err != nil {
			return Protocol2Response{}, protocol2MutationError{error: err, attempted: true}
		}
		return Protocol2Response{}, protocol2MutationError{error: errInvalidRuntimeResponse, attempted: true}
	}
	if response.OK && (response.State != "running_development" || response.Execution != "development" ||
		response.ActivePackage == nil || response.ActivePackage.PackageID != packageID ||
		response.ActivePackage.Composition != nil || !reflect.DeepEqual(response.ActivePackage.SlotComposition, &c) ||
		!reflect.DeepEqual(response.ActivePackage.ROMLink, identity)) {
		return Protocol2Response{}, protocol2MutationError{error: errInvalidRuntimeResponse, attempted: true}
	}
	return response, nil
}

// LoadSlotComposedCore programs a multi-slot shell whose linked payload was
// independently composed from the retained shell and card directories.
func (client *Client) LoadSlotComposedCore(ctx context.Context, path, packageID string, expansions []SlotExpansionPath, payloadPath string, composition expansion.SlotComposition) (Protocol2Response, error) {
	if !validRuntimePath(path) || !validSlotLoad(packageID, expansions, payloadPath, composition) {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	if _, err := client.Protocol2Status(ctx); err != nil {
		return Protocol2Response{}, protocol2MutationError{error: err, attempted: false}
	}
	line, attempted, err := client.callRawTracked(ctx, struct {
		Protocol    int                       `json:"protocol"`
		Operation   string                    `json:"operation"`
		PackagePath string                    `json:"package_path"`
		PackageID   string                    `json:"package_id"`
		Expansions  []SlotExpansionPath       `json:"expansions"`
		PayloadPath string                    `json:"payload_path"`
		Composition expansion.SlotComposition `json:"composition"`
	}{2, "load_composed_core", path, packageID, expansions, payloadPath, composition})
	if err != nil {
		return Protocol2Response{}, protocol2MutationError{error: err, attempted: attempted}
	}
	return slotLoadResponse(line, packageID, composition, nil)
}

// LoadROMSlotComposedCore programs the ROM-patched multi-slot composition.
func (client *Client) LoadROMSlotComposedCore(ctx context.Context, path, packageID string, expansions []SlotExpansionPath, payloadPath string, composition expansion.SlotComposition, programmedPath string, identity corepackage.ROMLinkIdentity) (Protocol2Response, error) {
	return client.LoadROMSlotComposedCoreWithInitialMedia(ctx, path, packageID, expansions, payloadPath, composition, programmedPath, identity, nil)
}
func (client *Client) LoadROMSlotComposedCoreWithInitialMedia(ctx context.Context, path, packageID string, expansions []SlotExpansionPath, payloadPath string, composition expansion.SlotComposition, programmedPath string, identity corepackage.ROMLinkIdentity, initial *InitialMediaRequest) (Protocol2Response, error) {
	if !validInitialMediaRequest(initial) {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	if !validRuntimePath(path) || !validRuntimePath(programmedPath) || !validSlotLoad(packageID, expansions, payloadPath, composition) ||
		!protocol2Identifier.MatchString(identity.ROMID) ||
		!identity.ValidFor(corepackage.Descriptor{Format: 3, ROM: &corepackage.ROM{ID: identity.ROMID, SHA256: identity.MapSHA256, SourceSize: identity.SourceSize}}) {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	before, err := client.Protocol2Status(ctx)
	if err != nil {
		return Protocol2Response{}, protocol2MutationError{error: err, attempted: false}
	}
	if before.Capabilities.ROMLinking != 1 {
		return Protocol2Response{}, protocol2MutationError{error: errInvalidRuntimeResponse, attempted: false}
	}
	line, attempted, err := client.callRawTracked(ctx, struct {
		InitialMedia   *InitialMediaRequest        `json:"initial_media,omitempty"`
		Protocol       int                         `json:"protocol"`
		Operation      string                      `json:"operation"`
		PackagePath    string                      `json:"package_path"`
		PackageID      string                      `json:"package_id"`
		Expansions     []SlotExpansionPath         `json:"expansions"`
		PayloadPath    string                      `json:"payload_path"`
		Composition    expansion.SlotComposition   `json:"composition"`
		ProgrammedPath string                      `json:"programmed_path"`
		ROMLink        corepackage.ROMLinkIdentity `json:"rom_link"`
	}{initial, 2, "load_rom_composed_core", path, packageID, expansions, payloadPath, composition, programmedPath, identity})
	if err != nil {
		return Protocol2Response{}, protocol2MutationError{error: err, attempted: attempted}
	}
	response, err := slotLoadResponse(line, packageID, composition, &identity)
	if err == nil && response.OK && !initialRequestMatches(response, initial) {
		return Protocol2Response{}, protocol2MutationError{error: errInvalidRuntimeResponse, attempted: true}
	}
	return response, err
}

// LoadInitializedSlotComposedCore programs an initialized multi-slot image
// with the v2 request form. No FogCast producer emits one today.
func (client *Client) LoadInitializedSlotComposedCore(ctx context.Context, path, packageID string, expansions []SlotExpansionPath, payloadPath string, composition expansion.SlotComposition, programmedPath, programmedSHA string) (Protocol2Response, error) {
	if !validRuntimePath(path) || !validRuntimePath(programmedPath) || !protocol2Hex64.MatchString(programmedSHA) ||
		!validSlotLoad(packageID, expansions, payloadPath, composition) {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	if _, err := client.Protocol2Status(ctx); err != nil {
		return Protocol2Response{}, protocol2MutationError{error: err, attempted: false}
	}
	line, attempted, err := client.callRawTracked(ctx, struct {
		Protocol         int                       `json:"protocol"`
		Operation        string                    `json:"operation"`
		PackagePath      string                    `json:"package_path"`
		PackageID        string                    `json:"package_id"`
		Expansions       []SlotExpansionPath       `json:"expansions"`
		PayloadPath      string                    `json:"payload_path"`
		Composition      expansion.SlotComposition `json:"composition"`
		ProgrammedPath   string                    `json:"programmed_path"`
		ProgrammedSHA256 string                    `json:"programmed_sha256"`
	}{2, "load_initialized_composed_core", path, packageID, expansions, payloadPath, composition, programmedPath, programmedSHA})
	if err != nil {
		return Protocol2Response{}, protocol2MutationError{error: err, attempted: attempted}
	}
	return slotLoadResponse(line, packageID, composition, nil)
}

type protocol2SlotCompositionControl interface {
	LoadSlotComposedCore(context.Context, string, string, []SlotExpansionPath, string, expansion.SlotComposition) (Protocol2Response, error)
}

type protocol2ROMSlotControl interface {
	LoadROMSlotComposedCore(context.Context, string, string, []SlotExpansionPath, string, expansion.SlotComposition, string, corepackage.ROMLinkIdentity) (Protocol2Response, error)
}
