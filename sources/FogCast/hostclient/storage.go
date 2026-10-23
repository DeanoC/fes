package hostclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/DeanoC/FogCast/storage"
)

type DriveInventory struct {
	Drives []storage.Drive `json:"drives"`
	Notice string          `json:"notice,omitempty"`
}

func (c *Client) storageJSON(ctx context.Context, method, path string, body, result any) error {
	var input io.Reader = http.NoBody
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		input = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, input)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.launchHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("drive request failed (%d): check the drive, share and credentials", resp.StatusCode)
	}
	if result == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(result)
}

func (c *Client) StorageDrives(ctx context.Context, discover bool) (DriveInventory, error) {
	path := "/api/v1/storage/drives"
	if discover {
		path += "?discover=true"
	}
	var out DriveInventory
	err := c.storageJSON(ctx, http.MethodGet, path, nil, &out)
	return out, err
}
func (c *Client) SaveStorageDrive(ctx context.Context, d storage.Drive) (storage.Drive, error) {
	var out storage.Drive
	err := c.storageJSON(ctx, http.MethodPut, "/api/v1/storage/drives", d, &out)
	return out, err
}
func (c *Client) ForgetStorageDrive(ctx context.Context, id string) error {
	return c.storageJSON(ctx, http.MethodDelete, "/api/v1/storage/drives/"+url.PathEscape(id), nil, nil)
}
func (c *Client) StorageEntries(ctx context.Context, id, path string) ([]storage.Entry, error) {
	var out struct {
		Entries []storage.Entry `json:"entries"`
	}
	err := c.storageJSON(ctx, http.MethodGet, "/api/v1/storage/drives/"+url.PathEscape(id)+"/entries?path="+url.QueryEscape(path), nil, &out)
	return out.Entries, err
}
func (c *Client) OpenStorageFile(ctx context.Context, id, path string) (io.ReadCloser, int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/storage/drives/"+url.PathEscape(id)+"/file?path="+url.QueryEscape(path), http.NoBody)
	if err != nil {
		return nil, 0, err
	}
	resp, err := c.launchHTTP.Do(req)
	if err != nil {
		return nil, 0, err
	}
	if resp.StatusCode != http.StatusOK || resp.ContentLength < 1 || resp.ContentLength > storage.MaxFileBytes {
		resp.Body.Close()
		return nil, 0, fmt.Errorf("drive file unavailable or exceeds %d bytes", storage.MaxFileBytes)
	}
	return resp.Body, resp.ContentLength, nil
}
