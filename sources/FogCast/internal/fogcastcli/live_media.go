package fogcastcli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/protocol"
)

func runLiveMediaCommand(ctx context.Context, origin string, args []string) commandResult {
	fail := func(err error) commandResult { return commandResult{err: err, exit: 1} }
	if len(args) == 0 {
		return fail(errors.New("usage: fogcast change-tape <media-id-or-.p-path> | eject-tape | change-disk <media-id-or-disk-path> | eject-disk | change-cassette <media-id-or-.tap-path> | eject-cassette"))
	}
	switch args[0] {
	case "change-disk":
		if len(args) != 2 {
			return fail(errors.New("usage: fogcast change-disk <media-id-or-.dsk/.do/.d64-path>"))
		}
		return changeDiskThroughHostAPI(ctx, origin, args[1])
	case "eject-disk":
		if len(args) != 1 {
			return fail(errors.New("usage: fogcast eject-disk"))
		}
		return ejectDiskThroughHostAPI(ctx, origin)
	case "change-cassette":
		if len(args) != 2 {
			return fail(errors.New("usage: fogcast change-cassette <media-id-or-.tap-path>"))
		}
		return changeCassetteThroughHostAPI(ctx, origin, args[1])
	case "eject-cassette":
		if len(args) != 1 {
			return fail(errors.New("usage: fogcast eject-cassette"))
		}
		return ejectCassetteThroughHostAPI(ctx, origin)
	case "change-tape":
		if len(args) != 2 {
			return fail(errors.New("usage: fogcast change-tape <media-id-or-.p-path>"))
		}
		return changeTapeThroughHostAPI(ctx, origin, args[1])
	case "eject-tape":
		if len(args) != 1 {
			return fail(errors.New("usage: fogcast eject-tape"))
		}
		return ejectTapeThroughHostAPI(ctx, origin)
	default:
		return fail(errors.New("unknown live media command"))
	}
}

func changeTapeThroughHostAPI(ctx context.Context, origin, arg string) commandResult {
	fail := func(err error) commandResult { return commandResult{err: err, exit: 1} }
	mediaID, name, err := resolveTapeArgument(ctx, origin, arg)
	if err != nil {
		return fail(err)
	}
	client := hostJSONClient()
	prior, err := fetchLiveMediaSession(ctx, client, origin)
	if err != nil {
		return fail(err)
	}
	b := protocol.DevelopmentMediaBinding{
		PackageID: prior.CorePackage.PackageID, Generation: prior.CorePackage.Generation,
		Target: prior.Target, TargetID: prior.TargetID,
	}
	if !b.Valid() || !protocol.LiveMediaCapable(prior.CorePackage) {
		return fail(protocol.LiveMediaIdentityError())
	}
	payload, _ := json.Marshal(protocol.LiveMediaRequest{MediaID: mediaID, Name: name})
	after, err := postLiveMediaSession(ctx, client, origin, "/api/v1/session/live-media", payload, &b, prior.ID)
	if err != nil {
		return fail(err)
	}
	if after.ID != prior.ID || after.CorePackage.PackageID != b.PackageID || after.CorePackage.Generation != b.Generation {
		return fail(protocol.LiveMediaIdentityError())
	}
	result := statusResult{State: after.State, CorePackage: after.CorePackage}
	return commandResult{jsonValue: result, human: func(w io.Writer) error { return writeHumanStatusResult(w, result) }}
}

func ejectTapeThroughHostAPI(ctx context.Context, origin string) commandResult {
	fail := func(err error) commandResult { return commandResult{err: err, exit: 1} }
	client := hostJSONClient()
	prior, err := fetchLiveMediaSession(ctx, client, origin)
	if err != nil {
		return fail(err)
	}
	b := protocol.DevelopmentMediaBinding{
		PackageID: prior.CorePackage.PackageID, Generation: prior.CorePackage.Generation,
		Target: prior.Target, TargetID: prior.TargetID,
	}
	if !b.Valid() || !protocol.LiveMediaCapable(prior.CorePackage) {
		return fail(protocol.LiveMediaIdentityError())
	}
	after, err := postLiveMediaSession(ctx, client, origin, "/api/v1/session/live-media/clear", nil, &b, prior.ID)
	if err != nil {
		return fail(err)
	}
	if after.ID != prior.ID || after.CorePackage.PackageID != b.PackageID || after.CorePackage.Generation != b.Generation {
		return fail(protocol.LiveMediaIdentityError())
	}
	result := statusResult{State: after.State, CorePackage: after.CorePackage}
	return commandResult{jsonValue: result, human: func(w io.Writer) error { return writeHumanStatusResult(w, result) }}
}

func resolveTapeArgument(ctx context.Context, origin, arg string) (mediaID, name string, err error) {
	if protocol.ValidateDigest(arg) == nil {
		return arg, "tape.p", nil
	}
	before, err := os.Lstat(arg)
	if err != nil || !before.Mode().IsRegular() {
		return "", "", protocol.LiveMediaRequestError()
	}
	base := filepath.Base(arg)
	if !protocol.AdmitTapeMediaName(base) || !protocol.AdmitTapeMediaSize(before.Size()) {
		return "", "", protocol.LiveMediaRequestError()
	}
	file, err := os.Open(arg)
	if err != nil {
		return "", "", protocol.LiveMediaRequestError()
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(before, opened) {
		return "", "", protocol.LiveMediaRequestError()
	}
	data, apiErr := protocol.ReadDevelopmentMedia(opened.Size(), file)
	if apiErr != nil {
		return "", "", protocol.LiveMediaRequestError()
	}
	mediaID, err = importCoreMediaBytes(ctx, origin, data)
	return mediaID, base, err
}

func importCoreMediaBytes(ctx context.Context, origin string, data []byte) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, origin+"/api/v1/core-media", bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Accept", "application/json")
	req.ContentLength = int64(len(data))
	resp, err := hostJSONClient().Do(req)
	if err != nil {
		return "", &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "running FogCast host API is unavailable"}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(body) > 1<<20 {
		return "", errors.New("invalid core-media response")
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		var envelope protocol.ErrorEnvelope
		if json.Unmarshal(body, &envelope) == nil && envelope.Error.Code != "" {
			return "", &envelope.Error
		}
		return "", errors.New("core-media import failed")
	}
	var media struct {
		MediaID string `json:"media_id"`
	}
	if json.Unmarshal(body, &media) != nil || protocol.ValidateDigest(media.MediaID) != nil {
		return "", errors.New("invalid core-media response")
	}
	return media.MediaID, nil
}

type liveMediaSession struct {
	ID       string `json:"id"`
	Target   string `json:"target"`
	TargetID string `json:"target_id"`
	developmentCoreSession
}

func hostJSONClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second, CheckRedirect: refuseRedirect}
}

// hostMutationClient carries a non-replayable disk insert or eject. The host
// allows the disk transfer at least 150 s, so a short client timeout
// could abandon an insert that still completes; only the caller's context and
// the host's own deadline bound it.
func hostMutationClient() *http.Client {
	return &http.Client{CheckRedirect: refuseRedirect}
}

func refuseRedirect(*http.Request, []*http.Request) error {
	return errors.New("redirects are not accepted")
}

func fetchLiveMediaSession(ctx context.Context, client *http.Client, origin string) (liveMediaSession, error) {
	return postLiveMediaSession(ctx, client, origin, "/api/v1/session", nil, nil, "")
}

func postLiveMediaSession(ctx context.Context, client *http.Client, origin, path string, data []byte, b *protocol.DevelopmentMediaBinding, id string) (liveMediaSession, error) {
	return postSessionMedia(ctx, client, origin, path, data, b, id, tapeMedia)
}

// sessionMediaForm names the media a command addresses: which sessions carry
// it and whether native library play is accepted.
type sessionMediaForm struct {
	capable func(*protocol.CorePackageStatus) bool
	native  bool
}

var (
	tapeMedia = sessionMediaForm{capable: protocol.LiveMediaCapable}
	// A recognized fes.computer library launch runs as native play.
	diskMedia     = sessionMediaForm{capable: diskCapable, native: true}
	cassetteMedia = sessionMediaForm{capable: cassetteCapable, native: true}
)

// postSessionMedia performs one session read or live-media mutation and
// requires the returned session to be capable of the addressed media form.
func postSessionMedia(ctx context.Context, client *http.Client, origin, path string, data []byte, b *protocol.DevelopmentMediaBinding, id string, form sessionMediaForm) (liveMediaSession, error) {
	var bodyReader io.Reader
	method := http.MethodGet
	if b != nil {
		method = http.MethodPost
		if data != nil {
			bodyReader = bytes.NewReader(data)
		} else {
			bodyReader = http.NoBody
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, origin+path, bodyReader)
	if err != nil {
		return liveMediaSession{}, err
	}
	req.Header.Set("Accept", "application/json")
	if b != nil {
		if data != nil {
			req.Header.Set("Content-Type", "application/json")
			req.ContentLength = int64(len(data))
		}
		req.Header.Set(protocol.HostSessionIDHeader, id)
		b.SetHeaders(req.Header)
	}
	resp, err := client.Do(req)
	if err != nil {
		return liveMediaSession{}, &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "running FogCast host API is unavailable"}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(body) > 1<<20 {
		return liveMediaSession{}, errors.New("invalid host session response")
	}
	if resp.StatusCode != http.StatusOK {
		var envelope protocol.ErrorEnvelope
		if json.Unmarshal(body, &envelope) == nil && envelope.Error.Code != "" {
			return liveMediaSession{}, &envelope.Error
		}
		return liveMediaSession{}, errors.New("host session request failed")
	}
	var session liveMediaSession
	if json.Unmarshal(body, &session) != nil {
		return session, protocol.LiveMediaIdentityError()
	}
	execution := session.Execution == fogcast.ExecutionFPGADevelopment ||
		(form.native && session.Execution == fogcast.ExecutionFPGANative)
	if session.ID == "" || session.Target == "" || session.State != protocol.StateActive ||
		!execution || !form.capable(session.CorePackage) {
		return session, protocol.LiveMediaIdentityError()
	}
	return session, nil
}

func liveMediaCommand(name string) bool {
	return name == "change-tape" || name == "eject-tape" || name == "change-disk" || name == "eject-disk" ||
		name == "change-cassette" || name == "eject-cassette"
}

// diskCapable reports a session whose active fes.computer generation has the
// Apple II floppy or the C64 disk on unit 0. Spectrum tapes use cassetteCapable.
func diskCapable(p *protocol.CorePackageStatus) bool {
	unit, ok := protocol.MediaUnit(p, protocol.Apple2FloppyUnit)
	if !ok {
		return false
	}
	return unit.Interface == protocol.Apple2FloppyInterface() ||
		unit.Interface == protocol.C64DiskInterface()
}

func cassetteCapable(p *protocol.CorePackageStatus) bool {
	unit, ok := protocol.MediaUnit(p, protocol.SpectrumTapeUnit)
	return ok && unit.Interface == protocol.SpectrumTapeInterface()
}

// changeDiskThroughHostAPI inserts a household disk into the running machine
// through POST /api/v1/session/live-media. A disk path is imported first.
func changeDiskThroughHostAPI(ctx context.Context, origin, arg string) commandResult {
	fail := func(err error) commandResult { return commandResult{err: err, exit: 1} }
	client := hostJSONClient()
	prior, err := fetchDiskSession(ctx, client, origin)
	if err != nil {
		return fail(err)
	}
	mediaID, name, err := resolveDiskArgument(ctx, origin, arg, prior.CorePackage)
	if err != nil {
		return fail(err)
	}
	b := protocol.DevelopmentMediaBinding{PackageID: prior.CorePackage.PackageID, Generation: prior.CorePackage.Generation, Target: prior.Target, TargetID: prior.TargetID}
	payload, _ := json.Marshal(protocol.LiveMediaRequest{MediaID: mediaID, Name: name})
	after, err := postSessionMedia(ctx, hostMutationClient(), origin, "/api/v1/session/live-media", payload, &b, prior.ID, diskMedia)
	if err != nil {
		return fail(err)
	}
	return diskResult(prior, after, b)
}

func ejectDiskThroughHostAPI(ctx context.Context, origin string) commandResult {
	fail := func(err error) commandResult { return commandResult{err: err, exit: 1} }
	client := hostJSONClient()
	prior, err := fetchDiskSession(ctx, client, origin)
	if err != nil {
		return fail(err)
	}
	b := protocol.DevelopmentMediaBinding{PackageID: prior.CorePackage.PackageID, Generation: prior.CorePackage.Generation, Target: prior.Target, TargetID: prior.TargetID}
	after, err := postSessionMedia(ctx, hostMutationClient(), origin, "/api/v1/session/live-media/clear", nil, &b, prior.ID, diskMedia)
	if err != nil {
		return fail(err)
	}
	return diskResult(prior, after, b)
}

func changeCassetteThroughHostAPI(ctx context.Context, origin, arg string) commandResult {
	fail := func(err error) commandResult { return commandResult{err: err, exit: 1} }
	mediaID, name, err := resolveCassetteArgument(ctx, origin, arg)
	if err != nil {
		return fail(err)
	}
	client := hostJSONClient()
	prior, err := fetchCassetteSession(ctx, client, origin)
	if err != nil {
		return fail(err)
	}
	b := protocol.DevelopmentMediaBinding{PackageID: prior.CorePackage.PackageID, Generation: prior.CorePackage.Generation, Target: prior.Target, TargetID: prior.TargetID}
	payload, _ := json.Marshal(protocol.LiveMediaRequest{MediaID: mediaID, Name: name})
	after, err := postSessionMedia(ctx, hostMutationClient(), origin, "/api/v1/session/live-media", payload, &b, prior.ID, cassetteMedia)
	if err != nil {
		return fail(err)
	}
	return diskResult(prior, after, b)
}

func ejectCassetteThroughHostAPI(ctx context.Context, origin string) commandResult {
	fail := func(err error) commandResult { return commandResult{err: err, exit: 1} }
	client := hostJSONClient()
	prior, err := fetchCassetteSession(ctx, client, origin)
	if err != nil {
		return fail(err)
	}
	b := protocol.DevelopmentMediaBinding{PackageID: prior.CorePackage.PackageID, Generation: prior.CorePackage.Generation, Target: prior.Target, TargetID: prior.TargetID}
	after, err := postSessionMedia(ctx, hostMutationClient(), origin, "/api/v1/session/live-media/clear", nil, &b, prior.ID, cassetteMedia)
	if err != nil {
		return fail(err)
	}
	return diskResult(prior, after, b)
}

func fetchCassetteSession(ctx context.Context, client *http.Client, origin string) (liveMediaSession, error) {
	session, err := postSessionMedia(ctx, client, origin, "/api/v1/session", nil, nil, "", cassetteMedia)
	if err == nil && !(protocol.DevelopmentMediaBinding{PackageID: session.CorePackage.PackageID, Generation: session.CorePackage.Generation}).Valid() {
		err = protocol.MediaUnitIdentityError()
	}
	return session, err
}

func resolveCassetteArgument(ctx context.Context, origin, arg string) (mediaID, name string, err error) {
	if protocol.ValidateDigest(arg) == nil {
		return arg, "program.tap", nil
	}
	before, err := os.Lstat(arg)
	base := filepath.Base(arg)
	if err != nil || !before.Mode().IsRegular() || !protocol.AdmitSpectrumTapeName(base) || !protocol.AdmitSpectrumTapeSize(before.Size()) {
		return "", "", protocol.CassetteMediaRequestError()
	}
	file, err := os.Open(arg)
	if err != nil {
		return "", "", protocol.CassetteMediaRequestError()
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(before, opened) {
		return "", "", protocol.CassetteMediaRequestError()
	}
	data, err := io.ReadAll(io.LimitReader(file, protocol.SpectrumTapeMaxBytes+1))
	if err != nil || !protocol.AdmitSpectrumTapeSize(int64(len(data))) || int64(len(data)) != before.Size() {
		return "", "", protocol.CassetteMediaRequestError()
	}
	mediaID, err = importCoreMediaBytes(ctx, origin, data)
	return mediaID, base, err
}

func fetchDiskSession(ctx context.Context, client *http.Client, origin string) (liveMediaSession, error) {
	session, err := postSessionMedia(ctx, client, origin, "/api/v1/session", nil, nil, "", diskMedia)
	if err == nil && !(protocol.DevelopmentMediaBinding{PackageID: session.CorePackage.PackageID, Generation: session.CorePackage.Generation}).Valid() {
		err = protocol.MediaUnitIdentityError()
	}
	return session, err
}

func diskResult(prior, after liveMediaSession, b protocol.DevelopmentMediaBinding) commandResult {
	if after.ID != prior.ID || after.CorePackage.PackageID != b.PackageID || after.CorePackage.Generation != b.Generation {
		return commandResult{err: protocol.MediaUnitIdentityError(), exit: 1}
	}
	result := statusResult{State: after.State, CorePackage: after.CorePackage}
	return commandResult{jsonValue: result, human: func(w io.Writer) error { return writeHumanStatusResult(w, result) }}
}

// resolveDiskArgument accepts a stored media ID or snapshots an exact disk
// file and imports it through POST /api/v1/core-media. A stored ID takes its
// name from the active unit so the host checks the C64 or Apple II size.
func resolveDiskArgument(ctx context.Context, origin, arg string, active *protocol.CorePackageStatus) (mediaID, name string, err error) {
	if protocol.ValidateDigest(arg) == nil {
		name, ok := syntheticDiskName(active)
		if !ok {
			return "", "", protocol.DiskMediaRequestError()
		}
		return arg, name, nil
	}
	before, err := os.Lstat(arg)
	base := filepath.Base(arg)
	var limit int64
	switch {
	case protocol.AdmitDiskMediaName(base):
		limit = protocol.Apple2FloppyBytes
	case protocol.AdmitC64DiskName(base):
		limit = protocol.C64DiskBytes
	}
	if err != nil || !before.Mode().IsRegular() || limit == 0 || before.Size() != limit {
		return "", "", protocol.DiskMediaRequestError()
	}
	file, err := os.Open(arg)
	if err != nil {
		return "", "", protocol.DiskMediaRequestError()
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(before, opened) {
		return "", "", protocol.DiskMediaRequestError()
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(data)) != limit {
		return "", "", protocol.DiskMediaRequestError()
	}
	mediaID, err = importCoreMediaBytes(ctx, origin, data)
	return mediaID, base, err
}

// syntheticDiskName names a stored object after the disk the running
// generation accepts. Both home-computer disks occupy unit 0.
func syntheticDiskName(active *protocol.CorePackageStatus) (string, bool) {
	unit, ok := protocol.MediaUnit(active, protocol.Apple2FloppyUnit)
	if !ok {
		return "", false
	}
	switch unit.Interface.ID {
	case protocol.C64DiskInterface().ID:
		return "disk.d64", true
	case protocol.Apple2FloppyInterface().ID:
		return "disk.dsk", true
	default:
		return "", false
	}
}
