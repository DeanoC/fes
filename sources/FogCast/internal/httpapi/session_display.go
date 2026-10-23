package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/DeanoC/FogCast/protocol"
)

type sessionDisplayController interface {
	SetSessionDisplay(context.Context, protocol.SessionDisplay, protocol.DevelopmentMediaBinding) (protocol.Status, *protocol.APIError)
}

func sessionDisplayHandler(controller DevelopmentController) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, valid := protocol.DevelopmentMediaHeaders(r.Header)
		r.Body = http.MaxBytesReader(w, r.Body, 128)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var request protocol.SessionDisplayRequest
		var requested protocol.SessionDisplay
		var displayValid bool
		if valid && exactContentType(r, "application/json") && len(r.TransferEncoding) == 0 &&
			decoder.Decode(&request) == nil && decoder.Decode(&struct{}{}) == io.EOF {
			requested, displayValid = request.Display()
		}
		if !displayValid {
			writeBadRequest(w, r, protocol.SessionDisplayRequestError().Message)
			return
		}
		display, ok := controller.(sessionDisplayController)
		if !ok {
			writeAPIError(w, http.StatusBadRequest, &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "requested operation is unsupported"})
			return
		}
		status, apiErr := display.SetSessionDisplay(r.Context(), requested, b)
		setRequestState(r, status)
		if apiErr != nil {
			setRequestError(r, apiErr.Code)
			writeAPIError(w, statusForError(apiErr.Code), apiErr)
			return
		}
		writeJSON(w, http.StatusOK, status)
	})
}
