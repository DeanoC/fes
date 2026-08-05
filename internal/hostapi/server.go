// Package hostapi exposes the local, privacy-safe FogCast application API.
package hostapi

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"sort"
	"strings"

	"github.com/DeanoC/FogCast-POC/catalog"
	"github.com/DeanoC/FogCast-POC/protocol"
)

// Service is the read-only host operation surface required by the first POC3
// API slice. It deliberately does not expose source paths or target credentials.
type Service interface {
	Games(context.Context) ([]catalog.Game, error)
	Search(context.Context, string) ([]catalog.Game, error)
	Game(context.Context, string) (catalog.Game, error)
	Health(context.Context) (protocol.Health, error)
	Status(context.Context) (protocol.Status, error)
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
}

type gamesResult struct {
	Games []gameResult `json:"games"`
}

type healthResult struct {
	Ready  bool         `json:"ready"`
	Target targetHealth `json:"target"`
}

type statusResult struct {
	State  protocol.State   `json:"state"`
	GameID *string          `json:"game_id,omitempty"`
	System *protocol.System `json:"system,omitempty"`
	Core   *string          `json:"core,omitempty"`
	Error  *apiError        `json:"error,omitempty"`
}

type targetHealth struct {
	Reachable bool `json:"reachable"`
	Ready     bool `json:"ready"`
}

type errorResult struct {
	Error apiError `json:"error"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func New(service Service) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		target, err := service.Health(r.Context())
		result := healthResult{Ready: true, Target: targetHealth{Reachable: err == nil, Ready: err == nil && target.Ready}}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("GET /api/v1/status", func(w http.ResponseWriter, r *http.Request) {
		status, err := service.Status(r.Context())
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "TARGET_UNAVAILABLE", "target status is unavailable")
			return
		}
		result := statusResult{State: status.State, GameID: status.GameID, System: status.System}
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
		query := strings.TrimSpace(r.URL.Query().Get("q"))
		var games []catalog.Game
		var err error
		if query == "" {
			games, err = service.Games(r.Context())
		} else {
			games, err = service.Search(r.Context(), query)
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL", "catalog is unavailable")
			return
		}
		sort.SliceStable(games, func(i, j int) bool {
			if strings.EqualFold(games[i].Title, games[j].Title) {
				return games[i].ID < games[j].ID
			}
			return strings.ToLower(games[i].Title) < strings.ToLower(games[j].Title)
		})
		result := gamesResult{Games: make([]gameResult, 0, len(games))}
		for _, game := range games {
			result.Games = append(result.Games, publicGame(game))
		}
		writeJSON(w, http.StatusOK, result)
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
		writeJSON(w, http.StatusOK, publicGame(game))
	})
	return noStore(rejectUnexpectedHost(mux))
}

func rejectUnexpectedHost(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

func publicErrorMessage(code protocol.ErrorCode) string {
	switch code {
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
	case protocol.CodeInvalidROMPath:
		return "target ROM path is invalid"
	case protocol.CodeSourceUnavailable:
		return "game source is unavailable"
	case protocol.CodeTransferFailed:
		return "content transfer failed"
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
	return gameResult{
		ID: game.ID, Title: game.Title, System: game.System, Kind: game.Kind,
		State: game.State, RootOnline: game.RootOnline, ContentPrepared: game.Content != nil,
		Execution: "fpga_native",
	}
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
