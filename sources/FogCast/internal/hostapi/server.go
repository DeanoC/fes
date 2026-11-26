// Package hostapi exposes the local, privacy-safe FogCast application API.
package hostapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/host"
	"github.com/DeanoC/FogCast/internal/metadata"
	"github.com/DeanoC/FogCast/internal/version"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/storage"
)

// Service is the host operation surface used by the privacy-safe application API.
type Service interface {
	Games(context.Context) ([]catalog.Game, error)
	Search(context.Context, string) ([]catalog.Game, error)
	Game(context.Context, string) (catalog.Game, error)
	Health(context.Context) (protocol.Health, error)
	Status(context.Context) (protocol.Status, error)
	Launch(context.Context, string, fogcast.ProgressFunc) (protocol.CachedLaunchResponse, error)
	LoadDevelopmentRBF(context.Context, int64, io.Reader) (protocol.Status, error)
	Stop(context.Context) (protocol.Status, error)
	// MeshBackendLibrary is the combined library. The host API does not
	// treat a missing projection as an empty title list.
	MeshBackendLibrary(context.Context) ([]fogcast.MeshBackendRow, []fogcast.MeshSkip)
}

type serverOptions struct {
	storage            *storage.Manager
	remoteInput        host.RemoteInputController
	remoteInputFactory func(string) host.RemoteInputController
	media              MediaSession
	mediaPreview       http.Handler
	metadata           metadata.Runtime
	metadataState      metadata.ConfigState
}

type ServerOption func(*serverOptions)

func WithRemoteInput(remoteInput host.RemoteInputController) ServerOption {
	return func(options *serverOptions) { options.remoteInput = remoteInput }
}

// WithRemoteInputFactory provides an independent input bridge for each named
// target session. The factory must be concurrency-safe.
func WithRemoteInputFactory(factory func(string) host.RemoteInputController) ServerOption {
	return func(options *serverOptions) { options.remoteInputFactory = factory }
}

func WithMediaSession(media MediaSession) ServerOption {
	return func(options *serverOptions) { options.media = media }
}

// WithMediaPreview exposes a browser-safe picture stream owned by the active
// host media session.
func WithMediaPreview(preview http.Handler) ServerOption {
	return func(options *serverOptions) { options.mediaPreview = preview }
}

func WithMetadata(runtime metadata.Runtime, states ...metadata.ConfigState) ServerOption {
	return func(options *serverOptions) {
		options.metadata = runtime
		if len(states) != 0 {
			options.metadataState = states[0]
		} else if runtime != nil {
			options.metadataState = metadata.StateReady
		} else {
			options.metadataState = metadata.StateUnconfigured
		}
	}
}

func requestSessionCoordinator(root *sessionCoordinator, service Service, r *http.Request, target string, foregroundOnly ...bool) *sessionCoordinator {
	if target == "" {
		target = launcherTargetFromContext(r.Context())
	}
	if target == "" {
		if pairedID := fogcast.PairedTargetFromContext(r.Context()); pairedID != "" {
			if resolver, ok := service.(interface{ TargetNameForID(string) string }); ok {
				target = resolver.TargetNameForID(pairedID)
			}
		}
	}
	if target == "" {
		// Unscoped reads and mutations follow the foreground session, falling
		// back to the selected target only when no kit is active.
		if len(foregroundOnly) > 0 && foregroundOnly[0] {
			if resolver, ok := service.(interface{ ForegroundSessionTargetName() string }); ok {
				target = resolver.ForegroundSessionTargetName()
				// A restarted kit host has not observed its one local target
				// yet. Use that coordinator on the first read as well, so
				// adoption does not change session identity or input owner.
				if target == "" {
					if kit, ok := service.(interface{ KitHostMode() bool }); ok && kit.KitHostMode() {
						if selected, ok := service.(interface{ SessionTargetName() string }); ok {
							target = selected.SessionTargetName()
						}
					}
				}
			} else if resolver, ok := service.(interface{ SessionTargetName() string }); ok {
				target = resolver.SessionTargetName()
			} else if resolver, ok := service.(interface{ SelectedTargetConfig() fogcast.TargetConfig }); ok {
				target = resolver.SelectedTargetConfig().Name
			}
		} else if resolver, ok := service.(interface{ SessionTargetName() string }); ok {
			target = resolver.SessionTargetName()
		} else if resolver, ok := service.(interface{ SelectedTargetConfig() fogcast.TargetConfig }); ok {
			target = resolver.SelectedTargetConfig().Name
		}
	}
	return root.forTarget(target)
}

func New(service Service, options ...ServerOption) http.Handler {
	if service == nil {
		panic("hostapi: nil service")
	}
	var config serverOptions
	for _, option := range options {
		if option != nil {
			option(&config)
		}
	}
	mux := http.NewServeMux()
	registerStorage(mux, config.storage)
	registerCoreLibrary(mux, service)
	registerKitSoftwareInventory(mux, service)
	registerCoreData(mux, service)
	registerMeshHostContent(mux, service)
	registerLibraryTitles(mux, service)
	registerLibrarySources(mux, service)
	session := newSessionCoordinator(service, config.remoteInput, config.media)
	session.remoteInputFactory = config.remoteInputFactory
	registerHardware(mux, service, session)
	uiEvents := newUIEventRing(uiEventRingCapacity)
	registerDebugUIRoutes(mux, uiEvents)
	mux.HandleFunc("GET /api/v1/session", func(w http.ResponseWriter, r *http.Request) {
		target := r.URL.Query().Get("target")
		if !configuredSessionTarget(service, target) {
			writeError(w, http.StatusNotFound, string(protocol.CodeTargetNotFound), "target is not configured")
			return
		}
		coordinator := requestSessionCoordinator(session, service, r, target, true)
		result, err := coordinator.status(r.Context())
		if err != nil {
			var apiErr *protocol.APIError
			if errors.As(err, &apiErr) && (apiErr.Code == protocol.CodeKitLeaseDenied || apiErr.Code == protocol.CodeKitLeaseBusy) {
				writeSessionError(w, err)
				return
			}
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": apiError{Code: "TARGET_UNAVAILABLE", Message: "target status is unavailable"}, "connection": targetConnection(service, target)})
			return
		}
		result.Connection = targetConnection(service, target)
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("GET /api/v1/sessions", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/sessions" {
			http.NotFound(w, r)
			return
		}
		lister, ok := service.(interface{ PlaySessions() []fogcast.PlaySession })
		if !ok {
			writeJSON(w, http.StatusOK, map[string]any{"sessions": []any{}})
			return
		}
		plays := lister.PlaySessions()
		sessions := make([]map[string]any, 0, len(plays))
		for _, play := range plays {
			item := map[string]any{"target": play.Target, "execution": play.Execution, "state": protocol.StateActive}
			if play.TargetID != "" {
				item["target_id"] = play.TargetID
			}
			if play.GameID != "" {
				item["game_id"] = play.GameID
			}
			if play.System != "" {
				item["system"] = play.System
			}
			sessions = append(sessions, item)
		}
		writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
	})
	if config.mediaPreview != nil {
		mux.Handle("GET /api/v1/session/preview", config.mediaPreview)
	}
	mux.HandleFunc("GET /api/v1/session/events", func(w http.ResponseWriter, r *http.Request) {
		coordinator := requestSessionCoordinator(session, service, r, "", true)
		var after uint64
		if raw := r.URL.Query().Get("after"); raw != "" {
			if _, err := fmt.Sscanf(raw, "%d", &after); err != nil {
				writeError(w, http.StatusBadRequest, "BAD_REQUEST", "after must be a non-negative sequence")
				return
			}
		}
		writeJSON(w, http.StatusOK, sessionEventsResult{Events: publicEvents(coordinator.eventsAfter(after))})
	})
	mux.HandleFunc("POST /api/v1/session/launch", func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			GameID       string `json:"game_id"`
			Target       string `json:"target"`
			ClientTsUTC  string `json:"client_ts_utc"`
			ClientMonoMS *int64 `json:"client_mono_ms"`
			FlightID     string `json:"flight_id"`
		}
		if err := decodeSingleJSON(w, r, &request); err != nil {
			return
		}
		if protocol.ValidateGameID(request.GameID) != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "game ID is invalid")
			return
		}
		stamp := parseClientStamp(r, request.ClientTsUTC, request.ClientMonoMS, request.FlightID)
		// Launch routing is intentionally independent from the foreground
		// session: preserve an omitted target so FogCast can apply placement.
		target := request.Target
		pairedTarget := fogcast.PairedTargetFromContext(r.Context())
		if target == "" && pairedTarget == "" {
			target = launcherTargetFromContext(r.Context())
		}
		coordinatorTarget := target
		if !configuredSessionTarget(service, target) {
			writeError(w, http.StatusNotFound, string(protocol.CodeTargetNotFound), "target is not configured")
			return
		}
		if target == "" && pairedTarget == "" {
			if selected, ok := service.(interface{ SelectedTargetConfig() fogcast.TargetConfig }); ok {
				coordinatorTarget = selected.SelectedTargetConfig().Name
			}
		}
		coordinator := requestSessionCoordinator(session, service, r, coordinatorTarget)
		result, err := coordinator.launch(r.Context(), request.GameID, target, stamp)
		if err != nil {
			writeSessionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("POST /api/v1/session/development-rbf", func(w http.ResponseWriter, r *http.Request) {
		contentTypes := r.Header.Values("Content-Type")
		if len(contentTypes) != 1 || contentTypes[0] != "application/octet-stream" || len(r.TransferEncoding) != 0 ||
			r.ContentLength < 1 || r.ContentLength > protocol.MaxDevelopmentRBFBytes {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "development RBF upload requires a bounded application/octet-stream body")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, protocol.MaxDevelopmentRBFBytes)
		coordinator := requestSessionCoordinator(session, service, r, "")
		result, err := coordinator.loadDevelopmentRBF(r.Context(), r.ContentLength, r.Body, parseClientStamp(r, "", nil, ""))
		if err != nil {
			writeSessionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("POST /api/v1/session/development-core", func(w http.ResponseWriter, r *http.Request) {
		contentTypes := r.Header.Values("Content-Type")
		if len(contentTypes) != 1 || contentTypes[0] != "application/octet-stream" || len(r.TransferEncoding) != 0 ||
			r.ContentLength < 1 || r.ContentLength > corepackage.MaxArchiveSize {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "development core upload requires a bounded application/octet-stream body")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, corepackage.MaxArchiveSize)
		coordinator := requestSessionCoordinator(session, service, r, "")
		result, err := coordinator.loadDevelopmentCore(r.Context(), r.ContentLength, r.Body, parseClientStamp(r, "", nil, ""))
		if err != nil {
			writeSessionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	registerDevelopmentMediaRoute(mux, session)
	registerLiveMediaSessionRoutes(mux, session)
	registerSessionDisplayRoute(mux, session)
	registerCartridgeSessionRoute(mux, session)
	mux.HandleFunc("POST /api/v1/session/stop", func(w http.ResponseWriter, r *http.Request) {
		stamp, retainLease, releaseIdle, expected, explicitTarget, err := decodeOptionalStopRequest(w, r)
		if err != nil {
			return
		}
		pairedTarget := launcherTargetFromContext(r.Context())
		if pairedTarget != "" && explicitTarget != "" && explicitTarget != pairedTarget {
			writeError(w, http.StatusForbidden, "TARGET_MISMATCH", "launcher target does not match the paired target")
			return
		}
		if !configuredSessionTarget(service, explicitTarget) {
			writeError(w, http.StatusNotFound, string(protocol.CodeTargetNotFound), "target is not configured")
			return
		}
		coordinator := requestSessionCoordinator(session, service, r, explicitTarget, true)
		result, err := coordinator.stop(r.Context(), stamp, retainLease, releaseIdle, expected)
		if err != nil {
			writeSessionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("GET /api/v1/session/input", func(w http.ResponseWriter, r *http.Request) {
		coordinator := requestSessionCoordinator(session, service, r, "")
		status, ok := coordinator.inputStatus()
		if !ok {
			writeError(w, http.StatusNotFound, "INPUT_UNAVAILABLE", "remote input is unavailable")
			return
		}
		writeJSON(w, http.StatusOK, status)
	})
	mux.HandleFunc("POST /api/v1/session/input/attach", func(w http.ResponseWriter, r *http.Request) {
		if err := rejectBody(w, r); err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "attach request body must be empty")
			return
		}
		coordinator := requestSessionCoordinator(session, service, r, "")
		result, err := coordinator.attachInput(r.Context())
		if err != nil {
			writeSessionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("POST /api/v1/session/input/detach", func(w http.ResponseWriter, r *http.Request) {
		if err := rejectBody(w, r); err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "detach request body must be empty")
			return
		}
		coordinator := requestSessionCoordinator(session, service, r, "")
		result, err := coordinator.detachInput(r.Context())
		if err != nil {
			writeSessionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("POST /api/v1/session/input/tap", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			SessionID string             `json:"session_id"`
			Event     *remoteinput.Event `json:"event"`
			Modifiers json.RawMessage    `json:"modifiers"`
		}
		decoder := json.NewDecoder(io.LimitReader(r.Body, 4096))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&body); err != nil || body.SessionID == "" || body.Event == nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "input tap is invalid")
			return
		}
		var modifiers uint8
		if len(body.Modifiers) != 0 && (string(body.Modifiers) == "null" || json.Unmarshal(body.Modifiers, &modifiers) != nil) {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "input tap modifiers are invalid")
			return
		}
		coordinator := requestSessionCoordinator(session, service, r, "")
		duration, err := coordinator.tapCoreKey(r.Context(), body.SessionID, *body.Event, modifiers)
		if err != nil {
			if errors.Is(err, host.ErrRemoteInputUnconfirmed) {
				writeError(w, http.StatusServiceUnavailable, "INPUT_UNCONFIRMED", "input may have applied but could not be confirmed")
				return
			}
			if errors.Is(err, host.ErrRemoteInputInvalid) {
				writeError(w, http.StatusBadRequest, "BAD_REQUEST", "input tap or session is invalid")
				return
			}
			if errors.Is(err, host.ErrRemoteInputBusy) {
				writeError(w, http.StatusConflict, "INPUT_BUSY", "input key or source is already held")
				return
			}
			if errors.Is(err, host.ErrRemoteInputNoStream) {
				writeError(w, http.StatusServiceUnavailable, "INPUT_UNAVAILABLE", "no live input stream")
				return
			}
			writeSessionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "host_duration_ms": float64(duration) / float64(time.Millisecond)})
	})
	mux.HandleFunc("POST /api/v1/session/input/event", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Event *remoteinput.Event `json:"event"`
		}
		decoder := json.NewDecoder(io.LimitReader(r.Body, 4096))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&body); err != nil || body.Event == nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "input event is invalid")
			return
		}
		coordinator := requestSessionCoordinator(session, service, r, "")
		if err := coordinator.sendCoreKey(r.Context(), *body.Event); err != nil {
			if errors.Is(err, host.ErrRemoteInputUnconfirmed) {
				writeError(w, http.StatusServiceUnavailable, "INPUT_UNCONFIRMED", "input may have applied but could not be confirmed")
				return
			}
			if errors.Is(err, host.ErrRemoteInputNoStream) {
				writeError(w, http.StatusServiceUnavailable, "INPUT_UNAVAILABLE", "no live input stream")
				return
			}
			writeSessionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})

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
	return &applicationHandler{browser: noStore(rejectUnexpectedHost(mux)), routes: mux, service: service, remoteInput: config.remoteInput, session: session}
}
