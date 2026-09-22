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
		return fail(errors.New("usage: fogcast change-tape <media-id-or-.p-path> | eject-tape"))
	}
	switch args[0] {
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, origin+"/api/v1/core-media", bytes.NewReader(data))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Accept", "application/json")
	req.ContentLength = int64(len(data))
	resp, err := hostJSONClient().Do(req)
	if err != nil {
		return "", "", &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "running FogCast host API is unavailable"}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(body) > 1<<20 {
		return "", "", errors.New("invalid core-media response")
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		var envelope protocol.ErrorEnvelope
		if json.Unmarshal(body, &envelope) == nil && envelope.Error.Code != "" {
			return "", "", &envelope.Error
		}
		return "", "", errors.New("core-media import failed")
	}
	var media struct {
		MediaID string `json:"media_id"`
	}
	if json.Unmarshal(body, &media) != nil || protocol.ValidateDigest(media.MediaID) != nil {
		return "", "", errors.New("invalid core-media response")
	}
	return media.MediaID, base, nil
}

type liveMediaSession struct {
	ID       string `json:"id"`
	Target   string `json:"target"`
	TargetID string `json:"target_id"`
	developmentCoreSession
}

func hostJSONClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("redirects are not accepted")
	}}
}

func fetchLiveMediaSession(ctx context.Context, client *http.Client, origin string) (liveMediaSession, error) {
	return postLiveMediaSession(ctx, client, origin, "/api/v1/session", nil, nil, "")
}

func postLiveMediaSession(ctx context.Context, client *http.Client, origin, path string, data []byte, b *protocol.DevelopmentMediaBinding, id string) (liveMediaSession, error) {
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
	if json.Unmarshal(body, &session) != nil || session.ID == "" || session.Target == "" ||
		session.State != protocol.StateActive || session.Execution != fogcast.ExecutionFPGADevelopment ||
		!protocol.LiveMediaCapable(session.CorePackage) {
		return session, protocol.LiveMediaIdentityError()
	}
	return session, nil
}

func liveMediaCommand(name string) bool {
	return name == "change-tape" || name == "eject-tape"
}