package misterruntime

import (
	"context"
	"reflect"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/misteross/expansion"
)

func validStaticComposition(c expansion.StaticComposition, packageID string, descriptor *corepackage.Descriptor) bool {
	id, err := expansion.StaticCompositionID(c)
	if err != nil || id != c.ID || c.PackageID != packageID {
		return false
	}
	if descriptor == nil {
		return true
	}
	s := descriptor.StaticExpansion
	if descriptor.Format != 5 || s == nil || c.BaseSHA256 != descriptor.Payload.SHA256 || c.BaseSize != descriptor.Payload.Size || c.ControlBuildID != s.ControlBuildID {
		return false
	}
	return c.ExpansionID != "" || (c.PersonaSHA256 == s.SHA256 && c.PersonaSize == s.Size && c.HardwareID == uint32(s.HardwareID))
}

func cloneStaticComposition(c *expansion.StaticComposition) *expansion.StaticComposition {
	if c == nil {
		return nil
	}
	owned := *c
	return &owned
}

// LoadStaticComposedCore uses the existing cold composed-core operation.
// The runtime independently admits both images; there is no linked payload
// path, live persona command or programming capability added by this client.
func (client *Client) LoadStaticComposedCore(ctx context.Context, path, packageID, expansionPath string, composition expansion.StaticComposition) (Protocol2Response, error) {
	if !validRuntimePath(path) || !validStaticComposition(composition, packageID, nil) ||
		(composition.ExpansionID == "") != (expansionPath == "") || (expansionPath != "" && !validRuntimePath(expansionPath)) {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	if _, err := client.Protocol2Status(ctx); err != nil {
		return Protocol2Response{}, protocol2MutationError{error: err, attempted: false}
	}
	line, attempted, err := client.callRawTracked(ctx, struct {
		Protocol      int                         `json:"protocol"`
		Operation     string                      `json:"operation"`
		PackagePath   string                      `json:"package_path"`
		PackageID     string                      `json:"package_id"`
		ExpansionPath string                      `json:"expansion_path,omitempty"`
		Composition   expansion.StaticComposition `json:"composition"`
	}{2, "load_composed_core", path, packageID, expansionPath, composition})
	if err != nil {
		return Protocol2Response{}, protocol2MutationError{error: err, attempted: attempted}
	}
	response, err := decodeProtocol2Response(line)
	if err != nil {
		return Protocol2Response{}, protocol2MutationError{error: err, attempted: true}
	}
	if response.InspectedPackage != nil || (response.OK && (response.State != "running_development" || response.Execution != "development" ||
		response.ActivePackage == nil || response.ActivePackage.PackageID != packageID || !reflect.DeepEqual(response.ActivePackage.StaticComposition, &composition))) {
		return Protocol2Response{}, protocol2MutationError{error: errInvalidRuntimeResponse, attempted: true}
	}
	return response, nil
}

type protocol2StaticCompositionControl interface {
	LoadStaticComposedCore(context.Context, string, string, string, expansion.StaticComposition) (Protocol2Response, error)
}
