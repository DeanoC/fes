package targetclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
)

// InstalledCorePackages reads durable installations without claiming the kit.
func (c *Client) InstalledCorePackages(ctx context.Context) (protocol.KitCorePackages, error) {
	var result protocol.KitCorePackages
	if err := c.doJSON(ctx, http.MethodGet, "/v1/core-packages", nil, &result); err != nil {
		return result, err
	}
	if result.Packages == nil || result.Selections == nil {
		return protocol.KitCorePackages{}, fmt.Errorf("incomplete installed core inventory response")
	}
	packages := make(map[string]string, len(result.Packages))
	for _, value := range result.Packages {
		if !validInstalledCorePackage(value) {
			return protocol.KitCorePackages{}, fmt.Errorf("invalid installed core package response")
		}
		if _, exists := packages[value.PackageID]; exists {
			return protocol.KitCorePackages{}, fmt.Errorf("duplicate installed package identity")
		}
		packages[value.PackageID] = value.Descriptor.Core.ID
	}
	if err := validateCoreSelections(result.Selections); err != nil {
		return protocol.KitCorePackages{}, err
	}
	for _, selection := range result.Selections {
		if packages[selection.PackageID] != selection.CoreID {
			return protocol.KitCorePackages{}, fmt.Errorf("selection does not identify an installed package for its core")
		}
	}
	return result, nil
}

func (c *Client) CoreSelections(ctx context.Context) ([]protocol.KitCoreSelection, error) {
	var result []protocol.KitCoreSelection
	if err := c.doJSON(ctx, http.MethodGet, "/v1/core-selections", nil, &result); err != nil {
		return nil, err
	}
	if result == nil {
		return nil, fmt.Errorf("incomplete core selection inventory response")
	}
	if err := validateCoreSelections(result); err != nil {
		return nil, err
	}
	return result, nil
}

// InstallCorePackage transfers an immutable package independently of execution.
// The caller supplies the identity from its local inspection; success does not
// select or activate it. A lost reply must be reconciled using inventory.
func (c *Client) InstallCorePackage(ctx context.Context, size int64, content io.Reader, expectedPackageID string) (protocol.KitCoreInstall, error) {
	if size < 1 || size > corepackage.MaxArchiveSize || content == nil || !lowerHex(expectedPackageID, 64) {
		return protocol.KitCoreInstall{}, fmt.Errorf("invalid core package installation input")
	}
	var result protocol.KitCoreInstall
	if err := c.corePackageRequest(ctx, http.MethodPost, "/v1/core-packages", "application/octet-stream", size, content, &result); err != nil {
		return result, err
	}
	if !validInstalledCorePackage(result.Package) || result.Package.PackageID != expectedPackageID {
		return protocol.KitCoreInstall{}, fmt.Errorf("installed core identity does not match requested package")
	}
	return result, nil
}

// SelectCorePackage changes only future launch selection using an explicit
// expected identity. It neither claims a kit lease nor retries the mutation.
func (c *Client) SelectCorePackage(ctx context.Context, coreID, expectedPackageID, packageID string) (protocol.KitCoreSelection, error) {
	if !romStatusIDRE.MatchString(coreID) || !lowerHex(packageID, 64) || (expectedPackageID != "" && !lowerHex(expectedPackageID, 64)) {
		return protocol.KitCoreSelection{}, fmt.Errorf("invalid core package selection input")
	}
	body, err := json.Marshal(protocol.KitCoreSelectionRequest{ExpectedPackageID: expectedPackageID, PackageID: packageID})
	if err != nil {
		return protocol.KitCoreSelection{}, err
	}
	var result protocol.KitCoreSelection
	if err := c.corePackageRequest(ctx, http.MethodPut, "/v1/core-selections/"+coreID, "application/json", int64(len(body)), bytes.NewReader(body), &result); err != nil {
		return result, err
	}
	if result.CoreID != coreID || result.PackageID != packageID {
		return protocol.KitCoreSelection{}, fmt.Errorf("selected core identity does not match requested package")
	}
	return result, nil
}

func (c *Client) corePackageRequest(ctx context.Context, method, path, contentType string, size int64, content io.Reader, result any) error {
	request, err := http.NewRequestWithContext(ctx, method, c.endpoint(path, nil).String(), readOnlyReader{Reader: content})
	if err != nil {
		return fmt.Errorf("create package request: %w", err)
	}
	request.ContentLength = size
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Content-Type", contentType)
	response, err := c.httpClient.Do(request)
	if err != nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		return fmt.Errorf("send package request: %w", err)
	}
	defer response.Body.Close()
	return decodeResponse(response, result)
}

func validInstalledCorePackage(value corepackage.Inspection) bool {
	return lowerHex(value.PackageID, 64) && corepackage.ValidateDescriptor(value.Descriptor) == nil
}

func validateCoreSelections(values []protocol.KitCoreSelection) error {
	cores := make(map[string]bool, len(values))
	for _, value := range values {
		if !romStatusIDRE.MatchString(value.CoreID) || !lowerHex(value.PackageID, 64) || cores[value.CoreID] {
			return fmt.Errorf("invalid core selection response")
		}
		cores[value.CoreID] = true
	}
	return nil
}
