package httpapi

import (
	"context"
	"io"
	"net/http"
	"regexp"
	"strconv"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
)

type coreDataController interface {
	LoadLibraryCore(context.Context, int64, io.Reader, string) (protocol.Status, *protocol.APIError)
	InspectCoreData(context.Context, int64, io.Reader, string) (protocol.CoreDataInspection, *protocol.APIError)
	UpdateCoreSettings(context.Context, int64, io.Reader, protocol.CoreSettingsUpdate) (protocol.CoreDataInspection, *protocol.APIError)
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
			if r.URL.Path == "/v1/library/core/compose" {
				limit = corepackage.MaxCompositionArchiveSize
			}
			ids := r.Header.Values("X-FogCast-Package-ID")
			if r.URL.RawQuery != "" || !exactContentType(r, "application/octet-stream") || len(r.TransferEncoding) != 0 || r.ContentLength < 1 || r.ContentLength > limit || len(ids) != 1 || !dataPackageIDRE.MatchString(ids[0]) {
				writeBadRequest(w, r, "core data requires an exact package identity and bounded archive")
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, limit)
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
				status, apiErr = c.LoadLibraryCore(r.Context(), r.ContentLength, r.Body, ids[0])
				setRequestState(r, status)
				result = status
			case "/v1/library/core/data/inspect":
				result, apiErr = c.InspectCoreData(r.Context(), r.ContentLength, r.Body, ids[0])
			case "/v1/library/core/settings":
				revisions, speeds := r.Header.Values("X-FogCast-Expected-Revision"), r.Header.Values("X-FogCast-Paddle-Speed")
				if len(revisions) != 1 || len(speeds) != 1 || !protocol.ValidCoreDataRevision(revisions[0]) || (speeds[0] != "0" && speeds[0] != "1" && speeds[0] != "2") {
					writeBadRequest(w, r, "core settings revision or speed is invalid")
					return
				}
				speed, _ := strconv.Atoi(speeds[0])
				result, apiErr = c.UpdateCoreSettings(r.Context(), r.ContentLength, r.Body, protocol.CoreSettingsUpdate{ExpectedPackageID: ids[0], ExpectedRevision: revisions[0], PaddleSpeed: protocol.PaddleSpeed(speed)})
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
