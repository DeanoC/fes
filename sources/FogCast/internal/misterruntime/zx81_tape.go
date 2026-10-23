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

// CaptureZX81Tape asks the runtime to write the SAVE image, then returns those
// bytes. An empty or clean machine is an error; Stop ignores it.
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
	if err != nil || !protocol.AdmitZx81TapeSize(int64(len(data))) {
		return nil, unavailableError()
	}
	return data, nil
}
