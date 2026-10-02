package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/DeanoC/FogCast/protocol"
)

type sessionDisplayController interface {
	SetSessionDisplay(context.Context, bool, protocol.DevelopmentMediaBinding) (protocol.Status, *protocol.APIError)
}

func sessionDisplayHandler(controller DevelopmentController) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, valid := protocol.DevelopmentMediaHeaders(r.Header)
		r.Body = http.MaxBytesReader(w, r.Body, 128)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var request protocol.SessionDisplayRequest
		if !valid || !exactContentType(r, "application/json") || len(r.TransferEncoding) != 0 ||
			decoder.Decode(&request) != nil || request.Visible == nil || decoder.Decode(&struct{}{}) != io.EOF {
			writeBadRequest(w, r, protocol.SessionDisplayRequestError().Message)
			return
		}
		display, ok := controller.(sessionDisplayController)
		if !ok {
			writeAPIError(w, http.StatusBadRequest, &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "requested operation is unsupported"})
			return
		}
		status, apiErr := display.SetSessionDisplay(r.Context(), *request.Visible, b)
		setRequestState(r, status)
		if apiErr != nil {
			setRequestError(r, apiErr.Code)
			writeAPIError(w, statusForError(apiErr.Code), apiErr)
			return
		}
		writeJSON(w, http.StatusOK, status)
	})
}
