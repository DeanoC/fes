package misterruntime

import (
	"context"
)

// SendMouseRelative delivers a transient vector plus complete two-button state.
// A lost response is never replayed: the runtime may have accepted the motion.
func (client *Client) SendMouseRelative(ctx context.Context, packageID string, generation uint64, dx, dy int16, buttons uint8) (Protocol2Response, error) {
	if !protocol2Hex64.MatchString(packageID) || generation == 0 || buttons > 3 {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	line, err := client.callRaw(ctx, struct {
		Protocol           int    `json:"protocol"`
		Operation          string `json:"operation"`
		PackageID          string `json:"package_id"`
		ExpectedGeneration uint64 `json:"expected_generation"`
		DX                 int16  `json:"dx"`
		DY                 int16  `json:"dy"`
		Buttons            uint8  `json:"buttons"`
	}{2, "send_mouse_relative", packageID, generation, dx, dy, buttons})
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

type protocol2MouseControl interface {
	SendMouseRelative(context.Context, string, uint64, int16, int16, uint8) (Protocol2Response, error)
}

func (r *Runtime) SendMouseRelative(ctx context.Context, packageID string, generation uint64, dx, dy int16, buttons uint8) error {
	control, ok := r.control.(protocol2MouseControl)
	if !ok {
		return unsupportedOperationError()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	response, err := control.SendMouseRelative(ctx, packageID, generation, dx, dy, buttons)
	if err != nil {
		return err
	}
	if !response.OK {
		return mapProtocol2Error(response.Error)
	}
	return nil
}
