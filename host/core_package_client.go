package host

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"sort"

	"github.com/DeanoC/FogCast/internal/corepackage"
	"github.com/DeanoC/FogCast/protocol"
)

// LoadCore sends one bounded package mutation through the client's shared kit
// lease and accepts only a complete custom-development status.
func (c *Client) LoadCore(ctx context.Context, size int64, content io.Reader) (protocol.Status, error) {
	if size < 1 || size > corepackage.MaxArchiveSize || content == nil {
		return protocol.Status{}, fmt.Errorf("development core input is invalid")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.endpoint("/v1/development/core", nil).String(), readOnlyReader{Reader: content})
	if err != nil {
		return protocol.Status{}, fmt.Errorf("create request: %w", err)
	}
	request.ContentLength = size
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Content-Type", "application/octet-stream")
	if err := c.authorizeMutation(request); err != nil {
		return protocol.Status{}, err
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		return protocol.Status{}, newTransferError(err)
	}
	defer response.Body.Close()
	var status protocol.Status
	if err := decodeResponse(response, &status); err != nil {
		return protocol.Status{}, err
	}
	if !validCorePackageStatus(status) {
		return protocol.Status{}, fmt.Errorf("development core response does not match requested load")
	}
	return status, nil
}

func validCorePackageStatus(status protocol.Status) bool {
	if status.State != protocol.StateActive || !status.Development || status.GameID != nil ||
		status.System != nil || status.ExpectedCore != nil || status.LastError != nil ||
		status.Recovery != "" || status.CorePackage == nil {
		return false
	}
	value := status.CorePackage
	if !lowerHex(value.PackageID, 64) || value.Generation == 0 || value.ABI.ID == "" ||
		value.ABI.Major == 0 || !lowerHex(value.BuildID, 32) ||
		!sort.SliceIsSorted(value.ActiveInterfaces, func(i, j int) bool {
			return value.ActiveInterfaces[i].ID < value.ActiveInterfaces[j].ID
		}) {
		return false
	}
	gamepad := false
	for index, contract := range value.ActiveInterfaces {
		if contract.ID == "" || contract.Major == 0 ||
			(index > 0 && value.ActiveInterfaces[index-1].ID == contract.ID) {
			return false
		}
		if contract.ID == "fes.gamepad" && contract.Major == 1 && contract.Minor == 0 {
			gamepad = true
		}
	}
	return value.Gamepad == gamepad
}

func lowerHex(value string, size int) bool {
	if len(value) != size {
		return false
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != size/2 {
		return false
	}
	for _, char := range value {
		if char >= 'A' && char <= 'F' {
			return false
		}
	}
	return true
}
