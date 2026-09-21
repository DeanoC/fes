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
