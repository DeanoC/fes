package targetclient

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/DeanoC/FogCast/protocol"
)

// CaptureZX81Tape reads the SAVE image from the running machine. The body is
// the raw .p. A machine that has not completed SAVE returns an error.
func (c *Client) CaptureZX81Tape(ctx context.Context, packageID string, generation uint64) ([]byte, error) {
	binding := protocol.MediaUnitBinding{PackageID: packageID, Generation: generation, Unit: protocol.Zx81TapeUnit}
	if c == nil || !binding.Valid() || binding.Unit != protocol.Zx81TapeUnit {
		return nil, protocol.MediaUnitRequestError()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint("/v1/zx81-tape/capture", nil).String(), http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	binding.SetHeaders(request.Header)
	request.Header.Set("Authorization", "Bearer "+c.token)
	if err := c.authorizeMutation(request); err != nil {
		return nil, err
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, protocol.Zx81TapeMaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read ZX81 tape: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return nil, decodeResponse(&http.Response{StatusCode: response.StatusCode, Body: io.NopCloser(bytes.NewReader(data)), Header: response.Header}, &struct{}{})
	}
	if int64(len(data)) > protocol.Zx81TapeMaxBytes || !protocol.AdmitZx81TapeSize(int64(len(data))) {
		return nil, fmt.Errorf("ZX81 tape capture is the wrong size")
	}
	return data, nil
}
