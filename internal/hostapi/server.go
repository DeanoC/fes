// Package hostapi exposes the local, privacy-safe FogCast application API.
package hostapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/host"
	"github.com/DeanoC/FogCast/internal/corepackage"
	"github.com/DeanoC/FogCast/internal/metadata"
	"github.com/DeanoC/FogCast/internal/version"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
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
}

type gameResult struct {
	ID              string              `json:"id"`
	Title           string              `json:"title"`
	System          protocol.System     `json:"system"`
	Kind            catalog.SourceKind  `json:"kind"`
	State           catalog.SourceState `json:"state"`
	RootOnline      bool                `json:"root_online"`
	ContentPrepared bool                `json:"content_prepared"`
	Execution       string              `json:"execution"`
	Genre           string              `json:"genre,omitempty"`
	Year            string              `json:"year,omitempty"`
	Platform        protocol.System     `json:"platform,omitempty"`
	Favorite        bool                `json:"favorite,omitempty"`
	PlayCount       int64               `json:"play_count,omitempty"`
	LastPlayedAt    int64               `json:"last_played_at,omitempty"`
	Collections     []string            `json:"collections,omitempty"`
	Cover           string              `json:"cover,omitempty"`
	Launchable      bool                `json:"launchable"`
	ROMCached       *bool               `json:"rom_cached,omitempty"`
	CanonicalTitle  string              `json:"canonical_title,omitempty"`
	Region          string              `json:"region,omitempty"`
	Revision        string              `json:"revision,omitempty"`
	DumpFlags       string              `json:"dump_flags,omitempty"`
	GroupKey        string              `json:"group_key,omitempty"`
	VariantCount    int                 `json:"variant_count,omitempty"`
	Variants        []gameResult        `json:"variants,omitempty"`
}

type gamesResult struct {
	Games      []gameResult `json:"games"`
	NextCursor string       `json:"next_cursor,omitempty"`
}

type collectionResult struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt int64  `json:"created_at,omitempty"`
}

type healthResult struct {
	Ready  bool         `json:"ready"`
	Host   hostIdentity `json:"host"`
	Target targetHealth `json:"target"`
}

type hostIdentity struct {
	Version  string `json:"version"`
	Revision string `json:"revision,omitempty"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
}

type statusResult struct {
	Connection *fogcast.TargetConnection `json:"connection,omitempty"`
	State      protocol.State            `json:"state"`
	GameID     *string                   `json:"game_id,omitempty"`
	System     *protocol.System          `json:"system,omitempty"`
	Core       *string                   `json:"core,omitempty"`
	Error      *apiError                 `json:"error,omitempty"`
}

type targetHealth struct {
	Connection *fogcast.TargetConnection `json:"connection,omitempty"`
	Reachable  bool                      `json:"reachable"`
	Ready      bool                      `json:"ready"`
	Artifacts  *protocol.Artifacts       `json:"artifacts,omitempty"`
}

type errorResult struct {
	Error apiError `json:"error"`
}

type presentationResult struct {
	GameID       string                   `json:"game_id"`
	State        string                   `json:"state"`
	Presentation *presentationPayload     `json:"presentation,omitempty"`
	Attribution  *presentationAttribution `json:"attribution,omitempty"`
}

type presentationPayload struct {
	Summary               string   `json:"summary"`
	Year                  string   `json:"year"`
	Genre                 string   `json:"genre"`
	Studio                string   `json:"studio"`
	Players               string   `json:"players"`
	Series                string   `json:"series,omitempty"`
	CoverArtworkHandle    string   `json:"cover_artwork_id,omitempty"`
	BackdropArtworkHandle string   `json:"backdrop_artwork_id,omitempty"`
	LogoHandle            string   `json:"logo_id,omitempty"`
	MarqueeHandle         string   `json:"marquee_id,omitempty"`
	Box3DHandle           string   `json:"box3d_id,omitempty"`
	VideoHandle           string   `json:"video_id,omitempty"`
	ScreenshotHandles     []string `json:"screenshot_ids,omitempty"`
}

type presentationAttribution struct {
	Provider string `json:"provider"`
	Label    string `json:"label"`
}

type apiError struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	Phase    string `json:"phase,omitempty"`
	Expected string `json:"expected,omitempty"`
	Observed string `json:"observed,omitempty"`
}

type serverOptions struct {
	remoteInput   host.RemoteInputController
	media         MediaSession
	mediaPreview  http.Handler
	metadata      metadata.Runtime
	metadataState metadata.ConfigState
}

type ServerOption func(*serverOptions)

func WithRemoteInput(remoteInput host.RemoteInputController) ServerOption {
	return func(options *serverOptions) { options.remoteInput = remoteInput }
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
	registerCoreLibrary(mux, service)
	registerCoreData(mux, service)
	session := newSessionCoordinator(service, config.remoteInput, config.media)
	uiEvents := newUIEventRing(uiEventRingCapacity)
	registerDebugUIRoutes(mux, uiEvents)
	mux.HandleFunc("GET /api/v1/session", func(w http.ResponseWriter, r *http.Request) {
		result, err := session.status(r.Context())
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": apiError{Code: "TARGET_UNAVAILABLE", Message: "target status is unavailable"}, "connection": targetConnection(service)})
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	if config.mediaPreview != nil {
		mux.Handle("GET /api/v1/session/preview", config.mediaPreview)
	}
	mux.HandleFunc("GET /api/v1/session/events", func(w http.ResponseWriter, r *http.Request) {
		var after uint64
		if raw := r.URL.Query().Get("after"); raw != "" {
			if _, err := fmt.Sscanf(raw, "%d", &after); err != nil {
				writeError(w, http.StatusBadRequest, "BAD_REQUEST", "after must be a non-negative sequence")
				return
			}
		}
		writeJSON(w, http.StatusOK, sessionEventsResult{Events: publicEvents(session.eventsAfter(after))})
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
		result, err := session.launch(r.Context(), request.GameID, request.Target, stamp)
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
		result, err := session.loadDevelopmentRBF(r.Context(), r.ContentLength, r.Body, parseClientStamp(r, "", nil, ""))
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
		result, err := session.loadDevelopmentCore(r.Context(), r.ContentLength, r.Body, parseClientStamp(r, "", nil, ""))
		if err != nil {
			writeSessionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("POST /api/v1/session/stop", func(w http.ResponseWriter, r *http.Request) {
		stamp, err := decodeOptionalStopStamp(w, r)
		if err != nil {
			return
		}
		result, err := session.stop(r.Context(), stamp)
		if err != nil {
			writeSessionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("GET /api/v1/session/input", func(w http.ResponseWriter, r *http.Request) {
		status, ok := session.inputStatus()
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
		result, err := session.attachInput(r.Context())
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
		result, err := session.detachInput(r.Context())
		if err != nil {
			writeSessionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
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
		if err := session.sendCoreKey(r.Context(), *body.Event); err != nil {
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
			Target: targetHealth{
				Reachable: err == nil, Ready: err == nil && target.Ready,
				Connection: targetConnection(service),
			},
		}
		if err == nil {
			result.Target.Artifacts = target.Artifacts
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("GET /api/v1/status", func(w http.ResponseWriter, r *http.Request) {
		status, err := service.Status(r.Context())
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": apiError{Code: "TARGET_UNAVAILABLE", Message: "target status is unavailable"}, "connection": targetConnection(service)})
			return
		}
		result := statusResult{State: status.State, GameID: status.GameID, System: status.System, Connection: targetConnection(service)}
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
		writeJSON(w, http.StatusOK, enrichGameResult(r.Context(), service, publicGameWithVariants(r.Context(), service, game)))
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
	return &applicationHandler{browser: noStore(rejectUnexpectedHost(mux)), routes: mux, service: service, remoteInput: config.remoteInput}
}

func rejectUnexpectedHost(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath := r.URL.Path
		if strings.HasPrefix(r.RequestURI, "/") {
			requestPath = strings.SplitN(r.RequestURI, "?", 2)[0]
		}
		if strings.HasPrefix(requestPath, "/api/v1/presentation/") && (strings.Contains(requestPath, "/../") || strings.Contains(requestPath, "/./") || strings.HasSuffix(requestPath, "/..") || strings.HasSuffix(requestPath, "/.")) {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "presentation path is invalid")
			return
		}
		host := r.Host
		if host == "" {
			host = r.URL.Host
		}
		hostname, _, err := net.SplitHostPort(host)
		if err != nil {
			hostname = strings.Trim(host, "[]")
		}
		ip := net.ParseIP(hostname)
		if !strings.EqualFold(hostname, "localhost") && (ip == nil || !ip.IsLoopback()) {
			writeError(w, http.StatusForbidden, "HOST_NOT_ALLOWED", "host is not allowed")
			return
		}
		next.ServeHTTP(w, r)
	})
}

var presentationHandlePattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func safePresentation(result metadata.Result) (presentationPayload, presentationAttribution, bool) {
	label, ok := presentationLabel(result.Attribution)
	if !ok {
		return presentationPayload{}, presentationAttribution{}, false
	}
	values := []struct {
		value string
		limit int
	}{
		{result.Presentation.Summary, 240},
		{result.Presentation.Year, 20},
		{result.Presentation.Genre, 40},
		{result.Presentation.Studio, 60},
		{result.Presentation.Players, 40},
		{result.Presentation.Series, 80},
	}
	for _, value := range values {
		if !utf8.ValidString(value.value) {
			return presentationPayload{}, presentationAttribution{}, false
		}
	}
	return presentationPayload{
		Summary:               boundedPresentationText(result.Presentation.Summary, 240),
		Year:                  boundedPresentationText(result.Presentation.Year, 20),
		Genre:                 boundedPresentationText(result.Presentation.Genre, 40),
		Studio:                boundedPresentationText(result.Presentation.Studio, 60),
		Players:               boundedPresentationText(result.Presentation.Players, 40),
		Series:                boundedPresentationText(result.Presentation.Series, 80),
		CoverArtworkHandle:    safePresentationHandle(result.Presentation.CoverArtworkID),
		BackdropArtworkHandle: safePresentationHandle(result.Presentation.BackdropArtworkID),
		LogoHandle:            safePresentationHandle(result.Presentation.LogoArtworkID),
		MarqueeHandle:         safePresentationHandle(result.Presentation.MarqueeArtworkID),
		Box3DHandle:           safePresentationHandle(result.Presentation.Box3DArtworkID),
	}, presentationAttribution{Provider: string(result.Attribution.Provider), Label: label}, true
}

func presentationLabel(attribution metadata.Attribution) (string, bool) {
	switch {
	case attribution.Provider == metadata.ProviderIGDB && attribution.Label == "Data from IGDB.com":
		return attribution.Label, true
	case attribution.Provider == metadata.ProviderLaunchBox && attribution.Label == "Data from LaunchBox Games Database":
		return attribution.Label, true
	default:
		return "", false
	}
}

func boundedPresentationText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if !utf8.ValidString(value) {
		return ""
	}
	runes := []rune(value)
	if len(runes) > limit {
		runes = runes[:limit]
	}
	return string(runes)
}

func safePresentationHandle(value string) string {
	if presentationHandlePattern.MatchString(value) {
		return value
	}
	return ""
}

func requireMetadata(w http.ResponseWriter, state metadata.ConfigState) error {
	switch state {
	case metadata.StateReady:
		return nil
	case metadata.StateDisabled:
		writeError(w, http.StatusNotFound, "METADATA_DISABLED", "presentation metadata is disabled")
	default:
		writeError(w, http.StatusServiceUnavailable, "METADATA_UNCONFIGURED", "presentation metadata is not configured")
	}
	return errors.New("metadata is not ready")
}

func metadataCode(err error) metadata.ErrorCode {
	var operation *metadata.OpError
	if errors.As(err, &operation) && operation != nil {
		return operation.Code
	}
	return ""
}

func writeMetadataError(w http.ResponseWriter, err error) {
	switch metadataCode(err) {
	case metadata.ErrRateLimited:
		writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", "metadata provider is rate limited")
	case metadata.ErrDeadline:
		writeError(w, http.StatusGatewayTimeout, "UPSTREAM_TIMEOUT", "metadata provider timed out")
	case metadata.ErrCanceled:
		writeError(w, http.StatusServiceUnavailable, "CANCELED", "metadata request was canceled")
	case metadata.ErrUpstreamUnavailable, metadata.ErrUnauthorized, metadata.ErrInvalidResponse:
		writeError(w, http.StatusBadGateway, "UPSTREAM_UNAVAILABLE", "metadata provider is unavailable")
	case metadata.ErrStorage:
		writeError(w, http.StatusServiceUnavailable, "STORAGE_UNAVAILABLE", "metadata storage is unavailable")
	case metadata.ErrPolicyBlocked:
		writeError(w, http.StatusServiceUnavailable, "POLICY_BLOCKED", "metadata request was blocked")
	default:
		writeError(w, http.StatusServiceUnavailable, "METADATA_UNAVAILABLE", "metadata is unavailable")
	}
}

func publicErrorMessage(code protocol.ErrorCode) string {
	switch code {
	case protocol.CodeCorruptData:
		return "stored core data is corrupt"
	case protocol.CodeIncompatibleData:
		return "stored or selected core data is incompatible"
	case protocol.CodeStaleRevision:
		return "core package selection or data revision changed; refresh before retrying"
	case protocol.CodeSaveFailed:
		return "core data could not be durably written; inspect status before retrying"
	case protocol.CodeBadRequest:
		return "FogCast request is invalid"
	case protocol.CodeUnauthorized:
		return "target authentication failed"
	case protocol.CodeROMNotFound:
		return "catalog game was not found"
	case protocol.CodeBusy:
		return "another launch or stop transition is running"
	case protocol.CodeUnsupportedSystem:
		return "game system is unsupported"
	case protocol.CodeUnsupportedOperation:
		return "requested operation is unsupported"
	case protocol.CodeInvalidROMPath:
		return "target ROM path is invalid"
	case protocol.CodeSourceUnavailable:
		return "game source is unavailable"
	case protocol.CodeTransferFailed:
		return "content transfer failed"
	case protocol.CodeKitLeaseDenied:
		return "kit lease is foreign; HID is fail-closed"
	case protocol.CodeVersionMismatch:
		return "target artifacts do not match this host"
	case protocol.CodeMiSTerUnavailable:
		return "MiSTer is unavailable"
	case protocol.CodeCoreTimeout:
		return "core transition timed out"
	case protocol.CodeUnrecognizedCore:
		return "active core is unrecognized"
	default:
		return "FogCast operation failed internally"
	}
}

func publicGame(game catalog.Game) gameResult {
	canonical := game.CanonicalTitle
	if canonical == "" {
		canonical = catalog.ParseDump(game.Title).CanonicalTitle
	}
	result := gameResult{
		ID: game.ID, Title: game.Title, System: game.System, Kind: game.Kind,
		State: game.State, RootOnline: game.RootOnline, ContentPrepared: game.Content != nil,
		Execution: fogcast.ExecutionFPGANative, Platform: game.System, Launchable: catalog.Launchable(game.System),
		CanonicalTitle: canonical, Region: game.Region, Revision: game.Revision, DumpFlags: game.DumpFlags,
		GroupKey: game.GroupKey, VariantCount: game.VariantCount, Genre: game.Genre, Year: game.Year,
	}
	if game.Kind == catalog.SourceKindCorePackage {
		result.Execution = fogcast.ExecutionFPGADevelopment
	}
	if result.VariantCount <= 0 {
		result.VariantCount = 1
	}
	return result
}

func metadataLookupTitles(game catalog.Game) []string {
	title := strings.TrimSpace(game.Title)
	canonical := strings.TrimSpace(game.CanonicalTitle)
	if canonical == "" {
		canonical = strings.TrimSpace(catalog.ParseDump(title).CanonicalTitle)
	}
	if canonical != "" {
		normalizedCanonical, canonicalErr := metadata.NormalizeTitle(canonical)
		decoratedTitle, decoratedErr := metadata.DecoratedTitle(title)
		if canonicalErr == nil && decoratedErr == nil && decoratedTitle != normalizedCanonical {
			return []string{title, canonical}
		}
		return []string{canonical}
	}
	return []string{game.Title}
}

func lookupGameMetadata(ctx context.Context, runtime metadata.Runtime, game catalog.Game) (metadata.Result, error) {
	var fallback metadata.Result
	titles := metadataLookupTitles(game)
	for index, title := range titles {
		result, err := runtime.Lookup(ctx, metadata.LookupInput{Title: title, System: game.System})
		if err != nil {
			return metadata.Result{}, err
		}
		if result.Outcome != metadata.OutcomeNoMatch || index == len(titles)-1 {
			return result, nil
		}
		fallback = result
	}
	return fallback, nil
}

func publicGameWithGenre(ctx context.Context, config serverOptions, game catalog.Game) gameResult {
	result := publicGame(game)
	if config.metadata == nil || config.metadataState != metadata.StateReady {
		return result
	}
	lookup, err := lookupGameMetadata(ctx, config.metadata, game)
	if err != nil || (lookup.Outcome != metadata.OutcomeExact && lookup.Outcome != metadata.OutcomeConfident) {
		return result
	}
	result.Genre = strings.TrimSpace(lookup.Presentation.Genre)
	result.Year = strings.TrimSpace(lookup.Presentation.Year)
	return result
}

func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorResult{Error: apiError{Code: code, Message: message}})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func decodeSingleJSON(w http.ResponseWriter, r *http.Request, value any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "request body must contain one valid JSON object")
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "request body must contain exactly one JSON object")
		return errors.New("trailing JSON")
	}
	return nil
}

func rejectBody(w http.ResponseWriter, r *http.Request) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1)
	body, err := io.ReadAll(r.Body)
	if err != nil || len(body) != 0 {
		return errors.New("non-empty body")
	}
	return nil
}

func writeSessionError(w http.ResponseWriter, err error) {
	var apiErr *protocol.APIError
	if errors.As(err, &apiErr) {
		status := http.StatusInternalServerError
		if apiErr.Code == protocol.CodeBusy {
			status = http.StatusConflict
		}
		if apiErr.Code == protocol.CodeKitLeaseDenied {
			status = http.StatusForbidden
		}
		if apiErr.Code == protocol.CodeROMNotFound {
			status = http.StatusNotFound
		}
		if apiErr.Code == protocol.CodeBadRequest || apiErr.Code == protocol.CodeUnsupportedSystem || apiErr.Code == protocol.CodeUnsupportedOperation {
			status = http.StatusBadRequest
		}
		writeJSON(w, status, map[string]any{"error": apiError{
			Code: string(apiErr.Code), Message: publicErrorMessage(apiErr.Code), Phase: apiErr.Phase,
			Expected: apiErr.Expected, Observed: apiErr.Observed,
		}})
		return
	}
	writeError(w, http.StatusServiceUnavailable, "TARGET_UNAVAILABLE", "session operation failed")
}

func targetConnection(service Service) *fogcast.TargetConnection {
	if provider, ok := service.(interface {
		TargetConnection() fogcast.TargetConnection
	}); ok {
		connection := provider.TargetConnection()
		return &connection
	}
	return nil
}
