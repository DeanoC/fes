package fogcastcli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/internal/corepackage"
	"github.com/DeanoC/FogCast/protocol"
)

func coreLibraryCommand(name string) bool {
	switch name {
	case "core-settings", "core-progress", "core-settings-set", "core-install", "core-list", "core-check", "core-entry", "core-select":
		return true
	}
	return false
}

func runCoreLibraryCommand(ctx context.Context, origin string, args []string) commandResult {
	method, path := http.MethodGet, "/api/v1/core-packages"
	var data []byte
	contentType := "application/json"
	expectedID := ""
	var expectedInspection *corepackage.Inspection
	switch args[0] {
	case "core-settings", "core-progress", "core-settings-set":
		if protocol.ValidateGameID(args[1]) != nil {
			return commandResult{err: &protocol.APIError{Code: protocol.CodeBadRequest, Message: "game ID is invalid"}, exit: 1}
		}
		path = "/api/v1/library/core-entries/" + url.PathEscape(args[1]) + "/settings"
		if args[0] == "core-progress" {
			path = "/api/v1/library/core-entries/" + url.PathEscape(args[1]) + "/progress"
		}
		if args[0] == "core-settings-set" {
			speed, err := strconv.ParseUint(args[4], 10, 16)
			update := protocol.CoreSettingsUpdate{ExpectedPackageID: args[2], ExpectedRevision: args[3], PaddleSpeed: protocol.PaddleSpeed(speed)}
			if err != nil || !update.Valid() {
				return commandResult{err: &protocol.APIError{Code: protocol.CodeBadRequest, Message: "core settings require package ID, revision and speed 0, 1 or 2"}, exit: 1}
			}
			method = http.MethodPut
			data, _ = json.Marshal(update)
		}

	case "core-install":
		snapshot, err := snapshotCoreArchive(args[1])
		if err != nil {
			return commandResult{err: err, exit: 1}
		}
		method, data, contentType, expectedID = http.MethodPost, snapshot.data, "application/octet-stream", snapshot.inspection.PackageID
		expectedInspection = &snapshot.inspection
	case "core-check":
		method, path = http.MethodPost, path+"/"+url.PathEscape(args[1])+"/compatibility"
	case "core-entry":
		method, path = http.MethodPost, "/api/v1/library/core-entries"
		data, _ = json.Marshal(map[string]string{"title": args[1], "package_id": args[2]})
	case "core-select":
		method, path = http.MethodPut, "/api/v1/library/core-entries/"+url.PathEscape(args[1])
		data, _ = json.Marshal(map[string]string{"expected_package_id": args[2], "package_id": args[3]})
	}
	request, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(origin, "/")+path, bytes.NewReader(data))
	if err != nil {
		return commandResult{err: err, exit: 1}
	}
	request.Header.Set("Accept", "application/json")
	if len(data) > 0 {
		request.Header.Set("Content-Type", contentType)
	}
	client := &http.Client{Timeout: 2 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirects are not accepted") }}
	response, err := client.Do(request)
	if err != nil {
		return commandResult{err: &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "running FogCast host API is unavailable"}, exit: 1}
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
	if err != nil || len(body) > 4<<20 || !json.Valid(body) {
		return commandResult{err: &protocol.APIError{Code: protocol.CodeInternal, Message: "invalid package library response"}, exit: 1}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var envelope protocol.ErrorEnvelope
		if json.Unmarshal(body, &envelope) == nil && envelope.Error.Code != "" {
			return commandResult{err: &envelope.Error, exit: 1}
		}
		return commandResult{err: &protocol.APIError{Code: protocol.CodeInternal, Message: "package library request failed"}, exit: 1}
	}
	if expectedID != "" {
		var result corepackage.Inspection
		if json.Unmarshal(body, &result) != nil || expectedInspection == nil || result.PackageID != expectedID || !reflect.DeepEqual(result, *expectedInspection) {
			return commandResult{err: &protocol.APIError{Code: protocol.CodeInternal, Message: "installed package response differs from imported archive"}, exit: 1}
		}
	}
	value := json.RawMessage(body)
	return commandResult{jsonValue: value, human: func(output io.Writer) error {
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, body, "", "  "); err != nil {
			return err
		}
		pretty.WriteByte('\n')
		_, err := output.Write(pretty.Bytes())
		return err
	}}
}
