package httpapi

import (
	"context"
	"io"
	"net/http"

	"github.com/DeanoC/FogCast/protocol"
)

type developmentMediaController interface {
	LoadDevelopmentMedia(context.Context, int64, io.Reader, protocol.DevelopmentMediaBinding) (protocol.Status, *protocol.APIError)
}

func developmentMediaHandler(controller DevelopmentController) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		binding, valid := protocol.DevelopmentMediaHeaders(r.Header)
		limit := protocol.MaxDevelopmentMediaBytes
		binding.Stream = r.URL.Path == "/v1/development/media-stream"
		if binding.Stream {
			limit = protocol.MaxDeclaredMediaStreamBytes
		}
		if r.URL.Path == "/v1/development/firmware" {
			binding.Role = protocol.FirmwareRole
			limit = protocol.FirmwareBytes
		}
		if !valid || !exactContentType(r, "application/octet-stream") || len(r.TransferEncoding) != 0 || r.ContentLength < 1 || r.ContentLength > limit ||
			(binding.Role == protocol.FirmwareRole && r.ContentLength != protocol.FirmwareBytes) {
			writeBadRequest(w, r, protocol.DevelopmentMediaRequestError().Message)
			return
		}
		loader, ok := controller.(developmentMediaController)
		if !ok {
			writeAPIError(w, http.StatusBadRequest, &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "requested operation is unsupported"})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, limit)
		status, apiErr := loader.LoadDevelopmentMedia(r.Context(), r.ContentLength, r.Body, binding)
		setRequestState(r, status)
		if apiErr != nil {
			setRequestError(r, apiErr.Code)
			writeAPIError(w, statusForError(apiErr.Code), apiErr)
			return
		}
		writeJSON(w, http.StatusOK, status)
	})
}
