package httpapi

import (
	"context"
	"github.com/DeanoC/FogCast/protocol"
	"io"
	"net/http"
)

type mediaDataController interface {
	InsertLibraryMedia(context.Context, int64, io.Reader, protocol.LibraryMediaBinding) (protocol.Status, *protocol.APIError)
	SaveMedia(context.Context, protocol.MediaUnitBinding) (protocol.Status, *protocol.APIError)
}

func registerMediaDataRoutes(mux *http.ServeMux, token string, controller DevelopmentController) {
	mux.Handle("/v1/library/media/insert", authenticate(token, exactMethod(http.MethodPost, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, valid := protocol.LibraryMediaHeaders(r.Header)
		if !valid || !exactContentType(r, "application/octet-stream") || len(r.TransferEncoding) != 0 || r.ContentLength != protocol.AtariStFloppyBytes {
			writeBadRequest(w, r, protocol.MediaUnitRequestError().Message)
			return
		}
		c, ok := controller.(mediaDataController)
		if !ok {
			writeAPIError(w, 400, &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "requested operation is unsupported"})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, r.ContentLength)
		s, e := c.InsertLibraryMedia(r.Context(), r.ContentLength, r.Body, b)
		setRequestState(r, s)
		if e != nil {
			setRequestError(r, e.Code)
			writeAPIError(w, statusForError(e.Code), e)
			return
		}
		writeJSON(w, 200, s)
	}))))
	mux.Handle("/v1/library/media/save", authenticate(token, exactMethod(http.MethodPost, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, valid := protocol.MediaUnitHeaders(r.Header)
		r.Body = http.MaxBytesReader(w, r.Body, 1)
		body, err := io.ReadAll(r.Body)
		if !valid || err != nil || len(body) != 0 || len(r.TransferEncoding) != 0 {
			writeBadRequest(w, r, protocol.MediaUnitRequestError().Message)
			return
		}
		c, ok := controller.(mediaDataController)
		if !ok {
			writeAPIError(w, 400, &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "requested operation is unsupported"})
			return
		}
		s, e := c.SaveMedia(r.Context(), b)
		setRequestState(r, s)
		if e != nil {
			setRequestError(r, e.Code)
			writeAPIError(w, statusForError(e.Code), e)
			return
		}
		writeJSON(w, 200, s)
	}))))
}
