package httpapi

import (
	"context"
	"io"
	"net/http"

	"github.com/DeanoC/FogCast/protocol"
)

type liveMediaController interface {
	ReplaceLiveMedia(context.Context, int64, io.Reader, protocol.DevelopmentMediaBinding) (protocol.Status, *protocol.APIError)
	ClearLiveMedia(context.Context, protocol.DevelopmentMediaBinding) (protocol.Status, *protocol.APIError)
}

func registerLiveMediaRoutes(mux *http.ServeMux, token string, controller DevelopmentController) {
	mux.Handle("/v1/development/live-media", authenticate(token, exactMethod(http.MethodPost, replaceLiveMediaHandler(controller))))
	mux.Handle("/v1/development/clear-media", authenticate(token, exactMethod(http.MethodPost, clearLiveMediaHandler(controller))))
}

func replaceLiveMediaHandler(controller DevelopmentController) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		binding, valid := protocol.DevelopmentMediaHeaders(r.Header)
		if !valid || !exactContentType(r, "application/octet-stream") || len(r.TransferEncoding) != 0 ||
			!protocol.AdmitTapeMediaSize(r.ContentLength) {
			writeBadRequest(w, r, protocol.LiveMediaRequestError().Message)
			return
		}
		loader, ok := controller.(liveMediaController)
		if !ok {
			writeAPIError(w, http.StatusBadRequest, &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "requested operation is unsupported"})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, protocol.MaxDevelopmentMediaBytes)
		status, apiErr := loader.ReplaceLiveMedia(r.Context(), r.ContentLength, r.Body, binding)
		setRequestState(r, status)
		if apiErr != nil {
			setRequestError(r, apiErr.Code)
			writeAPIError(w, statusForError(apiErr.Code), apiErr)
			return
		}
		writeJSON(w, http.StatusOK, status)
	})
}

func clearLiveMediaHandler(controller DevelopmentController) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		binding, valid := protocol.DevelopmentMediaHeaders(r.Header)
		r.Body = http.MaxBytesReader(w, r.Body, 1)
		body, err := io.ReadAll(r.Body)
		if !valid || err != nil || len(body) != 0 || len(r.TransferEncoding) != 0 {
			writeBadRequest(w, r, protocol.LiveMediaRequestError().Message)
			return
		}
		loader, ok := controller.(liveMediaController)
		if !ok {
			writeAPIError(w, http.StatusBadRequest, &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "requested operation is unsupported"})
			return
		}
		status, apiErr := loader.ClearLiveMedia(r.Context(), binding)
		setRequestState(r, status)
		if apiErr != nil {
			setRequestError(r, apiErr.Code)
			writeAPIError(w, statusForError(apiErr.Code), apiErr)
			return
		}
		writeJSON(w, http.StatusOK, status)
	})
}
