package hostapi

import (
	"errors"
	"net/http"
	"runtime"
	"strings"

	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/internal/version"
	"github.com/DeanoC/FogCast/protocol"
)

func registerStatusAndCatalogRoutes(mux *http.ServeMux, service Service) {
	mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		target, err := service.Health(r.Context())
		result := healthResult{
			Ready: true,
			Host:  hostIdentity{Version: version.Version, Revision: version.Revision, OS: runtime.GOOS, Arch: runtime.GOARCH},
			Mesh:  map[string]int{"major": 1, "minor": 0},
			Target: targetHealth{
				Reachable: err == nil, Ready: err == nil && target.Ready,
				Connection: targetConnection(service),
			},
		}
		if targetID := fogcast.PairedTargetFromContext(r.Context()); targetID != "" {
			state := "disconnected"
			if err == nil {
				state = "connected"
			}
			result.Target.Connection = &fogcast.TargetConnection{TargetID: targetID, State: state}
		}
		if err == nil {
			result.Target.APIVersion = target.APIVersion
			result.Target.Artifacts = target.Artifacts
			result.Target.Software = target.Software
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("GET /api/v1/status", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if target := r.URL.Query().Get("target"); target != "" {
			if !configuredSessionTarget(service, target) {
				writeError(w, http.StatusNotFound, string(protocol.CodeTargetNotFound), "target is not configured")
				return
			}
			ctx = fogcast.WithSessionTarget(ctx, target)
		}
		status, err := service.Status(ctx)
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": apiError{Code: "TARGET_UNAVAILABLE", Message: "target status is unavailable"}, "connection": targetConnection(service, r.URL.Query().Get("target"))})
			return
		}
		connection := targetConnection(service, r.URL.Query().Get("target"))
		if targetID := fogcast.PairedTargetFromContext(r.Context()); targetID != "" {
			state := "connected"
			if status.State == "" {
				state = "disconnected"
			}
			connection = &fogcast.TargetConnection{TargetID: targetID, State: state}
		}
		result := statusResult{State: status.State, GameID: status.GameID, System: status.System, Connection: connection}
		if status.ObservedCore != nil {
			result.Core = status.ObservedCore
		} else {
			result.Core = status.ExpectedCore
		}
		if status.LastError != nil {
			result.Error = &apiError{Code: string(status.LastError.Code), Message: publicErrorMessage(status.LastError.Code)}
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("GET /api/v1/mesh/nodes", func(w http.ResponseWriter, r *http.Request) {
		nodes := []fogcast.MeshNode{}
		if provider, ok := service.(interface{ MeshNodes() []fogcast.MeshNode }); ok {
			if listed := provider.MeshNodes(); len(listed) > 0 {
				nodes = listed
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"nodes": nodes})
	})
	mux.HandleFunc("GET /api/v1/games", func(w http.ResponseWriter, r *http.Request) {
		handleGamesList(w, r, service)
	})
	mux.HandleFunc("GET /api/v1/platforms", func(w http.ResponseWriter, r *http.Request) {
		handlePlatforms(w, r, service)
	})
	mux.HandleFunc("PUT /api/v1/library/favorites/{id}", func(w http.ResponseWriter, r *http.Request) {
		handleFavorite(w, r, service, true)
	})
	mux.HandleFunc("DELETE /api/v1/library/favorites/{id}", func(w http.ResponseWriter, r *http.Request) {
		handleFavorite(w, r, service, false)
	})
	mux.HandleFunc("GET /api/v1/library/collections", func(w http.ResponseWriter, r *http.Request) {
		handleCollections(w, r, service)
	})
	mux.HandleFunc("GET /api/v1/library/edition-preferences", func(w http.ResponseWriter, r *http.Request) {
		handleEditionPreferences(w, r, service)
	})
	mux.HandleFunc("PUT /api/v1/library/edition-preferences", func(w http.ResponseWriter, r *http.Request) {
		handleSetEditionPreference(w, r, service)
	})
	mux.HandleFunc("PUT /api/v1/library/collections/{id}/{gameId}", func(w http.ResponseWriter, r *http.Request) {
		handleCollectionMember(w, r, service, true)
	})
	mux.HandleFunc("DELETE /api/v1/library/collections/{id}/{gameId}", func(w http.ResponseWriter, r *http.Request) {
		handleCollectionMember(w, r, service, false)
	})
	mux.HandleFunc("PUT /api/v1/library/collections/{id}", func(w http.ResponseWriter, r *http.Request) {
		handleCollection(w, r, service, true)
	})
	mux.HandleFunc("DELETE /api/v1/library/collections/{id}", func(w http.ResponseWriter, r *http.Request) {
		handleCollection(w, r, service, false)
	})
	mux.HandleFunc("GET /api/v1/library/attract", func(w http.ResponseWriter, r *http.Request) {
		handleAttract(w, r, service)
	})
	mux.HandleFunc("GET /api/v1/library/cache", func(w http.ResponseWriter, r *http.Request) {
		handleLibraryCache(w, r, service)
	})
	mux.HandleFunc("GET /api/v1/library/facets", func(w http.ResponseWriter, r *http.Request) {
		handleFacets(w, r, service)
	})
	mux.HandleFunc("GET /api/v1/library/settings", func(w http.ResponseWriter, r *http.Request) {
		handleLibrarySettings(w, r, service)
	})
	mux.HandleFunc("PUT /api/v1/library/settings", func(w http.ResponseWriter, r *http.Request) {
		handleLibrarySettings(w, r, service)
	})
	mux.HandleFunc("PATCH /api/v1/library/settings", func(w http.ResponseWriter, r *http.Request) {
		handleLibrarySettings(w, r, service)
	})
	mux.HandleFunc("GET /api/v1/games/{id}", func(w http.ResponseWriter, r *http.Request) {
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
		result, err := enrichGameResult(r.Context(), service, publicGameWithVariants(r.Context(), service, game))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL", "catalog is unavailable")
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
}
