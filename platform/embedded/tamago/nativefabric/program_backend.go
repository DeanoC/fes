// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
)

// ProgramEntry binds a UI identifier to a platform-selected retained request.
// Display names grant no compatibility, display, input or media capability.
type ProgramEntry struct {
	ID, Title string
	Request   packagestore.Request
}

type programPlay struct {
	ID         string `json:"id"`
	Game       string `json:"game_id"`
	Target     string `json:"target"`
	TargetID   string `json:"target_id"`
	PackageID  string `json:"package_id"`
	Generation uint64 `json:"generation"`
	Flight     string `json:"flight_id"`
}

// ProgramBackend is an in-process Tenfoot HTTP transport over the actual native
// ProgramService. The caller transfers exclusive frontend ownership of a Ready
// service and keeps it rooted until reboot. It is not a network listener or a
// normal package-admission profile. The service still preflights every load.
// No filesystem access is needed to read the cached catalog while SD is held.
type ProgramBackend struct {
	operation     sync.Mutex
	viewMu        sync.RWMutex
	wire          map[string]any
	service       *ProgramService
	entries       []ProgramEntry
	session       ProgramSession
	play          programPlay
	romLink       *hostclient.SessionROMLink
	keyboard      *programKeyboard
	inputAttached bool
	inputFault    error
	inputPolling  programInputPolling
	inputBoundary uint64
}

func NewProgramBackend(service *ProgramService, entries []ProgramEntry) (*ProgramBackend, error) {
	if service == nil || service.state == nil || len(entries) == 0 || len(entries) > 32 {
		return nil, errors.New("native program backend: ready service and bounded catalog required")
	}
	b := &ProgramBackend{service: service}
	seen := map[string]bool{}
	for _, entry := range entries {
		if protocol.ValidateGameID(entry.ID) != nil || seen[entry.ID] || entry.Title == "" || len(entry.Title) > 256 || !nonzeroDigest(entry.Request.PackageID) {
			return nil, errors.New("native program backend: invalid catalog entry")
		}
		seen[entry.ID] = true
		entry.Request.Sources = append([]packagestore.Source(nil), entry.Request.Sources...)
		entry.Request.Parts = append([]packagestore.Part(nil), entry.Request.Parts...)
		b.entries = append(b.entries, entry)
	}
	p := service.state
	p.operation.Lock()
	defer p.operation.Unlock()
	if service.Status().State != ProgramReady || p.backendClaimed {
		return nil, errors.New("native program backend: lifecycle already owned or unavailable")
	}
	p.backendClaimed = true
	b.publish()
	return b, nil
}

func programResponse(r *http.Request, status int, value any) (*http.Response, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return &http.Response{Request: r, StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, ContentLength: int64(len(data)), Body: io.NopCloser(bytes.NewReader(data))}, nil
}
func programError(r *http.Request, status int, code, message string) (*http.Response, error) {
	return programResponse(r, status, map[string]any{"error": map[string]any{"code": code, "message": message}})
}
func (b *ProgramBackend) snapshot() map[string]any {
	v := b.service.Status()
	state := "failed"
	switch v.State {
	case ProgramReady:
		state = "idle"
	case ProgramLoading:
		state = "launching"
	case ProgramReturning:
		state = "stopping"
	case ProgramRunning:
		state = "active"
	}
	wire := map[string]any{"state": state, "execution": "fpga_native", "media": "stopped"}
	if v.State == ProgramRunning {
		p := b.play
		// A caller violating exclusive ownership cannot publish a foreign play.
		if p.Generation != v.Generation || p.PackageID != v.Identity.PackageID {
			wire["state"] = "failed"
			return wire
		}
		wire["id"], wire["game_id"], wire["target"], wire["target_id"], wire["flight_id"] = p.ID, p.Game, p.Target, p.TargetID, p.Flight
		wire["system"], wire["media"] = "native", "active"
		wire["core_package"] = &hostclient.SessionCorePackage{CoreID: v.Identity.CoreID, PackageID: v.Identity.PackageID, Generation: v.Generation, ROMLink: b.romLink, ABI: hostclient.SessionCoreABI{ID: v.Identity.ABI, Major: v.Identity.Major, Minor: v.Identity.Minor}}
	}
	return wire
}

// Cached reads never contend with a long-running native mutation. The complete
// previous observation remains visible until the owned operation finishes.
func (b *ProgramBackend) publish() {
	wire := b.snapshot()
	b.viewMu.Lock()
	b.wire = wire
	b.viewMu.Unlock()
}
func (b *ProgramBackend) cached() map[string]any {
	b.viewMu.RLock()
	defer b.viewMu.RUnlock()
	return b.wire
}

// RoundTrip accepts catalog IDs only. Paths, package identities and generations
// used for programming come from the private catalog and ProgramService.
func (b *ProgramBackend) RoundTrip(r *http.Request) (*http.Response, error) {
	if r == nil || r.URL == nil {
		return nil, errors.New("native program backend: request required")
	}
	if r.Body != nil {
		defer r.Body.Close()
	}
	if err := r.Context().Err(); err != nil {
		return nil, err
	}
	path := r.URL.Path
	if r.Method == "GET" {
		switch path {
		case "/api/v1/session":
			return programResponse(r, 200, b.cached())
		case "/api/v1/games":
			games := make([]hostclient.Game, 0, len(b.entries))
			for _, e := range b.entries {
				games = append(games, hostclient.Game{ID: e.ID, Title: e.Title, System: "native", Execution: "fpga_native", State: "available", RootOnline: true, Launchable: true})
			}
			return programResponse(r, 200, map[string]any{"games": games})
		case "/api/v1/platforms":
			return programResponse(r, 200, map[string]any{"platforms": []any{}})
		case "/api/v1/library/collections":
			return programResponse(r, 200, map[string]any{"collections": []any{}})
		case "/api/v1/library/edition-preferences":
			return programResponse(r, 200, map[string]any{"preferences": []any{}})
		case "/api/v1/library/facets":
			return programResponse(r, 200, map[string]any{"genres": []any{}, "years": []any{}})
		}
	}
	if r.Method != "POST" || (path != "/api/v1/session/launch" && path != "/api/v1/session/stop") {
		return programError(r, 404, "UNSUPPORTED", "native operation unavailable")
	}
	if !b.operation.TryLock() {
		return programError(r, 409, "BUSY", "another native operation is active")
	}
	defer b.operation.Unlock()
	defer b.publish()
	var data []byte
	var err error
	if r.Body != nil {
		data, err = io.ReadAll(io.LimitReader(r.Body, 4097))
	}
	if err != nil {
		return nil, err
	}
	if len(data) > 4096 {
		return programError(r, 400, "INVALID_REQUEST", "request too large")
	}
	var body struct {
		Game        string       `json:"game_id"`
		Expected    *programPlay `json:"expected_session"`
		Retain      bool         `json:"retain_lease"`
		ReleaseIdle bool         `json:"release_idle"`
	}
	// Token validation rejects duplicate keys, including nested play identity.
	if len(bytes.TrimSpace(data)) > 0 {
		if !uniqueProgramJSON(data) {
			return programError(r, 400, "INVALID_REQUEST", "invalid request object")
		}
		d := json.NewDecoder(bytes.NewReader(data))
		d.DisallowUnknownFields()
		if d.Decode(&body) != nil {
			return programError(r, 400, "INVALID_REQUEST", "unknown or invalid request fields")
		}
	}
	v := b.service.Status()
	if path == "/api/v1/session/launch" {
		if b.inputFault != nil {
			return programError(r, 409, "INPUT_UNAVAILABLE", b.inputFault.Error())
		}
		if body.Expected != nil || body.Retain || body.ReleaseIdle || body.Game == "" {
			return programError(r, 400, "INVALID_REQUEST", "launch requires only a catalog ID")
		}
		if v.State != ProgramReady {
			return programError(r, 409, "NOT_READY", "native service is not ready")
		}
		var selected *ProgramEntry
		for i := range b.entries {
			if b.entries[i].ID == body.Game {
				selected = &b.entries[i]
				break
			}
		}
		if selected == nil {
			return programError(r, 404, "UNKNOWN_GAME", "catalog ID is unavailable")
		}
		err := b.withInputPaused(r.Context(), func() error {
			session, err := b.service.Launch(r.Context(), v.Generation, selected.Request)
			if err != nil {
				return err
			}
			b.session = session
			b.romLink = nil
			if b.service.Status().Identity.ActivationReceiptSHA256 != "" {
				// Launch retained this private immutable receipt. Do not rebuild
				// running ROM metadata from the mutable catalog or filesystem.
				link := b.service.state.plan.state.selection.Linked().Receipt().ROMLink
				b.romLink = &hostclient.SessionROMLink{ROMID: link.ROMID, Role: link.Role, SourceSHA256: link.SourceSHA256, SourceSize: link.SourceSize}
			}
			b.play = programPlay{ID: fmt.Sprintf("nano-%016x-%d", v.Owner, session.Generation()), Game: selected.ID, Target: "de25-nano", TargetID: fmt.Sprintf("%016x", v.Owner), PackageID: selected.Request.PackageID, Generation: session.Generation(), Flight: r.Header.Get("X-FogCast-Flight-Id")}
			return nil
		})
		if err != nil {
			return programError(r, 409, "NATIVE_LAUNCH_FAILED", err.Error())
		}
	} else {
		if body.Game != "" {
			return programError(r, 400, "INVALID_REQUEST", "stop does not select a game")
		}
		if body.Expected != nil && *body.Expected != b.play {
			return programError(r, 409, "STALE_SESSION", "displayed play has changed")
		}
		if body.ReleaseIdle {
			if v.State != ProgramReady || body.Expected != nil {
				return programError(r, 409, "NOT_IDLE", "native service is not idle")
			}
		} else if v.State == ProgramRunning {
			err := b.withInputPaused(r.Context(), func() error {
				if _, err := b.service.Return(r.Context(), b.session); err != nil {
					return err
				}
				b.session, b.play = ProgramSession{}, programPlay{}
				return nil
			})
			if err != nil {
				return programError(r, 409, "NATIVE_RETURN_FAILED", err.Error())
			}
		} else if v.State != ProgramReady {
			return programError(r, 409, "NOT_READY", "native service is not ready")
		}
	}
	return programResponse(r, 200, b.snapshot())
}

func uniqueProgramJSON(raw []byte) bool {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var object func() bool
	object = func() bool {
		token, err := d.Token()
		if err != nil || token != json.Delim('{') {
			return false
		}
		seen := map[string]bool{}
		for d.More() {
			key, err := d.Token()
			name, ok := key.(string)
			if err != nil || !ok || seen[name] {
				return false
			}
			seen[name] = true
			// Nested objects are only permitted for expected_session.
			if name == "expected_session" {
				if !object() {
					return false
				}
			} else {
				value, err := d.Token()
				if err != nil {
					return false
				}
				if _, compound := value.(json.Delim); compound {
					return false
				}
			}
		}
		end, err := d.Token()
		return err == nil && end == json.Delim('}')
	}
	if !object() {
		return false
	}
	_, err := d.Token()
	return err == io.EOF
}
