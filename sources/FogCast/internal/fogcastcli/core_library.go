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

	"github.com/DeanoC/FogCast/atarist"
	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
)

func coreLibraryCommand(name string) bool {
	switch name {
	case "video-parts", "video-part-install", "video-profile", "core-video", "core-media-capabilities", "core-media-install", "core-media-select", "core-settings", "core-progress", "core-settings-set", "core-install", "core-list", "core-check", "core-entry", "core-select", "core-firmware-select", "create-blank-st-disk":
		return true
	}
	return false
}

func runCoreLibraryCommand(ctx context.Context, origin string, args []string) (result commandResult) {
	method, path := http.MethodGet, "/api/v1/core-packages"
	var data []byte
	contentType := "application/json"
	expectedID := ""
	var expectedInspection *corepackage.Inspection
	expectedMediaID := ""
	var mediaSnapshot *coreMediaSnapshot
	var expectedVideoPart *catalog.CoreVideoPart
	expectedVideoProfile := ""
	var blankDisk *blankAtariStCommand
	switch args[0] {
	case "video-parts":
		path = "/api/v1/library/video-parts"
	case "video-part-install":
		if !catalog.ValidVideoProfile(args[1]) {
			return commandResult{err: &protocol.APIError{Code: protocol.CodeBadRequest, Message: "choose direct or scanlines explicitly"}, exit: 1}
		}
		snapshot, asset, err := snapshotVideoPart(args[2])
		if err != nil {
			return commandResult{err: err, exit: 1}
		}
		method, path, data, contentType = http.MethodPost, "/api/v1/library/video-parts/"+args[1], snapshot, "application/octet-stream"
		expectedVideoPart = &catalog.CoreVideoPart{PartID: asset.ID, PackageID: asset.Manifest.ShellPackageID, Profile: args[1]}
	case "video-profile":
		path = "/api/v1/library/settings"
		if len(args) == 2 {
			if !catalog.ValidVideoProfile(args[1]) {
				return commandResult{err: &protocol.APIError{Code: protocol.CodeBadRequest, Message: "choose direct or scanlines explicitly"}, exit: 1}
			}
			method, expectedVideoProfile = http.MethodPatch, args[1]
			data, _ = json.Marshal(map[string]string{"video_profile": args[1]})
		}
	case "core-video":
		if protocol.ValidateGameID(args[1]) != nil {
			return commandResult{err: &protocol.APIError{Code: protocol.CodeBadRequest, Message: "game ID is invalid"}, exit: 1}
		}
		path = "/api/v1/library/core-entries/" + url.PathEscape(args[1]) + "/video"
	case "core-media-capabilities":
		if protocol.ValidateDigest(args[1]) != nil {
			return commandResult{err: &protocol.APIError{Code: protocol.CodeBadRequest, Message: "package ID is invalid"}, exit: 1}
		}
		path += "/" + url.PathEscape(args[1]) + "/media-capabilities"
	case "core-media-install":
		snapshot, err := snapshotCoreMedia(ctx, args[1])
		if err != nil {
			return commandResult{err: err, exit: 1}
		}
		defer func() {
			if err := snapshot.Close(); err != nil {
				result.err = errors.Join(result.err, err)
				result.exit = 1
			}
		}()
		mediaSnapshot = snapshot
		method, path, contentType = http.MethodPost, "/api/v1/core-media", "application/octet-stream"
		expectedMediaID = snapshot.mediaID
	case "core-media-select":
		expected, next := args[3], args[4]
		if expected == "none" {
			expected = ""
		}
		if next == "none" {
			next = ""
		}
		role := ""
		if next != "" {
			role = "blob"
			if len(args) == 6 {
				role = args[5]
			}
		}
		method, path = http.MethodPut, "/api/v1/library/core-entries/"+url.PathEscape(args[1])+"/media"
		data, _ = json.Marshal(map[string]string{"expected_package_id": args[2], "expected_media_id": expected, "media_role": role, "media_id": next})
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
		entry := map[string]any{"title": args[1], "package_id": args[2]}
		rest := args[3:]
		if n := len(rest); n > 0 && rest[n-1] == "firmware" {
			entry["firmware_required"] = true
			rest = rest[:n-1]
		}
		if len(rest) == 2 {
			entry["media_role"], entry["media_id"] = rest[0], rest[1]
		} else if len(rest) != 0 {
			return commandResult{err: &protocol.APIError{Code: protocol.CodeBadRequest, Message: "core entry is title, package ID, optional media, optional firmware"}, exit: 1}
		}
		data, _ = json.Marshal(entry)
	case "core-firmware-select":
		next := args[1]
		if next == "none" {
			next = ""
		}
		method, path = http.MethodPut, "/api/v1/library/firmware"
		data, _ = json.Marshal(map[string]string{"slot": "firmware", "media_id": next})
	case "core-select":
		method, path = http.MethodPut, "/api/v1/library/core-entries/"+url.PathEscape(args[1])
		data, _ = json.Marshal(map[string]string{"expected_package_id": args[2], "package_id": args[3]})
	case "create-blank-st-disk":
		parsed, err := parseBlankAtariSt(args[1:])
		if err != nil {
			return commandResult{err: err, exit: 1}
		}
		blankDisk = &parsed
		method, path = http.MethodPost, "/api/v1/library/atari-st-disks"
		data = parsed.payload()
	}
	var requestBody io.Reader = bytes.NewReader(data)
	if mediaSnapshot != nil {
		requestBody = io.LimitReader(mediaSnapshot.file, mediaSnapshot.size)
	}
	request, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(origin, "/")+path, requestBody)
	if err != nil {
		return commandResult{err: err, exit: 1}
	}
	request.Header.Set("Accept", "application/json")
	if expectedVideoPart != nil {
		request.GetBody = nil
	}
	if mediaSnapshot != nil {
		request.ContentLength = mediaSnapshot.size
		// No GetBody: an ambiguous import must never be replayed.
	}
	if len(data) > 0 || mediaSnapshot != nil {
		request.Header.Set("Content-Type", contentType)
	}
	client := &http.Client{Timeout: 2 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirects are not accepted") }}
	response, err := client.Do(request)
	if err != nil {
		if expectedVideoPart != nil || expectedVideoProfile != "" {
			return commandResult{err: &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Phase: "video-change-outcome"}, exit: 1}
		}
		return commandResult{err: &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "running FogCast host API is unavailable"}, exit: 1}
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
	if err != nil || len(body) > 4<<20 || !json.Valid(body) {
		if expectedVideoPart != nil || expectedVideoProfile != "" {
			return commandResult{err: &protocol.APIError{Code: protocol.CodeInternal, Phase: "video-change-outcome"}, exit: 1}
		}
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
	if expectedVideoPart != nil {
		var imported catalog.CoreVideoPart
		if json.Unmarshal(body, &imported) != nil || imported != *expectedVideoPart {
			return commandResult{err: &protocol.APIError{Code: protocol.CodeInternal, Phase: "video-change-outcome"}, exit: 1}
		}
	}
	if expectedVideoProfile != "" {
		var settings struct {
			VideoProfile string `json:"video_profile"`
		}
		if json.Unmarshal(body, &settings) != nil || settings.VideoProfile != expectedVideoProfile {
			return commandResult{err: &protocol.APIError{Code: protocol.CodeInternal, Phase: "video-change-outcome"}, exit: 1}
		}
	}
	if blankDisk != nil {
		if err := blankDisk.matches(body); err != nil {
			return commandResult{err: err, exit: 1}
		}
	}
	if expectedMediaID != "" {
		var result struct {
			MediaID string `json:"media_id"`
			Size    int64  `json:"size"`
		}
		if json.Unmarshal(body, &result) != nil || result.MediaID != expectedMediaID || result.Size != mediaSnapshot.size {
			return commandResult{err: &protocol.APIError{Code: protocol.CodeInternal, Message: "installed media response differs from imported bytes"}, exit: 1}
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

type blankAtariStCommand struct {
	Name, GameID, PackageID, ExpectedMedia string
	Tracks, Heads, Sectors                 int
	Bind                                   bool
}

func parseBlankAtariSt(args []string) (blankAtariStCommand, error) {
	bad := &protocol.APIError{Code: protocol.CodeBadRequest, Message: "blank ST disk is a name, optional tracks heads sectors, and optional bind game package expected-media", Phase: "request"}
	if len(args) == 0 {
		return blankAtariStCommand{}, bad
	}
	cmd := blankAtariStCommand{Name: args[0]}
	rest := args[1:]
	if len(rest) >= 3 && rest[0] != "bind" {
		tracks, err1 := strconv.Atoi(rest[0])
		heads, err2 := strconv.Atoi(rest[1])
		sectors, err3 := strconv.Atoi(rest[2])
		if err1 != nil || err2 != nil || err3 != nil {
			return blankAtariStCommand{}, bad
		}
		cmd.Tracks, cmd.Heads, cmd.Sectors = tracks, heads, sectors
		rest = rest[3:]
	}
	if _, ok := atarist.Geometry(cmd.Tracks, cmd.Heads, cmd.Sectors); !ok {
		return blankAtariStCommand{}, bad
	}
	if len(rest) == 0 {
		return cmd, nil
	}
	if len(rest) != 4 || rest[0] != "bind" {
		return blankAtariStCommand{}, bad
	}
	expected := rest[3]
	if expected == "none" {
		expected = ""
	}
	if protocol.ValidateGameID(rest[1]) != nil || protocol.ValidateDigest(rest[2]) != nil || (expected != "" && protocol.ValidateDigest(expected) != nil) {
		return blankAtariStCommand{}, bad
	}
	cmd.Bind, cmd.GameID, cmd.PackageID, cmd.ExpectedMedia = true, rest[1], rest[2], expected
	return cmd, nil
}

func (cmd blankAtariStCommand) payload() []byte {
	geometry, _ := atarist.Geometry(cmd.Tracks, cmd.Heads, cmd.Sectors)
	body := map[string]any{"name": cmd.Name, "tracks": geometry.Tracks, "heads": geometry.Heads, "sectors": geometry.Sectors}
	if cmd.Bind {
		body["bind"] = true
		body["game_id"] = cmd.GameID
		body["expected_package_id"] = cmd.PackageID
		body["expected_media_id"] = cmd.ExpectedMedia
	}
	data, _ := json.Marshal(body)
	return data
}

func (cmd blankAtariStCommand) matches(body []byte) error {
	geometry, _ := atarist.Geometry(cmd.Tracks, cmd.Heads, cmd.Sectors)
	var result struct {
		MediaID string `json:"media_id"`
		Size    int64  `json:"size"`
		Name    string `json:"name"`
		Tracks  int    `json:"tracks"`
		Heads   int    `json:"heads"`
		Sectors int    `json:"sectors"`
		Entry   *struct {
			GameID    string `json:"game_id"`
			PackageID string `json:"package_id"`
			MediaID   string `json:"media_id"`
			MediaRole string `json:"media_role"`
		} `json:"entry"`
	}
	want := int64(geometry.Tracks) * int64(geometry.Heads) * int64(geometry.Sectors) * 512
	if json.Unmarshal(body, &result) != nil || protocol.ValidateDigest(result.MediaID) != nil || result.Size != want || result.Tracks != geometry.Tracks || result.Heads != geometry.Heads || result.Sectors != geometry.Sectors || result.Name == "" {
		return &protocol.APIError{Code: protocol.CodeInternal, Message: "blank ST disk response does not match the request"}
	}
	if cmd.Bind {
		if result.Entry == nil || result.Entry.GameID != cmd.GameID || result.Entry.PackageID != cmd.PackageID || result.Entry.MediaRole != protocol.DiskRole || result.Entry.MediaID != result.MediaID {
			return &protocol.APIError{Code: protocol.CodeInternal, Message: "blank ST disk response does not match the request"}
		}
	} else if result.Entry != nil {
		return &protocol.APIError{Code: protocol.CodeInternal, Message: "blank ST disk response does not match the request"}
	}
	return nil
}
