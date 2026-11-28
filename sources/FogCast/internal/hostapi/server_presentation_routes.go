package hostapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/DeanoC/FogCast/internal/metadata"
	"github.com/DeanoC/FogCast/protocol"
)

func registerPresentationRoutes(mux *http.ServeMux, service Service, config serverOptions) {
	mux.HandleFunc("GET /api/v1/presentation/games/{id}", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" || r.Body != nil && r.Body != http.NoBody {
			if err := rejectBody(w, r); err != nil || r.URL.RawQuery != "" {
				writeError(w, http.StatusBadRequest, "BAD_REQUEST", "presentation requests do not accept query parameters or bodies")
				return
			}
		}
		id := r.PathValue("id")
		if id == "" || strings.Contains(id, "/") || protocol.ValidateGameID(id) != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "game ID is invalid")
			return
		}
		game, err := service.Game(r.Context(), id)
		if err != nil {
			var apiErr *protocol.APIError
			if errors.As(err, &apiErr) && apiErr.Code == protocol.CodeROMNotFound {
				writeError(w, http.StatusNotFound, "GAME_NOT_FOUND", "game was not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "INTERNAL", "catalog is unavailable")
			return
		}
		if config.metadataState == metadata.StateDisabled {
			writeJSON(w, http.StatusOK, overlayPresentation(r.Context(), service, game, presentationResult{GameID: game.ID, State: string(metadata.StateDisabled)}))
			return
		}
		if config.metadataState == metadata.StateUnconfigured || config.metadata == nil {
			writeJSON(w, http.StatusOK, overlayPresentation(r.Context(), service, game, presentationResult{GameID: game.ID, State: string(metadata.StateUnconfigured)}))
			return
		}
		result, err := lookupGameMetadata(r.Context(), config.metadata, game)
		if err != nil {
			writeJSON(w, http.StatusOK, overlayPresentation(r.Context(), service, game, presentationResult{GameID: game.ID, State: "offline"}))
			return
		}
		switch result.Outcome {
		case metadata.OutcomeExact, metadata.OutcomeConfident:
			presentation, attribution, ok := safePresentation(result)
			if !ok {
				writeJSON(w, http.StatusOK, overlayPresentation(r.Context(), service, game, presentationResult{GameID: game.ID, State: "offline"}))
				return
			}
			if strings.TrimSpace(presentation.Genre) != "" || strings.TrimSpace(presentation.Year) != "" {
				if writer, ok := service.(interface {
					SetFacets(context.Context, string, string, string, string) error
				}); ok {
					_ = writer.SetFacets(r.Context(), game.ID, presentation.Genre, presentation.Year, "")
				}
			}
			writeJSON(w, http.StatusOK, overlayPresentation(r.Context(), service, game, presentationResult{GameID: game.ID, State: "ready", Presentation: &presentation, Attribution: &attribution}))
		case metadata.OutcomeNoMatch:
			writeJSON(w, http.StatusOK, overlayPresentation(r.Context(), service, game, presentationResult{GameID: game.ID, State: string(metadata.OutcomeNoMatch)}))
		case metadata.OutcomeAmbiguous:
			writeJSON(w, http.StatusOK, overlayPresentation(r.Context(), service, game, presentationResult{GameID: game.ID, State: string(metadata.OutcomeAmbiguous)}))
		default:
			writeJSON(w, http.StatusOK, overlayPresentation(r.Context(), service, game, presentationResult{GameID: game.ID, State: "offline"}))
		}
	})
	mux.HandleFunc("GET /api/v1/presentation/artwork/{handle}", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" || r.Body != nil && r.Body != http.NoBody {
			_ = rejectBody(w, r)
			writeError(w, http.StatusNotFound, "ARTWORK_UNAVAILABLE", "artwork is unavailable")
			return
		}
		handle := r.PathValue("handle")
		if !presentationHandlePattern.MatchString(handle) {
			writeError(w, http.StatusNotFound, "ARTWORK_UNAVAILABLE", "artwork is unavailable")
			return
		}
		if config.metadataState != metadata.StateReady || config.metadata == nil {
			if opener, ok := service.(mediaOpenService); ok {
				opened, openErr := opener.OpenMedia(r.Context(), handle)
				if openErr == nil && opened.Reader != nil {
					defer opened.Reader.Close()
					librarymediaServe(w, r, opened)
					return
				}
			}
			writeError(w, http.StatusNotFound, "ARTWORK_UNAVAILABLE", "artwork is unavailable")
			return
		}
		artwork, err := config.metadata.OpenArtwork(r.Context(), handle)
		if err != nil {
			if opener, ok := service.(mediaOpenService); ok {
				opened, openErr := opener.OpenMedia(r.Context(), handle)
				if openErr == nil && opened.Reader != nil {
					defer opened.Reader.Close()
					librarymediaServe(w, r, opened)
					return
				}
			}
			writeError(w, http.StatusNotFound, "ARTWORK_UNAVAILABLE", "artwork is unavailable")
			return
		}
		if artwork.Reader == nil || (artwork.MIME != "image/jpeg" && artwork.MIME != "image/png") || artwork.Size < 1 || artwork.Size > 8<<20 {
			if artwork.Reader != nil {
				_ = artwork.Reader.Close()
			}
			writeError(w, http.StatusNotFound, "ARTWORK_UNAVAILABLE", "artwork is unavailable")
			return
		}
		defer artwork.Reader.Close()
		w.Header().Set("Content-Type", artwork.MIME)
		w.Header().Set("Content-Length", strconv.FormatInt(artwork.Size, 10))
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
		if _, err := io.CopyN(w, artwork.Reader, artwork.Size); err != nil {
			return
		}
	})
	mux.HandleFunc("GET /api/v1/presentation/media/{handle}", func(w http.ResponseWriter, r *http.Request) {
		handle := r.PathValue("handle")
		if !presentationHandlePattern.MatchString(handle) {
			writeError(w, http.StatusNotFound, "ARTWORK_UNAVAILABLE", "artwork is unavailable")
			return
		}
		opener, ok := service.(mediaOpenService)
		if !ok {
			writeError(w, http.StatusNotFound, "ARTWORK_UNAVAILABLE", "artwork is unavailable")
			return
		}
		opened, err := opener.OpenMedia(r.Context(), handle)
		if err != nil || opened.Reader == nil {
			writeError(w, http.StatusNotFound, "ARTWORK_UNAVAILABLE", "artwork is unavailable")
			return
		}
		defer opened.Reader.Close()
		librarymediaServe(w, r, opened)
	})
}
