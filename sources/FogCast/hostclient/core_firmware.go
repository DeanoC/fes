package hostclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/DeanoC/FogCast/protocol"
)

// CoreMedia is one content-addressed household media object from POST /api/v1/core-media.
type CoreMedia struct {
	MediaID string `json:"media_id"`
	Size    int64  `json:"size"`
}

// CoreFirmware is GET/PUT /api/v1/library/firmware. Empty MediaID means unset.
type CoreFirmware struct {
	Slot    string `json:"slot"`
	MediaID string `json:"media_id,omitempty"`
	Size    int64  `json:"size,omitempty"`
}

// ImportCoreMedia posts a bounded application/octet-stream body to
// POST /api/v1/core-media. Content-Length is required. HTTP 200 and 201 are
// both success (existing digest vs newly stored).
func (c *Client) ImportCoreMedia(ctx context.Context, size int64, content io.Reader) (CoreMedia, error) {
	if size < 1 {
		return CoreMedia{}, fmt.Errorf("core media is empty")
	}
	if size > protocol.MaxContentBytes {
		return CoreMedia{}, fmt.Errorf("core media exceeds %d bytes", protocol.MaxContentBytes)
	}
	if content == nil {
		return CoreMedia{}, fmt.Errorf("core media input is invalid")
	}
	req, err := c.NewRequest(ctx, http.MethodPost, "/api/v1/core-media", content)
	if err != nil {
		return CoreMedia{}, err
	}
	req.ContentLength = size
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTPClient().Do(req)
	if err != nil {
		return CoreMedia{}, err
	}
	defer resp.Body.Close()
	body, err := ReadResponseBody(resp, maxResponseBytes)
	if err != nil {
		return CoreMedia{}, err
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return CoreMedia{}, APIStatusError(resp.StatusCode, body)
	}
	var media CoreMedia
	if err := json.Unmarshal(body, &media); err != nil {
		return CoreMedia{}, fmt.Errorf("core media: %w", err)
	}
	if protocol.ValidateDigest(media.MediaID) != nil || media.Size != size {
		return CoreMedia{}, fmt.Errorf("core media identity or size is invalid")
	}
	return media, nil
}

// HouseholdFirmware loads GET /api/v1/library/firmware.
func (c *Client) HouseholdFirmware(ctx context.Context) (CoreFirmware, error) {
	var slot CoreFirmware
	if err := c.getJSON(ctx, "/api/v1/library/firmware", &slot); err != nil {
		return CoreFirmware{}, err
	}
	return slot, nil
}

// SelectHouseholdFirmware binds or clears the household firmware slot.
// Empty mediaID clears the slot.
func (c *Client) SelectHouseholdFirmware(ctx context.Context, mediaID string) (CoreFirmware, error) {
	mediaID = strings.TrimSpace(mediaID)
	if mediaID != "" && protocol.ValidateDigest(mediaID) != nil {
		return CoreFirmware{}, fmt.Errorf("firmware media ID is invalid")
	}
	payload, err := json.Marshal(map[string]string{
		"slot":     protocol.FirmwareRole,
		"media_id": mediaID,
	})
	if err != nil {
		return CoreFirmware{}, err
	}
	req, err := c.NewRequest(ctx, http.MethodPut, "/api/v1/library/firmware", bytes.NewReader(payload))
	if err != nil {
		return CoreFirmware{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTPClient().Do(req)
	if err != nil {
		return CoreFirmware{}, err
	}
	defer resp.Body.Close()
	body, err := ReadResponseBody(resp, maxResponseBytes)
	if err != nil {
		return CoreFirmware{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return CoreFirmware{}, APIStatusError(resp.StatusCode, body)
	}
	var slot CoreFirmware
	if err := json.Unmarshal(body, &slot); err != nil {
		return CoreFirmware{}, fmt.Errorf("household firmware: %w", err)
	}
	if strings.TrimSpace(slot.Slot) == "" {
		slot.Slot = protocol.FirmwareRole
	}
	return slot, nil
}

// BundledFirmware lists the firmware the host ships and selects by default.
// A host from before bundled firmware has no route; it answers 404 (or 405
// or 501 behind a launcher), and that reads as no bundled firmware, so a
// caller never claims built-in firmware the host cannot confirm.
func (c *Client) BundledFirmware(ctx context.Context) ([]protocol.BundledFirmware, error) {
	req, err := c.NewRequest(ctx, http.MethodGet, "/api/v1/library/firmware/bundled", http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTPClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := ReadResponseBody(resp, maxResponseBytes)
	if err != nil {
		return nil, err
	}
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented:
		return nil, nil
	default:
		return nil, APIStatusError(resp.StatusCode, body)
	}
	var list protocol.BundledFirmwareList
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, fmt.Errorf("bundled firmware: %w", err)
	}
	out := make([]protocol.BundledFirmware, 0, len(list.Firmware))
	for _, fw := range list.Firmware {
		if fw.CoreID != "" && fw.ROMID != "" && fw.Size > 0 && protocol.ValidateDigest(fw.MediaID) == nil {
			out = append(out, fw)
		}
	}
	return out, nil
}

// BundledFirmwareFor finds the bundled image for one core's named ROM.
func BundledFirmwareFor(list []protocol.BundledFirmware, coreID, romID string, size int64) (protocol.BundledFirmware, bool) {
	for _, fw := range list {
		if fw.CoreID == coreID && fw.ROMID == romID && fw.Size == size {
			return fw, true
		}
	}
	return protocol.BundledFirmware{}, false
}
