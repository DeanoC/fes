package httpapi

import (
	"context"
	"io"
	"net/http"
	"strconv"

	"github.com/DeanoC/FogCast/protocol"
)

type mediaDataController interface {
	InsertLibraryMedia(context.Context, int64, io.Reader, protocol.LibraryMediaBinding) (protocol.Status, *protocol.APIError)
	SaveMedia(context.Context, protocol.MediaUnitBinding) (protocol.Status, *protocol.APIError)
}

func registerMediaDataRoutes(mux *http.ServeMux, token string, controller DevelopmentController) {
	mux.Handle("/v1/library/media/insert", authenticate(token, exactMethod(http.MethodPost, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, valid := protocol.LibraryMediaHeaders(r.Header)
		if !valid || !exactContentType(r, "application/octet-stream") || len(r.TransferEncoding) != 0 || !protocol.AdmitAtariStFloppySize(r.ContentLength) {
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
	mux.Handle("/v1/zx81-tape/capture", authenticate(token, exactMethod(http.MethodPost, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		binding, valid := protocol.MediaUnitHeaders(r.Header)
		r.Body = http.MaxBytesReader(w, r.Body, 1)
		body, err := io.ReadAll(r.Body)
		if !valid || binding.Unit != protocol.Zx81TapeUnit || err != nil || len(body) != 0 || len(r.TransferEncoding) != 0 {
			writeBadRequest(w, r, protocol.MediaUnitRequestError().Message)
			return
		}
		capturer, ok := controller.(interface {
			CaptureZX81Tape(context.Context, string, uint64) ([]byte, *protocol.APIError)
		})
		if !ok {
			writeAPIError(w, 400, &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "requested operation is unsupported"})
			return
		}
		data, apiErr := capturer.CaptureZX81Tape(r.Context(), binding.PackageID, binding.Generation)
		if apiErr != nil {
			setRequestError(r, apiErr.Code)
			writeAPIError(w, statusForError(apiErr.Code), apiErr)
			return
		}
		if len(data) == 0 {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if !protocol.AdmitZx81TapeSize(int64(len(data))) {
			writeAPIError(w, http.StatusBadGateway, &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "ZX81 tape capture is the wrong size"})
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	}))))
	mux.Handle("/v1/zx81-tape/acknowledge", authenticate(token, exactMethod(http.MethodPost, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		binding, valid := protocol.MediaUnitHeaders(r.Header)
		r.Body = http.MaxBytesReader(w, r.Body, 1)
		body, err := io.ReadAll(r.Body)
		if !valid || binding.Unit != protocol.Zx81TapeUnit || err != nil || len(body) != 0 || len(r.TransferEncoding) != 0 {
			writeBadRequest(w, r, protocol.MediaUnitRequestError().Message)
			return
		}
		acknowledger, ok := controller.(interface {
			AcknowledgeZX81Tape(context.Context, string, uint64) (protocol.Status, *protocol.APIError)
		})
		if !ok {
			writeAPIError(w, 400, &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "requested operation is unsupported"})
			return
		}
		status, apiErr := acknowledger.AcknowledgeZX81Tape(r.Context(), binding.PackageID, binding.Generation)
		setRequestState(r, status)
		if apiErr != nil {
			setRequestError(r, apiErr.Code)
			writeAPIError(w, statusForError(apiErr.Code), apiErr)
			return
		}
		writeJSON(w, http.StatusOK, status)
	}))))
}
