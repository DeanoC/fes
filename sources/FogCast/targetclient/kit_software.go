package targetclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode"

	"github.com/DeanoC/FogCast/appliance/software"
	"github.com/DeanoC/FogCast/appliance/softwarecontrol"
	"github.com/DeanoC/FogCast/protocol"
)

const maxKitSoftwareResponse = 32 << 20

func (c *Client) KitSoftwareStatus(ctx context.Context) (softwarecontrol.Status, error) {
	var result softwarecontrol.Status
	err := c.kitSoftwareRequest(ctx, http.MethodGet, "/v1/kit/software", "", 0, nil, &result)
	return result, err
}

func (c *Client) KitSoftwareReleases(ctx context.Context) ([]software.Release, error) {
	var result []software.Release
	if err := c.kitSoftwareRequest(ctx, http.MethodGet, "/v1/kit/software/releases", "", 0, nil, &result); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, release := range result {
		if !validSoftwareRelease(release) || seen[release.ID] {
			return nil, errors.New("invalid or duplicate software release inventory")
		}
		seen[release.ID] = true
	}
	return result, nil
}

// InstallKitSoftware never selects or activates the uploaded immutable bundle.
// Mutations are sent once. Reconcile a lost reply through status and inventory.
func (c *Client) InstallKitSoftware(ctx context.Context, size int64, content io.Reader, expectedID string) (software.Release, error) {
	if size < 1 || size > software.MaxBundleSize || content == nil || !software.ValidID(expectedID) {
		return software.Release{}, errors.New("invalid software installation input")
	}
	var result software.Release
	if err := c.kitSoftwareRequest(ctx, http.MethodPost, "/v1/kit/software/releases", "application/octet-stream", size, content, &result); err != nil {
		return result, err
	}
	if !validSoftwareRelease(result) || result.ID != expectedID {
		return software.Release{}, errors.New("installed software identity does not match the requested bundle")
	}
	return result, nil
}

// QueueKitSoftware requests a future idle-boundary activation, without claiming
// the running session. expected_generation rejects competing selection changes.
func (c *Client) QueueKitSoftware(ctx context.Context, request softwarecontrol.Request) (softwarecontrol.Status, error) {
	if !software.ValidID(request.ID) || !software.ValidID(request.Candidate) || request.Requester == "" || len(request.Requester) > 160 || strings.TrimSpace(request.Requester) != request.Requester || strings.ContainsFunc(request.Requester, unicode.IsControl) {
		return softwarecontrol.Status{}, errors.New("invalid software activation request")
	}
	result, err := c.kitSoftwareMutation(ctx, "/v1/kit/software/activate", request)
	if err != nil {
		return result, err
	}
	if result.Pending != nil && result.Pending.Request == request {
		return result, nil
	}
	if receipt, ok := result.Receipts[request.ID]; ok && receipt.Request == request {
		return result, nil
	}
	return softwarecontrol.Status{}, errors.New("software response does not identify the requested operation; inspect status")
}

func (c *Client) CancelKitSoftware(ctx context.Context, request softwarecontrol.CancelRequest) (softwarecontrol.Status, error) {
	if !software.ValidID(request.ID) {
		return softwarecontrol.Status{}, errors.New("invalid software cancellation request")
	}
	result, err := c.kitSoftwareMutation(ctx, "/v1/kit/software/cancel", request)
	if err != nil {
		return result, err
	}
	if receipt, ok := result.Receipts[request.ID]; ok && receipt.ID == request.ID && receipt.Generation == request.Generation && receipt.Result == "cancelled" {
		return result, nil
	}
	return softwarecontrol.Status{}, errors.New("software response does not identify the requested cancellation; inspect status")
}

func (c *Client) kitSoftwareMutation(ctx context.Context, path string, value any) (softwarecontrol.Status, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return softwarecontrol.Status{}, err
	}
	var result softwarecontrol.Status
	err = c.kitSoftwareRequest(ctx, http.MethodPost, path, "application/json", int64(len(data)), strings.NewReader(string(data)), &result)
	return result, err
}

func validSoftwareRelease(release software.Release) bool {
	id, err := release.Manifest.ID()
	return err == nil && id == release.ID
}

func (c *Client) kitSoftwareRequest(ctx context.Context, method, path, contentType string, size int64, content io.Reader, result any) error {
	var body io.Reader
	if content != nil {
		body = readOnlyReader{Reader: content}
	}
	request, err := http.NewRequestWithContext(ctx, method, c.endpoint(path, nil).String(), body)
	if err != nil {
		return err
	}
	request.ContentLength = size
	request.Header.Set("Authorization", "Bearer "+c.token)
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("software request failed; inspect software status before repeating a mutation: %w", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxKitSoftwareResponse+1))
	if err != nil {
		return err
	}
	if len(data) > maxKitSoftwareResponse {
		return errors.New("software response exceeds size limit")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var envelope protocol.ErrorEnvelope
		if err := json.Unmarshal(data, &envelope); err != nil {
			return fmt.Errorf("decode software HTTP %d error: %w", response.StatusCode, err)
		}
		if envelope.Error.Code == "" {
			return fmt.Errorf("software HTTP %d response has no symbolic error code", response.StatusCode)
		}
		return &envelope.Error
	}
	if err := json.Unmarshal(data, result); err != nil {
		return fmt.Errorf("decode software response: %w", err)
	}
	return nil
}
