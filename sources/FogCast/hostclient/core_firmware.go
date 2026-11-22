package hostclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/DeanoC/FogCast/protocol"
)

// CoreMedia is one content-addressed household media object from POST /api/v1/core-media.
type CoreMedia struct {
	MediaID string `json:"media_id"`
	Size    int64  `json:"size"`
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
	return c.importCoreMedia(ctx, "/api/v1/core-media", size, content, c.HTTPClient())
}

// HardDiskImportBudget allows a 128 KiB/s link, matching the host's import
// allowance with a 30-second client margin.
func HardDiskImportBudget(size int64) time.Duration {
	return 90*time.Second + time.Duration(size/(128<<10))*time.Second
}

// ImportHardDiskMedia uses the raw ATA image route without widening ordinary imports.
func (c *Client) ImportHardDiskMedia(ctx context.Context, size int64, content io.Reader) (CoreMedia, error) {
	if !protocol.AdmitAtaDiskSize(size) || content == nil {
		return CoreMedia{}, fmt.Errorf("hard-disk image must be 1 to 128 MiB in whole 512-byte sectors")
	}
	client := *c.LaunchHTTPClient()
	client.Timeout = HardDiskImportBudget(size)
	return c.importCoreMedia(ctx, "/api/v1/core-media/hard-disk", size, content, &client)
}

func (c *Client) importCoreMedia(ctx context.Context, path string, size int64, content io.Reader, client *http.Client) (CoreMedia, error) {
	req, err := c.NewRequest(ctx, http.MethodPost, path, content)
	if err != nil {
		return CoreMedia{}, err
	}
	req.ContentLength = size
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
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

// BundledFirmware lists the firmware the host ships and selects by default.
// A host from before bundled firmware has no route; it answers 404 (or 405
// or 501 behind a launcher), and that reads as no bundled firmware, so a
// caller never claims built-in firmware the host cannot confirm.
func (c *Client) BundledFirmware(ctx context.Context) ([]protocol.BundledFirmware, error) {
	list, err := c.bundledObjects(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]protocol.BundledFirmware, 0, len(list.Firmware))
	for _, fw := range list.Firmware {
		if fw.CoreID != "" && fw.ROMID != "" && fw.Size > 0 && protocol.ValidateDigest(fw.MediaID) == nil {
			out = append(out, fw)
		}
	}
	return out, nil
}

// BundledDisks lists the disks the host ships, such as the PC/XT's FreeDOS
// boot disk. A host from before bundled disks lists none.
func (c *Client) BundledDisks(ctx context.Context) ([]protocol.BundledDisk, error) {
	list, err := c.bundledObjects(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]protocol.BundledDisk, 0, len(list.Disks))
	for _, d := range list.Disks {
		if d.CoreID != "" && d.Size > 0 && protocol.ValidateDigest(d.MediaID) == nil {
			out = append(out, d)
		}
	}
	return out, nil
}

func (c *Client) bundledObjects(ctx context.Context) (protocol.BundledFirmwareList, error) {
	req, err := c.NewRequest(ctx, http.MethodGet, "/api/v1/library/firmware/bundled", http.NoBody)
	if err != nil {
		return protocol.BundledFirmwareList{}, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTPClient().Do(req)
	if err != nil {
		return protocol.BundledFirmwareList{}, err
	}
	defer resp.Body.Close()
	body, err := ReadResponseBody(resp, maxResponseBytes)
	if err != nil {
		return protocol.BundledFirmwareList{}, err
	}
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented:
		return protocol.BundledFirmwareList{}, nil
	default:
		return protocol.BundledFirmwareList{}, APIStatusError(resp.StatusCode, body)
	}
	var list protocol.BundledFirmwareList
	if err := json.Unmarshal(body, &list); err != nil {
		return protocol.BundledFirmwareList{}, fmt.Errorf("bundled firmware: %w", err)
	}
	return list, nil
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
