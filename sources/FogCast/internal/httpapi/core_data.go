package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
)

type coreDataController interface {
	LoadLibraryCore(context.Context, int64, io.Reader, string, string) (protocol.Status, *protocol.APIError)
	InspectCoreData(context.Context, int64, io.Reader, string, string) (protocol.CoreDataInspection, *protocol.APIError)
	UpdateCoreSettings(context.Context, int64, io.Reader, protocol.CoreSettingsUpdate) (protocol.CoreDataInspection, *protocol.APIError)
}

type coreAtaDiskController interface {
	LoadLibraryCoreWithAtaDisk(context.Context, int64, io.Reader, string, string, string,
		protocol.AtaDiskSelection) (protocol.Status, *protocol.APIError)
}

var dataPackageIDRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

func registerCoreDataRoutes(mux *http.ServeMux, token string, controller DevelopmentController) {
	for _, path := range []string{"/v1/library/core/load", "/v1/library/core/compose", "/v1/library/core/data/inspect", "/v1/library/core/settings"} {
		mux.Handle(path, authenticate(token, exactMethod(http.MethodPost, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, ok := controller.(coreDataController)
			if !ok {
				writeAPIError(w, 400, &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "requested operation is unsupported"})
				return
			}
			limit := int64(corepackage.MaxArchiveSize)
			if r.URL.Path == "/v1/library/core/load" {
				limit = corepackage.MaxROMInputSize
			}
			if r.URL.Path == "/v1/library/core/compose" {
				limit = corepackage.MaxCompositionArchiveSize
			}
			ids := r.Header.Values("X-FogCast-Package-ID")
			gameIDs := r.Header.Values("X-FogCast-Game-ID")
			if r.URL.RawQuery != "" || !exactContentType(r, "application/octet-stream") || len(r.TransferEncoding) != 0 || r.ContentLength < 1 || len(ids) != 1 || !dataPackageIDRE.MatchString(ids[0]) || len(gameIDs) > 1 || (len(gameIDs) == 1 && protocol.ValidateGameID(gameIDs[0]) != nil) {
				writeBadRequest(w, r, "core data requires an exact package identity and bounded archive")
				return
			}
			gameID := ""
			if len(gameIDs) == 1 {
				gameID = gameIDs[0]
			}
			packageSize, disk, diskPath, diskErr := prepareAtaDiskLoad(w, r, limit)
			if diskErr != nil {
				writeBadRequest(w, r, "library core load body or ATA disk metadata is invalid")
				return
			}
			if disk != nil {
				defer os.Remove(diskPath)
			}
			var result any
			var apiErr *protocol.APIError
			switch r.URL.Path {
			case "/v1/library/core/compose":
				composed, ok := controller.(interface {
					LoadComposedCore(context.Context, int64, io.Reader, string) (protocol.Status, *protocol.APIError)
				})
				if !ok {
					writeAPIError(w, 400, &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "requested operation is unsupported"})
					return
				}
				var status protocol.Status
				status, apiErr = composed.LoadComposedCore(r.Context(), r.ContentLength, r.Body, ids[0])
				setRequestState(r, status)
				result = status
			case "/v1/library/core/load":
				var status protocol.Status
				if disk != nil {
					diskController, ok := controller.(coreAtaDiskController)
					if !ok {
						writeAPIError(w, 400, &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "requested operation is unsupported"})
						return
					}
					status, apiErr = diskController.LoadLibraryCoreWithAtaDisk(r.Context(), packageSize,
						io.LimitReader(r.Body, packageSize), ids[0], gameID, diskPath, *disk)
				} else {
					r.Body = http.MaxBytesReader(w, r.Body, limit)
					status, apiErr = c.LoadLibraryCore(r.Context(), packageSize, r.Body, ids[0], gameID)
				}
				setRequestState(r, status)
				result = status
			case "/v1/library/core/data/inspect":
				result, apiErr = c.InspectCoreData(r.Context(), r.ContentLength, r.Body, ids[0], gameID)
			case "/v1/library/core/settings":
				revisions, speeds, machines := r.Header.Values("X-FogCast-Expected-Revision"), r.Header.Values("X-FogCast-Paddle-Speed"), r.Header.Values("X-FogCast-PC386-Machine")
				if len(revisions) != 1 || !protocol.ValidCoreDataRevision(revisions[0]) || (len(speeds) == 1) == (len(machines) == 1) || len(speeds) > 1 || len(machines) > 1 {
					writeBadRequest(w, r, "core settings revision or settings object is invalid")
					return
				}
				update := protocol.CoreSettingsUpdate{ExpectedPackageID: ids[0], ExpectedRevision: revisions[0], GameID: gameID}
				if len(speeds) == 1 {
					if speeds[0] != "0" && speeds[0] != "1" && speeds[0] != "2" {
						writeBadRequest(w, r, "core settings paddle speed is invalid")
						return
					}
					speed, _ := strconv.Atoi(speeds[0])
					update.PaddleSpeed = protocol.PaddleSpeed(speed)
				} else {
					var machine protocol.PC386MachineSettings
					decoder := json.NewDecoder(strings.NewReader(machines[0]))
					decoder.DisallowUnknownFields()
					if decoder.Decode(&machine) != nil || decoder.Decode(new(any)) != io.EOF || !machine.Valid() {
						writeBadRequest(w, r, "core settings machine is invalid")
						return
					}
					update.Machine = &machine
				}
				if !update.Valid() {
					writeBadRequest(w, r, "core settings request is invalid")
					return
				}
				result, apiErr = c.UpdateCoreSettings(r.Context(), r.ContentLength, r.Body, update)
			}
			if apiErr != nil {
				setRequestError(r, apiErr.Code)
				writeAPIError(w, statusForError(apiErr.Code), apiErr)
				return
			}
			writeJSON(w, 200, result)
		}))))
	}
}

func prepareAtaDiskLoad(w http.ResponseWriter, r *http.Request, packageLimit int64) (int64, *protocol.AtaDiskSelection, string, error) {
	headers := [][]string{
		r.Header.Values(protocol.CorePackageSizeHeader), r.Header.Values(protocol.AtaDiskSizeHeader),
		r.Header.Values(protocol.AtaDiskBaseIDHeader), r.Header.Values(protocol.AtaDiskCylindersHeader),
		r.Header.Values(protocol.AtaDiskHeadsHeader), r.Header.Values(protocol.AtaDiskSectorsHeader),
	}
	any := false
	for _, values := range headers {
		any = any || len(values) != 0
	}
	if !any {
		if r.ContentLength < 1 || r.ContentLength > packageLimit {
			return 0, nil, "", io.ErrUnexpectedEOF
		}
		r.Body = http.MaxBytesReader(w, r.Body, packageLimit)
		return r.ContentLength, nil, "", nil
	}
	if r.URL.Path != "/v1/library/core/load" || len(headers[0]) != 1 || len(headers[1]) != 1 ||
		len(headers[2]) != 1 || len(headers[3]) != 1 || len(headers[4]) != 1 || len(headers[5]) != 1 {
		return 0, nil, "", io.ErrUnexpectedEOF
	}
	gameID := r.Header.Values("X-FogCast-Game-ID")
	if len(gameID) != 1 || protocol.ValidateGameID(gameID[0]) != nil {
		return 0, nil, "", io.ErrUnexpectedEOF
	}
	parse := func(value string, max uint64) (uint64, bool) {
		parsed, err := strconv.ParseUint(value, 10, 32)
		return parsed, err == nil && parsed > 0 && parsed <= max
	}
	packageSize, ok := parse(headers[0][0], uint64(packageLimit))
	if !ok {
		return 0, nil, "", io.ErrUnexpectedEOF
	}
	size, ok := parse(headers[1][0], uint64(protocol.AtaDiskMaxBytes))
	if !ok {
		return 0, nil, "", io.ErrUnexpectedEOF
	}
	cylinders, okCylinders := parse(headers[3][0], uint64(^uint16(0)))
	heads, okHeads := parse(headers[4][0], uint64(^uint16(0)))
	sectors, okSectors := parse(headers[5][0], uint64(^uint16(0)))
	disk := protocol.AtaDiskSelection{BaseMediaID: headers[2][0], Size: uint32(size),
		Cylinders: uint16(cylinders), Heads: uint16(heads), SectorsPerTrack: uint16(sectors)}
	if !okCylinders || !okHeads || !okSectors || !disk.Valid() ||
		int64(packageSize)+int64(size) != r.ContentLength || r.ContentLength > packageLimit+protocol.AtaDiskMaxBytes {
		return 0, nil, "", io.ErrUnexpectedEOF
	}
	r.Body = http.MaxBytesReader(w, r.Body, packageLimit+protocol.AtaDiskMaxBytes)
	file, err := os.CreateTemp("", "fes-pc386-ata-*.img")
	if err != nil {
		return 0, nil, "", err
	}
	path := file.Name()
	if _, err = io.CopyN(file, r.Body, int64(size)); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		_ = os.Remove(path)
		return 0, nil, "", io.ErrUnexpectedEOF
	}
	return int64(packageSize), &disk, path, nil
}
