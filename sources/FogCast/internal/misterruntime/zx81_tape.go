package misterruntime

import (
	"context"
	"os"
	"path/filepath"

	"github.com/DeanoC/FogCast/protocol"
)

type protocol2ZX81TapeControl interface {
	CaptureZX81Tape(context.Context, string, string, uint64) (Protocol2Response, error)
}

type protocol2ZX81TapeAcknowledger interface {
	AcknowledgeZX81Tape(context.Context, string, uint64) (Protocol2Response, error)
}

func (c *Client) CaptureZX81Tape(ctx context.Context, path, packageID string, generation uint64) (Protocol2Response, error) {
	if !validRuntimePath(path) || protocol.ValidateDigest(packageID) != nil || generation == 0 {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	line, _, err := c.callRawTracked(ctx, struct {
		Protocol   int    `json:"protocol"`
		Operation  string `json:"operation"`
		Path       string `json:"path"`
		PackageID  string `json:"expected_package_id"`
		Generation uint64 `json:"expected_generation"`
	}{2, "capture_zx81_tape", path, packageID, generation})
	if err != nil {
		return Protocol2Response{}, err
	}
	return decodeComputerMediaResponse(line, packageID, generation)
}

func (c *Client) AcknowledgeZX81Tape(ctx context.Context, packageID string, generation uint64) (Protocol2Response, error) {
	if protocol.ValidateDigest(packageID) != nil || generation == 0 {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	line, _, err := c.callRawTracked(ctx, struct {
		Protocol   int    `json:"protocol"`
		Operation  string `json:"operation"`
		PackageID  string `json:"expected_package_id"`
		Generation uint64 `json:"expected_generation"`
	}{2, "acknowledge_zx81_tape", packageID, generation})
	if err != nil {
		return Protocol2Response{}, err
	}
	return decodeComputerMediaResponse(line, packageID, generation)
}

// CaptureZX81Tape asks the runtime to write a pending SAVE image, then returns
// those bytes. An empty result means no SAVE is pending.
func (r *Runtime) CaptureZX81Tape(ctx context.Context, packageID string, generation uint64) ([]byte, *protocol.APIError) {
	if protocol.ValidateDigest(packageID) != nil || generation == 0 {
		return nil, protocol.MediaUnitRequestError()
	}
	control, ok := r.control.(protocol2ZX81TapeControl)
	if !ok {
		return nil, unsupportedOperationError()
	}
	dir, err := os.MkdirTemp("", "fogcast-zx81-tape-")
	if err != nil {
		return nil, unavailableError()
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "program.p")
	response, err := control.CaptureZX81Tape(ctx, path, packageID, generation)
	r.noteDispatch("capture_zx81_tape", err == nil && response.OK)
	if err != nil {
		return nil, unavailableError()
	}
	if !response.OK || response.Error != nil {
		return nil, mapProtocol2Error(response.Error)
	}
	data, err := os.ReadFile(path)
	if err != nil || (len(data) != 0 && !protocol.AdmitZx81TapeSize(int64(len(data)))) {
		return nil, unavailableError()
	}
	return data, nil
}

// AcknowledgeZX81Tape releases the pending SAVE only after the host library
// has stored its bytes and row durably.
func (r *Runtime) AcknowledgeZX81Tape(ctx context.Context, packageID string, generation uint64) *protocol.APIError {
	if protocol.ValidateDigest(packageID) != nil || generation == 0 {
		return protocol.MediaUnitRequestError()
	}
	control, ok := r.control.(protocol2ZX81TapeAcknowledger)
	if !ok {
		return unsupportedOperationError()
	}
	response, err := control.AcknowledgeZX81Tape(ctx, packageID, generation)
	r.noteDispatch("acknowledge_zx81_tape", err == nil && response.OK)
	if err != nil {
		return unavailableError()
	}
	if !response.OK || response.Error != nil {
		return mapProtocol2Error(response.Error)
	}
	return nil
}
