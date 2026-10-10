package httpapi

import (
	"context"
	"io"
	"net/http"

	"github.com/DeanoC/FogCast/protocol"
)

type mediaUnitController interface {
	InsertMedia(context.Context, int64, io.Reader, protocol.MediaUnitBinding) (protocol.Status, *protocol.APIError)
	EjectMedia(context.Context, protocol.MediaUnitBinding) (protocol.Status, *protocol.APIError)
}

// registerMediaUnitRoutes adds the fes.computer removable-media routes. Both
// use the package, generation, target and X-FogCast-Media-Unit headers, the
// existing kit lease and update exclusion; no caller supplies a path.
func registerMediaUnitRoutes(mux *http.ServeMux, token string, controller DevelopmentController) {
	registerMediaDataRoutes(mux, token, controller)
	mux.Handle("/v1/development/insert-media", authenticate(token, exactMethod(http.MethodPost, insertMediaHandler(controller))))
	mux.Handle("/v1/development/eject-media", authenticate(token, exactMethod(http.MethodPost, ejectMediaHandler(controller))))
}

func insertMediaHandler(controller DevelopmentController) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		binding, valid := protocol.MediaUnitHeaders(r.Header)
		if !valid || !exactContentType(r, "application/octet-stream") || len(r.TransferEncoding) != 0 ||
			r.ContentLength < 1 || r.ContentLength > protocol.MaxComputerMediaBytes {
			writeBadRequest(w, r, protocol.MediaUnitRequestError().Message)
			return
		}
		units, ok := controller.(mediaUnitController)
		if !ok {
			writeAPIError(w, http.StatusBadRequest, &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "requested operation is unsupported"})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, r.ContentLength)
		status, apiErr := units.InsertMedia(r.Context(), r.ContentLength, r.Body, binding)
		setRequestState(r, status)
		if apiErr != nil {
			setRequestError(r, apiErr.Code)
			writeAPIError(w, statusForError(apiErr.Code), apiErr)
			return
		}
		writeJSON(w, http.StatusOK, status)
	})
}

func ejectMediaHandler(controller DevelopmentController) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		binding, valid := protocol.MediaUnitHeaders(r.Header)
		r.Body = http.MaxBytesReader(w, r.Body, 1)
		body, err := io.ReadAll(r.Body)
		if !valid || err != nil || len(body) != 0 || len(r.TransferEncoding) != 0 {
			writeBadRequest(w, r, protocol.MediaUnitRequestError().Message)
			return
		}
		units, ok := controller.(mediaUnitController)
		if !ok {
			writeAPIError(w, http.StatusBadRequest, &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "requested operation is unsupported"})
			return
		}
		status, apiErr := units.EjectMedia(r.Context(), binding)
		setRequestState(r, status)
		if apiErr != nil {
			setRequestError(r, apiErr.Code)
			writeAPIError(w, statusForError(apiErr.Code), apiErr)
			return
		}
		writeJSON(w, http.StatusOK, status)
	})
}
