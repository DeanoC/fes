package host

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/DeanoC/FogCast-POC/protocol"
)

func (c *Client) ProbeContent(ctx context.Context, system protocol.System, content protocol.ContentIdentity) (protocol.CacheProbeResponse, error) {
	path, query, err := contentEndpoint(system, content)
	if err != nil {
		return protocol.CacheProbeResponse{}, err
	}

	var response protocol.CacheProbeResponse
	if err := c.doJSONQuery(ctx, http.MethodGet, path, query, nil, &response); err != nil {
		return protocol.CacheProbeResponse{}, err
	}
	if !response.Present {
		if response.System != nil || response.Content != nil {
			return protocol.CacheProbeResponse{}, fmt.Errorf("cache probe response includes identity for absent content")
		}
		return response, nil
	}
	if response.System == nil || *response.System != system || response.Content == nil || *response.Content != content {
		return protocol.CacheProbeResponse{}, fmt.Errorf("cache probe response does not match requested content")
	}
	return response, nil
}

func (c *Client) UploadContent(ctx context.Context, system protocol.System, content protocol.ContentIdentity, body io.Reader) (protocol.CacheUploadResponse, error) {
	path, query, err := contentEndpoint(system, content)
	if err != nil {
		return protocol.CacheUploadResponse{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, c.endpoint(path, query).String(), readOnlyReader{Reader: body})
	if err != nil {
		return protocol.CacheUploadResponse{}, fmt.Errorf("create request: %w", err)
	}
	request.ContentLength = content.Size
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Content-Type", "application/octet-stream")

	response, err := c.httpClient.Do(request)
	if err != nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		return protocol.CacheUploadResponse{}, newTransferError(err)
	}
	defer response.Body.Close()

	var result protocol.CacheUploadResponse
	if err := decodeResponse(response, &result); err != nil {
		return protocol.CacheUploadResponse{}, err
	}
	switch result.Result {
	case protocol.CacheUploadPresent, protocol.CacheUploadCreated:
	default:
		return protocol.CacheUploadResponse{}, fmt.Errorf("cache upload response has invalid result")
	}
	if result.System != system || result.Content != content {
		return protocol.CacheUploadResponse{}, fmt.Errorf("cache upload response does not match requested content")
	}
	return result, nil
}

func (c *Client) LaunchContent(ctx context.Context, request protocol.CachedLaunchRequest) (protocol.CachedLaunchResponse, error) {
	if err := protocol.ValidateGameID(request.GameID); err != nil {
		return protocol.CachedLaunchResponse{}, fmt.Errorf("validate game ID: %w", err)
	}
	if err := validateSystemContent(request.System, request.Content); err != nil {
		return protocol.CachedLaunchResponse{}, err
	}

	var response protocol.CachedLaunchResponse
	if err := c.doJSON(ctx, http.MethodPost, "/v2/launch", request, &response); err != nil {
		return protocol.CachedLaunchResponse{}, err
	}
	if response.Status.State != protocol.StateActive ||
		response.Status.GameID == nil || *response.Status.GameID != request.GameID ||
		response.Status.System == nil || *response.Status.System != request.System ||
		response.Content != request.Content {
		return protocol.CachedLaunchResponse{}, fmt.Errorf("content launch response does not match requested launch")
	}
	return response, nil
}

func contentEndpoint(system protocol.System, content protocol.ContentIdentity) (string, url.Values, error) {
	if err := validateSystemContent(system, content); err != nil {
		return "", nil, err
	}
	return "/v2/cache/" + string(system) + "/" + content.SHA256, url.Values{"extension": {content.Extension}}, nil
}

func validateSystemContent(system protocol.System, content protocol.ContentIdentity) error {
	if err := protocol.ValidateSystem(system); err != nil {
		return fmt.Errorf("validate system: %w", err)
	}
	if err := protocol.ValidateContentIdentity(content); err != nil {
		return fmt.Errorf("validate content: %w", err)
	}
	return nil
}

// readOnlyReader intentionally hides concrete replay helpers such as
// bytes.Reader.WriteTo and the types for which net/http populates GetBody.
type readOnlyReader struct {
	io.Reader
}

type transferError struct {
	api   *protocol.APIError
	cause error
}

func newTransferError(cause error) error {
	return &transferError{
		api:   &protocol.APIError{Code: protocol.CodeTransferFailed, Message: "content upload transfer failed"},
		cause: cause,
	}
}

func (e *transferError) Error() string {
	return e.api.Error()
}

func (e *transferError) Unwrap() []error {
	return []error{e.api, e.cause}
}
