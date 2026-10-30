package hostclient

import (
	"context"
	"fmt"
	"net/url"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
)

// CorePackage reads an installed package's immutable identity and descriptor.
// It does not depend on the current library filter or next-start selection.
func (c *Client) CorePackage(ctx context.Context, id string) (corepackage.Inspection, error) {
	if protocol.ValidateDigest(id) != nil {
		return corepackage.Inspection{}, fmt.Errorf("invalid package identity")
	}
	var result corepackage.Inspection
	if err := c.getJSON(ctx, "/api/v1/core-packages/"+url.PathEscape(id), &result); err != nil {
		return result, err
	}
	if result.PackageID != id || result.Descriptor.Core.ID == "" {
		return corepackage.Inspection{}, fmt.Errorf("installed package identity changed")
	}
	return result, nil
}
