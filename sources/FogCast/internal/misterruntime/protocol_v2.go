package misterruntime

import (
	"context"
	"errors"
	"reflect"
	"regexp"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/misteross/expansion"
)

var errProtocol2Unsupported = errors.New("runtime protocol 2 is unsupported")

type protocol2MutationError struct {
	error
	attempted bool
}

func (err protocol2MutationError) Unwrap() error { return err.error }

var (
	protocol2Identifier = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,95}$`)
	protocol2Hex64      = regexp.MustCompile(`^[0-9a-f]{64}$`)
	protocol2Instance   = regexp.MustCompile(`^[0-9a-f]{32}$`)
)

type Protocol2Error struct {
	Code     string  `json:"code"`
	Message  string  `json:"message"`
	Phase    string  `json:"phase"`
	Expected *string `json:"expected,omitempty"`
	Observed *string `json:"observed,omitempty"`
}

// Protocol2MenuDisplayLive is what the idle display and the ADV7513 last
// reported about themselves, rather than what the last present set up.
type Protocol2MenuDisplayLive struct {
	Enabled  bool `json:"enabled"`
	Faulted  bool `json:"faulted"`
	Quiesced bool `json:"quiesced"`
	// DisplayedSequence is the register as the check read it, kept for logs
	// and diagnostics. A client does not compare it with the sequence its own
	// present returned: those are two different instants. ScanoutFrame is the
	// comparison, made where both numbers are read together.
	DisplayedSequence uint64 `json:"displayed_sequence"`
	// ScanoutFrame is the runtime's verdict on whether scanout shows the
	// committed frame: "matching", "mismatched", or "unknown" when no
	// measurement describes the frame now on the wire. Empty from a runtime
	// that publishes no verdict.
	ScanoutFrame string `json:"scanout_frame,omitempty"`
	// UnderflowsTotal is the absolute hardware counter since the display was
	// programmed, not the per-present delta the sibling Underflows carries.
	UnderflowsTotal uint64 `json:"underflows_total"`
	// RecoveryAbandoned says the runtime has stopped trying to recover the
	// loss this sample describes, because a reactivation already failed for
	// this reason or the bounded budget is spent. It is false on a healthy
	// display and false while a loss may still be reactivated.
	RecoveryAbandoned bool `json:"recovery_abandoned"`
	// Transmitter is the ADV7513 reads, present only when the runtime owns
	// the transmitter. The four arrive together or not at all, which a
	// pointer keeps true on encode as well as on decode.
	Transmitter *Protocol2MenuDisplayTransmitter `json:"transmitter,omitempty"`
}

// Protocol2MenuDisplayTransmitter is one all-or-nothing set of ADV7513 reads.
type Protocol2MenuDisplayTransmitter struct {
	HotPlug bool   `json:"hot_plug"`
	RxSense bool   `json:"rx_sense"`
	PLLLock bool   `json:"pll_lock"`
	VIC     uint64 `json:"vic"`
}

type Protocol2MenuDisplay struct {
	Available      bool   `json:"available"`
	FrameMapping   bool   `json:"frame_mapping,omitempty"`
	Session        bool   `json:"session,omitempty"`
	CoreGeneration uint64 `json:"core_generation,omitempty"`
	// Focus is who receives input on a session plane. A shown plane with
	// machine focus composites while the machine keeps its input.
	Focus protocol.SessionFocus `json:"focus,omitempty"`
	// Picture is a session plane's machine picture as [x, y, width, height]
	// inside the launcher frame, for drawing a surround around it.
	Picture    *[4]uint16 `json:"picture,omitempty"`
	PackageID  *string    `json:"package_id"`
	Generation uint64     `json:"generation"`
	Width      uint64     `json:"width"`
	Height     uint64     `json:"height"`
	Stride     uint64     `json:"stride"`
	ByteCount  uint64     `json:"byte_count"`
	SlotBytes  uint64     `json:"slot_bytes"`
	Slots      uint64     `json:"slots"`
	// Format is the pixel layout of the granted slots the client draws into.
	Format            string `json:"format"`
	DisplayedSequence uint64 `json:"displayed_sequence"`
	Underflows        uint64 `json:"underflows"`
	// Live is the runtime's periodic re-read of the idle display and its
	// HDMI transmitter. Absent from runtimes that perform no such read.
	Live  *Protocol2MenuDisplayLive `json:"live,omitempty"`
	Error *Protocol2Error           `json:"error"`
}

type Protocol2Response struct {
	RuntimeInstance  *string                 `json:"runtime_instance"`
	CoreData         *protocol.CoreData      `json:"core_data,omitempty"`
	MediaData        *protocol.MediaRecord   `json:"media_data,omitempty"`
	Protocol         int                     `json:"protocol"`
	OK               bool                    `json:"ok"`
	State            string                  `json:"state"`
	Execution        string                  `json:"execution"`
	System           *string                 `json:"system"`
	Core             *string                 `json:"core"`
	Error            *Protocol2Error         `json:"error"`
	Version          string                  `json:"version"`
	Capabilities     Protocol2Capabilities   `json:"capabilities"`
	ActivePackage    *Protocol2ActivePackage `json:"active_package"`
	Generation       *uint64                 `json:"generation"`
	InspectedPackage *Protocol2Inspection    `json:"inspected_package"`
	MenuDisplay      *Protocol2MenuDisplay   `json:"menu_display,omitempty"`
}

// protocol2ROMLink is the closed format-3 receipt accepted by the runtime.
// The ROM role comes from the active package descriptor, not this wire object.
type protocol2ROMLink struct {
	ROMID            string `json:"rom_id"`
	MapSHA256        string `json:"map_sha256"`
	SourceSHA256     string `json:"source_sha256"`
	SourceSize       int64  `json:"source_size"`
	ProgrammedSHA256 string `json:"programmed_sha256"`
	ProgrammedSize   int64  `json:"programmed_size"`
}

func protocol2ROMLinkFromIdentity(identity corepackage.ROMLinkIdentity) protocol2ROMLink {
	return protocol2ROMLink{
		ROMID: identity.ROMID, MapSHA256: identity.MapSHA256,
		SourceSHA256: identity.SourceSHA256, SourceSize: identity.SourceSize,
		ProgrammedSHA256: identity.ProgrammedSHA256, ProgrammedSize: identity.ProgrammedSize,
	}
}

func (client *Client) SetKeyboard(ctx context.Context, instance, packageID string, generation, matrix uint64) (Protocol2Response, error) {
	if !protocol2Instance.MatchString(instance) || instance == "00000000000000000000000000000000" || (packageID != "" && protocol.ValidateDigest(packageID) != nil) || generation == 0 || matrix > 0xffffffffff {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	line, err := client.callRaw(ctx, struct {
		Protocol           int    `json:"protocol"`
		Operation          string `json:"operation"`
		RuntimeInstance    string `json:"runtime_instance"`
		PackageID          string `json:"package_id"`
		ExpectedGeneration uint64 `json:"expected_generation"`
		Matrix             uint64 `json:"matrix"`
	}{Protocol: 2, Operation: "set_keyboard", RuntimeInstance: instance, PackageID: packageID, ExpectedGeneration: generation, Matrix: matrix})
	if err != nil {
		return Protocol2Response{}, err
	}
	return decodeProtocol2Response(line)
}

func (client *Client) Protocol2Status(ctx context.Context) (Protocol2Response, error) {
	line, err := client.callRaw(ctx, struct {
		Protocol  int    `json:"protocol"`
		Operation string `json:"operation"`
	}{Protocol: 2, Operation: "status"})
	if err != nil {
		return Protocol2Response{}, err
	}
	response, err := decodeNegotiationResponse(line)
	if err != nil {
		return Protocol2Response{}, err
	}
	if response.InspectedPackage != nil || (!response.OK && response.State != "reboot_required" && !protocol2ResumedSaveFailure(response)) {
		return Protocol2Response{}, errInvalidRuntimeResponse
	}
	return response, nil
}

func protocol2ResumedSaveFailure(response Protocol2Response) bool {
	return response.Error != nil && response.Error.Code == "save_failed" && response.Error.Phase == "save" &&
		(response.State == "running_development")
}

func describedPackageRuntimeState(response Protocol2Response) bool {
	return response.State == "running_development" || (response.State == "reboot_required" && response.ActivePackage != nil)
}

func (client *Client) LoadCore(ctx context.Context, path, packageID string) (Protocol2Response, error) {
	response, _, err := client.loadCoreWithStatus(ctx, path, packageID)
	return response, err
}

func (client *Client) loadCoreWithStatus(ctx context.Context, path, packageID string) (Protocol2Response, Protocol2Response, error) {
	return client.loadCoreWithRoot(ctx, path, packageID, "")
}

func (client *Client) loadCoreOperation(ctx context.Context, path, id, root string) (Protocol2Response, error) {
	if !validRuntimePath(root) {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	response, _, err := client.loadCoreWithRoot(ctx, path, id, root)
	return response, err
}

func (client *Client) loadCoreWithRoot(ctx context.Context, path, packageID, root string) (Protocol2Response, Protocol2Response, error) {
	return client.loadCoreWithComposition(ctx, path, packageID, root, "", "", nil)
}

func (client *Client) LoadInitializedCore(ctx context.Context, path, packageID, programmedPath, programmedSHA string) (Protocol2Response, error) {
	return client.loadInitialized(ctx, path, packageID, "", "", "", nil, programmedPath, programmedSHA, "load_initialized_core")
}

func (client *Client) LoadInitializedLibraryCore(ctx context.Context, path, packageID, root, programmedPath, programmedSHA string) (Protocol2Response, error) {
	return client.loadInitialized(ctx, path, packageID, root, "", "", nil, programmedPath, programmedSHA, "load_initialized_library_core")
}

func (client *Client) LoadInitializedComposedCore(ctx context.Context, path, packageID, expansionPath, payloadPath string, composition expansion.Composition, programmedPath, programmedSHA string) (Protocol2Response, error) {
	if !validRuntimePath(expansionPath) || !validRuntimePath(payloadPath) || !validComposition(composition, packageID) {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	return client.loadInitialized(ctx, path, packageID, "", expansionPath, payloadPath, &composition, programmedPath, programmedSHA, "load_initialized_composed_core")
}

func (client *Client) loadInitialized(ctx context.Context, path, packageID, root, expansionPath, payloadPath string, composition *expansion.Composition, programmedPath, programmedSHA, operation string) (Protocol2Response, error) {
	if !validRuntimePath(path) || !validRuntimePath(programmedPath) || !protocol2Hex64.MatchString(packageID) || !protocol2Hex64.MatchString(programmedSHA) {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	if root != "" && !validRuntimePath(root) {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	if _, err := client.Protocol2Status(ctx); err != nil {
		return Protocol2Response{}, protocol2MutationError{error: err, attempted: false}
	}
	line, attempted, err := client.callRawTracked(ctx, struct {
		Protocol         int                    `json:"protocol"`
		Operation        string                 `json:"operation"`
		PackagePath      string                 `json:"package_path"`
		PackageID        string                 `json:"package_id"`
		DataRoot         string                 `json:"data_root,omitempty"`
		ExpansionPath    string                 `json:"expansion_path,omitempty"`
		PayloadPath      string                 `json:"payload_path,omitempty"`
		Composition      *expansion.Composition `json:"composition,omitempty"`
		ProgrammedPath   string                 `json:"programmed_path"`
		ProgrammedSHA256 string                 `json:"programmed_sha256"`
	}{Protocol: 2, Operation: operation, PackagePath: path, PackageID: packageID, DataRoot: root, ExpansionPath: expansionPath, PayloadPath: payloadPath, Composition: composition, ProgrammedPath: programmedPath, ProgrammedSHA256: programmedSHA})
	if err != nil {
		return Protocol2Response{}, protocol2MutationError{error: err, attempted: attempted}
	}
	response, err := decodeProtocol2Response(line)
	if err != nil || response.InspectedPackage != nil {
		if err != nil {
			return Protocol2Response{}, protocol2MutationError{error: err, attempted: true}
		}
		return Protocol2Response{}, protocol2MutationError{error: errInvalidRuntimeResponse, attempted: true}
	}
	if response.OK && (response.State != "running_development" ||
		response.Execution != "development" || response.ActivePackage == nil ||
		response.ActivePackage.PackageID != packageID || response.ActivePackage.StaticComposition != nil || response.ActivePackage.SlotComposition != nil || !reflect.DeepEqual(response.ActivePackage.Composition, composition)) {
		return Protocol2Response{}, protocol2MutationError{error: errInvalidRuntimeResponse, attempted: true}
	}
	return response, nil
}

func (client *Client) LoadROMLinkedCore(ctx context.Context, path, packageID, root, expansionPath, payloadPath string, composition *expansion.Composition, programmedPath string, identity corepackage.ROMLinkIdentity) (Protocol2Response, error) {
	return client.LoadROMLinkedCoreWithInitialMedia(ctx, path, packageID, root, expansionPath, payloadPath, composition, programmedPath, identity, nil)
}

func (client *Client) LoadROMLinkedCoreWithInitialMedia(ctx context.Context, path, packageID, root, expansionPath, payloadPath string, composition *expansion.Composition, programmedPath string, identity corepackage.ROMLinkIdentity, initial *InitialMediaRequest) (Protocol2Response, error) {
	return client.loadROMCore(ctx, path, packageID, root, "", expansionPath, payloadPath, composition, programmedPath, "", identity, initial)
}

// LoadFirmwareCore launches a package whose firmware the runtime uploads over
// the fes.computer mailbox. firmwarePath is the ROM source itself; the receipt
// binds it to the package's unchanged payload. The runtime's computer ABI must
// list fes.fabric.firmware-upload 1.0; rom_linking is not required.
func (client *Client) LoadFirmwareCore(ctx context.Context, path, packageID, root, firmwarePath string, identity corepackage.ROMLinkIdentity, initial *InitialMediaRequest, gameID string) (Protocol2Response, error) {
	if !validRuntimePath(firmwarePath) || identity.Role != protocol.FirmwareRole || (gameID != "" && !protocol.ValidMediaGameID(gameID)) {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	return client.loadROMCore(ctx, path, packageID, root, gameID, "", "", nil, "", firmwarePath, identity, initial)
}

// firmwareUploadSupported reports a runtime that uploads a ROM source itself.
func firmwareUploadSupported(capabilities Protocol2Capabilities) bool {
	for _, abi := range capabilities.ABIs {
		if abi.ID != protocol.ComputerABIID {
			continue
		}
		for _, i := range abi.Interfaces {
			if i == (Protocol2Interface{ID: "fes.fabric.firmware-upload", Major: 1}) {
				return true
			}
		}
	}
	return false
}

// romDeliverySupported is the capability a single-ROM package needs: an
// upload-capable computer for a firmware-upload package, a linker otherwise.
func romDeliverySupported(capabilities Protocol2Capabilities, descriptor corepackage.Descriptor) bool {
	if corepackage.FirmwareUpload(descriptor) {
		return firmwareUploadSupported(capabilities)
	}
	return capabilities.ROMLinking == 1
}

// loadROMCore sends one single-ROM load: a linked bitstream at programmedPath,
// or a ROM source at firmwarePath, never both.
func (client *Client) loadROMCore(ctx context.Context, path, packageID, root, gameID, expansionPath, payloadPath string, composition *expansion.Composition, programmedPath, firmwarePath string, identity corepackage.ROMLinkIdentity, initial *InitialMediaRequest) (Protocol2Response, error) {
	if !validInitialMediaRequest(initial) {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	if !validRuntimePath(path) || (firmwarePath == "") == (programmedPath == "") || (firmwarePath == "" && !validRuntimePath(programmedPath)) || !protocol2Hex64.MatchString(packageID) || !protocol2Identifier.MatchString(identity.ROMID) || !identity.ValidFor(corepackage.Descriptor{Format: 3, ROM: &corepackage.ROM{ID: identity.ROMID, Role: identity.Role, SHA256: identity.MapSHA256, SourceSize: identity.SourceSize}}) {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	if (root != "" && !validRuntimePath(root)) || (gameID != "" && (root == "" || firmwarePath == "" || !protocol.ValidMediaGameID(gameID))) {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	if composition != nil && (root != "" || !validRuntimePath(expansionPath) || !validRuntimePath(payloadPath) || !validComposition(*composition, packageID)) {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	if composition == nil && (expansionPath != "" || payloadPath != "") {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	before, err := client.Protocol2Status(ctx)
	if err != nil {
		return Protocol2Response{}, protocol2MutationError{error: err, attempted: false}
	}
	if (firmwarePath == "" && before.Capabilities.ROMLinking != 1) || (firmwarePath != "" && !firmwareUploadSupported(before.Capabilities)) {
		return Protocol2Response{}, protocol2MutationError{error: errInvalidRuntimeResponse, attempted: false}
	}
	operation := "load_rom_core"
	if root != "" {
		operation = "load_rom_library_core"
	}
	if composition != nil {
		operation = "load_rom_composed_core"
	}
	line, attempted, err := client.callRawTracked(ctx, struct {
		InitialMedia   *InitialMediaRequest   `json:"initial_media,omitempty"`
		Protocol       int                    `json:"protocol"`
		Operation      string                 `json:"operation"`
		PackagePath    string                 `json:"package_path"`
		PackageID      string                 `json:"package_id"`
		DataRoot       string                 `json:"data_root,omitempty"`
		GameID         string                 `json:"game_id,omitempty"`
		ExpansionPath  string                 `json:"expansion_path,omitempty"`
		PayloadPath    string                 `json:"payload_path,omitempty"`
		Composition    *expansion.Composition `json:"composition,omitempty"`
		ProgrammedPath string                 `json:"programmed_path,omitempty"`
		FirmwarePath   string                 `json:"firmware_path,omitempty"`
		ROMLink        protocol2ROMLink       `json:"rom_link"`
	}{InitialMedia: initial, Protocol: 2, Operation: operation, PackagePath: path, PackageID: packageID, DataRoot: root, GameID: gameID, ExpansionPath: expansionPath, PayloadPath: payloadPath, Composition: composition, ProgrammedPath: programmedPath, FirmwarePath: firmwarePath, ROMLink: protocol2ROMLinkFromIdentity(identity)})
	if err != nil {
		return Protocol2Response{}, protocol2MutationError{error: err, attempted: attempted}
	}
	response, err := decodeProtocol2Response(line)
	if err != nil || response.InspectedPackage != nil {
		if err != nil {
			return Protocol2Response{}, protocol2MutationError{error: err, attempted: true}
		}
		return Protocol2Response{}, protocol2MutationError{error: errInvalidRuntimeResponse, attempted: true}
	}
	if response.OK && (response.State != "running_development" ||
		response.Execution != "development" || response.ActivePackage == nil ||
		response.ActivePackage.PackageID != packageID || response.ActivePackage.StaticComposition != nil || response.ActivePackage.SlotComposition != nil || !reflect.DeepEqual(response.ActivePackage.Composition, composition) || !reflect.DeepEqual(response.ActivePackage.ROMLink, &identity) || !initialRequestMatches(response, initial)) {
		return Protocol2Response{}, protocol2MutationError{error: errInvalidRuntimeResponse, attempted: true}
	}
	return response, nil
}

// validROMLinksRequest checks the syntax of a two-source receipt; the daemon
// binds it to the sealed package.
func validROMLinksRequest(identity corepackage.ROMLinksIdentity) bool {
	if len(identity.Sources) != 2 || !protocol2Hex64.MatchString(identity.MapSHA256) || !protocol2Hex64.MatchString(identity.ProgrammedSHA256) || identity.ProgrammedSize < 1 || identity.ProgrammedSize > corepackage.MaxPayloadSize {
		return false
	}
	for index, source := range identity.Sources {
		role := "firmware"
		if index == 1 {
			role = "cartridge"
		}
		if !protocol2Identifier.MatchString(source.ID) || source.Role != role || !protocol2Hex64.MatchString(source.SourceSHA256) || source.SourceSize < 1024 || source.SourceSize > 262144 || source.SourceSize%1024 != 0 {
			return false
		}
	}
	return identity.Sources[0].ID != identity.Sources[1].ID && identity.Sources[0].SourceSize+identity.Sources[1].SourceSize <= 262144
}

// LoadROMLinksLinkedCore uses the same physical operation with a distinct
// two-source identity. An older daemon rejects the unknown field before load.
func (client *Client) LoadROMLinksLinkedCore(ctx context.Context, path, packageID, root, expansionPath, payloadPath string, composition *expansion.Composition, programmedPath string, identity corepackage.ROMLinksIdentity) (Protocol2Response, error) {
	if !validRuntimePath(path) || !validRuntimePath(programmedPath) || !protocol2Hex64.MatchString(packageID) || !validROMLinksRequest(identity) {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	if root != "" && !validRuntimePath(root) {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	if composition != nil && (root != "" || !validRuntimePath(expansionPath) || !validRuntimePath(payloadPath) || !validComposition(*composition, packageID)) {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	if composition == nil && (expansionPath != "" || payloadPath != "") {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	before, err := client.Protocol2Status(ctx)
	if err != nil {
		return Protocol2Response{}, protocol2MutationError{error: err, attempted: false}
	}
	if before.Capabilities.ROMLinking != 1 {
		return Protocol2Response{}, protocol2MutationError{error: errInvalidRuntimeResponse, attempted: false}
	}
	operation := "load_rom_core"
	if root != "" {
		operation = "load_rom_library_core"
	}
	if composition != nil {
		operation = "load_rom_composed_core"
	}
	line, attempted, err := client.callRawTracked(ctx, struct {
		Protocol       int                          `json:"protocol"`
		Operation      string                       `json:"operation"`
		PackagePath    string                       `json:"package_path"`
		PackageID      string                       `json:"package_id"`
		DataRoot       string                       `json:"data_root,omitempty"`
		ExpansionPath  string                       `json:"expansion_path,omitempty"`
		PayloadPath    string                       `json:"payload_path,omitempty"`
		Composition    *expansion.Composition       `json:"composition,omitempty"`
		ProgrammedPath string                       `json:"programmed_path"`
		ROMLinks       corepackage.ROMLinksIdentity `json:"rom_links"`
	}{2, operation, path, packageID, root, expansionPath, payloadPath, composition, programmedPath, identity})
	if err != nil {
		return Protocol2Response{}, protocol2MutationError{error: err, attempted: attempted}
	}
	response, err := decodeProtocol2Response(line)
	if err != nil || response.InspectedPackage != nil {
		if err != nil {
			return Protocol2Response{}, protocol2MutationError{error: err, attempted: true}
		}
		return Protocol2Response{}, protocol2MutationError{error: errInvalidRuntimeResponse, attempted: true}
	}
	if response.OK && (response.State != "running_development" || response.Execution != "development" || response.ActivePackage == nil || response.ActivePackage.PackageID != packageID || response.ActivePackage.StaticComposition != nil || response.ActivePackage.SlotComposition != nil || !reflect.DeepEqual(response.ActivePackage.Composition, composition) || !reflect.DeepEqual(response.ActivePackage.ROMLinks, &identity)) {
		return Protocol2Response{}, protocol2MutationError{error: errInvalidRuntimeResponse, attempted: true}
	}
	return response, nil
}

func (client *Client) LoadComposedCore(ctx context.Context, path, packageID, expansionPath, payloadPath string, composition expansion.Composition) (Protocol2Response, error) {
	if !validRuntimePath(expansionPath) || !validRuntimePath(payloadPath) || !validComposition(composition, packageID) {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	response, _, err := client.loadCoreWithComposition(ctx, path, packageID, "", expansionPath, payloadPath, &composition)
	return response, err
}

func (client *Client) loadCoreWithComposition(ctx context.Context, path, packageID, root, expansionPath, payloadPath string, composition *expansion.Composition) (Protocol2Response, Protocol2Response, error) {
	if !validRuntimePath(path) || !protocol2Hex64.MatchString(packageID) {
		return Protocol2Response{}, Protocol2Response{}, errInvalidRuntimeRequest
	}
	before, err := client.Protocol2Status(ctx)
	if err != nil {
		return Protocol2Response{}, Protocol2Response{}, protocol2MutationError{error: err, attempted: false}
	}
	operation := "load_core"
	if composition != nil {
		operation = "load_composed_core"
	}
	if root != "" {
		operation = "load_library_core"
	}
	line, attempted, err := client.callRawTracked(ctx, struct {
		Protocol      int                    `json:"protocol"`
		Operation     string                 `json:"operation"`
		PackagePath   string                 `json:"package_path"`
		PackageID     string                 `json:"package_id"`
		DataRoot      string                 `json:"data_root,omitempty"`
		ExpansionPath string                 `json:"expansion_path,omitempty"`
		PayloadPath   string                 `json:"payload_path,omitempty"`
		Composition   *expansion.Composition `json:"composition,omitempty"`
	}{Protocol: 2, Operation: operation, PackagePath: path, PackageID: packageID, DataRoot: root, ExpansionPath: expansionPath, PayloadPath: payloadPath, Composition: composition})
	if err != nil {
		return Protocol2Response{}, before, protocol2MutationError{error: err, attempted: attempted}
	}
	response, err := decodeProtocol2Response(line)
	if err != nil || response.InspectedPackage != nil {
		if err != nil {
			return Protocol2Response{}, before, protocol2MutationError{error: err, attempted: true}
		}
		return Protocol2Response{}, before, protocol2MutationError{error: errInvalidRuntimeResponse, attempted: true}
	}
	if response.OK && (response.State != "running_development" ||
		response.Execution != "development" || response.ActivePackage == nil ||
		response.ActivePackage.PackageID != packageID || response.ActivePackage.StaticComposition != nil || response.ActivePackage.SlotComposition != nil || !reflect.DeepEqual(response.ActivePackage.Composition, composition)) {
		return Protocol2Response{}, before, protocol2MutationError{error: errInvalidRuntimeResponse, attempted: true}
	}
	return response, before, nil
}

func (client *Client) InspectCore(ctx context.Context, path, packageID string) (Protocol2Response, error) {
	if !validRuntimePath(path) || !protocol2Hex64.MatchString(packageID) {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	line, err := client.callRaw(ctx, struct {
		Protocol    int    `json:"protocol"`
		Operation   string `json:"operation"`
		PackagePath string `json:"package_path"`
		PackageID   string `json:"package_id"`
	}{Protocol: 2, Operation: "inspect_core", PackagePath: path, PackageID: packageID})
	if err != nil {
		return Protocol2Response{}, err
	}
	response, err := decodeProtocol2Response(line)
	if err != nil {
		return Protocol2Response{}, err
	}
	if response.OK != (response.InspectedPackage != nil) ||
		(response.OK && response.InspectedPackage.PackageID != packageID) {
		return Protocol2Response{}, errInvalidRuntimeResponse
	}
	return response, nil
}

// Protocol2LoadDevelopmentRBF admits only the contained diagnostic profile.
func (client *Client) Protocol2LoadDevelopmentRBF(ctx context.Context, rbf string) (Protocol2Response, error) {
	if !validRuntimePath(rbf) {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	line, attempted, err := client.callRawTracked(ctx, struct {
		Protocol           int    `json:"protocol"`
		Operation          string `json:"operation"`
		RBF                string `json:"rbf"`
		ProgrammingProfile string `json:"programming_profile"`
	}{2, "load_development_rbf", rbf, "development-contained-v1"})
	if err != nil {
		return Protocol2Response{}, protocol2MutationError{error: err, attempted: attempted}
	}
	response, err := decodeProtocol2Response(line)
	if err != nil || response.InspectedPackage != nil || response.CoreData != nil || (response.OK && !validDevelopmentRunning(response)) {
		return Protocol2Response{}, protocol2MutationError{error: errInvalidRuntimeResponse, attempted: true}
	}
	return response, nil
}
