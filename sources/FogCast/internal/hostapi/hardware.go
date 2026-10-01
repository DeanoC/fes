package hostapi

import (
	"context"
	"net/http"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/fogcast"
)

type hardwareService interface {
	Hardware(context.Context) ([]fogcast.HardwareMachine, error)
}

type coreExpansionPresentationService interface {
	CoreExpansionPresentation(context.Context, string) (catalog.CoreExpansionPresentation, error)
	SetCoreExpansionPresentation(context.Context, string, string, string) (catalog.CoreExpansionPresentation, error)
}

func registerHardware(mux *http.ServeMux, service Service, session *sessionCoordinator) {
	registerZX81Tapes(mux, service)
	mux.HandleFunc("GET /api/v1/library/hardware", func(w http.ResponseWriter, r *http.Request) {
		if rejectCoreLibraryBody(w, r) {
			return
		}
		reader, ok := service.(hardwareService)
		if !ok {
			writeError(w, http.StatusNotImplemented, "UNSUPPORTED_OPERATION", "hardware setups are unavailable")
			return
		}
		machines, err := reader.Hardware(r.Context())
		if err != nil {
			writeCoreLibraryError(w, err)
			return
		}
		result := struct {
			Machines     []fogcast.HardwareMachine `json:"machines"`
			Session      *sessionResult            `json:"session,omitempty"`
			SessionError string                    `json:"session_error,omitempty"`
		}{Machines: machines}
		if result.Machines == nil {
			result.Machines = []fogcast.HardwareMachine{}
		}
		active, err := session.status(r.Context())
		if err != nil {
			result.SessionError = "The running machine is unavailable. Reconnect before changing live media or stopping."
		} else {
			result.Session = &active
		}
		writeJSON(w, http.StatusOK, result)
	})
	withPresentation := func(fn func(http.ResponseWriter, *http.Request, coreExpansionPresentationService)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !corePackageIDRE.MatchString(r.PathValue("expansion_id")) {
				writeError(w, http.StatusBadRequest, "BAD_REQUEST", "expansion identity is invalid")
				return
			}
			s, ok := service.(coreExpansionPresentationService)
			if !ok {
				writeError(w, http.StatusNotImplemented, "UNSUPPORTED_OPERATION", "expansion descriptions are unavailable")
				return
			}
			fn(w, r, s)
		}
	}
	mux.HandleFunc("GET /api/v1/core-expansions/{expansion_id}/presentation", withPresentation(func(w http.ResponseWriter, r *http.Request, s coreExpansionPresentationService) {
		if rejectCoreLibraryBody(w, r) {
			return
		}
		value, err := s.CoreExpansionPresentation(r.Context(), r.PathValue("expansion_id"))
		if err != nil {
			writeCoreLibraryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, value)
	}))
	mux.HandleFunc("PUT /api/v1/core-expansions/{expansion_id}/presentation", withPresentation(func(w http.ResponseWriter, r *http.Request, s coreExpansionPresentationService) {
		var request struct {
			Label       *string `json:"label"`
			Description *string `json:"description"`
			InProgress  *bool   `json:"in_progress"`
		}
		if decodeSingleJSON(w, r, &request) != nil {
			return
		}
		if request.Label == nil || request.Description == nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "label and description are required; use empty text to clear them")
			return
		}
		var value catalog.CoreExpansionPresentation
		var err error
		if request.InProgress != nil {
			writer, ok := service.(interface {
				SetCoreExpansionPresentationWithProgress(context.Context, string, string, string, bool) (catalog.CoreExpansionPresentation, error)
			})
			if !ok {
				writeError(w, 501, "UNSUPPORTED_OPERATION", "expansion progress labels are unavailable")
				return
			}
			value, err = writer.SetCoreExpansionPresentationWithProgress(r.Context(), r.PathValue("expansion_id"), *request.Label, *request.Description, *request.InProgress)
		} else {
			value, err = s.SetCoreExpansionPresentation(r.Context(), r.PathValue("expansion_id"), *request.Label, *request.Description)
		}
		if err != nil {
			writeCoreLibraryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, value)
	}))
}
