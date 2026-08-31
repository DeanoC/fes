package hostapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/host"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/protocol"
)

type fakeService struct {
	games           []catalog.Game
	search          []catalog.Game
	query           string
	game            catalog.Game
	gamesErr        error
	gameErr         error
	health          protocol.Health
	healthErr       error
	status          protocol.Status
	statusErr       error
	statusStarted   chan struct{}
	statusRelease   chan struct{}
	statusOnce      sync.Once
	launch          protocol.CachedLaunchResponse
	launchCalls     int
	launchErr       error
	launchHook      func(context.Context)
	development     protocol.Status
	developmentErr  error
	developmentBody []byte
	developmentSize int64
	stopped         protocol.Status
	stopErr         error
	stopResults     []error
	stopCalled      chan struct{}
	stopCtxErrs     []error
	stopHasDeadline bool
	progress        []string
	execution       string
	executionErr    error
	order           *[]string
}

func (s *fakeService) Games(context.Context) ([]catalog.Game, error) {
	return append([]catalog.Game(nil), s.games...), s.gamesErr
}
func (s *fakeService) Search(_ context.Context, query string) ([]catalog.Game, error) {
	s.query = query
	return append([]catalog.Game(nil), s.search...), s.gamesErr
}
func (s *fakeService) SessionExecution(context.Context, string) (string, error) {
	return s.execution, s.executionErr
}
func (s *fakeService) DevelopmentActive(context.Context) (bool, error) {
	return s.status.Development && s.status.State != protocol.StateIdle, s.statusErr
}
func (s *fakeService) Game(context.Context, string) (catalog.Game, error) { return s.game, s.gameErr }
func (s *fakeService) Health(context.Context) (protocol.Health, error)    { return s.health, s.healthErr }
func (s *fakeService) Status(ctx context.Context) (protocol.Status, error) {
	if s.statusStarted != nil {
		s.statusOnce.Do(func() { close(s.statusStarted) })
	}
	if s.statusRelease != nil {
		select {
		case <-s.statusRelease:
		case <-ctx.Done():
			return protocol.Status{}, ctx.Err()
		}
	}
	return s.status, s.statusErr
}
func (s *fakeService) Launch(ctx context.Context, _ string, progress fogcast.ProgressFunc) (protocol.CachedLaunchResponse, error) {
	s.launchCalls++
	if s.launchHook != nil {
		s.launchHook(ctx)
	}
	if progress != nil {
		progress(fogcast.Progress{Stage: "launch", Message: "launching"})
	}
	return s.launch, s.launchErr
}
func (s *fakeService) LoadDevelopmentRBF(_ context.Context, size int64, body io.Reader) (protocol.Status, error) {
	s.developmentSize = size
	content, err := io.ReadAll(body)
	if err != nil {
		return protocol.Status{}, err
	}
	s.developmentBody = content
	return s.development, s.developmentErr
}
func (s *fakeService) Stop(ctx context.Context) (protocol.Status, error) {
	s.stopCtxErrs = append(s.stopCtxErrs, ctx.Err())
	_, s.stopHasDeadline = ctx.Deadline()
	if s.order != nil {
		*s.order = append(*s.order, "service.stop")
	}
	if s.stopCalled != nil {
		select {
		case <-s.stopCalled:
		default:
			close(s.stopCalled)
		}
	}
	if len(s.stopResults) > 0 {
		err := s.stopResults[0]
		s.stopResults = s.stopResults[1:]
		return s.stopped, err
	}
	return s.stopped, s.stopErr
}

type fakeRemoteInput struct {
	status    host.RemoteInputStatus
	attach    []string
	detach    []string
	attachErr error
}

func (r *fakeRemoteInput) Attach(_ context.Context, core string) error {
	r.attach = append(r.attach, core)
	if r.attachErr != nil {
		r.status = host.RemoteInputStatus{State: host.RemoteInputFailed}
		return r.attachErr
	}
	r.status = host.RemoteInputStatus{State: host.RemoteInputAttached, Ready: true}
	return nil
}
func (r *fakeRemoteInput) Detach(_ context.Context, reason string) error {
	if r.status.State == host.RemoteInputAttached {
		r.detach = append(r.detach, reason)
	}
	r.status = host.RemoteInputStatus{State: host.RemoteInputDetached, Metrics: host.RemoteInputMetrics{ShutdownReason: reason}}
	return nil
}
func (r *fakeRemoteInput) Status() host.RemoteInputStatus { return r.status }

type fakeMediaSession struct {
	start          []string
	stop           []string
	order          *[]string
	err            error
	partialOnError bool
	stopErr        error
	stopResults    []error
	nilHandle      bool
	stopCtx        []context.Context
	stopErrs       []error
	stopDeadlines  []time.Time
	startCtx       []context.Context
	startCheck     func()
	done           chan struct{}
}

type fakeMediaHandle struct{ owner *fakeMediaSession }

type generationMediaSession struct {
	mu      sync.Mutex
	handles []*generationMediaHandle
}

type generationMediaHandle struct {
	mu    sync.Mutex
	stops int
}

type partialMediaSession struct {
	mu          sync.Mutex
	stopResults []error
	stops       int
	done        chan struct{}
}

type partialMediaHandle struct{ owner *partialMediaSession }

func (m *partialMediaSession) Start(context.Context, string) (hostapi.MediaHandle, error) {
	return &partialMediaHandle{owner: m}, errors.New("partial preview start failed")
}

func (h *partialMediaHandle) Stop(context.Context) error {
	h.owner.mu.Lock()
	defer h.owner.mu.Unlock()
	h.owner.stops++
	if len(h.owner.stopResults) == 0 {
		return nil
	}
	err := h.owner.stopResults[0]
	h.owner.stopResults = h.owner.stopResults[1:]
	return err
}

func (h *partialMediaHandle) Done() <-chan struct{} { return h.owner.done }

func (m *partialMediaSession) stopCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stops
}

func (m *generationMediaSession) Start(context.Context, string) (hostapi.MediaHandle, error) {
	handle := &generationMediaHandle{}
	m.mu.Lock()
	m.handles = append(m.handles, handle)
	m.mu.Unlock()
	return handle, nil
}

func (h *generationMediaHandle) Stop(context.Context) error {
	h.mu.Lock()
	h.stops++
	h.mu.Unlock()
	return nil
}

func (m *fakeMediaSession) Start(ctx context.Context, gameID string) (hostapi.MediaHandle, error) {
	if m.startCheck != nil {
		m.startCheck()
	}
	m.start = append(m.start, gameID)
	m.startCtx = append(m.startCtx, ctx)
	if m.order != nil {
		*m.order = append(*m.order, "start:"+gameID)
	}
	if m.err != nil {
		if !m.partialOnError {
			return nil, m.err
		}
		return &fakeMediaHandle{owner: m}, m.err
	}
	if m.nilHandle {
		return nil, nil
	}
	return &fakeMediaHandle{owner: m}, nil
}

func (h *fakeMediaHandle) Stop(ctx context.Context) error {
	h.owner.stop = append(h.owner.stop, "stopped")
	h.owner.stopCtx = append(h.owner.stopCtx, ctx)
	h.owner.stopErrs = append(h.owner.stopErrs, ctx.Err())
	deadline, _ := ctx.Deadline()
	h.owner.stopDeadlines = append(h.owner.stopDeadlines, deadline)
	if h.owner.order != nil {
		*h.owner.order = append(*h.owner.order, "stop")
	}
	if len(h.owner.stopResults) > 0 {
		err := h.owner.stopResults[0]
		h.owner.stopResults = h.owner.stopResults[1:]
		return err
	}
	return h.owner.stopErr
}

func (h *fakeMediaHandle) Done() <-chan struct{} { return h.owner.done }

func TestGamesReturnsStablePublicCatalogWithoutPrivatePathsOrDigests(t *testing.T) {
	service := &fakeService{games: []catalog.Game{{
		ID: "megadrive-sonic-test", Title: "Sonic", LibraryID: "private-root", RelativePath: "secret/Sonic.zip",
		System: protocol.SystemMegaDrive, Kind: catalog.SourceKindZIP, State: catalog.SourceStateAvailable, RootOnline: true,
		Content: &catalog.Content{SHA256: strings.Repeat("a", 64), Size: 123, Extension: "md"},
	}}}
	response := serve(t, hostapi.New(service), http.MethodGet, "/api/v1/games")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, private := range []string{"private-root", "secret/", strings.Repeat("a", 64), "relative_path", "sha256"} {
		if strings.Contains(body, private) {
			t.Fatalf("response leaked %q: %s", private, body)
		}
	}
	var result struct {
		Games []struct {
			ID              string              `json:"id"`
			Title           string              `json:"title"`
			System          protocol.System     `json:"system"`
			State           catalog.SourceState `json:"state"`
			RootOnline      bool                `json:"root_online"`
			ContentPrepared bool                `json:"content_prepared"`
		} `json:"games"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Games) != 1 || result.Games[0].ID != "megadrive-sonic-test" || !result.Games[0].ContentPrepared {
		t.Fatalf("games = %#v", result.Games)
	}
	assertJSONHeaders(t, response)
}

func TestSessionPreviewRouteUsesInjectedHostPictureHandler(t *testing.T) {
	service := &fakeService{}
	handler := hostapi.New(service, hostapi.WithMediaPreview(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=test-frame")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write([]byte("fixture-frame"))
	})))
	response := serve(t, handler, http.MethodGet, "/api/v1/session/preview")
	if response.Code != http.StatusOK || response.Body.String() != "fixture-frame" || response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("preview = %d headers=%#v body=%q", response.Code, response.Header(), response.Body.String())
	}
}

func TestGamesSupportsSearchAndExecutionCapability(t *testing.T) {
	service := &fakeService{search: []catalog.Game{{ID: "snes-mario", Title: "Mario", System: protocol.SystemSNES, Kind: catalog.SourceKindZIP, State: catalog.SourceStateAvailable}}}
	response := serve(t, hostapi.New(service), http.MethodGet, "/api/v1/games?q=mario")
	if response.Code != http.StatusOK || service.query != "mario" {
		t.Fatalf("status=%d query=%q body=%s", response.Code, service.query, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"execution":"fpga_native"`) {
		t.Fatalf("execution capability missing: %s", response.Body.String())
	}
}

func TestGameDetailUsesPathIDAndReturnsNotFound(t *testing.T) {
	service := &fakeService{gameErr: &protocol.APIError{Code: protocol.CodeROMNotFound, Message: "not found"}}
	response := serve(t, hostapi.New(service), http.MethodGet, "/api/v1/games/missing-game")
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Error.Code != "GAME_NOT_FOUND" {
		t.Fatalf("error = %#v", envelope.Error)
	}
}

func TestGameDetailMapsInternalFailureToJSON500(t *testing.T) {
	service := &fakeService{gameErr: &protocol.APIError{Code: protocol.CodeInternal, Message: "private failure"}}
	response := serve(t, hostapi.New(service), http.MethodGet, "/api/v1/games/known-game")
	if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "private failure") {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestSessionLaunchAndStopUseOnlyGameIDAndExposeProgress(t *testing.T) {
	// The launch response should contain the latest host-side progress and the event endpoint should expose it.
	gameID := "megadrive-sonic-test"
	system := protocol.SystemMegaDrive
	service := &fakeService{
		launch:  protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system}},
		stopped: protocol.Status{State: protocol.StateIdle},
	}
	handler := hostapi.New(service)
	launch := httptest.NewRequest(http.MethodPost, "/api/v1/session/launch", strings.NewReader(`{"game_id":"megadrive-sonic-test"}`))
	launch.Host = "127.0.0.1"
	launchResponse := httptest.NewRecorder()
	handler.ServeHTTP(launchResponse, launch)
	if launchResponse.Code != http.StatusOK {
		t.Fatalf("launch status = %d body=%s", launchResponse.Code, launchResponse.Body.String())
	}
	stop := httptest.NewRequest(http.MethodPost, "/api/v1/session/stop", nil)
	stop.Host = "127.0.0.1"
	stopResponse := httptest.NewRecorder()
	handler.ServeHTTP(stopResponse, stop)
	if stopResponse.Code != http.StatusOK || !strings.Contains(stopResponse.Body.String(), `"state":"idle"`) {
		t.Fatalf("stop response = %d %s", stopResponse.Code, stopResponse.Body.String())
	}
}

func TestSessionDevelopmentRBFStreamsWithoutMediaOrInput(t *testing.T) {
	payload := []byte("development-rbf")
	observed := "DEVCORE"
	service := &fakeService{
		development: protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &observed},
		stopped:     protocol.Status{State: protocol.StateIdle},
	}
	remoteInput := &fakeRemoteInput{status: host.RemoteInputStatus{State: host.RemoteInputAttached, Ready: true}}
	media := &fakeMediaSession{}
	handler := hostapi.New(service, hostapi.WithRemoteInput(remoteInput), hostapi.WithMediaSession(media))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/session/development-rbf", bytes.NewReader(payload))
	request.Host = "127.0.0.1"
	request.Header.Set("Content-Type", "application/octet-stream")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("development load response = %d %s", response.Code, response.Body.String())
	}
	if service.developmentSize != int64(len(payload)) || !bytes.Equal(service.developmentBody, payload) {
		t.Fatalf("development upload size = %d body = %q", service.developmentSize, service.developmentBody)
	}
	if len(remoteInput.detach) != 1 || remoteInput.detach[0] != "session_replace" {
		t.Fatalf("remote input detach = %v", remoteInput.detach)
	}
	if len(media.start) != 0 {
		t.Fatalf("media starts = %v", media.start)
	}
	var result struct {
		State     protocol.State `json:"state"`
		Execution string         `json:"execution"`
		GameID    *string        `json:"game_id"`
		System    *string        `json:"system"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.State != protocol.StateActive || result.Execution != fogcast.ExecutionFPGADevelopment || result.GameID != nil || result.System != nil {
		t.Fatalf("development session = %+v", result)
	}
	stop := httptest.NewRequest(http.MethodPost, "/api/v1/session/stop", nil)
	stop.Host = "127.0.0.1"
	stopResponse := httptest.NewRecorder()
	handler.ServeHTTP(stopResponse, stop)
	if stopResponse.Code != http.StatusOK {
		t.Fatalf("development stop = %d %s", stopResponse.Code, stopResponse.Body.String())
	}
	if service.stopHasDeadline {
		t.Fatal("development stop was forced through the bounded cleanup timeout")
	}
	service.status = protocol.Status{State: protocol.StateIdle}
	idle := serve(t, handler, http.MethodGet, "/api/v1/session")
	if idle.Code != http.StatusOK || strings.Contains(idle.Body.String(), "execution") {
		t.Fatalf("idle development session = %d %s", idle.Code, idle.Body.String())
	}
}

func TestSessionDevelopmentRBFMustStopBeforeCatalogLaunch(t *testing.T) {
	for _, execution := range []string{fogcast.ExecutionFPGANative, fogcast.ExecutionHostOnly} {
		t.Run(execution, func(t *testing.T) {
			observed := "DEVCORE"
			development := protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &observed}
			service := &fakeService{
				development: development,
				status:      development,
				execution:   execution,
				launch:      protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateActive}},
			}
			handler := hostapi.New(service)
			load := httptest.NewRequest(http.MethodPost, "/api/v1/session/development-rbf", strings.NewReader("rbf"))
			load.Host = "127.0.0.1"
			load.Header.Set("Content-Type", "application/octet-stream")
			loadResponse := httptest.NewRecorder()
			handler.ServeHTTP(loadResponse, load)
			if loadResponse.Code != http.StatusOK {
				t.Fatalf("development load = %d %s", loadResponse.Code, loadResponse.Body.String())
			}

			launch := httptest.NewRequest(http.MethodPost, "/api/v1/session/launch", strings.NewReader(`{"game_id":"replacement"}`))
			launch.Host = "127.0.0.1"
			launchResponse := httptest.NewRecorder()
			handler.ServeHTTP(launchResponse, launch)
			if launchResponse.Code != http.StatusConflict || service.launchCalls != 0 {
				t.Fatalf("replacement launch = %d %s calls=%d", launchResponse.Code, launchResponse.Body.String(), service.launchCalls)
			}
			status := serve(t, handler, http.MethodGet, "/api/v1/session")
			if !strings.Contains(status.Body.String(), `"execution":"fpga_development"`) {
				t.Fatalf("development ownership was lost: %s", status.Body.String())
			}
		})
	}
}

func TestSessionStopReconstructsDevelopmentAfterHostRestart(t *testing.T) {
	observed := "DEVCORE"
	service := &fakeService{
		status:  protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &observed},
		stopped: protocol.Status{State: protocol.StateIdle},
	}
	handler := hostapi.New(service)
	response := serve(t, handler, http.MethodPost, "/api/v1/session/stop")
	if response.Code != http.StatusOK || service.stopHasDeadline {
		t.Fatalf("restart recovery stop = %d %s deadline=%t", response.Code, response.Body.String(), service.stopHasDeadline)
	}
}

func TestSessionUnknownDevelopmentStateBlocksLaunchAfterHostRestart(t *testing.T) {
	service := &fakeService{
		statusErr: &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "target unavailable"},
		execution: fogcast.ExecutionHostOnly,
		launch:    protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateActive}},
	}
	handler := hostapi.New(service)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/session/launch", strings.NewReader(`{"game_id":"replacement"}`))
	request.Host = "127.0.0.1"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError || service.launchCalls != 0 || !strings.Contains(response.Body.String(), `"code":"MISTER_UNAVAILABLE"`) {
		t.Fatalf("unknown-state launch = %d %s calls=%d", response.Code, response.Body.String(), service.launchCalls)
	}
}

func TestSessionUnknownDevelopmentStateDoesNotUseBoundedStopAfterHostRestart(t *testing.T) {
	service := &fakeService{
		statusErr: &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "target unavailable"},
		stopped:   protocol.Status{State: protocol.StateIdle},
	}
	handler := hostapi.New(service)
	response := serve(t, handler, http.MethodPost, "/api/v1/session/stop")
	if response.Code != http.StatusInternalServerError || len(service.stopCtxErrs) != 0 || !strings.Contains(response.Body.String(), `"code":"MISTER_UNAVAILABLE"`) {
		t.Fatalf("unknown-state stop = %d %s stop calls=%d", response.Code, response.Body.String(), len(service.stopCtxErrs))
	}
}

func TestSessionDevelopmentRBFRejectsInvalidStreamMetadata(t *testing.T) {
	for _, test := range []struct {
		name    string
		content []byte
		setup   func(*http.Request)
	}{
		{name: "missing content type", content: []byte("rbf")},
		{name: "empty", content: nil, setup: func(request *http.Request) {
			request.Header.Set("Content-Type", "application/octet-stream")
		}},
		{name: "chunked", content: []byte("rbf"), setup: func(request *http.Request) {
			request.Header.Set("Content-Type", "application/octet-stream")
			request.TransferEncoding = []string{"chunked"}
			request.ContentLength = -1
		}},
		{name: "too large", content: []byte("rbf"), setup: func(request *http.Request) {
			request.Header.Set("Content-Type", "application/octet-stream")
			request.ContentLength = protocol.MaxDevelopmentRBFBytes + 1
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &fakeService{development: protocol.Status{State: protocol.StateActive, Development: true}}
			handler := hostapi.New(service)
			request := httptest.NewRequest(http.MethodPost, "/api/v1/session/development-rbf", bytes.NewReader(test.content))
			request.Host = "127.0.0.1"
			if test.setup != nil {
				test.setup(request)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest || service.developmentSize != 0 {
				t.Fatalf("response = %d %s development size = %d", response.Code, response.Body.String(), service.developmentSize)
			}
		})
	}
}

func TestFailedDevelopmentRBFLoadClearsStoppedHostOnlyOwnership(t *testing.T) {
	gameID := "host-game"
	service := &fakeService{
		execution:      fogcast.ExecutionHostOnly,
		launch:         protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateActive, GameID: &gameID}},
		status:         protocol.Status{State: protocol.StateIdle},
		developmentErr: &protocol.APIError{Code: protocol.CodeTransferFailed, Message: "upload failed"},
	}
	media := &fakeMediaSession{}
	handler := hostapi.New(service, hostapi.WithMediaSession(media))
	launchSession(t, handler, gameID)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/session/development-rbf", strings.NewReader("rbf"))
	request.Host = "127.0.0.1"
	request.Header.Set("Content-Type", "application/octet-stream")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code == http.StatusOK {
		t.Fatalf("development load unexpectedly succeeded: %s", response.Body.String())
	}
	status := serve(t, handler, http.MethodGet, "/api/v1/session")
	if strings.Contains(status.Body.String(), `"execution":"host_only"`) {
		t.Fatalf("failed replacement retained stopped host ownership: %s", status.Body.String())
	}
}

func TestSessionRejectsMalformedLaunchWithoutCallingService(t *testing.T) {
	handler := hostapi.New(&fakeService{})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/session/launch", strings.NewReader(`{"game_id":"../private"}`))
	request.Host = "127.0.0.1"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
}

func TestSessionOwnsRemoteInputAttachDetachAndStatusLifecycle(t *testing.T) {
	gameID := "snes-test"
	system := protocol.SystemSNES
	core := "SNES"
	service := &fakeService{
		launch:  protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system, ObservedCore: &core}},
		status:  protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system, ObservedCore: &core},
		stopped: protocol.Status{State: protocol.StateIdle},
	}
	input := &fakeRemoteInput{status: host.RemoteInputStatus{State: host.RemoteInputDetached}}
	handler := hostapi.New(service, hostapi.WithRemoteInput(input))

	launch := httptest.NewRequest(http.MethodPost, "/api/v1/session/launch", strings.NewReader(`{"game_id":"snes-test"}`))
	launch.Host = "127.0.0.1"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, launch)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"execution":"fpga_native"`) {
		t.Fatalf("launch = %d %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"input":{"state":"attached"`) {
		t.Fatalf("FPGA launch omitted attached input: %s", response.Body.String())
	}
	if len(input.attach) != 1 || input.attach[0] != core {
		t.Fatalf("FPGA launch attach calls = %#v", input.attach)
	}

	status := serve(t, handler, http.MethodGet, "/api/v1/session/input")
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"state":"attached"`) {
		t.Fatalf("input status = %d %s", status.Code, status.Body.String())
	}

	detach := httptest.NewRequest(http.MethodPost, "/api/v1/session/input/detach", nil)
	detach.Host = "127.0.0.1"
	detachResponse := httptest.NewRecorder()
	handler.ServeHTTP(detachResponse, detach)
	if detachResponse.Code != http.StatusOK || len(input.detach) != 1 || input.detach[0] != "operator_detach" {
		t.Fatalf("detach = %d %s calls=%#v", detachResponse.Code, detachResponse.Body.String(), input.detach)
	}

	attach := httptest.NewRequest(http.MethodPost, "/api/v1/session/input/attach", nil)
	attach.Host = "127.0.0.1"
	attachResponse := httptest.NewRecorder()
	handler.ServeHTTP(attachResponse, attach)
	if attachResponse.Code != http.StatusOK || len(input.attach) != 2 || input.attach[1] != core {
		t.Fatalf("explicit attach after detach = %d %s calls=%#v", attachResponse.Code, attachResponse.Body.String(), input.attach)
	}
}

func TestFPGANativeSessionStopDetachesRemoteInput(t *testing.T) {
	gameID := "actraiser"
	system := protocol.SystemSNES
	core := "SNES"
	service := &fakeService{
		launch:  protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system, ObservedCore: &core}},
		status:  protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system, ObservedCore: &core},
		stopped: protocol.Status{State: protocol.StateIdle},
	}
	input := &fakeRemoteInput{status: host.RemoteInputStatus{State: host.RemoteInputDetached}}
	handler := hostapi.New(service, hostapi.WithRemoteInput(input))

	launch := launchSession(t, handler, gameID)
	if launch.Code != http.StatusOK || len(input.attach) != 1 {
		t.Fatalf("launch = %d %s attach=%#v", launch.Code, launch.Body.String(), input.attach)
	}

	stop := serve(t, handler, http.MethodPost, "/api/v1/session/stop")
	if stop.Code != http.StatusOK || !strings.Contains(stop.Body.String(), `"state":"idle"`) {
		t.Fatalf("stop = %d %s", stop.Code, stop.Body.String())
	}
	if len(input.detach) != 1 || input.detach[0] != "session_stop" {
		t.Fatalf("stop detach calls = %#v", input.detach)
	}
	if strings.Contains(stop.Body.String(), `"state":"attached"`) {
		t.Fatalf("stop left input attached: %s", stop.Body.String())
	}
}

func TestFPGANativeLaunchAttachFailureStopsSession(t *testing.T) {
	gameID := "actraiser"
	system := protocol.SystemSNES
	core := "SNES"
	stopCalled := make(chan struct{})
	service := &fakeService{
		launch:     protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system, ObservedCore: &core}},
		stopped:    protocol.Status{State: protocol.StateIdle},
		stopCalled: stopCalled,
	}
	input := &fakeRemoteInput{
		status:    host.RemoteInputStatus{State: host.RemoteInputDetached},
		attachErr: errors.New("bridge failed"),
	}
	handler := hostapi.New(service, hostapi.WithRemoteInput(input))

	launch := launchSession(t, handler, gameID)
	if launch.Code == http.StatusOK {
		t.Fatalf("attach failure unexpectedly succeeded: %s", launch.Body.String())
	}
	if len(input.attach) != 1 || input.attach[0] != core {
		t.Fatalf("attach attempts = %#v", input.attach)
	}
	select {
	case <-stopCalled:
	default:
		t.Fatal("attach failure did not stop the FPGA session")
	}
}

func TestFPGANativeLaunchAttachFailureWhenCoreMissingStopsSession(t *testing.T) {
	gameID := "actraiser"
	system := protocol.SystemSNES
	stopCalled := make(chan struct{})
	service := &fakeService{
		launch:     protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system}},
		stopped:    protocol.Status{State: protocol.StateIdle},
		stopCalled: stopCalled,
	}
	input := &fakeRemoteInput{status: host.RemoteInputStatus{State: host.RemoteInputDetached}}
	handler := hostapi.New(service, hostapi.WithRemoteInput(input))

	launch := launchSession(t, handler, gameID)
	if launch.Code == http.StatusOK {
		t.Fatalf("missing-core launch unexpectedly succeeded: %s", launch.Body.String())
	}
	if len(input.attach) != 0 {
		t.Fatalf("missing-core launch attached: %#v", input.attach)
	}
	select {
	case <-stopCalled:
	default:
		t.Fatal("missing-core attach did not stop the FPGA session")
	}
}

func TestFPGANativeLaunchAttachFailureStopsAfterCanceledRequest(t *testing.T) {
	gameID := "actraiser"
	system := protocol.SystemSNES
	core := "SNES"
	service := &fakeService{
		launch:  protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system, ObservedCore: &core}},
		stopped: protocol.Status{State: protocol.StateIdle},
	}
	input := &fakeRemoteInput{
		status:    host.RemoteInputStatus{State: host.RemoteInputDetached},
		attachErr: errors.New("bridge failed"),
	}
	handler := hostapi.New(service, hostapi.WithRemoteInput(input))

	request := httptest.NewRequest(http.MethodPost, "/api/v1/session/launch", strings.NewReader(`{"game_id":"`+gameID+`"}`))
	request.Host = "127.0.0.1"
	ctx, cancel := context.WithCancel(request.Context())
	cancel()
	request = request.WithContext(ctx)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code == http.StatusOK {
		t.Fatalf("canceled attach failure unexpectedly succeeded: %s", response.Body.String())
	}
	if len(service.stopCtxErrs) == 0 {
		t.Fatal("attach failure did not stop the FPGA session")
	}
	for index, err := range service.stopCtxErrs {
		if err != nil {
			t.Fatalf("stop context %d was already done: %v", index, err)
		}
	}
}

func TestFPGANativeLaunchAttachFailureReportsBoundedStopError(t *testing.T) {
	gameID := "actraiser"
	system := protocol.SystemSNES
	core := "SNES"
	service := &fakeService{
		launch:  protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system, ObservedCore: &core}},
		stopped: protocol.Status{State: protocol.StateIdle},
		stopErr: errors.New("stop failed"),
	}
	input := &fakeRemoteInput{
		status:    host.RemoteInputStatus{State: host.RemoteInputDetached},
		attachErr: errors.New("bridge failed"),
	}
	handler := hostapi.New(service, hostapi.WithRemoteInput(input))

	launch := launchSession(t, handler, gameID)
	if launch.Code != http.StatusServiceUnavailable || !strings.Contains(launch.Body.String(), `"code":"TARGET_UNAVAILABLE"`) {
		t.Fatalf("cleanup failure = %d %s", launch.Code, launch.Body.String())
	}
	if strings.Contains(launch.Body.String(), `"code":"MISTER_UNAVAILABLE"`) {
		t.Fatalf("attach error hid the stop cleanup failure: %s", launch.Body.String())
	}
	if len(service.stopCtxErrs) == 0 {
		t.Fatal("failed cleanup did not attempt bounded stop")
	}
}

func TestFPGANativeLaunchSkipsAttachWhenRemoteInputDisabled(t *testing.T) {
	gameID := "actraiser"
	system := protocol.SystemSNES
	core := "SNES"
	service := &fakeService{
		launch:  protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system, ObservedCore: &core}},
		stopped: protocol.Status{State: protocol.StateIdle},
	}
	handler := hostapi.New(service)

	launch := launchSession(t, handler, gameID)
	if launch.Code != http.StatusOK || !strings.Contains(launch.Body.String(), `"execution":"fpga_native"`) {
		t.Fatalf("launch = %d %s", launch.Code, launch.Body.String())
	}
	if strings.Contains(launch.Body.String(), `"input":`) {
		t.Fatalf("disabled remote input leaked input status: %s", launch.Body.String())
	}
}

func TestFPGANativePlayStartsMediaAfterLaunchAndKeepsRemoteInputAttached(t *testing.T) {
	gameID := "actraiser"
	system := protocol.SystemSNES
	core := "SNES"
	service := &fakeService{
		launch:  protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system, ObservedCore: &core}},
		status:  protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system, ObservedCore: &core},
		stopped: protocol.Status{State: protocol.StateIdle},
	}
	input := &fakeRemoteInput{status: host.RemoteInputStatus{State: host.RemoteInputDetached}}
	media := &fakeMediaSession{startCheck: func() {
		if service.launchCalls != 1 {
			t.Fatalf("media started before FPGA launch: launch calls = %d", service.launchCalls)
		}
		if len(input.attach) != 0 {
			t.Fatalf("media started after remote input attach: %#v", input.attach)
		}
	}}
	handler := hostapi.New(service, hostapi.WithMediaSession(media), hostapi.WithRemoteInput(input))

	launch := launchSession(t, handler, gameID)
	if launch.Code != http.StatusOK || !strings.Contains(launch.Body.String(), `"execution":"fpga_native"`) || !strings.Contains(launch.Body.String(), `"media":"active"`) || !strings.Contains(launch.Body.String(), `"input":{"state":"attached"`) {
		t.Fatalf("launch = %d %s", launch.Code, launch.Body.String())
	}
	if len(media.start) != 1 || media.start[0] != gameID || len(input.attach) != 1 || input.attach[0] != core {
		t.Fatalf("media starts=%#v input attaches=%#v", media.start, input.attach)
	}

	stop := serve(t, handler, http.MethodPost, "/api/v1/session/stop")
	if stop.Code != http.StatusOK || !strings.Contains(stop.Body.String(), `"media":"stopped"`) {
		t.Fatalf("stop = %d %s", stop.Code, stop.Body.String())
	}
	if len(media.stop) != 1 || len(input.detach) != 1 || input.detach[0] != "session_stop" {
		t.Fatalf("media stops=%#v input detaches=%#v", media.stop, input.detach)
	}
}

func TestFPGANativePreviewStartFailureKeepsSessionAndPads(t *testing.T) {
	gameID := "actraiser"
	system := protocol.SystemSNES
	core := "SNES"
	service := &fakeService{
		launch:  protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system, ObservedCore: &core}},
		status:  protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system, ObservedCore: &core},
		stopped: protocol.Status{State: protocol.StateIdle},
	}
	input := &fakeRemoteInput{status: host.RemoteInputStatus{State: host.RemoteInputDetached}}
	media := &fakeMediaSession{err: errors.New("capture device is busy")}
	handler := hostapi.New(service, hostapi.WithMediaSession(media), hostapi.WithRemoteInput(input))

	launch := launchSession(t, handler, gameID)
	if launch.Code != http.StatusOK || !strings.Contains(launch.Body.String(), `"execution":"fpga_native"`) || !strings.Contains(launch.Body.String(), `"media":"failed"`) || !strings.Contains(launch.Body.String(), `"input":{"state":"attached"`) {
		t.Fatalf("launch = %d %s", launch.Code, launch.Body.String())
	}
	if len(service.stopCtxErrs) != 0 {
		t.Fatalf("preview start failure stopped the FPGA session: stops=%d", len(service.stopCtxErrs))
	}
	if len(input.attach) != 1 || input.attach[0] != core || len(input.detach) != 0 {
		t.Fatalf("pads = attach %#v detach %#v", input.attach, input.detach)
	}

	again := launchSession(t, handler, gameID)
	if again.Code != http.StatusOK || !strings.Contains(again.Body.String(), `"state":"active"`) {
		t.Fatalf("second launch = %d %s", again.Code, again.Body.String())
	}
	if strings.Contains(again.Body.String(), `"code":"TARGET_UNAVAILABLE"`) {
		t.Fatalf("second launch lost the target: %s", again.Body.String())
	}
}

func TestFPGANativePreviewStartFailureStopClearsFailedMedia(t *testing.T) {
	gameID := "actraiser"
	system := protocol.SystemSNES
	core := "SNES"
	service := &fakeService{
		launch:  protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system, ObservedCore: &core}},
		status:  protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system, ObservedCore: &core},
		stopped: protocol.Status{State: protocol.StateIdle},
	}
	input := &fakeRemoteInput{status: host.RemoteInputStatus{State: host.RemoteInputDetached}}
	media := &fakeMediaSession{err: errors.New("capture device is busy")}
	handler := hostapi.New(service, hostapi.WithMediaSession(media), hostapi.WithRemoteInput(input))

	launch := launchSession(t, handler, gameID)
	if launch.Code != http.StatusOK || !strings.Contains(launch.Body.String(), `"media":"failed"`) || !strings.Contains(launch.Body.String(), `"input":{"state":"attached"`) {
		t.Fatalf("launch = %d %s", launch.Code, launch.Body.String())
	}
	status := serve(t, handler, http.MethodGet, "/api/v1/session")
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"state":"active"`) || !strings.Contains(status.Body.String(), `"media":"failed"`) {
		t.Fatalf("status after failed preview = %d %s", status.Code, status.Body.String())
	}
	if len(service.stopCtxErrs) != 0 || len(input.detach) != 0 {
		t.Fatalf("failed preview stopped session or pads: service stops=%d input detaches=%#v", len(service.stopCtxErrs), input.detach)
	}

	stop := serve(t, handler, http.MethodPost, "/api/v1/session/stop")
	if stop.Code != http.StatusOK || strings.Contains(stop.Body.String(), `"media":"failed"`) || !strings.Contains(stop.Body.String(), `"media":"stopped"`) {
		t.Fatalf("stop = %d %s", stop.Code, stop.Body.String())
	}
	if len(media.stop) != 0 {
		t.Fatalf("handle-less failed preview called media stop: %#v", media.stop)
	}
	if len(service.stopCtxErrs) == 0 {
		t.Fatalf("stop did not stop FPGA session")
	}
	if len(input.detach) != 1 || input.detach[0] != "session_stop" {
		t.Fatalf("stop pads = %#v", input.detach)
	}

	service.status = protocol.Status{State: protocol.StateIdle}
	idle := serve(t, handler, http.MethodGet, "/api/v1/session")
	if idle.Code != http.StatusOK || !strings.Contains(idle.Body.String(), `"state":"idle"`) || strings.Contains(idle.Body.String(), `"media":"failed"`) {
		t.Fatalf("idle status after stop kept failed media: %d %s", idle.Code, idle.Body.String())
	}
}

func TestFPGANativePreviewStartFailureReapsPartialHandle(t *testing.T) {
	gameID := "actraiser"
	system := protocol.SystemSNES
	core := "SNES"
	service := &fakeService{
		launch: protocol.CachedLaunchResponse{Status: protocol.Status{
			State: protocol.StateActive, GameID: &gameID, System: &system, ObservedCore: &core,
		}},
		status: protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system, ObservedCore: &core},
	}
	input := &fakeRemoteInput{status: host.RemoteInputStatus{State: host.RemoteInputDetached}}
	media := &partialMediaSession{
		stopResults: []error{errors.New("first cleanup failed"), errors.New("retry cleanup failed"), nil},
		done:        make(chan struct{}),
	}
	handler := hostapi.New(service, hostapi.WithMediaSession(media), hostapi.WithRemoteInput(input))

	launch := launchSession(t, handler, gameID)
	if launch.Code != http.StatusOK || !strings.Contains(launch.Body.String(), `"state":"active"`) || !strings.Contains(launch.Body.String(), `"media":"failed"`) {
		t.Fatalf("launch = %d %s", launch.Code, launch.Body.String())
	}
	if got := media.stopCount(); got != 2 {
		t.Fatalf("partial preview cleanup attempts = %d, want 2", got)
	}
	if len(service.stopCtxErrs) != 0 || len(input.detach) != 0 {
		t.Fatalf("preview cleanup stopped session or pads: service stops=%d input detaches=%#v", len(service.stopCtxErrs), input.detach)
	}

	close(media.done)
	deadline := time.Now().Add(time.Second)
	for media.stopCount() != 3 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := media.stopCount(); got != 3 {
		t.Fatalf("terminal partial preview cleanup attempts = %d, want 3", got)
	}
	status := serve(t, handler, http.MethodGet, "/api/v1/session")
	deadline = time.Now().Add(time.Second)
	for !strings.Contains(status.Body.String(), `"media":"stopped"`) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
		status = serve(t, handler, http.MethodGet, "/api/v1/session")
	}
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"state":"active"`) || !strings.Contains(status.Body.String(), `"media":"stopped"`) || !strings.Contains(status.Body.String(), `"input":{"state":"attached"`) {
		t.Fatalf("status after terminal preview cleanup = %d %s", status.Code, status.Body.String())
	}
	if len(service.stopCtxErrs) != 0 || len(input.detach) != 0 {
		t.Fatalf("terminal preview cleanup stopped session or pads: service stops=%d input detaches=%#v", len(service.stopCtxErrs), input.detach)
	}
}

func TestFPGANativePreviewExitKeepsSessionAndPads(t *testing.T) {
	gameID := "actraiser"
	system := protocol.SystemSNES
	core := "SNES"
	service := &fakeService{
		launch:  protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system, ObservedCore: &core}},
		status:  protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system, ObservedCore: &core},
		stopped: protocol.Status{State: protocol.StateIdle},
	}
	input := &fakeRemoteInput{status: host.RemoteInputStatus{State: host.RemoteInputDetached}}
	done := make(chan struct{})
	media := &fakeMediaSession{done: done}
	handler := hostapi.New(service, hostapi.WithMediaSession(media), hostapi.WithRemoteInput(input))
	launch := launchSession(t, handler, gameID)
	if launch.Code != http.StatusOK || !strings.Contains(launch.Body.String(), `"media":"active"`) {
		t.Fatalf("launch = %d %s", launch.Code, launch.Body.String())
	}
	close(done)

	status := serve(t, handler, http.MethodGet, "/api/v1/session")
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"state":"active"`) || !strings.Contains(status.Body.String(), `"media":"stopped"`) || !strings.Contains(status.Body.String(), `"input":{"state":"attached"`) {
		t.Fatalf("status after preview exit = %d %s", status.Code, status.Body.String())
	}
	if len(service.stopCtxErrs) != 0 {
		t.Fatalf("preview exit stopped the FPGA session: stops=%d", len(service.stopCtxErrs))
	}
	if len(input.detach) != 0 {
		t.Fatalf("preview exit detached pads: %#v", input.detach)
	}
	if len(media.stop) != 1 {
		t.Fatalf("preview exit did not stop media only: %#v", media.stop)
	}
}

func TestFPGANativeInputAttachFailureStopsStartedMedia(t *testing.T) {
	gameID := "actraiser"
	system := protocol.SystemSNES
	core := "SNES"
	service := &fakeService{
		launch:  protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system, ObservedCore: &core}},
		stopped: protocol.Status{State: protocol.StateIdle},
	}
	input := &fakeRemoteInput{status: host.RemoteInputStatus{State: host.RemoteInputDetached}, attachErr: errors.New("bridge failed")}
	media := &fakeMediaSession{}
	handler := hostapi.New(service, hostapi.WithMediaSession(media), hostapi.WithRemoteInput(input))

	launch := launchSession(t, handler, gameID)
	if launch.Code == http.StatusOK {
		t.Fatalf("attach failure unexpectedly succeeded: %s", launch.Body.String())
	}
	if len(media.start) != 1 || len(media.stop) != 1 || len(service.stopCtxErrs) == 0 {
		t.Fatalf("starts=%#v stops=%#v service stops=%d", media.start, media.stop, len(service.stopCtxErrs))
	}
}

func TestHostOnlySessionOwnsMediaLifecycleAndPublishesSafeEvents(t *testing.T) {
	gameID := "host-game"
	system := protocol.SystemSNES
	service := &fakeService{
		execution: "host_only",
		launch:    protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system}},
		stopped:   protocol.Status{State: protocol.StateIdle},
	}
	media := &fakeMediaSession{}
	input := &fakeRemoteInput{status: host.RemoteInputStatus{State: host.RemoteInputDetached}}
	handler := hostapi.New(service, hostapi.WithMediaSession(media), hostapi.WithRemoteInput(input))

	launch := httptest.NewRequest(http.MethodPost, "/api/v1/session/launch", strings.NewReader(`{"game_id":"host-game"}`))
	launch.Host = "127.0.0.1"
	launchResponse := httptest.NewRecorder()
	handler.ServeHTTP(launchResponse, launch)
	if launchResponse.Code != http.StatusOK || len(media.start) != 1 || media.start[0] != gameID {
		t.Fatalf("launch = %d %s, media starts=%#v", launchResponse.Code, launchResponse.Body.String(), media.start)
	}
	if !strings.Contains(launchResponse.Body.String(), `"execution":"host_only"`) || !strings.Contains(launchResponse.Body.String(), `"media":"active"`) {
		t.Fatalf("launch omitted public media state: %s", launchResponse.Body.String())
	}
	if len(input.attach) != 0 {
		t.Fatalf("host-only launch attached target remote input: %#v", input.attach)
	}

	stop := httptest.NewRequest(http.MethodPost, "/api/v1/session/stop", nil)
	stop.Host = "127.0.0.1"
	stopResponse := httptest.NewRecorder()
	handler.ServeHTTP(stopResponse, stop)
	if stopResponse.Code != http.StatusOK || len(media.stop) != 1 || !strings.Contains(stopResponse.Body.String(), `"media":"stopped"`) {
		t.Fatalf("stop = %d %s, media stops=%#v", stopResponse.Code, stopResponse.Body.String(), media.stop)
	}

	events := serve(t, handler, http.MethodGet, "/api/v1/session/events")
	body := events.Body.String()
	if !strings.Contains(body, `"event":"session.media.start"`) || !strings.Contains(body, `"event":"session.media.stop"`) {
		t.Fatalf("media lifecycle events missing: %s", body)
	}
	for _, secret := range []string{"/private", "Bearer", "sha256"} {
		if strings.Contains(body, secret) {
			t.Fatalf("event leaked %q: %s", secret, body)
		}
	}
}

func TestHostOnlyStopTearsDownMediaBeforeSessionService(t *testing.T) {
	gameID := "host-game"
	order := []string{}
	service := &fakeService{
		execution: "host_only",
		launch:    protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateActive, GameID: &gameID}},
		stopped:   protocol.Status{State: protocol.StateIdle},
		order:     &order,
	}
	media := &fakeMediaSession{order: &order}
	handler := hostapi.New(service, hostapi.WithMediaSession(media))

	launchSession(t, handler, gameID)
	serve(t, handler, http.MethodPost, "/api/v1/session/stop")

	if got, want := strings.Join(order, ","), "start:host-game,stop,service.stop"; got != want {
		t.Fatalf("stop order = %q, want %q", got, want)
	}
}

func TestHostOnlyStatusReapsUnexpectedMediaExit(t *testing.T) {
	gameID := "host-game"
	order := []string{}
	service := &fakeService{
		execution: "host_only",
		launch:    protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateActive, GameID: &gameID}},
		stopped:   protocol.Status{State: protocol.StateIdle},
		order:     &order,
	}
	done := make(chan struct{})
	media := &fakeMediaSession{order: &order, done: done}
	handler := hostapi.New(service, hostapi.WithMediaSession(media))
	launchSession(t, handler, gameID)
	close(done)

	status := serve(t, handler, http.MethodGet, "/api/v1/session")
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"state":"idle"`) || !strings.Contains(status.Body.String(), `"media":"stopped"`) {
		t.Fatalf("status after media exit = %d %s", status.Code, status.Body.String())
	}
	if got, want := strings.Join(order, ","), "start:host-game,stop,service.stop"; got != want {
		t.Fatalf("reap order = %q, want %q", got, want)
	}
}

func TestHostOnlyLaunchFailureStopsMedia(t *testing.T) {
	service := &fakeService{execution: "host_only", launchErr: &protocol.APIError{Code: protocol.CodeInternal, Message: "/private/secret"}}
	media := &fakeMediaSession{}
	handler := hostapi.New(service, hostapi.WithMediaSession(media))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/session/launch", strings.NewReader(`{"game_id":"host-game"}`))
	request.Host = "127.0.0.1"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError || len(media.start) != 1 || len(media.stop) != 1 {
		t.Fatalf("launch failure = %d %s, starts=%#v stops=%#v", response.Code, response.Body.String(), media.start, media.stop)
	}
	events := serve(t, handler, http.MethodGet, "/api/v1/session/events")
	if !strings.Contains(events.Body.String(), `"event":"session.media.stop"`) || strings.Contains(events.Body.String(), "/private/secret") {
		t.Fatalf("failure events = %s", events.Body.String())
	}
	service.status = protocol.Status{State: protocol.StateIdle}
	status := serve(t, handler, http.MethodGet, "/api/v1/session")
	if strings.Contains(status.Body.String(), `"execution"`) {
		t.Fatalf("failed launch retained replacement ownership: %s", status.Body.String())
	}
}

func TestHostOnlyReplacementStopsExistingMediaBeforeStartingNext(t *testing.T) {
	gameID := "first"
	service := &fakeService{execution: "host_only", launch: protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateActive, GameID: &gameID}}}
	order := []string{}
	media := &fakeMediaSession{order: &order}
	handler := hostapi.New(service, hostapi.WithMediaSession(media))
	launchSession(t, handler, "first")
	launchSession(t, handler, "second")
	if got, want := strings.Join(order, ","), "start:first,stop,start:second"; got != want {
		t.Fatalf("media order = %q, want %q", got, want)
	}
}

func TestHostOnlyNonActiveLaunchCleansUpMedia(t *testing.T) {
	service := &fakeService{execution: "host_only", launch: protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateIdle}}}
	media := &fakeMediaSession{}
	handler := hostapi.New(service, hostapi.WithMediaSession(media))
	response := launchSession(t, handler, "host-game")
	if response.Code != http.StatusOK || len(media.stop) != 1 || strings.Contains(response.Body.String(), `"media":"active"`) {
		t.Fatalf("launch = %d %s, stops=%#v", response.Code, response.Body.String(), media.stop)
	}
}

func TestHostOnlyCleanupUsesBoundedContextAfterRequestCancellation(t *testing.T) {
	service := &fakeService{execution: "host_only", launch: protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateActive}}}
	media := &fakeMediaSession{}
	handler := hostapi.New(service, hostapi.WithMediaSession(media))
	launchSession(t, handler, "first")
	service.launchErr = errors.New("launch failed")
	request := httptest.NewRequest(http.MethodPost, "/api/v1/session/launch", strings.NewReader(`{"game_id":"second"}`))
	request.Host = "127.0.0.1"
	ctx, cancel := context.WithCancel(request.Context())
	request = request.WithContext(ctx)
	service.launchHook = func(context.Context) { cancel() }
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if len(media.stopCtx) != 2 {
		t.Fatalf("stop contexts = %d, want replacement and cleanup", len(media.stopCtx))
	}
	if media.stopErrs[1] != nil {
		t.Fatalf("cleanup context was canceled during Stop: %v", media.stopErrs[1])
	}
	if media.stopDeadlines[1].IsZero() || time.Until(media.stopDeadlines[1]) <= 0 {
		t.Fatalf("cleanup context has no live bounded deadline")
	}
}

func TestSessionStatusPreservesHostOnlyExecutionAndMediaState(t *testing.T) {
	gameID := "host-game"
	service := &fakeService{execution: "host_only", launch: protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateActive, GameID: &gameID}}}
	media := &fakeMediaSession{}
	handler := hostapi.New(service, hostapi.WithMediaSession(media))
	launchSession(t, handler, gameID)
	status := serve(t, handler, http.MethodGet, "/api/v1/session")
	if !strings.Contains(status.Body.String(), `"execution":"host_only"`) || !strings.Contains(status.Body.String(), `"media":"active"`) {
		t.Fatalf("status = %s", status.Body.String())
	}
}

func TestUnexpectedHostExitStopsMediaAndRecordsSanitizedSessionExit(t *testing.T) {
	gameID := "host-game"
	service := &fakeService{
		execution: "host_only",
		// Launch succeeded, but the next service status observes the host process as idle.
		launch: protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateActive, GameID: &gameID}},
		status: protocol.Status{State: protocol.StateIdle},
	}
	media := &fakeMediaSession{}
	handler := hostapi.New(service, hostapi.WithMediaSession(media))
	launchSession(t, handler, gameID)

	status := serve(t, handler, http.MethodGet, "/api/v1/session")
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"state":"idle"`) || !strings.Contains(status.Body.String(), `"media":"stopped"`) {
		t.Fatalf("unexpected-exit status = %d %s", status.Code, status.Body.String())
	}
	if len(media.stop) != 1 {
		t.Fatalf("media stops = %#v, want one teardown", media.stop)
	}
	events := serve(t, handler, http.MethodGet, "/api/v1/session/events")
	body := events.Body.String()
	if !strings.Contains(body, `"event":"session.exit"`) || !strings.Contains(body, `"state":"idle"`) || !strings.Contains(body, `"media":"stopped"`) {
		t.Fatalf("exit event missing: %s", body)
	}
	for _, secret := range []string{"/private", "Bearer", "sha256", "path"} {
		if strings.Contains(body, secret) {
			t.Fatalf("exit event leaked %q: %s", secret, body)
		}
	}
}

func TestMediaTerminationAutonomouslyStopsHostSessionWithoutStatusRequest(t *testing.T) {
	gameID := "host-game"
	stopCalled := make(chan struct{})
	service := &fakeService{
		execution:  "host_only",
		launch:     protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateActive, GameID: &gameID}},
		stopped:    protocol.Status{State: protocol.StateIdle},
		stopCalled: stopCalled,
	}
	media := &fakeMediaSession{done: make(chan struct{})}
	handler := hostapi.New(service, hostapi.WithMediaSession(media))
	launchSession(t, handler, gameID)
	close(media.done)
	select {
	case <-stopCalled:
	case <-time.After(time.Second):
		t.Fatal("media termination did not autonomously stop the host session")
	}
	if len(media.stop) != 1 {
		t.Fatalf("media stops = %#v, want one autonomous teardown", media.stop)
	}
}

func TestNilMediaHandleDoesNotPanicOrClaimActive(t *testing.T) {
	service := &fakeService{execution: "host_only", launch: protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateActive}}}
	media := &fakeMediaSession{nilHandle: true}
	handler := hostapi.New(service, hostapi.WithMediaSession(media))
	response := launchSession(t, handler, "host-game")
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), `"media":"active"`) {
		t.Fatalf("launch = %d %s", response.Code, response.Body.String())
	}
}

func TestMediaStopFailureDoesNotClaimStopped(t *testing.T) {
	order := []string{}
	service := &fakeService{execution: "host_only", launch: protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateActive}}, order: &order}
	media := &fakeMediaSession{stopErr: errors.New("stop failed")}
	handler := hostapi.New(service, hostapi.WithMediaSession(media))
	launchSession(t, handler, "host-game")
	response := serve(t, handler, http.MethodPost, "/api/v1/session/stop")
	if response.Code == http.StatusOK || strings.Contains(response.Body.String(), `"media":"stopped"`) {
		t.Fatalf("stop claimed success: %d %s", response.Code, response.Body.String())
	}
	status := serve(t, handler, http.MethodGet, "/api/v1/session")
	if !strings.Contains(status.Body.String(), `"media":"active"`) {
		t.Fatalf("status lost active media after failed stop: %s", status.Body.String())
	}
	if !slices.Contains(order, "service.stop") {
		t.Fatalf("media failure short-circuited host stop: %#v", order)
	}
}

func TestTransientMediaStopFailureRetriesBeforeReturning(t *testing.T) {
	order := []string{}
	service := &fakeService{
		execution: "host_only",
		launch:    protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateActive}},
		stopped:   protocol.Status{State: protocol.StateIdle},
		order:     &order,
	}
	media := &fakeMediaSession{stopResults: []error{errors.New("transient media stop failure"), nil}, order: &order}
	handler := hostapi.New(service, hostapi.WithMediaSession(media))
	launchSession(t, handler, "host-game")
	response := serve(t, handler, http.MethodPost, "/api/v1/session/stop")
	if response.Code == http.StatusOK {
		t.Fatalf("stop hid first cleanup failure: %s", response.Body.String())
	}
	if len(media.stop) != 2 {
		t.Fatalf("media stop attempts = %d, want failed attempt plus retry", len(media.stop))
	}
	if !slices.Contains(order, "service.stop") {
		t.Fatalf("media retry prevented independent service stop: %#v", order)
	}
}

func TestStatusObservedMediaExitRetriesHostServiceStop(t *testing.T) {
	order := []string{}
	service := &fakeService{
		execution:   "host_only",
		status:      protocol.Status{State: protocol.StateActive},
		launch:      protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateActive}},
		stopped:     protocol.Status{State: protocol.StateIdle},
		stopResults: []error{errors.New("transient host stop failure"), nil},
		order:       &order,
	}
	media := &fakeMediaSession{}
	handler := hostapi.New(service, hostapi.WithMediaSession(media))
	launchSession(t, handler, "host-game")
	media.done = make(chan struct{})
	close(media.done)
	response := serve(t, handler, http.MethodGet, "/api/v1/session")
	if response.Code == http.StatusOK {
		t.Fatalf("status hid first host-stop failure: %s", response.Body.String())
	}
	serviceStops := 0
	for _, entry := range order {
		if entry == "service.stop" {
			serviceStops++
		}
	}
	if serviceStops != 2 {
		t.Fatalf("service stop attempts = %d, want failed attempt plus retry; order=%#v", serviceStops, order)
	}
}

func TestExplicitStopRetriesServiceButPreservesFirstFailure(t *testing.T) {
	service := &fakeService{
		execution:   "host_only",
		launch:      protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateActive}},
		stopped:     protocol.Status{State: protocol.StateIdle},
		stopResults: []error{errors.New("transient host stop failure"), nil},
	}
	media := &fakeMediaSession{}
	handler := hostapi.New(service, hostapi.WithMediaSession(media))
	launchSession(t, handler, "host-game")
	response := serve(t, handler, http.MethodPost, "/api/v1/session/stop")
	if response.Code == http.StatusOK {
		t.Fatalf("explicit stop hid first host-stop failure: %s", response.Body.String())
	}
	if len(service.stopResults) != 0 {
		t.Fatalf("service retry did not consume both results: %#v", service.stopResults)
	}
}

func TestBlockedStatusCannotApplyStaleIdleToReplacementMedia(t *testing.T) {
	statusStarted := make(chan struct{})
	statusRelease := make(chan struct{})
	service := &fakeService{
		execution:     "host_only",
		status:        protocol.Status{State: protocol.StateIdle},
		statusStarted: statusStarted,
		statusRelease: statusRelease,
		launch:        protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateActive}},
		stopped:       protocol.Status{State: protocol.StateIdle},
	}
	media := &generationMediaSession{}
	handler := hostapi.New(service, hostapi.WithMediaSession(media))
	launchSession(t, handler, "first")

	statusDone := make(chan struct{})
	go func() {
		defer close(statusDone)
		_ = serve(t, handler, http.MethodGet, "/api/v1/session")
	}()
	select {
	case <-statusStarted:
	case <-time.After(time.Second):
		t.Fatal("status did not reach blocked service observation")
	}

	launchEntered := make(chan struct{})
	var launchEnteredOnce sync.Once
	service.launchHook = func(context.Context) { launchEnteredOnce.Do(func() { close(launchEntered) }) }
	launchDone := make(chan struct{})
	go func() {
		defer close(launchDone)
		launchSession(t, handler, "replacement")
	}()
	select {
	case <-launchEntered:
		t.Fatal("replacement launch entered service while stale status was still in flight")
	case <-time.After(50 * time.Millisecond):
	}

	close(statusRelease)
	select {
	case <-statusDone:
	case <-time.After(time.Second):
		t.Fatal("status did not complete")
	}
	select {
	case <-launchDone:
	case <-time.After(time.Second):
		t.Fatal("replacement launch did not complete")
	}

	media.mu.Lock()
	handles := append([]*generationMediaHandle(nil), media.handles...)
	media.mu.Unlock()
	if len(handles) != 2 {
		t.Fatalf("media generations = %d, want 2", len(handles))
	}
	handles[0].mu.Lock()
	firstStops := handles[0].stops
	handles[0].mu.Unlock()
	handles[1].mu.Lock()
	replacementStops := handles[1].stops
	handles[1].mu.Unlock()
	if firstStops != 1 || replacementStops != 0 {
		t.Fatalf("media stops = first %d replacement %d, want 1 and 0", firstStops, replacementStops)
	}
}

func TestAutonomousMediaCleanupFailureStillStopsHostService(t *testing.T) {
	stopCalled := make(chan struct{})
	service := &fakeService{
		execution:  "host_only",
		launch:     protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateActive}},
		stopCalled: stopCalled,
	}
	media := &fakeMediaSession{stopErr: errors.New("media cleanup failed"), done: make(chan struct{})}
	handler := hostapi.New(service, hostapi.WithMediaSession(media))
	launchSession(t, handler, "host-game")
	close(media.done)
	select {
	case <-stopCalled:
	case <-time.After(time.Second):
		t.Fatal("media cleanup failure prevented autonomous host stop")
	}
}

func TestAutonomousServiceRetryPreservesFirstFailureEvent(t *testing.T) {
	service := &fakeService{
		execution:   "host_only",
		launch:      protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateActive}},
		stopped:     protocol.Status{State: protocol.StateIdle},
		stopResults: []error{errors.New("transient host stop failure"), nil},
	}
	media := &fakeMediaSession{done: make(chan struct{})}
	handler := hostapi.New(service, hostapi.WithMediaSession(media))
	launchSession(t, handler, "host-game")
	close(media.done)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		events := serve(t, handler, http.MethodGet, "/api/v1/session/events")
		if strings.Contains(events.Body.String(), `"event":"session.media.exit_failed"`) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("autonomous retry hid the first host-stop failure event")
}

func TestFailedMediaStartRetainsPartialHandleForNormalSessionStop(t *testing.T) {
	service := &fakeService{
		execution: "host_only",
		stopped:   protocol.Status{State: protocol.StateIdle},
	}
	media := &fakeMediaSession{err: errors.New("partial start failed"), partialOnError: true}
	handler := hostapi.New(service, hostapi.WithMediaSession(media))
	launch := launchSession(t, handler, "host-game")
	if launch.Code == http.StatusOK {
		t.Fatalf("partial start unexpectedly succeeded: %s", launch.Body.String())
	}
	stop := serve(t, handler, http.MethodPost, "/api/v1/session/stop")
	if stop.Code != http.StatusOK || !strings.Contains(stop.Body.String(), `"media":"stopped"`) {
		t.Fatalf("normal stop did not reap partial media: %d %s", stop.Code, stop.Body.String())
	}
	if len(media.stop) != 1 {
		t.Fatalf("partial media stop count = %d, want 1", len(media.stop))
	}
}

func launchSession(t *testing.T, handler http.Handler, gameID string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/session/launch", strings.NewReader(`{"game_id":"`+gameID+`"}`))
	request.Host = "127.0.0.1"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestHealthSeparatesHostReadinessFromTargetReadiness(t *testing.T) {
	service := &fakeService{health: protocol.Health{Ready: false}}
	response := serve(t, hostapi.New(service), http.MethodGet, "/api/v1/health")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	var result struct {
		Ready  bool `json:"ready"`
		Target struct {
			Ready bool `json:"ready"`
		} `json:"target"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Ready || result.Target.Ready {
		t.Fatalf("health = %#v", result)
	}
}

func TestStatusRedactsTargetControlledErrorMessage(t *testing.T) {
	message := "/private/path Bearer secret-token"
	service := &fakeService{status: protocol.Status{State: protocol.StateIdle, LastError: &protocol.APIError{Code: protocol.CodeInternal, Message: message}}}
	response := serve(t, hostapi.New(service), http.MethodGet, "/api/v1/status")
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), message) || !strings.Contains(response.Body.String(), "FogCast operation failed internally") {
		t.Fatalf("status leaked or failed to canonicalize: %s", response.Body.String())
	}
}

func TestRoutesRejectWrongMethodsAndMalformedGamePaths(t *testing.T) {
	handler := hostapi.New(&fakeService{})
	for _, test := range []struct {
		method, path string
		want         int
	}{
		{http.MethodPost, "/api/v1/games", http.StatusMethodNotAllowed},
		{http.MethodGet, "/api/v1/games/a/b", http.StatusNotFound},
		{http.MethodGet, "/api/v1/unknown", http.StatusNotFound},
	} {
		response := serve(t, handler, test.method, test.path)
		if response.Code != test.want {
			t.Errorf("%s %s status = %d, want %d", test.method, test.path, response.Code, test.want)
		}
	}
}

func TestHostAPIRejectsUnexpectedBrowserHost(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/games", nil)
	request.Host = "evil.example"
	response := httptest.NewRecorder()
	hostapi.New(&fakeService{}).ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
}

func serve(t *testing.T, handler http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, nil)
	request.Host = "127.0.0.1"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func assertJSONHeaders(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content type = %q", got)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("cache control = %q", got)
	}
}
