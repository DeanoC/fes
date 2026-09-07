package hostapi

import (
	"bufio"
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"github.com/DeanoC/FogCast/fogcast"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/DeanoC/FogCast/host"
	"github.com/DeanoC/FogCast/internal/discovery"
	"github.com/DeanoC/FogCast/remoteinput"
)

// LauncherConfig authorizes one paired kit; this secret is separate from the
// host-to-agent bearer. It is never returned through the application API.
type LauncherConfig struct {
	Token    string `json:"token"`
	TargetID string `json:"target_id"`
}

func (c LauncherConfig) Validate() error {
	if len(c.Token) < 32 || len(c.Token) > 256 || strings.TrimSpace(c.Token) != c.Token || strings.ContainsAny(c.Token, "\r\n\t ") || !discovery.ValidID(c.TargetID) {
		return errors.New("invalid launcher configuration")
	}
	for _, b := range []byte(c.Token) {
		if b < 33 || b > 126 {
			return errors.New("invalid launcher configuration")
		}
	}
	return nil
}

type applicationHandler struct {
	targetMu    sync.RWMutex
	browser     http.Handler
	routes      http.Handler
	service     Service
	remoteInput host.RemoteInputController
}

func (a *applicationHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/v1/library/settings" && r.Method != http.MethodGet && r.Method != http.MethodHead {
		// Settings updates already serialize their actual persistence in the
		// service. Preserve concurrent patch admission while excluding paired
		// launcher operations between their target check and execution.
		a.targetMu.RLock()
		defer a.targetMu.RUnlock()
	}
	a.browser.ServeHTTP(w, r)
}

// NewLauncherHandler uses the same session coordinator as the browser API,
// while allowing only the explicitly enumerated launcher operations.
func NewLauncherHandler(api http.Handler, config LauncherConfig) (http.Handler, error) {
	a, ok := api.(*applicationHandler)
	if !ok || config.Validate() != nil {
		return nil, errors.New("invalid launcher configuration")
	}
	return noStore(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+config.Token)) != 1 {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "launcher authentication required")
			return
		}
		if r.Header.Get("X-FogCast-Target-ID") != config.TargetID {
			writeError(w, http.StatusForbidden, "TARGET_MISMATCH", "launcher target does not match the selected target")
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/launcher/input" {
			a.launcherInput(w, r, config.TargetID)
			return
		}
		a.targetMu.Lock()
		defer a.targetMu.Unlock()
		if a.selectedTargetID() != config.TargetID {
			writeError(w, http.StatusForbidden, "TARGET_MISMATCH", "launcher target does not match the selected target")
			return
		}
		allowed := launcherOperation(r.Method, r.URL.Path)
		if !allowed {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "launcher operation is unavailable")
			return
		}
		a.routes.ServeHTTP(w, r)
	})), nil
}

func launcherOperation(method, path string) bool {
	switch method + " " + path {
	case "GET /api/v1/games", "GET /api/v1/platforms", "GET /api/v1/health", "GET /api/v1/status", "GET /api/v1/session", "GET /api/v1/session/input", "POST /api/v1/session/launch", "POST /api/v1/session/stop":
		return true
	}
	return method == http.MethodGet && launcherArtworkPath(path)
}

func launcherArtworkPath(path string) bool {
	const prefix = "/api/v1/presentation/artwork/"
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	handle := path[len(prefix):]
	if len(handle) != 64 {
		return false
	}
	for _, r := range handle {
		if r >= 'A' && r <= 'F' {
			r += 'a' - 'A'
		}
		if r < '0' || (r > '9' && r < 'a') || r > 'f' {
			return false
		}
	}
	return true
}

func (a *applicationHandler) selectedTargetID() string {
	if provider, ok := a.service.(interface{ LibrarySettings() fogcast.LibraryConfig }); ok {
		settings := provider.LibrarySettings()
		for _, target := range settings.Targets {
			if target.Name == settings.SelectedTarget && target.Enabled {
				return target.TargetID
			}
		}
		return ""
	}
	if connection := targetConnection(a.service); connection != nil {
		return connection.TargetID
	}
	return ""
}

func (a *applicationHandler) launcherInput(w http.ResponseWriter, r *http.Request, targetID string) {
	a.targetMu.Lock()
	if a.selectedTargetID() != targetID {
		a.targetMu.Unlock()
		writeError(w, http.StatusForbidden, "TARGET_MISMATCH", "launcher target does not match the selected target")
		return
	}
	provider, ok := a.remoteInput.(interface {
		ClaimSource(string) (host.RemoteInputEventSource, error)
	})
	if !ok {
		a.targetMu.Unlock()
		writeError(w, http.StatusServiceUnavailable, "INPUT_UNAVAILABLE", "remote input is unavailable")
		return
	}
	source, err := provider.ClaimSource(r.URL.Query().Get("session_id"))
	a.targetMu.Unlock()
	if err != nil {
		writeError(w, http.StatusConflict, "INPUT_BUSY", "input session is stale or already owned")
		return
	}
	defer source.Close()
	control := http.NewResponseController(w)
	if err = control.EnableFullDuplex(); err != nil {
		writeError(w, http.StatusInternalServerError, "INPUT_UNAVAILABLE", "streaming is unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Connection", "close")
	_ = control.SetWriteDeadline(time.Now().Add(time.Second))
	if err = json.NewEncoder(w).Encode(map[string]any{"ready": true, "session_id": r.URL.Query().Get("session_id")}); err != nil {
		return
	}
	if err = control.Flush(); err != nil {
		return
	}
	_ = control.SetWriteDeadline(time.Time{})
	reader := bufio.NewReaderSize(r.Body, 4096)
	// Streaming requests are never reused. Bound any body drain after an
	// invalid packet or timeout instead of clearing its read deadline.
	defer func() { _ = control.SetReadDeadline(time.Now()) }()
	for {
		if err = control.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
			return
		}
		line, readErr := reader.ReadSlice('\n')
		if readErr != nil {
			return
		}
		if source.Check() != nil {
			return
		}
		var packet struct {
			Event *remoteinput.Event `json:"event,omitempty"`
		}
		decoder := json.NewDecoder(bytes.NewReader(line))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&packet) != nil {
			return
		}
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			return
		}
		if packet.Event != nil {
			if !launcherGamepadEvent(*packet.Event) {
				return
			}
			if source.SendEvent(r.Context(), *packet.Event, time.Now()) != nil {
				return
			}
		}
	}
}

func launcherGamepadEvent(e remoteinput.Event) bool {
	if e.Device != remoteinput.DeviceGamepad {
		return false
	}
	if e.Kind == remoteinput.KindAxis {
		return e.Action == remoteinput.ActionAbsolute && (e.Code == remoteinput.AxisLeftX || e.Code == remoteinput.AxisLeftY) && e.Value >= -32768 && e.Value <= 32767
	}
	if e.Kind != remoteinput.KindButton || (e.Action != remoteinput.ActionPress && e.Action != remoteinput.ActionRelease) || e.Value != 0 {
		return false
	}
	switch e.Code {
	case remoteinput.ButtonDPadUp, remoteinput.ButtonDPadDown, remoteinput.ButtonDPadLeft, remoteinput.ButtonDPadRight, remoteinput.ButtonA, remoteinput.ButtonB, remoteinput.ButtonC, remoteinput.ButtonX, remoteinput.ButtonY, remoteinput.ButtonL, remoteinput.ButtonR, remoteinput.ButtonStart, remoteinput.ButtonSelect:
		return true
	}
	return false
}
