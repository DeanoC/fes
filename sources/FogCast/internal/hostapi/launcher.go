package hostapi

import (
	"bufio"
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/host"
	"github.com/DeanoC/FogCast/internal/discovery"
	"github.com/DeanoC/FogCast/kitlease"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

// LauncherPairing is one kit authorized on the launcher listener.
// A bearer identifies exactly one kit: a token appears once and a target
// id appears once.
type LauncherPairing struct {
	Token    string `json:"token"`
	TargetID string `json:"target_id"`
}

// LauncherConfig authorizes one or more paired kits. The secret is separate
// from the host-to-agent bearer and is never returned through the application
// API. When either top-level field is set, both are required and count as one
// pairing, ahead of Pairings.
type LauncherConfig struct {
	Token    string            `json:"token,omitempty"`
	TargetID string            `json:"target_id,omitempty"`
	Pairings []LauncherPairing `json:"pairings,omitempty"`
}

// ErrLauncherSharedToken rejects a launcher configuration in which one
// bearer is paired with more than one target_id. Session stop, status and
// input are owner-scoped per kit, so a shared bearer would let one kit act
// as another. The message names the fix and never includes the token.
var ErrLauncherSharedToken = errors.New("launcher pairing token is shared by more than one target_id; mint a separate per-kit bearer for each paired kit")

type launcherTokenGroup struct {
	token   string
	targets map[string]struct{}
}

func (c LauncherConfig) effectivePairings() ([]LauncherPairing, error) {
	var out []LauncherPairing
	if c.Token != "" || c.TargetID != "" {
		if c.Token == "" || c.TargetID == "" {
			return nil, errors.New("invalid launcher configuration")
		}
		out = append(out, LauncherPairing{Token: c.Token, TargetID: c.TargetID})
	}
	return append(out, c.Pairings...), nil
}

func validLauncherToken(token string) bool {
	if len(token) < 32 || len(token) > 256 || strings.TrimSpace(token) != token || strings.ContainsAny(token, "\r\n\t ") {
		return false
	}
	for _, b := range []byte(token) {
		if b < 33 || b > 126 {
			return false
		}
	}
	return true
}

func (c LauncherConfig) Validate() error {
	pairings, err := c.effectivePairings()
	if err != nil || len(pairings) == 0 || len(pairings) > 32 {
		return errors.New("invalid launcher configuration")
	}
	seen := make(map[string]struct{}, len(pairings))
	for _, pairing := range pairings {
		if !validLauncherToken(pairing.Token) || !discovery.ValidID(pairing.TargetID) {
			return errors.New("invalid launcher configuration")
		}
		if _, dup := seen[pairing.TargetID]; dup {
			return errors.New("invalid launcher configuration")
		}
		seen[pairing.TargetID] = struct{}{}
	}
	// Every pair is compared in constant time; the loop does not stop at
	// the first match.
	shared := 0
	for i := range pairings {
		for j := i + 1; j < len(pairings); j++ {
			shared |= subtle.ConstantTimeCompare([]byte(pairings[i].Token), []byte(pairings[j].Token))
		}
	}
	if shared == 1 {
		return ErrLauncherSharedToken
	}
	return nil
}

// tokenGroups buckets effective pairings by distinct token. Validate
// admits one target id per token, so each group names one kit.
func (c LauncherConfig) tokenGroups() ([]launcherTokenGroup, error) {
	pairings, err := c.effectivePairings()
	if err != nil {
		return nil, err
	}
	groups := make([]launcherTokenGroup, 0, len(pairings))
	index := make(map[string]int, len(pairings))
	for _, pairing := range pairings {
		slot, ok := index[pairing.Token]
		if !ok {
			slot = len(groups)
			index[pairing.Token] = slot
			groups = append(groups, launcherTokenGroup{token: pairing.Token, targets: map[string]struct{}{}})
		}
		groups[slot].targets[pairing.TargetID] = struct{}{}
	}
	return groups, nil
}

// UsesToken reports whether token equals any configured pairing token.
// Comparison does not stop at the first hit and does not echo the token.
// An empty token is never a pairing token.
func (c LauncherConfig) UsesToken(token string) bool {
	if token == "" {
		return false
	}
	matched := subtle.ConstantTimeCompare([]byte(c.Token), []byte(token))
	for i := range c.Pairings {
		matched |= subtle.ConstantTimeCompare([]byte(c.Pairings[i].Token), []byte(token))
	}
	return matched == 1
}

// matchLauncherToken compares the Authorization header with every distinct
// pairing token. It does not return on the first hit.
func matchLauncherToken(groups []launcherTokenGroup, authorization string) (map[string]struct{}, bool) {
	auth := []byte(authorization)
	var targets map[string]struct{}
	matched := 0
	for i := range groups {
		equal := subtle.ConstantTimeCompare(auth, []byte("Bearer "+groups[i].token))
		if equal == 1 {
			targets = groups[i].targets
		}
		matched |= equal
	}
	if matched != 1 {
		return nil, false
	}
	return targets, true
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
	groups, groupErr := config.tokenGroups()
	if !ok || config.Validate() != nil || groupErr != nil || len(groups) == 0 {
		return nil, errors.New("invalid launcher configuration")
	}
	return noStore(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targets, authorized := matchLauncherToken(groups, r.Header.Get("Authorization"))
		if !authorized {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "launcher authentication required")
			return
		}
		headerID := r.Header.Get("X-FogCast-Target-ID")
		// Content GETs are how a kit reads BIOS, media, and expansion
		// while another target stays selected. They keep a launcher
		// bearer and name an enabled configured kit. They do not require
		// that kit to be paired to the presented token or to be the
		// foreground selection. Every other launcher operation does.
		contentRead := launcherMeshContentRead(r.Method, r.URL.Path)
		if contentRead {
			// Ensure calls this listener while Launch holds targetMu. Content
			// reads name an enabled kit, independent of foreground selection,
			// so they must not wait for the foreground operation to finish.
			if !a.launcherMeshContentTarget(headerID, targets) {
				writeError(w, http.StatusForbidden, "TARGET_MISMATCH", "launcher target does not match the selected target")
				return
			}
			a.routes.ServeHTTP(w, r)
			return
		}
		if _, pairedToken := targets[headerID]; !pairedToken {
			writeError(w, http.StatusForbidden, "TARGET_MISMATCH", "launcher target does not match the selected target")
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/launcher/input" {
			a.launcherInput(w, r, headerID)
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/api/v1/launcher/kit-lease" {
			provider, ok := a.service.(interface {
				PairedTargetKitLeaseStatus(context.Context, string) (kitlease.Status, error)
			})
			if !ok {
				writeError(w, http.StatusServiceUnavailable, "TARGET_UNAVAILABLE", "paired target lease status is unavailable")
				return
			}
			status, err := provider.PairedTargetKitLeaseStatus(r.Context(), headerID)
			if err != nil {
				writeError(w, http.StatusServiceUnavailable, "TARGET_UNAVAILABLE", "paired target lease status is unavailable")
				return
			}
			writeJSON(w, http.StatusOK, status)
			return
		}
		// Catalogue read (GET /api/v1/games) does not take targetMu.
		// Other operations keep the guard: they can reconcile session
		// state or target settings, and Launch holds it while running.
		catalogueRead := r.Method == http.MethodGet && r.URL.Path == "/api/v1/games"
		if !catalogueRead {
			a.targetMu.Lock()
			defer a.targetMu.Unlock()
		}
		name, pairedKit := a.kitTarget(headerID)
		if !pairedKit {
			writeError(w, http.StatusForbidden, "TARGET_MISMATCH", "launcher target does not match the selected target")
			return
		}
		if !launcherOperation(r.Method, r.URL.Path) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "launcher operation is unavailable")
			return
		}
		if launcherPairedRead(r.Method, r.URL.Path) {
			a.routes.ServeHTTP(w, r)
			return
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/session":
			if headerID == a.sessionOwnerID() {
				a.routes.ServeHTTP(w, r)
				return
			}
			a.writeKitSession(w, name, headerID)
			return
		case "GET /api/v1/status", "GET /api/v1/session/input", "POST /api/v1/session/stop":
			if headerID != a.sessionOwnerID() {
				writeError(w, http.StatusForbidden, "TARGET_MISMATCH", "launcher target does not match the selected target")
				return
			}
			a.routes.ServeHTTP(w, r)
			return
		case "POST /api/v1/session/launch":
			// The host has one foreground session. Launching on this kit
			// while another kit has a play would move the foreground here:
			// that kit's input bridge and media are torn down, its core keeps
			// running under a held lease, and nothing can stop it until this
			// kit's play ends. Fail closed instead of preempting it.
			// TODO(#288): per-target foreground sessions and Stop.
			if a.otherKitPlaying(name, headerID) {
				writeError(w, http.StatusConflict, "SESSION_BUSY_OTHER_KIT", "another kit's session is active; stop it on that kit first")
				return
			}
			// No library name preserves the old fakes: the selected target
			// must be this kit, and the body is forwarded unchanged.
			if name == "" {
				if a.selectedTargetID() != headerID {
					writeError(w, http.StatusForbidden, "TARGET_MISMATCH", "launcher target does not match the selected target")
					return
				}
				a.routes.ServeHTTP(w, r)
				return
			}
			// An explicit target also skips mesh placement. That is intended:
			// the kit menu launches on the kit itself.
			if !rewriteLauncherLaunch(w, r, name) {
				return
			}
			a.routes.ServeHTTP(w, r)
			return
		default:
			writeError(w, http.StatusNotFound, "NOT_FOUND", "launcher operation is unavailable")
		}
	})), nil
}

func launcherMeshContentRead(method, path string) bool {
	return method == http.MethodGet && (path == "/api/v1/mesh/content/source" || path == "/api/v1/mesh/content/object")
}

// launcherMeshContentTarget admits a content GET from an enabled
// configured kit. The foreground selected target is not consulted.
// A service that does not publish a target list admits only an id
// paired to the matched token.
func (a *applicationHandler) launcherMeshContentTarget(headerID string, paired map[string]struct{}) bool {
	if headerID == "" {
		return false
	}
	provider, ok := a.service.(interface{ LibrarySettings() fogcast.LibraryConfig })
	if !ok {
		_, ok := paired[headerID]
		return ok
	}
	settings := provider.LibrarySettings()
	if len(settings.Targets) == 0 {
		_, ok := paired[headerID]
		return ok
	}
	for _, target := range settings.Targets {
		if target.Enabled && target.TargetID == headerID {
			return true
		}
	}
	return false
}

// launcherPairedRead is a catalogue or presentation GET any enabled paired
// kit may perform, whether or not that kit is the selected target.
func launcherPairedRead(method, path string) bool {
	if method != http.MethodGet {
		return false
	}
	switch path {
	case "/api/v1/games", "/api/v1/platforms", "/api/v1/health", "/api/v1/launcher/kit-lease", "/api/v1/library/attract", "/api/v1/library/cache", "/api/v1/library/collections", "/api/v1/library/facets":
		return true
	}
	return launcherArtworkPath(path) || launcherPresentationGamePath(path) || launcherGamePath(path)
}

func launcherGamePath(path string) bool {
	const prefix = "/api/v1/games/"
	return strings.HasPrefix(path, prefix) && protocol.ValidateGameID(path[len(prefix):]) == nil
}

// kitTarget resolves a paired target id to its configured name.
// An enabled library target matches on id. An empty target list does not.
// With no library settings, only the selected target id matches and the
// name stays empty so launch keeps the historical body.
func (a *applicationHandler) kitTarget(id string) (string, bool) {
	if id == "" {
		return "", false
	}
	provider, ok := a.service.(interface{ LibrarySettings() fogcast.LibraryConfig })
	if !ok {
		if id == a.selectedTargetID() {
			return "", true
		}
		return "", false
	}
	settings := provider.LibrarySettings()
	if len(settings.Targets) == 0 {
		return "", false
	}
	for _, target := range settings.Targets {
		if target.Enabled && target.TargetID == id {
			return target.Name, true
		}
	}
	return "", false
}

// sessionOwnerID is the foreground session target. A reported id wins.
// Otherwise the selected target owns the session.
func (a *applicationHandler) sessionOwnerID() string {
	if provider, ok := a.service.(interface{ SessionTarget() (string, string) }); ok {
		_, id := provider.SessionTarget()
		if id != "" {
			return id
		}
	}
	return a.selectedTargetID()
}

// otherKitPlaying reports whether a play belongs to a target other than
// the requesting kit. A play matches the kit by target id, or by name when
// the kit has one. A play with neither is treated as another kit's.
func (a *applicationHandler) otherKitPlaying(name, id string) bool {
	lister, ok := a.service.(interface{ PlaySessions() []fogcast.PlaySession })
	if !ok {
		return false
	}
	for _, play := range lister.PlaySessions() {
		if play.TargetID != "" && play.TargetID == id {
			continue
		}
		if play.TargetID == "" && name != "" && play.Target == name {
			continue
		}
		return true
	}
	return false
}

// writeKitSession reports one paired kit's own play. It does not read
// or mutate the foreground session.
func (a *applicationHandler) writeKitSession(w http.ResponseWriter, name, id string) {
	result := sessionResult{Target: name, TargetID: id, State: protocol.StateIdle}
	if lister, ok := a.service.(interface{ PlaySessions() []fogcast.PlaySession }); ok {
		for _, play := range lister.PlaySessions() {
			if play.Target != name {
				continue
			}
			result.State = protocol.StateActive
			result.Execution = play.Execution
			if play.GameID != "" {
				gameID := play.GameID
				result.GameID = &gameID
			}
			if play.System != "" {
				system := play.System
				result.System = &system
			}
			break
		}
	}
	writeJSON(w, http.StatusOK, result)
}

// rewriteLauncherLaunch pins POST /api/v1/session/launch to this kit.
// An explicit target also skips mesh placement. That is intended: the
// kit menu launches on the kit itself.
func rewriteLauncherLaunch(w http.ResponseWriter, r *http.Request, name string) bool {
	limited := http.MaxBytesReader(w, r.Body, 16<<10)
	decoder := json.NewDecoder(limited)
	var fields map[string]json.RawMessage
	if decoder.Decode(&fields) != nil || fields == nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "request body must contain one valid JSON object")
		return false
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "request body must contain one valid JSON object")
		return false
	}
	if raw, present := fields["target"]; present {
		var target string
		if json.Unmarshal(raw, &target) != nil || target != name {
			writeError(w, http.StatusForbidden, "TARGET_MISMATCH", "launcher target does not match the selected target")
			return false
		}
	}
	encoded, err := json.Marshal(name)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "request body must contain one valid JSON object")
		return false
	}
	fields["target"] = encoded
	body, err := json.Marshal(fields)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "request body must contain one valid JSON object")
		return false
	}
	_ = limited.Close()
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	return true
}

func launcherOperation(method, path string) bool {
	switch method + " " + path {
	case "GET /api/v1/games", "GET /api/v1/platforms", "GET /api/v1/health", "GET /api/v1/launcher/kit-lease", "GET /api/v1/status", "GET /api/v1/session", "GET /api/v1/session/input", "GET /api/v1/library/attract", "GET /api/v1/library/cache", "GET /api/v1/library/collections", "GET /api/v1/library/facets", "GET /api/v1/mesh/content/source", "GET /api/v1/mesh/content/object", "POST /api/v1/session/launch", "POST /api/v1/session/stop":
		return true
	}
	return method == http.MethodGet && (launcherArtworkPath(path) || launcherPresentationGamePath(path) || launcherGamePath(path))
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

func launcherPresentationGamePath(path string) bool {
	const prefix = "/api/v1/presentation/games/"
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	return protocol.ValidateGameID(path[len(prefix):]) == nil
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
	if _, ok := a.kitTarget(targetID); !ok || a.sessionOwnerID() != targetID {
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
		if errors.Is(err, host.ErrRemoteInputNoStream) {
			writeError(w, http.StatusServiceUnavailable, "INPUT_UNAVAILABLE", "no live input stream")
			return
		}
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
			if !launcherPlayHIDEvent(*packet.Event) {
				return
			}
			if source.SendEvent(r.Context(), *packet.Event, time.Now()) != nil {
				return
			}
		}
	}
}

func launcherPlayHIDEvent(e remoteinput.Event) bool {
	if e.Player > 1 || (e.Player != 0 && e.Device != remoteinput.DeviceGamepad) {
		return false
	}
	if launcherKeyboardEvent(e) {
		return true
	}
	return launcherGamepadEvent(e)
}

func launcherKeyboardEvent(e remoteinput.Event) bool {
	if e.Device != remoteinput.DeviceKeyboard || e.Kind != remoteinput.KindKey || e.Value != 0 {
		return false
	}
	return e.Action == remoteinput.ActionPress || e.Action == remoteinput.ActionRelease
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
	if e.Code >= remoteinput.Keypad0 && e.Code <= remoteinput.KeypadHash {
		return true
	}
	switch e.Code {
	case remoteinput.ButtonDPadUp, remoteinput.ButtonDPadDown, remoteinput.ButtonDPadLeft, remoteinput.ButtonDPadRight, remoteinput.ButtonA, remoteinput.ButtonB, remoteinput.ButtonC, remoteinput.ButtonX, remoteinput.ButtonY, remoteinput.ButtonL, remoteinput.ButtonR, remoteinput.ButtonStart, remoteinput.ButtonSelect:
		return true
	}
	return false
}
