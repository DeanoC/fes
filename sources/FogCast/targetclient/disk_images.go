package targetclient

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/DeanoC/FogCast/protocol"
)

const (
	diskImageProbePath = "/v1/library/disk-images/probe"
	diskImageStagePath = "/v1/library/disk-images"
)

// diskImagesUnsupported is how an agent that predates hard-disk staging
// answers: its mux has no such route.
func diskImagesUnsupported() error {
	return &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Phase: "admission",
		Message: "the kit's software predates hard-disk images; install a newer kit bundle"}
}

// DiskImage reports whether the kit holds a staged hard-disk image.
func (c *Client) DiskImage(ctx context.Context, sha256 string) (protocol.DiskImageStatus, error) {
	if !lowerHex(sha256, 64) {
		return protocol.DiskImageStatus{}, fmt.Errorf("disk image identity is invalid")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(diskImageProbePath, nil).String(), http.NoBody)
	if err != nil {
		return protocol.DiskImageStatus{}, err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set(protocol.DiskImageHeader, sha256)
	return c.diskImageExchange(request, sha256)
}

// StageDiskImage stores a hard-disk image on the kit; the kit verifies its
// digest before it can be launched.
func (c *Client) StageDiskImage(ctx context.Context, sha256 string, size int64, body io.Reader) (protocol.DiskImageStatus, error) {
	if !lowerHex(sha256, 64) || !protocol.AdmitAtaDiskSize(size) || body == nil {
		return protocol.DiskImageStatus{}, fmt.Errorf("disk image upload is invalid")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(diskImageStagePath, nil).String(), readOnlyReader{Reader: body})
	if err != nil {
		return protocol.DiskImageStatus{}, err
	}
	request.ContentLength = size
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Content-Type", "application/octet-stream")
	request.Header.Set(protocol.DiskImageHeader, sha256)
	if err := c.authorizeMutation(request); err != nil {
		return protocol.DiskImageStatus{}, err
	}
	return c.diskImageExchange(request, sha256)
}

func (c *Client) diskImageExchange(request *http.Request, sha256 string) (protocol.DiskImageStatus, error) {
	response, err := c.httpClient.Do(request)
	if err != nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		return protocol.DiskImageStatus{}, newTransferError(err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return protocol.DiskImageStatus{}, diskImagesUnsupported()
	}
	var status protocol.DiskImageStatus
	if err := decodeResponse(response, &status); err != nil {
		return protocol.DiskImageStatus{}, err
	}
	if status.SHA256 != sha256 || (status.Staged && !protocol.AdmitAtaDiskSize(status.Size)) || (!status.Staged && status.Size != 0) {
		return protocol.DiskImageStatus{}, fmt.Errorf("disk image response does not match the request")
	}
	return status, nil
}
