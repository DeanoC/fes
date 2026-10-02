package httpapi

import (
	"context"
	"io"
	"net/http"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
)

type partsController interface {
	LoadPartsCore(context.Context, int64, io.Reader) (protocol.Status, *protocol.APIError)
	InspectPartsCore(context.Context, int64, io.Reader) (protocol.PartsInspection, *protocol.APIError)
}
type libraryPartsController interface {
	LoadLibraryPartsCore(context.Context, int64, io.Reader, string) (protocol.Status, *protocol.APIError)
}

func registerPartsRoutes(mux *http.ServeMux, token string, controller DevelopmentController) {
	mux.Handle("/v1/library/core/parts", authenticate(token, exactMethod(http.MethodPost, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, ok := controller.(libraryPartsController)
		if !ok {
			writeAPIError(w, 400, &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "requested operation is unsupported"})
			return
		}
		ids := r.Header.Values("X-FogCast-Package-ID")
		if r.URL.RawQuery != "" || !exactContentType(r, "application/octet-stream") || len(r.TransferEncoding) != 0 || r.ContentLength < 1 || r.ContentLength > corepackage.MaxPartsArchiveSize || len(ids) != 1 || !dataPackageIDRE.MatchString(ids[0]) {
			writeBadRequest(w, r, "library parts require an exact package identity and bounded archive")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, corepackage.MaxPartsArchiveSize)
		status, apiErr := c.LoadLibraryPartsCore(r.Context(), r.ContentLength, r.Body, ids[0])
		setRequestState(r, status)
		if apiErr != nil {
			setRequestError(r, apiErr.Code)
			writeAPIError(w, statusForError(apiErr.Code), apiErr)
			return
		}
		writeJSON(w, http.StatusOK, status)
	}))))
	for _, path := range []string{"/v1/development/core/parts", "/v1/development/core/parts/inspect"} {
		mux.Handle(path, authenticate(token, exactMethod(http.MethodPost, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, ok := controller.(partsController)
			if !ok {
				writeAPIError(w, 400, &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "requested operation is unsupported"})
				return
			}
			if r.URL.RawQuery != "" || !exactContentType(r, "application/octet-stream") || len(r.TransferEncoding) != 0 || r.ContentLength < 1 || r.ContentLength > corepackage.MaxPartsArchiveSize || len(r.Header.Values("X-FogCast-Package-ID")) != 0 {
				writeBadRequest(w, r, "developer parts require a bounded archive and no library context")
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, corepackage.MaxPartsArchiveSize)
			var result any
			var apiErr *protocol.APIError
			if r.URL.Path == "/v1/development/core/parts/inspect" {
				result, apiErr = c.InspectPartsCore(r.Context(), r.ContentLength, r.Body)
			} else {
				var status protocol.Status
				status, apiErr = c.LoadPartsCore(r.Context(), r.ContentLength, r.Body)
				setRequestState(r, status)
				result = status
			}
			if apiErr != nil {
				setRequestError(r, apiErr.Code)
				writeAPIError(w, statusForError(apiErr.Code), apiErr)
				return
			}
			writeJSON(w, http.StatusOK, result)
		}))))
	}
}
