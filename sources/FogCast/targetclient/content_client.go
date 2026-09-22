package targetclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/DeanoC/FogCast/protocol"
)

func (c *Client) CacheIndex(ctx context.Context) (protocol.CacheIndex, error) {
	var index protocol.CacheIndex
	if err := c.doJSON(ctx, http.MethodGet, "/v2/cache", nil, &index); err != nil {
		return protocol.CacheIndex{}, err
	}
	if index.UsedBytes < 0 || index.MaxBytes < 0 || index.FreeBytes < 0 {
		return protocol.CacheIndex{}, fmt.Errorf("cache index has invalid byte counts")
	}
	if index.Entries == nil {
		index.Entries = []protocol.CacheIndexEntry{}
	}
	for _, entry := range index.Entries {
		if err := protocol.ValidateSystem(entry.System); err != nil {
			return protocol.CacheIndex{}, fmt.Errorf("cache index entry system is invalid")
		}
		if err := protocol.ValidateContentKey(protocol.ContentKey{SHA256: entry.SHA256, Extension: entry.Extension}); err != nil {
			return protocol.CacheIndex{}, fmt.Errorf("cache index entry is invalid")
		}
		if entry.Size < 0 {
			return protocol.CacheIndex{}, fmt.Errorf("cache index entry size is invalid")
		}
	}
	return index, nil
}

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

func (c *Client) CachedIdentity(ctx context.Context, gameID string) (protocol.CachedIdentityResponse, error) {
	if err := protocol.ValidateGameID(gameID); err != nil {
		return protocol.CachedIdentityResponse{}, fmt.Errorf("validate game ID: %w", err)
	}
	var response protocol.CachedIdentityResponse
	if err := c.doJSON(ctx, http.MethodGet, "/v2/hostless/identity/"+gameID, nil, &response); err != nil {
		return protocol.CachedIdentityResponse{}, err
	}
	if !response.Present {
		if response.GameID != "" || response.System != nil || response.Content != nil {
			return protocol.CachedIdentityResponse{}, fmt.Errorf("cached identity response includes identity for absent content")
		}
		return response, nil
	}
	if response.GameID != gameID || response.System == nil || response.Content == nil ||
		protocol.ValidateSystem(*response.System) != nil || protocol.ValidateContentIdentity(*response.Content) != nil {
		return protocol.CachedIdentityResponse{}, fmt.Errorf("cached identity response does not match requested game")
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

func (r readOnlyReader) Close() error {
	closer, ok := r.Reader.(io.Closer)
	if !ok {
		return nil
	}
	return closer.Close()
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

func (e *transferError) AmbiguousMutation() bool {
	var transportErr *url.Error
	return errors.Is(e.cause, context.DeadlineExceeded) || errors.Is(e.cause, context.Canceled) ||
		errors.Is(e.cause, io.EOF) || errors.Is(e.cause, io.ErrUnexpectedEOF) || errors.As(e.cause, &transportErr)
}
