package misterruntime

import "context"

// ControllerRequest is a full snapshot tied to an observed package generation.
type ControllerRequest struct {
	PackageID  string `json:"package_id"`
	Generation uint64 `json:"expected_generation"`
	Port       uint8  `json:"port"`
	Buttons    uint8  `json:"buttons"`
	Keypad     uint16 `json:"keypad"`
}

func (client *Client) SetController(ctx context.Context, request ControllerRequest) (Protocol2Response, error) {
	if !protocol2Hex64.MatchString(request.PackageID) || request.Generation == 0 || request.Port > 1 || request.Keypad > 4095 {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	line, err := client.callRaw(ctx, struct {
		Protocol  int    `json:"protocol"`
		Operation string `json:"operation"`
		ControllerRequest
	}{2, "set_controller", request})
	if err != nil {
		return Protocol2Response{}, err
	}
	response, err := decodeProtocol2Response(line)
	if err == nil && response.OK && (response.State != "running_development" || response.ActivePackage == nil || response.ActivePackage.PackageID != request.PackageID || response.Generation == nil || *response.Generation != request.Generation) {
		return Protocol2Response{}, errInvalidRuntimeResponse
	}
	return response, err
}

func (r *Runtime) ControllerStatus(ctx context.Context) (Protocol2Response, error) {
	control, ok := r.control.(protocol2StatusControl)
	if !ok {
		return Protocol2Response{}, unsupportedOperationError()
	}
	return control.Protocol2Status(ctx)
}

func (r *Runtime) SetController(ctx context.Context, request ControllerRequest) error {
	control, ok := r.control.(interface {
		SetController(context.Context, ControllerRequest) (Protocol2Response, error)
	})
	if !ok {
		return unsupportedOperationError()
	}
	response, err := control.SetController(ctx, request)
	if err == nil && !response.OK {
		err = mapProtocol2Error(response.Error)
	}
	if err != nil && request.Buttons == 0 && request.Keypad == 0 {
		// An explicit Stop may have already retired this generation. Prove it
		// with a fresh observation; do not send zero to the replacement or hide
		// an error while this generation is still active or uncertain.
		if status, statusErr := r.ControllerStatus(ctx); statusErr == nil && controllerBindingRetired(status, request) {
			return nil
		}
	}
	return err
}

func controllerBindingRetired(status Protocol2Response, request ControllerRequest) bool {
	if !status.OK || status.Error != nil {
		return false
	}
	if status.State == "idle" {
		return status.ActivePackage == nil && status.Generation == nil
	}
	return status.State == "running_development" && status.ActivePackage != nil && status.Generation != nil && *status.Generation != 0 &&
		(status.ActivePackage.PackageID != request.PackageID || *status.Generation != request.Generation)
}
