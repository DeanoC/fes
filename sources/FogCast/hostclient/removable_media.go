package hostclient

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/protocol"
)

// RemovableMediaUnit returns the supported, observed unit 0 of this generation.
func RemovableMediaUnit(p *SessionCorePackage) (protocol.MediaUnitStatus, bool) {
	if p == nil {
		return protocol.MediaUnitStatus{}, false
	}
	u, ok := protocol.MediaUnit(sessionPackageStatus(p), 0)
	if !ok {
		return u, false
	}
	switch u.Interface {
	case protocol.Zx81TapeInterface(), protocol.SpectrumTapeInterface(), protocol.Apple2FloppyInterface(), protocol.C64DiskInterface(), protocol.AtariStFloppyInterface():
		return u, true
	}
	return protocol.MediaUnitStatus{}, false
}

func RemovableMediaCapable(p *SessionCorePackage) bool {
	_, ok := RemovableMediaUnit(p)
	return ok || LiveMediaCapable(p)
}

func spectrumSnapshotsCapable(p *SessionCorePackage) bool {
	if p == nil {
		return false
	}
	for _, i := range p.ActiveInterfaces {
		if i.ID == protocol.SpectrumSnapshotInterfaceID && i.Major == 1 && i.Minor == 0 {
			return true
		}
	}
	return false
}

// spectrumDiskCapable reports an active unit-1 Spectrum disk beside the
// cassette; the host converts .trd and .scl images for its TR-DOS drive.
func spectrumDiskCapable(p *SessionCorePackage) bool {
	if p == nil {
		return false
	}
	u, ok := protocol.MediaUnit(sessionPackageStatus(p), protocol.SpectrumDiskUnit)
	return ok && u.Interface == protocol.SpectrumDiskInterface()
}

// AdmitRemovableMedia applies the active unit's format and observed size
// limits. A Spectrum tape image is converted to the unit's PZX stream on
// insert, and a snapshot to its restore stream, so each is bounded as an
// image and the host checks the stream and whether the package runs it. A
// TR-DOS disk goes to the unit-1 disk beside the cassette.
func AdmitRemovableMedia(p *SessionCorePackage, name string, size int64) bool {
	if protocol.AdmitSpectrumDiskName(name) {
		return spectrumDiskCapable(p) && protocol.AdmitSpectrumDiskSize(size)
	}
	u, ok := RemovableMediaUnit(p)
	if !ok {
		return LiveMediaCapable(p) && protocol.AdmitTapeMediaName(name) && protocol.AdmitTapeMediaSize(size)
	}
	if u.Interface == protocol.SpectrumTapeInterface() {
		if protocol.AdmitSpectrumSnapshotName(name) && !spectrumSnapshotsCapable(p) {
			return false
		}
		return protocol.AdmitSpectrumCassetteName(name) && protocol.AdmitSpectrumTapeSize(size)
	}
	if size < int64(u.MinBytes) || size > int64(u.MaxBytes) {
		return false
	}
	switch u.Interface {
	case protocol.Zx81TapeInterface():
		return protocol.AdmitTapeMediaName(name)
	case protocol.Apple2FloppyInterface():
		return protocol.AdmitDiskMediaName(name)
	case protocol.C64DiskInterface():
		return protocol.AdmitC64DiskName(name)
	case protocol.AtariStFloppyInterface():
		return protocol.AdmitAtariStFloppyName(name) && protocol.AdmitAtariStFloppySize(size)
	}
	return false
}

func removableMediaBinding(s SessionResult) (protocol.DevelopmentMediaBinding, error) {
	b := protocol.DevelopmentMediaBinding{Target: s.Target, TargetID: s.TargetID}
	if s.CorePackage != nil {
		b.PackageID, b.Generation = s.CorePackage.PackageID, s.CorePackage.Generation
	}
	if s.State != "active" || strings.TrimSpace(s.ID) == "" || b.Target == "" || !b.Valid() || !RemovableMediaCapable(s.CorePackage) {
		return b, protocol.LiveMediaIdentityError()
	}
	return b, nil
}

// ReplaceRemovableMediaForSession never resolves a newer foreground session.
// The host checks the imported object's size and contents against this binding.
func (c *Client) ReplaceRemovableMediaForSession(ctx context.Context, prior SessionResult, mediaID, name string) (SessionResult, error) {
	b, err := removableMediaBinding(prior)
	if err != nil {
		return SessionResult{}, err
	}
	u, ok := RemovableMediaUnit(prior.CorePackage)
	size := int64(1)
	if ok {
		size = int64(u.MinBytes)
	}
	if protocol.ValidateDigest(mediaID) != nil || !AdmitRemovableMedia(prior.CorePackage, name, size) {
		return SessionResult{}, protocol.LiveMediaRequestError()
	}
	payload, err := json.Marshal(protocol.LiveMediaRequest{MediaID: mediaID, Name: name})
	if err != nil {
		return SessionResult{}, err
	}
	return c.postRemovableMedia(ctx, prior, "/api/v1/session/live-media", bytes.NewReader(payload), int64(len(payload)), prior.ID, b)
}

func (c *Client) ClearRemovableMediaForSession(ctx context.Context, prior SessionResult) (SessionResult, error) {
	b, err := removableMediaBinding(prior)
	if err != nil {
		return SessionResult{}, err
	}
	return c.postRemovableMedia(ctx, prior, "/api/v1/session/live-media/clear", http.NoBody, 0, prior.ID, b)
}

func (c *Client) postRemovableMedia(ctx context.Context, prior SessionResult, path string, body io.Reader, length int64, sessionID string, b protocol.DevelopmentMediaBinding) (SessionResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 450*time.Second)
	defer cancel()
	transport := *c.HTTPClient()
	transport.Timeout = 0
	client := *c
	client.httpClient = &transport
	result, err := client.postLiveMedia(ctx, path, body, length, sessionID, b)
	if err == nil && (result.ID != prior.ID || result.Target != prior.Target || result.TargetID != prior.TargetID || result.CorePackage == nil || result.CorePackage.PackageID != b.PackageID || result.CorePackage.Generation != b.Generation) {
		return result, &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "live media response did not confirm the captured session", Phase: "recovery"}
	}
	return result, err
}
