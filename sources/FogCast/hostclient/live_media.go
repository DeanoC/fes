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

// LiveMediaCapable reports whether the session can change a mid-session .p
// tape. The projection keeps the active interfaces and media units so the
// shared predicate can see fes.media.blob or fes.media.zx81-tape.
func LiveMediaCapable(p *SessionCorePackage) bool {
	return p != nil && protocol.LiveMediaCapable(sessionPackageStatus(p))
}

// ComputerTapeCapable reports an active fes.computer 1.0 ZX81 tape unit.
// A menu-display kit presents that unit through fes.video.session-display.
func ComputerTapeCapable(p *SessionCorePackage) bool {
	return p != nil && protocol.ComputerZx81TapeCapable(sessionPackageStatus(p))
}

func sessionPackageStatus(p *SessionCorePackage) *protocol.CorePackageStatus {
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
	if len(p.MediaUnits) > 0 {
		status.MediaUnits = protocol.CloneMediaUnits(p.MediaUnits)
	}
	return status
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
	return c.ReplaceLiveMediaForSession(ctx, prior, mediaID, name)
}

// ReplaceLiveMediaForSession uses the package/target/generation captured when
// the tape picker opened. It never re-resolves a changed foreground session.
func (c *Client) ReplaceLiveMediaForSession(ctx context.Context, prior SessionResult, mediaID, name string) (SessionResult, error) {
	mediaID, name = strings.TrimSpace(mediaID), strings.TrimSpace(name)
	if protocol.ValidateDigest(mediaID) != nil || !protocol.AdmitTapeMediaName(name) {
		return SessionResult{}, protocol.LiveMediaRequestError()
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
	return c.ClearLiveMediaForSession(ctx, prior)
}

// ClearLiveMediaForSession ejects only from the captured package generation.
func (c *Client) ClearLiveMediaForSession(ctx context.Context, prior SessionResult) (SessionResult, error) {
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
		return result, &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "live media response did not confirm the captured session", Phase: "recovery"}
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
		phase := strings.TrimSpace(result.ErrorPhase)
		if phase == "" {
			phase = "admission"
		}
		if phase == "admission" && strings.Contains(strings.ToLower(msg), "tape loader") {
			phase = "input"
		}
		return &protocol.APIError{Code: protocol.CodeBusy, Message: msg, Phase: phase, NoChange: result.ErrorNoChange}
	case protocol.CodeBadRequest:
		phase := strings.TrimSpace(result.ErrorPhase)
		if phase == "" {
			phase = "request"
		}
		return &protocol.APIError{Code: protocol.CodeBadRequest, Message: msg, Phase: phase, NoChange: result.ErrorNoChange}
	default:
		return &protocol.APIError{Code: code, Message: msg, Phase: strings.TrimSpace(result.ErrorPhase), NoChange: result.ErrorNoChange}
	}
}
