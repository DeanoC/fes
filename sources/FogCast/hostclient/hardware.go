package hostclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/DeanoC/FogCast/protocol"
)

// HardwareSnapshot keeps saved library setups separate from the current
// target-reported session. A nil Session means its state could not be read.
// ExpansionSelection is whether this listener accepts the single-entry
// next-start PUT. Nil means an older host did not advertise it.
type HardwareSnapshot struct {
	Machines           []HardwareMachine `json:"machines"`
	Computers          []ShelfComputer   `json:"computers,omitempty"`
	Session            *SessionResult    `json:"session,omitempty"`
	SessionError       string            `json:"session_error,omitempty"`
	ExpansionSelection *bool             `json:"expansion_selection,omitempty"`
}

// ShelfComputer is a host catalog computer Home can launch with session.launch.
// It is not a ZX81 workshop machine.
type ShelfComputer struct {
	GameID            string `json:"game_id"`
	Title             string `json:"title"`
	CoreID            string `json:"core_id"`
	PackageID         string `json:"package_id"`
	Ready             bool   `json:"ready"`
	UnavailableReason string `json:"unavailable_reason,omitempty"`
}

type HardwareMachine struct {
	GameID            string              `json:"game_id"`
	Title             string              `json:"title"`
	CoreID            string              `json:"core_id"`
	PackageID         string              `json:"package_id"`
	LaunchPackageID   string              `json:"launch_package_id,omitempty"`
	PackageReady      bool                `json:"package_ready"`
	FirmwareReady     bool                `json:"firmware_ready"`
	Ready             bool                `json:"ready"`
	UnavailableReason string              `json:"unavailable_reason,omitempty"`
	Socket            HardwareSocket      `json:"socket"`
	MediaID           string              `json:"media_id,omitempty"`
	MediaName         string              `json:"media_name,omitempty"`
	DraftExpansionID  string              `json:"draft_expansion_id"`
	Choices           []HardwareExpansion `json:"choices"`
}

type HardwareSocket struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Supported bool   `json:"supported"`
}

type HardwareExpansion struct {
	ExpansionID       string `json:"expansion_id"`
	Label             string `json:"label"`
	Description       string `json:"description"`
	InProgress        bool   `json:"in_progress,omitempty"`
	Ready             bool   `json:"ready"`
	UnavailableReason string `json:"unavailable_reason,omitempty"`
}

type CoreEntryExpansion struct {
	GameID      string `json:"game_id"`
	ExpansionID string `json:"expansion_id,omitempty"`
}

type CoreExpansionPresentation struct {
	ExpansionID string `json:"expansion_id"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

// Hardware reads admitted choices and the ordinary session projection in one
// host request. Local setup readiness does not assert target readiness.
func (c *Client) Hardware(ctx context.Context) (HardwareSnapshot, error) {
	var wire struct {
		Machines           []HardwareMachine `json:"machines"`
		Computers          []ShelfComputer   `json:"computers"`
		Session            json.RawMessage   `json:"session"`
		SessionError       string            `json:"session_error"`
		ExpansionSelection *bool             `json:"expansion_selection"`
	}
	if err := c.getJSON(ctx, "/api/v1/library/hardware", &wire); err != nil {
		return HardwareSnapshot{}, err
	}
	result := HardwareSnapshot{Machines: wire.Machines, Computers: wire.Computers, SessionError: wire.SessionError, ExpansionSelection: wire.ExpansionSelection}
	if result.Machines == nil {
		result.Machines = []HardwareMachine{}
	}
	for i := range result.Machines {
		if result.Machines[i].Choices == nil {
			result.Machines[i].Choices = []HardwareExpansion{}
		}
	}
	if len(wire.Session) != 0 && string(wire.Session) != "null" {
		session, err := DecodeSession(http.StatusOK, wire.Session)
		if err != nil {
			return HardwareSnapshot{}, fmt.Errorf("hardware session: %w", err)
		}
		result.Session = &session
	}
	return result, nil
}

// SelectCoreEntryExpansion saves a draft for the next ordinary library launch.
// Empty expansionID removes the card; expectedID must describe the last read.
func (c *Client) SelectCoreEntryExpansion(ctx context.Context, gameID, packageID, expectedID, expansionID string) (CoreEntryExpansion, error) {
	if protocol.ValidateGameID(gameID) != nil || protocol.ValidateDigest(packageID) != nil ||
		(expectedID != "" && protocol.ValidateDigest(expectedID) != nil) || (expansionID != "" && protocol.ValidateDigest(expansionID) != nil) {
		return CoreEntryExpansion{}, fmt.Errorf("expansion selection identity is invalid")
	}
	var result CoreEntryExpansion
	payload := map[string]string{"package_id": packageID, "expected_expansion_id": expectedID, "expansion_id": expansionID}
	path := "/api/v1/library/core-entries/" + url.PathEscape(gameID) + "/expansion"
	if err := c.doJSON(ctx, http.MethodPut, path, payload, &result); err != nil {
		return CoreEntryExpansion{}, err
	}
	if result.GameID != gameID || result.ExpansionID != expansionID {
		return CoreEntryExpansion{}, fmt.Errorf("host returned a different expansion selection")
	}
	return result, nil
}

func (c *Client) CoreExpansionPresentation(ctx context.Context, id string) (CoreExpansionPresentation, error) {
	if protocol.ValidateDigest(id) != nil {
		return CoreExpansionPresentation{}, fmt.Errorf("expansion identity is invalid")
	}
	var result CoreExpansionPresentation
	err := c.getJSON(ctx, "/api/v1/core-expansions/"+id+"/presentation", &result)
	return result, err
}

// SetCoreExpansionPresentation saves household labels only. The host still
// validates the immutable expansion against the exact package when fitting it.
func (c *Client) SetCoreExpansionPresentation(ctx context.Context, id, label, description string) (CoreExpansionPresentation, error) {
	if protocol.ValidateDigest(id) != nil {
		return CoreExpansionPresentation{}, fmt.Errorf("expansion identity is invalid")
	}
	var result CoreExpansionPresentation
	err := c.doJSON(ctx, http.MethodPut, "/api/v1/core-expansions/"+id+"/presentation", map[string]string{"label": label, "description": description}, &result)
	return result, err
}
