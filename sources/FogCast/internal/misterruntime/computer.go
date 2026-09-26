package misterruntime

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/DeanoC/FogCast/protocol"
)

// computerMediaBudget covers the runtime's live unit transfer (MediaInfo,
// Begin, 512-byte chunks, Commit and the ready check) plus its reply.
const computerMediaBudget = 135 * time.Second

// KeyboardHIDRows is one complete fes.keyboard.hid 1.0 state: rows 0..7 hold
// Keyboard/Keypad usages 16r..16r+15 and row 8 the 0xe0..0xe7 modifiers.
type KeyboardHIDRows [9]uint16

// Valid rejects the HID error usages 0..3 and undefined modifier bits.
func (rows KeyboardHIDRows) Valid() bool {
	return rows[0]&0x000f == 0 && rows[8]&0xff00 == 0
}

func computerResponseMatches(response Protocol2Response, packageID string, generation uint64) bool {
	return response.OK && response.Error == nil && response.State == "running_development" &&
		response.Execution == "development" && response.ActivePackage != nil &&
		response.ActivePackage.PackageID == packageID && response.Generation != nil && *response.Generation == generation &&
		protocol.ComputerABI(response.ActivePackage.Descriptor.ABI.ID, response.ActivePackage.Descriptor.ABI.Major, response.ActivePackage.Descriptor.ABI.Minor)
}

// SetKeyboardHID sends one complete nine-row key state bound to the exact
// active fes.computer package generation. The runtime writes only changed rows.
func (client *Client) SetKeyboardHID(ctx context.Context, packageID string, generation uint64, rows KeyboardHIDRows) (Protocol2Response, error) {
	if !protocol2Hex64.MatchString(packageID) || generation == 0 || !rows.Valid() {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	line, err := client.callRaw(ctx, struct {
		Protocol           int             `json:"protocol"`
		Operation          string          `json:"operation"`
		PackageID          string          `json:"package_id"`
		ExpectedGeneration uint64          `json:"expected_generation"`
		Rows               KeyboardHIDRows `json:"rows"`
	}{2, "set_keyboard_hid", packageID, generation, rows})
	if err != nil {
		return Protocol2Response{}, err
	}
	response, err := decodeProtocol2Response(line)
	if err != nil {
		return Protocol2Response{}, err
	}
	if response.InspectedPackage != nil || response.CoreData != nil || (response.OK && !computerResponseMatches(response, packageID, generation)) {
		return Protocol2Response{}, errInvalidRuntimeResponse
	}
	return response, nil
}

// InsertMedia transfers an already staged exact image into one unit while the
// machine runs. The runtime ejects the unit once on any failure.
func (client *Client) InsertMedia(ctx context.Context, path, packageID string, generation uint64, unit uint8, size uint32) (Protocol2Response, error) {
	if !validRuntimePath(path) || !protocol2Hex64.MatchString(packageID) || generation == 0 || unit > 7 ||
		size < 1 || int64(size) > protocol.MaxComputerMediaBytes {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	line, _, err := client.callRawTracked(ctx, struct {
		Protocol           int    `json:"protocol"`
		Operation          string `json:"operation"`
		Path               string `json:"path"`
		ExpectedPackageID  string `json:"expected_package_id"`
		ExpectedGeneration uint64 `json:"expected_generation"`
		Unit               uint8  `json:"unit"`
		Size               uint32 `json:"size"`
	}{2, "insert_media", path, packageID, generation, unit, size})
	if err != nil {
		return Protocol2Response{}, err
	}
	return decodeComputerMediaResponse(line, packageID, generation)
}

// EjectMedia empties one unit. It is idempotent in the runtime.
func (client *Client) EjectMedia(ctx context.Context, packageID string, generation uint64, unit uint8) (Protocol2Response, error) {
	if !protocol2Hex64.MatchString(packageID) || generation == 0 || unit > 7 {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	line, _, err := client.callRawTracked(ctx, struct {
		Protocol           int    `json:"protocol"`
		Operation          string `json:"operation"`
		ExpectedPackageID  string `json:"expected_package_id"`
		ExpectedGeneration uint64 `json:"expected_generation"`
		Unit               uint8  `json:"unit"`
	}{2, "eject_media", packageID, generation, unit})
	if err != nil {
		return Protocol2Response{}, err
	}
	return decodeComputerMediaResponse(line, packageID, generation)
}

func decodeComputerMediaResponse(line []byte, packageID string, generation uint64) (Protocol2Response, error) {
	response, err := decodeProtocol2Response(line)
	if err != nil {
		return Protocol2Response{}, err
	}
	if response.InspectedPackage != nil || response.CoreData != nil || (response.OK && !computerResponseMatches(response, packageID, generation)) {
		return Protocol2Response{}, errInvalidRuntimeResponse
	}
	return response, nil
}

type protocol2KeyboardHIDControl interface {
	SetKeyboardHID(context.Context, string, uint64, KeyboardHIDRows) (Protocol2Response, error)
}

type protocol2ComputerMediaControl interface {
	protocol2StatusControl
	InsertMedia(context.Context, string, string, uint64, uint8, uint32) (Protocol2Response, error)
	EjectMedia(context.Context, string, uint64, uint8) (Protocol2Response, error)
}

// SetKeyboardHID posts the complete key state for one package generation.
func (r *Runtime) SetKeyboardHID(ctx context.Context, packageID string, generation uint64, rows KeyboardHIDRows) error {
	control, ok := r.control.(protocol2KeyboardHIDControl)
	if !ok {
		return unsupportedOperationError()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// Unlike the simple-computer mailbox, live media never holds reset or
	// clears keys here, so posts do not wait behind a media transfer; the
	// runtime serializes its own GP session and every post is a full state.
	response, err := control.SetKeyboardHID(ctx, packageID, generation, rows)
	if err != nil {
		return err
	}
	if !response.OK {
		return mapProtocol2Error(response.Error)
	}
	return nil
}

func computerMediaStatus(response Protocol2Response) protocol.Status {
	if response.ActivePackage == nil {
		return protocol.Status{}
	}
	activation := activationFromProtocol2(response.ActivePackage.PackageID, response.ActivePackage.Descriptor, response)
	return protocol.Status{State: protocol.StateActive, Development: true, CorePackage: corePackageStatus(activation)}
}

func computerMediaUnitState(response Protocol2Response, b protocol.MediaUnitBinding) (string, bool) {
	if !computerResponseMatches(response, b.PackageID, b.Generation) {
		return "", false
	}
	status := computerMediaStatus(response)
	if !b.Matches(status) {
		return "", false
	}
	unit, _ := protocol.MediaUnit(status.CorePackage, b.Unit)
	return unit.State, true
}

// InsertMedia stages caller bytes privately and makes one insert_media call.
// Only this adapter names a local path. It never replays an ambiguous call.
// Success requires the unit to report ready; it returns the refreshed units.
func (r *Runtime) InsertMedia(ctx, owner context.Context, size int64, body io.Reader, b protocol.MediaUnitBinding) (units []protocol.MediaUnitStatus, apiErr *protocol.APIError) {
	if !b.Valid() || body == nil || size < 1 || size > protocol.MaxComputerMediaBytes {
		return nil, protocol.MediaUnitRequestError()
	}
	control, ok := r.control.(protocol2ComputerMediaControl)
	if !ok {
		return nil, unsupportedOperationError()
	}
	if ctx.Err() != nil {
		return nil, &protocol.APIError{Code: protocol.CodeTransferFailed, Message: "media upload cancelled", Phase: "admission", Cause: ctx.Err()}
	}
	r.computerMu.Lock()
	defer r.computerMu.Unlock()
	admit := func() *protocol.APIError {
		before, err := control.Protocol2Status(ctx)
		if err != nil {
			return unavailableError()
		}
		if _, ok := computerMediaUnitState(before, b); !ok {
			return protocol.MediaUnitIdentityError()
		}
		if !b.AcceptsSize(computerMediaStatus(before), size) {
			return protocol.MediaUnitRequestError()
		}
		return nil
	}
	if err := admit(); err != nil {
		return nil, err
	}
	root, err := filepath.Abs(os.TempDir())
	if err != nil {
		return nil, unavailableError()
	}
	dir, err := os.MkdirTemp(root, "fogcast-media-unit-")
	if err != nil {
		return nil, unavailableError()
	}
	defer func() {
		if err := os.RemoveAll(dir); err != nil {
			var cause error = err
			if apiErr != nil {
				cause = errors.Join(apiErr, err)
			}
			units = nil
			apiErr = &protocol.APIError{Code: protocol.CodeInternal, Message: "media unit staging cleanup failed", Phase: "recovery", Cause: cause}
		}
	}()
	path := filepath.Join(dir, "media.bin")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, unavailableError()
	}
	writeErr := stageMediaStream(ctx, file, body, size)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil || ctx.Err() != nil {
		return nil, &protocol.APIError{Code: protocol.CodeTransferFailed, Message: "media unit staging failed", Phase: "admission", Cause: errors.Join(writeErr, closeErr, ctx.Err())}
	}
	// Recheck after staging, immediately before the one local mutation.
	if err := admit(); err != nil {
		return nil, err
	}
	operation, cancel := context.WithTimeout(owner, computerMediaBudget)
	defer cancel()
	response, err := control.InsertMedia(operation, path, b.PackageID, b.Generation, b.Unit, uint32(size))
	r.noteDispatch("insert_media", err == nil)
	if err != nil {
		return nil, &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "media unit insert is unconfirmed; inspect the session before retrying", Phase: "transfer"}
	}
	if !response.OK || response.Error != nil {
		return nil, mapProtocol2Error(response.Error)
	}
	state, ok := computerMediaUnitState(response, b)
	if !ok || state != protocol.MediaUnitReady {
		return nil, unavailableError()
	}
	return cloneMediaUnits(response.Capabilities.MediaUnits), nil
}

// EjectMedia empties one unit of the exact generation and returns the units.
func (r *Runtime) EjectMedia(ctx context.Context, b protocol.MediaUnitBinding) ([]protocol.MediaUnitStatus, *protocol.APIError) {
	if !b.Valid() {
		return nil, protocol.MediaUnitRequestError()
	}
	control, ok := r.control.(protocol2ComputerMediaControl)
	if !ok {
		return nil, unsupportedOperationError()
	}
	r.computerMu.Lock()
	defer r.computerMu.Unlock()
	before, err := control.Protocol2Status(ctx)
	if err != nil {
		return nil, unavailableError()
	}
	if _, ok := computerMediaUnitState(before, b); !ok {
		return nil, protocol.MediaUnitIdentityError()
	}
	response, err := control.EjectMedia(ctx, b.PackageID, b.Generation, b.Unit)
	r.noteDispatch("eject_media", err == nil)
	if err != nil {
		return nil, &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "media unit eject is unconfirmed; inspect the session before retrying", Phase: "transfer"}
	}
	if !response.OK || response.Error != nil {
		return nil, mapProtocol2Error(response.Error)
	}
	state, ok := computerMediaUnitState(response, b)
	if !ok || state != protocol.MediaUnitEmpty {
		return nil, unavailableError()
	}
	return cloneMediaUnits(response.Capabilities.MediaUnits), nil
}

// MediaUnits reads the live unit states of one exact fes.computer generation,
// for example after a failed transfer that the runtime ejected.
func (r *Runtime) MediaUnits(ctx context.Context, packageID string, generation uint64) ([]protocol.MediaUnitStatus, bool) {
	response, err := r.boundedStatus(ctx)
	if err != nil || !computerResponseMatches(response, packageID, generation) {
		return nil, false
	}
	return cloneMediaUnits(response.Capabilities.MediaUnits), true
}
