package hostclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/DeanoC/FogCast/protocol"
)

// SessionDisplayCapable recognizes the verified simple-computer display
// contracts. A package name or an installed package alone cannot grant HDMI
// controls to the running machine.
func SessionDisplayCapable(p *SessionCorePackage) bool {
	if p == nil || p.Generation == 0 || protocol.ValidateDigest(p.PackageID) != nil {
		return false
	}
	status := &protocol.CorePackageStatus{PackageID: p.PackageID, Generation: p.Generation, ABI: protocol.RuntimeContract{ID: p.ABI.ID, Major: p.ABI.Major, Minor: p.ABI.Minor}}
	for _, i := range p.ActiveInterfaces {
		status.ActiveInterfaces = append(status.ActiveInterfaces, protocol.RuntimeInterface{ID: i.ID, Major: i.Major, Minor: i.Minor})
	}
	return protocol.SessionDisplayCapable(status)
}

// SetSessionDisplayForSession opens or returns the kit display for the captured
// session. It does not look up a new foreground session or start/stop a core.
func (c *Client) SetSessionDisplayForSession(ctx context.Context, prior SessionResult, visible bool) (SessionResult, error) {
	if prior.State != "active" || strings.TrimSpace(prior.ID) == "" || strings.TrimSpace(prior.Target) == "" || !SessionDisplayCapable(prior.CorePackage) {
		return SessionResult{}, fmt.Errorf("session HDMI controls are unavailable")
	}
	binding := protocol.DevelopmentMediaBinding{PackageID: prior.CorePackage.PackageID, Generation: prior.CorePackage.Generation, Target: prior.Target, TargetID: prior.TargetID}
	if !binding.Valid() {
		return SessionResult{}, fmt.Errorf("session HDMI display identity is invalid")
	}
	payload, err := json.Marshal(struct {
		Visible bool `json:"visible"`
	}{visible})
	if err != nil {
		return SessionResult{}, err
	}
	req, err := c.NewRequest(ctx, http.MethodPost, "/api/v1/session/display", bytes.NewReader(payload))
	if err != nil {
		return SessionResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set(protocol.HostSessionIDHeader, prior.ID)
	binding.SetHeaders(req.Header)
	resp, err := c.HTTPClient().Do(req)
	if err != nil {
		return SessionResult{}, err
	}
	defer resp.Body.Close()
	body, err := ReadResponseBody(resp, maxResponseBytes)
	if err != nil {
		return SessionResult{HTTPStatus: resp.StatusCode}, err
	}
	result, err := DecodeSession(resp.StatusCode, body)
	if err != nil {
		return result, fmt.Errorf("session display response: %w", err)
	}
	if resp.StatusCode != http.StatusOK || result.ErrorCode != "" {
		return result, APIStatusError(resp.StatusCode, body)
	}
	if result.State != "active" || result.ID != prior.ID || result.Target != prior.Target || result.TargetID != prior.TargetID ||
		result.GameID != prior.GameID || result.FlightID != prior.FlightID || result.CorePackage == nil ||
		result.CorePackage.PackageID != binding.PackageID || result.CorePackage.Generation != binding.Generation {
		return result, fmt.Errorf("running machine changed during session display request")
	}
	return result, nil
}
