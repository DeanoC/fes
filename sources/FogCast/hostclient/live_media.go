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

// LiveMediaCapable reports whether the session package can arm mid-session
// ZX81 /.p mailbox tape (fes.simple-computer + fes.media.blob).
func LiveMediaCapable(p *SessionCorePackage) bool {
	if p == nil {
		return false
	}
	status := &protocol.CorePackageStatus{
		PackageID:  p.PackageID,
		Generation: p.Generation,
		ABI:        protocol.RuntimeContract{ID: p.ABI.ID, Major: p.ABI.Major, Minor: p.ABI.Minor},
	}
	for _, i := range p.ActiveInterfaces {
		status.ActiveInterfaces = append(status.ActiveInterfaces, protocol.RuntimeInterface{
			ID: i.ID, Major: i.Major, Minor: i.Minor,
		})
	}
	return protocol.LiveMediaCapable(status)
}

// LiveMediaBinding builds the host change-tape / eject binding headers from a
// session projection. Target is required; TargetID is optional.
func LiveMediaBinding(s SessionResult) (protocol.DevelopmentMediaBinding, error) {
	if strings.TrimSpace(s.ID) == "" || s.State != "active" || s.CorePackage == nil ||
		!LiveMediaCapable(s.CorePackage) {
		return protocol.DevelopmentMediaBinding{}, protocol.LiveMediaIdentityError()
	}
	b := protocol.DevelopmentMediaBinding{
		PackageID:  strings.TrimSpace(s.CorePackage.PackageID),
		Generation: s.CorePackage.Generation,
		Target:     strings.TrimSpace(s.Target),
		TargetID:   strings.TrimSpace(s.TargetID),
	}
	if !b.Valid() || b.Target == "" {
		return protocol.DevelopmentMediaBinding{}, protocol.LiveMediaIdentityError()
	}
	return b, nil
}

// ReplaceLiveMedia arms a household core-media id into the active session via
// POST /api/v1/session/live-media. It does not inject BASIC LOAD "".
func (c *Client) ReplaceLiveMedia(ctx context.Context, mediaID, name string) (SessionResult, error) {
	mediaID = strings.TrimSpace(mediaID)
	name = strings.TrimSpace(name)
	if protocol.ValidateDigest(mediaID) != nil || !protocol.AdmitTapeMediaName(name) {
		return SessionResult{}, protocol.LiveMediaRequestError()
	}
	prior, err := c.Session(ctx)
	if err != nil {
		return SessionResult{}, err
	}
	b, err := LiveMediaBinding(prior)
	if err != nil {
		return SessionResult{}, err
	}
	payload, err := json.Marshal(protocol.LiveMediaRequest{MediaID: mediaID, Name: name})
	if err != nil {
		return SessionResult{}, err
	}
	return c.postLiveMedia(ctx, "/api/v1/session/live-media", bytes.NewReader(payload), int64(len(payload)), prior.ID, b)
}

// ClearLiveMedia ejects the armed mid-session tape via
// POST /api/v1/session/live-media/clear.
func (c *Client) ClearLiveMedia(ctx context.Context) (SessionResult, error) {
	prior, err := c.Session(ctx)
	if err != nil {
		return SessionResult{}, err
	}
	b, err := LiveMediaBinding(prior)
	if err != nil {
		return SessionResult{}, err
	}
	return c.postLiveMedia(ctx, "/api/v1/session/live-media/clear", http.NoBody, 0, prior.ID, b)
}

func (c *Client) postLiveMedia(ctx context.Context, path string, body io.Reader, contentLength int64, sessionID string, b protocol.DevelopmentMediaBinding) (SessionResult, error) {
	req, err := c.NewRequest(ctx, http.MethodPost, path, body)
	if err != nil {
		return SessionResult{}, err
	}
	if contentLength > 0 {
		req.Header.Set("Content-Type", "application/json")
		req.ContentLength = contentLength
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set(protocol.HostSessionIDHeader, sessionID)
	b.SetHeaders(req.Header)
	resp, err := c.HTTPClient().Do(req)
	if err != nil {
		return SessionResult{}, err
	}
	defer resp.Body.Close()
	raw, err := ReadResponseBody(resp, maxResponseBytes)
	if err != nil {
		return SessionResult{HTTPStatus: resp.StatusCode}, err
	}
	result, err := DecodeSession(resp.StatusCode, raw)
	if err != nil {
		return result, fmt.Errorf("live media response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		if result.ErrorCode != "" {
			return result, liveMediaAPIError(result)
		}
		return result, APIStatusError(resp.StatusCode, raw)
	}
	if result.State != "active" {
		return result, protocol.LiveMediaIdentityError()
	}
	return result, nil
}

func liveMediaAPIError(result SessionResult) error {
	msg := strings.TrimSpace(result.ErrorMessage)
	if msg == "" {
		msg = "live media request failed"
	}
	code := protocol.ErrorCode(result.ErrorCode)
	switch code {
	case protocol.CodeBusy:
		phase := "admission"
		if strings.Contains(strings.ToLower(msg), "tape loader") {
			phase = "input"
		}
		return &protocol.APIError{Code: protocol.CodeBusy, Message: msg, Phase: phase}
	case protocol.CodeBadRequest:
		return &protocol.APIError{Code: protocol.CodeBadRequest, Message: msg, Phase: "request"}
	default:
		return &protocol.APIError{Code: code, Message: msg}
	}
}
