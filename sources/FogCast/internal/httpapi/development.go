package httpapi

import (
	"context"
	"io"
	"net/http"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
)

type DevelopmentController interface {
	LoadDevelopmentRBF(context.Context, int64, io.Reader) (protocol.Status, *protocol.APIError)
	LoadCore(context.Context, int64, io.Reader) (protocol.Status, *protocol.APIError)
	InspectCore(context.Context, int64, io.Reader) (protocol.CoreInspection, *protocol.APIError)
	RebootDevelopment(context.Context) (protocol.Status, *protocol.APIError)
}

func WithDevelopment(controller DevelopmentController) Option {
	return func(options *serverOptions) { options.development = controller }
}

func registerDevelopmentRoutes(mux *http.ServeMux, token string, controller DevelopmentController) {
	mux.Handle("/v1/development/media", authenticate(token, exactMethod(http.MethodPost, developmentMediaHandler(controller))))
	mux.Handle("/v1/development/media-stream", authenticate(token, exactMethod(http.MethodPost, developmentMediaHandler(controller))))
	mux.Handle("/v1/development/firmware", authenticate(token, exactMethod(http.MethodPost, developmentMediaHandler(controller))))
	registerLiveMediaRoutes(mux, token, controller)
	registerCoreDataRoutes(mux, token, controller)
	mux.Handle("/v1/development/rbf", authenticate(token, exactMethod(http.MethodPost, developmentRBFHandler(controller))))
	mux.Handle("/v1/development/core", authenticate(token, exactMethod(http.MethodPost, developmentCoreHandler(controller))))
	mux.Handle("/v1/development/core/inspect", authenticate(token, exactMethod(http.MethodPost, developmentCoreInspectionHandler(controller))))
	mux.Handle("/v1/development/reboot", authenticate(token, exactMethod(http.MethodPost, developmentRebootHandler(controller))))
}

func developmentCoreInspectionHandler(controller DevelopmentController) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !exactContentType(r, "application/octet-stream") || len(r.TransferEncoding) != 0 ||
			r.ContentLength < 1 || r.ContentLength > corepackage.MaxArchiveSize {
			writeBadRequest(w, r, "development core inspection requires a bounded application/octet-stream body")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, corepackage.MaxArchiveSize)
		inspection, apiErr := controller.InspectCore(r.Context(), r.ContentLength, r.Body)
		if apiErr != nil {
			setRequestError(r, apiErr.Code)
			writeAPIError(w, statusForError(apiErr.Code), apiErr)
			return
		}
		writeJSON(w, http.StatusOK, inspection)
	})
}

func developmentCoreHandler(controller DevelopmentController) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !exactContentType(r, "application/octet-stream") || len(r.TransferEncoding) != 0 ||
			r.ContentLength < 1 || r.ContentLength > corepackage.MaxArchiveSize {
			writeBadRequest(w, r, "development core upload requires a bounded application/octet-stream body")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, corepackage.MaxArchiveSize)
		status, apiErr := controller.LoadCore(r.Context(), r.ContentLength, r.Body)
		setRequestState(r, status)
		if apiErr != nil {
			setRequestError(r, apiErr.Code)
			writeAPIError(w, statusForError(apiErr.Code), apiErr)
			return
		}
		writeJSON(w, http.StatusOK, status)
	})
}

func developmentRebootHandler(controller DevelopmentController) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 1)
		body, err := io.ReadAll(r.Body)
		if err != nil || len(body) != 0 {
			writeBadRequest(w, r, "development reboot request body must be empty")
			return
		}
		status, apiErr := controller.RebootDevelopment(r.Context())
		setRequestState(r, status)
		if apiErr != nil {
			setRequestError(r, apiErr.Code)
			writeAPIError(w, statusForError(apiErr.Code), apiErr)
			return
		}
		writeJSON(w, http.StatusOK, status)
	})
}

func developmentRBFHandler(controller DevelopmentController) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !exactContentType(r, "application/octet-stream") || len(r.TransferEncoding) != 0 ||
			r.ContentLength < 1 || r.ContentLength > protocol.MaxDevelopmentRBFBytes {
			writeBadRequest(w, r, "development RBF upload requires a bounded application/octet-stream body")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, protocol.MaxDevelopmentRBFBytes)
		status, apiErr := controller.LoadDevelopmentRBF(r.Context(), r.ContentLength, r.Body)
		setRequestState(r, status)
		if apiErr != nil {
			setRequestError(r, apiErr.Code)
			writeAPIError(w, statusForError(apiErr.Code), apiErr)
			return
		}
		writeJSON(w, http.StatusOK, status)
	})
}
