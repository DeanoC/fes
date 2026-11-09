package hostclient

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/DeanoC/FogCast/internal/localcores"
	"github.com/DeanoC/FogCast/protocol"
)

// PrepareLocalCoreEntry resolves a local package and optional catalog cartridge to a host entry.
func (c *Client) PrepareLocalCoreEntry(ctx context.Context, packageID, gameID string) (CoreEntry, error) {
	if (packageID == "" && gameID == "") || (packageID != "" && protocol.ValidateDigest(packageID) != nil) || (gameID != "" && protocol.ValidateGameID(gameID) != nil) {
		return CoreEntry{}, fmt.Errorf("invalid package or game identity")
	}
	var entry CoreEntry
	request := struct {
		PackageID string `json:"package_id"`
		GameID    string `json:"game_id"`
	}{packageID, gameID}
	operationHTTP := *c.launchHTTP
	if operationHTTP.Timeout != 0 && operationHTTP.Timeout < 120*time.Second {
		operationHTTP.Timeout = 120 * time.Second
	}
	operationClient := *c
	operationClient.httpClient = &operationHTTP
	if err := operationClient.doJSONStatuses(ctx, http.MethodPost, "/api/v1/library/local-core-entry", request, &entry, http.StatusOK); err != nil {
		return CoreEntry{}, err
	}
	if protocol.ValidateGameID(entry.GameID) != nil || protocol.ValidateDigest(entry.PackageID) != nil || (packageID != "" && entry.PackageID != packageID) || entry.CoreID == "" || (packageID == "" && entry.CoreID != "fes.sms") {
		return CoreEntry{}, fmt.Errorf("kit host entry identity mismatch")
	}
	return entry, nil
}

// KitLocalCores returns the kit agent's selected inventory through the host.
func (c *Client) KitLocalCores(ctx context.Context) ([]localcores.Core, error) {
	var result struct {
		Cores []localcores.Core `json:"cores"`
	}
	if err := c.getJSON(ctx, "/api/v1/kit/local-cores", &result); err != nil {
		return nil, err
	}
	if result.Cores == nil {
		return nil, fmt.Errorf("invalid kit core inventory")
	}
	for _, core := range result.Cores {
		if core.CoreID == "" || protocol.ValidateDigest(core.PackageID) != nil {
			return nil, fmt.Errorf("invalid kit core identity")
		}
	}
	return result.Cores, nil
}
