package fogcastcli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/protocol"
)

type developmentMediaSession struct {
	ID       string `json:"id"`
	Target   string `json:"target"`
	TargetID string `json:"target_id"`
	developmentCoreSession
}

func loadMediaThroughHostAPI(ctx context.Context, origin, path string) commandResult {
	fail := func(err error) commandResult { return commandResult{err: err, exit: 1} }
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Size() < 1 || before.Size() > protocol.MaxDevelopmentMediaBytes {
		return fail(protocol.DevelopmentMediaRequestError())
	}
	file, err := os.Open(path)
	if err != nil {
		return fail(protocol.DevelopmentMediaRequestError())
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(before, opened) {
		return fail(protocol.DevelopmentMediaRequestError())
	}
	data, apiErr := protocol.ReadDevelopmentMedia(opened.Size(), file)
	if apiErr != nil {
		return fail(apiErr)
	}
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirects are not accepted") }}
	call := func(method, path string, data []byte, b *protocol.DevelopmentMediaBinding, id string) (developmentMediaSession, error) {
		req, err := http.NewRequestWithContext(ctx, method, origin+path, bytes.NewReader(data))
		if err != nil {
			return developmentMediaSession{}, err
		}
		req.Header.Set("Accept", "application/json")
		if b != nil {
			req.Header.Set("Content-Type", "application/octet-stream")
			req.Header.Set(protocol.HostSessionIDHeader, id)
			b.SetHeaders(req.Header)
		}
		resp, err := client.Do(req)
		if err != nil {
			return developmentMediaSession{}, &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "running FogCast host API is unavailable"}
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
		if err != nil || len(body) > 1<<20 {
			return developmentMediaSession{}, errors.New("invalid host session response")
		}
		if resp.StatusCode != http.StatusOK {
			var envelope protocol.ErrorEnvelope
			if json.Unmarshal(body, &envelope) == nil && envelope.Error.Code != "" {
				return developmentMediaSession{}, &envelope.Error
			}
			return developmentMediaSession{}, errors.New("host session request failed")
		}
		var session developmentMediaSession
		if json.Unmarshal(body, &session) != nil || session.ID == "" || session.Target == "" || session.State != protocol.StateActive || session.Execution != fogcast.ExecutionFPGADevelopment || !protocol.DevelopmentMediaCapable(session.CorePackage) {
			return session, protocol.DevelopmentMediaIdentityError()
		}
		return session, nil
	}
	prior, err := call(http.MethodGet, "/api/v1/session", nil, nil, "")
	if err != nil {
		return fail(err)
	}
	b := protocol.DevelopmentMediaBinding{PackageID: prior.CorePackage.PackageID, Generation: prior.CorePackage.Generation, Target: prior.Target, TargetID: prior.TargetID}
	if !b.Valid() {
		return fail(protocol.DevelopmentMediaIdentityError())
	}
	after, err := call(http.MethodPost, "/api/v1/session/development-media", data, &b, prior.ID)
	if err != nil {
		return fail(err)
	}
	if after.ID != prior.ID || after.Target != prior.Target || after.TargetID != prior.TargetID || after.CorePackage.PackageID != b.PackageID || after.CorePackage.Generation != b.Generation {
		return fail(protocol.DevelopmentMediaIdentityError())
	}
	result := statusResult{State: after.State, CorePackage: after.CorePackage}
	return commandResult{jsonValue: result, human: func(w io.Writer) error { return writeHumanStatusResult(w, result) }}
}
