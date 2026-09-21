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
	"github.com/DeanoC/FogCast/internal/zx81keys"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/targetclient"
)

type fakeService struct {
	games                  []catalog.Game
	search                 []catalog.Game
	query                  string
	game                   catalog.Game
	gamesErr               error
	gameErr                error
	health                 protocol.Health
	healthErr              error
	status                 protocol.Status
	statusErr              error
	statusStarted          chan struct{}
	statusRelease          chan struct{}
	statusOnce             sync.Once
	statusHook             func(context.Context) (protocol.Status, error)
	launch                 protocol.CachedLaunchResponse
	launchCalls            int
	launchErr              error
	launchHook             func(context.Context)
	development            protocol.Status
	developmentErr         error
	developmentCalls       int
	developmentBody        []byte
	developmentSize        int64
	developmentHook        func(context.Context, int64, io.Reader) (protocol.Status, error)
	core                   protocol.Status
	coreErr                error
	coreCalls              int
	coreHook               func(context.Context, int64, io.Reader) (protocol.Status, error)
	stopped                protocol.Status
	stopErr                error
	stopResults            []error
	stopCalled             chan struct{}
	stopCtxErrs            []error
	stopHasDeadline        bool
	stopHook               func(context.Context) (protocol.Status, error)
	progress               []string
	execution              string
	executionErr           error
	reconstructedExecution string
	order                  *[]string
	sessionTarget          string
	sessionTargetID        string
	launchTarget           string
	playSessions           []fogcast.PlaySession
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
func (s *fakeService) SessionTarget() (string, string) {
	return s.sessionTarget, s.sessionTargetID
}
func (s *fakeService) PlaySessions() []fogcast.PlaySession {
	return s.playSessions
}
func (s *fakeService) DevelopmentActive(context.Context) (bool, error) {
	return s.status.Development && s.status.State != protocol.StateIdle, s.statusErr
}
func (s *fakeService) DevelopmentSessionState(context.Context) (bool, string, error) {
	if s.reconstructedExecution != "" {
		return s.reconstructedExecution == fogcast.ExecutionFPGADevelopment, s.reconstructedExecution, s.statusErr
	}
	if s.status.Development && (s.status.State == protocol.StateActive || s.status.State == protocol.StateStopping) {
		if s.status.CorePackage != nil && fogcast.RecognizedPlayABI(s.status.CorePackage.ABI.ID, int64(s.status.CorePackage.ABI.Major), int64(s.status.CorePackage.ABI.Minor)) {
			return false, fogcast.ExecutionFPGANative, s.statusErr
		}
		return true, fogcast.ExecutionFPGADevelopment, s.statusErr
	}
	if s.status.State == protocol.StateActive {
		return false, fogcast.ExecutionFPGANative, s.statusErr
	}
	return s.status.Development && s.status.State != protocol.StateIdle, s.reconstructedExecution, s.statusErr
}
func (s *fakeService) ActivePackageOwned() bool {
	return s.status.CorePackage != nil
}
func (s *fakeService) Game(context.Context, string) (catalog.Game, error) { return s.game, s.gameErr }
func (s *fakeService) Health(context.Context) (protocol.Health, error)    { return s.health, s.healthErr }
func (s *fakeService) Status(ctx context.Context) (protocol.Status, error) {
	if s.statusHook != nil {
		return s.statusHook(ctx)
	}
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
func (s *fakeService) LaunchOn(ctx context.Context, gameID, target string, progress fogcast.ProgressFunc) (protocol.CachedLaunchResponse, error) {
	s.launchTarget = target
	if target != "" {
		s.sessionTarget = target
	}
	return s.Launch(ctx, gameID, progress)
}
func (s *fakeService) LoadDevelopmentRBF(ctx context.Context, size int64, body io.Reader) (protocol.Status, error) {
	s.developmentCalls++
	if s.order != nil {
		*s.order = append(*s.order, "development.upload")
	}
	if s.developmentHook != nil {
		return s.developmentHook(ctx, size, body)
	}
	s.developmentSize = size
	content, err := io.ReadAll(body)
	if err != nil {
		return protocol.Status{}, err
	}
	s.developmentBody = content
	return s.development, s.developmentErr
}
func (s *fakeService) LoadCore(ctx context.Context, size int64, body io.Reader) (protocol.Status, error) {
	s.coreCalls++
	if s.coreHook != nil {
		return s.coreHook(ctx, size, body)
	}
	return s.core, s.coreErr
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
	if s.stopHook != nil {
		return s.stopHook(ctx)
	}
	if len(s.stopResults) > 0 {
		err := s.stopResults[0]
		s.stopResults = s.stopResults[1:]
		return s.stopped, err
	}
	return s.stopped, s.stopErr
}

func TestPublicStopAfterTwoSecondExpiryReconcilesIdleWithoutResubmitting(t *testing.T) {
	var stopCalls, statusCalls int
	terminal := make(chan struct{})
	service := &fakeService{status: protocol.Status{State: protocol.StateActive}}
	service.stopHook = func(ctx context.Context) (protocol.Status, error) {
		stopCalls++
		if stopCalls > 1 {
			return protocol.Status{}, errors.New("duplicate Stop must not be submitted")
		}
		<-ctx.Done()
		go func() {
			timer := time.NewTimer(100 * time.Millisecond)
			defer timer.Stop()
			<-timer.C
			close(terminal)
		}()
		return protocol.Status{}, ctx.Err()
	}
	service.statusHook = func(context.Context) (protocol.Status, error) {
		statusCalls++
		select {
		case <-terminal:
			return protocol.Status{State: protocol.StateIdle}, nil
		default:
			gameID, system, coreName := "megadrive-stop-deadline", protocol.SystemMegaDrive, "MegaDrive"
			return protocol.Status{State: protocol.StateStopping, GameID: &gameID, System: &system, ExpectedCore: &coreName, ObservedCore: &coreName}, nil
		}
	}
	handler := hostapi.New(service)
	started := time.Now()
	response := serve(t, handler, http.MethodPost, "/api/v1/session/stop")
	elapsed := time.Since(started)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"state":"idle"`) {
		t.Errorf("public Stop = %d %s, want reconciled idle", response.Code, response.Body.String())
	}
	if stopCalls != 1 {
		t.Errorf("service Stop calls = %d, want exactly one", stopCalls)
	}
	if statusCalls == 0 {
		t.Errorf("service Status calls = 0, want Status-only reconciliation")
	}
	if elapsed < 2*time.Second || elapsed >= 4*time.Second {
		t.Errorf("public Stop elapsed = %s, want bounded reconciliation after the 2s expiry", elapsed)
	}
}

func TestPublicStopDoesNotReconcileConclusiveFailureAtDeadlineBoundary(t *testing.T) {
	var stopCalls, statusCalls int
	service := &fakeService{status: protocol.Status{State: protocol.StateActive}}
	service.stopHook = func(context.Context) (protocol.Status, error) {
		stopCalls++
		return protocol.Status{}, errors.Join(
			&protocol.APIError{Code: protocol.CodeCoreTimeout, Message: "conclusive service response"},
			context.DeadlineExceeded,
		)
	}
	service.statusHook = func(context.Context) (protocol.Status, error) {
		statusCalls++
		return protocol.Status{State: protocol.StateIdle}, nil
	}
	response := serve(t, hostapi.New(service), http.MethodPost, "/api/v1/session/stop")
	if response.Code == http.StatusOK {
		t.Fatalf("public Stop hid conclusive failure: %s", response.Body.String())
	}
	if stopCalls != 1 || statusCalls != 0 {
		t.Fatalf("service calls = stop:%d status:%d, want 1/0", stopCalls, statusCalls)
	}
}

type fakeRemoteInput struct {
	status    host.RemoteInputStatus
	attach    []string
	detach    []string
	events    []remoteinput.Event
	attachErr error
	detachErr error
	sendErr   error
	order     *[]string
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
		if r.order != nil {
			*r.order = append(*r.order, "input.detach")
		}
	}
	if r.detachErr != nil {
		return r.detachErr
	}
	r.status = host.RemoteInputStatus{State: host.RemoteInputDetached, Metrics: host.RemoteInputMetrics{ShutdownReason: reason}}
	return nil
}
func (r *fakeRemoteInput) Status() host.RemoteInputStatus { return r.status }
func (r *fakeRemoteInput) SendEvent(_ context.Context, e remoteinput.Event, _ time.Time) error {
	if r.sendErr != nil {
		return r.sendErr
	}
	r.events = append(r.events, e)
	return nil
}

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

func TestSessionDevelopmentCorePreservesPriorInputUntilAdmissionThenReplacesIt(t *testing.T) {
	payload := []byte("fcore")
	core := "fes.pong"
	packageStatus := protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &core,
		CorePackage: &protocol.CorePackageStatus{PackageID: strings.Repeat("a", 64), Generation: 9,
			ABI: protocol.RuntimeContract{ID: "fes.simple-game", Major: 1}, BuildID: strings.Repeat("b", 32),
			ActiveInterfaces: []protocol.RuntimeInterface{{ID: "fes.gamepad", Major: 1}}, Gamepad: true}}
	input := &fakeRemoteInput{status: host.RemoteInputStatus{State: host.RemoteInputAttached, Ready: true}}
	service := &fakeService{core: packageStatus, status: packageStatus}
	service.coreHook = func(_ context.Context, size int64, body io.Reader) (protocol.Status, error) {
		if len(input.detach) != 0 {
			t.Fatal("prior input detached before package admission")
		}
		got, _ := io.ReadAll(body)
		if size != int64(len(payload)) || !bytes.Equal(got, payload) {
			t.Fatalf("size=%d body=%q", size, got)
		}
		return packageStatus, nil
	}
	handler := hostapi.New(service, hostapi.WithRemoteInput(input))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/session/development-core", bytes.NewReader(payload))
	request.Host = "127.0.0.1"
	request.Header.Set("Content-Type", "application/octet-stream")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || service.coreCalls != 1 || len(input.detach) != 1 || len(input.attach) != 1 || input.attach[0] != core {
		t.Fatalf("response=%d %s calls=%d detach=%v attach=%v", response.Code, response.Body.String(), service.coreCalls, input.detach, input.attach)
	}
	if !strings.Contains(response.Body.String(), `"execution":"fpga_development"`) ||
		!strings.Contains(response.Body.String(), `"generation":9`) ||
		!strings.Contains(response.Body.String(), `"input":{"state":"attached","ready":true`) {
		t.Fatalf("session=%s", response.Body.String())
	}
}

func TestSessionDevelopmentCoreAdmissionFailurePreservesPriorInput(t *testing.T) {
	priorCore := "SNES"
	service := &fakeService{status: protocol.Status{State: protocol.StateActive, ObservedCore: &priorCore},
		coreErr: &protocol.APIError{Code: protocol.CodeInvalidArchive, Message: "invalid", Phase: "admission"}}
	input := &fakeRemoteInput{status: host.RemoteInputStatus{State: host.RemoteInputAttached, Ready: true}}
	handler := hostapi.New(service, hostapi.WithRemoteInput(input))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/session/development-core", strings.NewReader("bad"))
	request.Host = "127.0.0.1"
	request.Header.Set("Content-Type", "application/octet-stream")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code == http.StatusOK || len(input.detach) != 0 || len(input.attach) != 0 {
		t.Fatalf("response=%d %s detach=%v attach=%v", response.Code, response.Body.String(), input.detach, input.attach)
	}
}

func TestSessionDevelopmentCorePredispatchFailurePreservesPriorInputAndMedia(t *testing.T) {
	gameID, system, core := "host-game", protocol.SystemSNES, "SNES"
	service := &fakeService{
		launch:  protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system, ObservedCore: &core}},
		status:  protocol.Status{State: protocol.StateIdle},
		coreErr: &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "status unavailable", Phase: "request"}}
	input := &fakeRemoteInput{status: host.RemoteInputStatus{State: host.RemoteInputDetached}}
	media := &fakeMediaSession{}
	handler := hostapi.New(service, hostapi.WithRemoteInput(input), hostapi.WithMediaSession(media))
	if launched := launchSession(t, handler, gameID); launched.Code != http.StatusOK || len(input.attach) != 1 || len(media.start) != 1 {
		t.Fatalf("launch=%d %s attach=%v media=%v", launched.Code, launched.Body.String(), input.attach, media.start)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/session/development-core", strings.NewReader("fcore"))
	request.Host = "127.0.0.1"
	request.Header.Set("Content-Type", "application/octet-stream")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code == http.StatusOK || !strings.Contains(response.Body.String(), `"phase":"request"`) ||
		len(input.detach) != 0 || len(media.stop) != 0 || input.status.State != host.RemoteInputAttached {
		t.Fatalf("response=%d %s detach=%v media.stop=%v input=%+v", response.Code, response.Body.String(), input.detach, media.stop, input.status)
	}
}

func TestSessionDevelopmentCoreReturnsConfirmedFailureAndRetiresPriorOwnership(t *testing.T) {
	gameID, system, nativeCore := "native-game", protocol.SystemSNES, "SNES"
	coreErr := &protocol.APIError{Code: protocol.CodeUnrecognizedCore, Message: "identity mismatch", Phase: "identity",
		Expected: "0123", Observed: "4567"}
	service := &fakeService{
		launch: protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateActive,
			GameID: &gameID, System: &system, ObservedCore: &nativeCore}},
		core:    protocol.Status{State: protocol.StateIdle, LastError: coreErr},
		coreErr: coreErr,
	}
	service.status = service.core
	input := &fakeRemoteInput{status: host.RemoteInputStatus{State: host.RemoteInputDetached}}
	media := &fakeMediaSession{}
	handler := hostapi.New(service, hostapi.WithRemoteInput(input), hostapi.WithMediaSession(media))
	if launched := launchSession(t, handler, gameID); launched.Code != http.StatusOK || len(input.attach) != 1 || len(media.start) != 1 {
		t.Fatalf("launch=%d %s attach=%v media=%v", launched.Code, launched.Body.String(), input.attach, media.start)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/session/development-core", strings.NewReader("fcore"))
	request.Host = "127.0.0.1"
	request.Header.Set("Content-Type", "application/octet-stream")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code == http.StatusOK || !strings.Contains(response.Body.String(), `"code":"UNRECOGNIZED_CORE"`) ||
		!strings.Contains(response.Body.String(), `"phase":"identity"`) ||
		!strings.Contains(response.Body.String(), `"expected":"0123"`) ||
		!strings.Contains(response.Body.String(), `"observed":"4567"`) ||
		len(input.detach) != 1 || len(media.stop) != 1 || input.status.State != host.RemoteInputDetached {
		t.Fatalf("response=%d %s detach=%v media.stop=%v input=%+v", response.Code, response.Body.String(), input.detach, media.stop, input.status)
	}
	status := serve(t, handler, http.MethodGet, "/api/v1/session")
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"state":"idle"`) ||
		strings.Contains(status.Body.String(), `"execution":"fpga_native"`) {
		t.Fatalf("retired ownership status=%d %s", status.Code, status.Body.String())
	}
}

func TestSessionDevelopmentCorePreservesRunningHostOnlyOwnerWhenCleanupFails(t *testing.T) {
	gameID, system := "host-game", protocol.SystemSNES
	identityErr := &protocol.APIError{Code: protocol.CodeUnrecognizedCore, Message: "identity mismatch", Phase: "identity",
		Expected: "0123", Observed: "4567"}
	service := &fakeService{
		execution: fogcast.ExecutionHostOnly,
		launch: protocol.CachedLaunchResponse{Status: protocol.Status{
			State: protocol.StateActive, GameID: &gameID, System: &system,
		}},
		status: protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system},
		core:   protocol.Status{State: protocol.StateIdle, LastError: identityErr},
		coreErr: &protocol.APIError{Code: protocol.CodeInternal,
			Message: "host cleanup failed after core package rejection", Phase: "recovery"},
	}
	input := &fakeRemoteInput{status: host.RemoteInputStatus{State: host.RemoteInputDetached}}
	media := &fakeMediaSession{}
	handler := hostapi.New(service, hostapi.WithRemoteInput(input), hostapi.WithMediaSession(media))
	if launched := launchSession(t, handler, gameID); launched.Code != http.StatusOK || len(media.start) != 1 {
		t.Fatalf("launch=%d %s media.start=%v", launched.Code, launched.Body.String(), media.start)
	}
	input.status = host.RemoteInputStatus{State: host.RemoteInputAttached, Ready: true}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/session/development-core", strings.NewReader("fcore"))
	request.Host = "127.0.0.1"
	request.Header.Set("Content-Type", "application/octet-stream")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code == http.StatusOK || !strings.Contains(response.Body.String(), `"code":"INTERNAL"`) ||
		!strings.Contains(response.Body.String(), `"phase":"recovery"`) || len(input.detach) != 1 ||
		input.status.State != host.RemoteInputDetached || len(media.stop) != 0 {
		t.Fatalf("response=%d %s detach=%v input=%+v media.stop=%v", response.Code, response.Body.String(), input.detach, input.status, media.stop)
	}

	status := serve(t, handler, http.MethodGet, "/api/v1/session")
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"state":"active"`) ||
		!strings.Contains(status.Body.String(), `"execution":"host_only"`) ||
		!strings.Contains(status.Body.String(), `"game_id":"host-game"`) ||
		!strings.Contains(status.Body.String(), `"media":"active"`) {
		t.Fatalf("preserved host ownership status=%d %s", status.Code, status.Body.String())
	}
}

func TestSessionDevelopmentCorePublishesNewOwnerBeforeMediaCleanupFailure(t *testing.T) {
	gameID, system := "host-game", protocol.SystemSNES
	core := "fes.pong"
	packageStatus := protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &core,
		CorePackage: &protocol.CorePackageStatus{PackageID: strings.Repeat("a", 64), Generation: 14,
			ABI: protocol.RuntimeContract{ID: "fes.simple-game", Major: 1}, BuildID: strings.Repeat("b", 32), Gamepad: true}}
	service := &fakeService{
		launch: protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system}},
		core:   packageStatus}
	service.coreHook = func(context.Context, int64, io.Reader) (protocol.Status, error) {
		service.status = packageStatus
		return packageStatus, nil
	}
	media := &fakeMediaSession{stopErr: errors.New("media stop failed")}
	handler := hostapi.New(service, hostapi.WithMediaSession(media))
	launched := launchSession(t, handler, gameID)
	if launched.Code != http.StatusOK || len(media.start) != 1 {
		t.Fatalf("launch=%d %s media=%v", launched.Code, launched.Body.String(), media.start)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/session/development-core", strings.NewReader("fcore"))
	request.Host = "127.0.0.1"
	request.Header.Set("Content-Type", "application/octet-stream")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code == http.StatusOK || !strings.Contains(response.Body.String(), `"phase":"recovery"`) {
		t.Fatalf("cleanup failure hidden: %s", response.Body.String())
	}
	media.stopErr = nil
	observed := serve(t, handler, http.MethodGet, "/api/v1/session")
	if observed.Code != http.StatusOK || !strings.Contains(observed.Body.String(), `"execution":"fpga_development"`) ||
		!strings.Contains(observed.Body.String(), `"generation":14`) || strings.Contains(observed.Body.String(), `"game_id"`) {
		t.Fatalf("truthful session=%d %s", observed.Code, observed.Body.String())
	}
}

func TestSessionDevelopmentCoreRetiresGenerationBindingAfterInputCleanupFailure(t *testing.T) {
	gameID, system, nativeCore := "native-game", protocol.SystemSNES, "SNES"
	packageCore := "fes.pong"
	packageStatus := protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &packageCore,
		CorePackage: &protocol.CorePackageStatus{PackageID: strings.Repeat("a", 64), Generation: 15,
			ABI: protocol.RuntimeContract{ID: "fes.simple-game", Major: 1}, BuildID: strings.Repeat("b", 32), Gamepad: true}}
	service := &fakeService{
		launch: protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system, ObservedCore: &nativeCore}},
		status: protocol.Status{State: protocol.StateIdle}}
	service.coreHook = func(context.Context, int64, io.Reader) (protocol.Status, error) {
		service.status = packageStatus
		return packageStatus, nil
	}
	input := &fakeRemoteInput{status: host.RemoteInputStatus{State: host.RemoteInputDetached}}
	handler := hostapi.New(service, hostapi.WithRemoteInput(input))
	if launched := launchSession(t, handler, gameID); launched.Code != http.StatusOK || len(input.attach) != 1 {
		t.Fatalf("launch=%d %s attach=%v", launched.Code, launched.Body.String(), input.attach)
	}
	input.detachErr = errors.New("input detach failed")
	request := httptest.NewRequest(http.MethodPost, "/api/v1/session/development-core", strings.NewReader("fcore"))
	request.Host = "127.0.0.1"
	request.Header.Set("Content-Type", "application/octet-stream")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code == http.StatusOK || !strings.Contains(response.Body.String(), `"phase":"recovery"`) || len(input.attach) != 1 {
		t.Fatalf("response=%d %s attach=%v detach=%v", response.Code, response.Body.String(), input.attach, input.detach)
	}
	input.detachErr = nil
	observed := serve(t, handler, http.MethodGet, "/api/v1/session")
	if observed.Code != http.StatusOK || len(input.attach) != 2 || input.attach[1] != packageCore ||
		!strings.Contains(observed.Body.String(), `"generation":15`) {
		t.Fatalf("observed=%d %s attach=%v detach=%v", observed.Code, observed.Body.String(), input.attach, input.detach)
	}
}

func TestSessionManualInputRejectsRawDevelopmentWithoutGamepadCapability(t *testing.T) {
	core := "DEVCORE"
	service := &fakeService{status: protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &core}}
	input := &fakeRemoteInput{status: host.RemoteInputStatus{State: host.RemoteInputDetached}}
	response := serve(t, hostapi.New(service, hostapi.WithRemoteInput(input)), http.MethodPost, "/api/v1/session/input/attach")
	if response.Code == http.StatusOK || len(input.attach) != 0 {
		t.Fatalf("response=%d %s attach=%v", response.Code, response.Body.String(), input.attach)
	}
}

func TestSessionStatusAttachesKeyboardComputerWithoutGamepad(t *testing.T) {
	core := "fes.zx81"
	status := protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &core,
		CorePackage: &protocol.CorePackageStatus{PackageID: strings.Repeat("a", 64), Generation: 3,
			ABI: protocol.RuntimeContract{ID: "fes.simple-computer", Major: 1}, BuildID: strings.Repeat("b", 32),
			ActiveInterfaces: []protocol.RuntimeInterface{
				{ID: "fes.keyboard", Major: 1},
				{ID: "fes.media.blob", Major: 1},
				{ID: "fes.video.fixed-720p60", Major: 1},
			}}}
	input := &fakeRemoteInput{status: host.RemoteInputStatus{State: host.RemoteInputDetached}}
	response := serve(t, hostapi.New(&fakeService{status: status}, hostapi.WithRemoteInput(input)), http.MethodGet, "/api/v1/session")
	if response.Code != http.StatusOK || len(input.attach) != 1 || input.attach[0] != core {
		t.Fatalf("response=%d %s attach=%v", response.Code, response.Body.String(), input.attach)
	}
}

func TestSessionStatusReconstructsCapablePackageInputAfterHostRestart(t *testing.T) {
	core := "fes.pong"
	status := protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &core,
		CorePackage: &protocol.CorePackageStatus{PackageID: strings.Repeat("a", 64), Generation: 11,
			ABI: protocol.RuntimeContract{ID: "fes.simple-game", Major: 1}, BuildID: strings.Repeat("b", 32),
			ActiveInterfaces: []protocol.RuntimeInterface{{ID: "fes.gamepad", Major: 1}}, Gamepad: true}}
	input := &fakeRemoteInput{status: host.RemoteInputStatus{State: host.RemoteInputDetached}}
	response := serve(t, hostapi.New(&fakeService{status: status}, hostapi.WithRemoteInput(input)), http.MethodGet, "/api/v1/session")
	if response.Code != http.StatusOK || len(input.attach) != 1 || input.attach[0] != core ||
		!strings.Contains(response.Body.String(), `"generation":11`) ||
		!strings.Contains(response.Body.String(), `"input":{"state":"attached","ready":true`) {
		t.Fatalf("response=%d %s attach=%v", response.Code, response.Body.String(), input.attach)
	}
}

func TestSessionStatusRetiresInputWhenReconstructedDevelopmentLacksCapability(t *testing.T) {
	core := "RAWDEV"
	status := protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &core}
	input := &fakeRemoteInput{status: host.RemoteInputStatus{State: host.RemoteInputAttached, Ready: true}}
	response := serve(t, hostapi.New(&fakeService{status: status}, hostapi.WithRemoteInput(input)), http.MethodGet, "/api/v1/session")
	if response.Code != http.StatusOK || len(input.detach) != 1 ||
		strings.Contains(response.Body.String(), `"ready":true`) {
		t.Fatalf("response=%d %s detach=%v", response.Code, response.Body.String(), input.detach)
	}
}

func TestSessionStatusDoesNotReplaceEligibleInputWhileItReconnects(t *testing.T) {
	core := "fes.pong"
	status := protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &core,
		CorePackage: &protocol.CorePackageStatus{PackageID: strings.Repeat("a", 64), Generation: 12,
			ABI: protocol.RuntimeContract{ID: "fes.simple-game", Major: 1}, BuildID: strings.Repeat("b", 32), Gamepad: true}}
	input := &fakeRemoteInput{status: host.RemoteInputStatus{State: host.RemoteInputDetached}}
	handler := hostapi.New(&fakeService{status: status}, hostapi.WithRemoteInput(input))
	if response := serve(t, handler, http.MethodGet, "/api/v1/session"); response.Code != http.StatusOK || len(input.attach) != 1 {
		t.Fatalf("initial response=%d %s attach=%v", response.Code, response.Body.String(), input.attach)
	}
	input.status = host.RemoteInputStatus{State: host.RemoteInputReconnecting, SessionID: "retained-session"}
	response := serve(t, handler, http.MethodGet, "/api/v1/session")
	if response.Code != http.StatusOK || len(input.attach) != 1 ||
		!strings.Contains(response.Body.String(), `"state":"reconnecting"`) {
		t.Fatalf("response=%d %s attach=%v", response.Code, response.Body.String(), input.attach)
	}
}

func TestSessionStatusReplacesInputWhenPackageGenerationChanges(t *testing.T) {
	core := "fes.pong"
	status := protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &core,
		CorePackage: &protocol.CorePackageStatus{PackageID: strings.Repeat("a", 64), Generation: 12,
			ABI: protocol.RuntimeContract{ID: "fes.simple-game", Major: 1}, BuildID: strings.Repeat("b", 32), Gamepad: true}}
	service := &fakeService{status: status}
	input := &fakeRemoteInput{status: host.RemoteInputStatus{State: host.RemoteInputDetached}}
	handler := hostapi.New(service, hostapi.WithRemoteInput(input))
	if response := serve(t, handler, http.MethodGet, "/api/v1/session"); response.Code != http.StatusOK || len(input.attach) != 1 {
		t.Fatalf("initial response=%d %s attach=%v", response.Code, response.Body.String(), input.attach)
	}
	input.status.SessionID = "generation-12"
	service.status.CorePackage.Generation = 13
	response := serve(t, handler, http.MethodGet, "/api/v1/session")
	if response.Code != http.StatusOK || len(input.detach) != 1 || len(input.attach) != 2 {
		t.Fatalf("replacement response=%d %s detach=%v attach=%v", response.Code, response.Body.String(), input.detach, input.attach)
	}
}

type forbiddenDevelopmentReader struct{ reads int }

func (r *forbiddenDevelopmentReader) Read([]byte) (int, error) {
	r.reads++
	return 0, errors.New("development body must remain unread")
}

func TestSessionReplaceNativeGameStopsToExactIdleBeforeDevelopmentUpload(t *testing.T) {
	gameID, system, core := "megadrive-active", protocol.SystemMegaDrive, "MegaDrive"
	order := []string{}
	active := protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system, ExpectedCore: &core, ObservedCore: &core}
	baseService := &fakeService{
		status:      active,
		execution:   fogcast.ExecutionFPGANative,
		launch:      protocol.CachedLaunchResponse{Status: active},
		stopped:     protocol.Status{State: protocol.StateIdle},
		development: protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &core},
		order:       &order,
	}
	service := &leasedService{fakeService: baseService}
	remoteInput := &fakeRemoteInput{order: &order}
	media := &fakeMediaSession{order: &order}
	handler := hostapi.New(service, hostapi.WithRemoteInput(remoteInput), hostapi.WithMediaSession(media))
	launchSession(t, handler, gameID)
	order = order[:0]

	request := httptest.NewRequest(http.MethodPost, "/api/v1/session/development-rbf", strings.NewReader("rbf"))
	request.Host = "127.0.0.1"
	request.Header.Set("Content-Type", "application/octet-stream")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("development replacement = %d %s", response.Code, response.Body.String())
	}
	if service.releases != 0 {
		t.Fatal("replacement released kit ownership")
	}
	if got := strings.Join(order, ","); got != "input.detach,stop,service.stop,development.upload" {
		t.Fatalf("replacement order = %q", got)
	}
	if service.developmentCalls != 1 || string(service.developmentBody) != "rbf" || !strings.Contains(response.Body.String(), `"execution":"fpga_development"`) {
		t.Fatalf("replacement upload calls=%d body=%q response=%s", service.developmentCalls, service.developmentBody, response.Body.String())
	}
}

func TestSessionDevelopmentRBFAfterExplicitNativeStopDoesNotStopAgain(t *testing.T) {
	gameID, system, core := "megadrive-stopped", protocol.SystemMegaDrive, "MegaDrive"
	active := protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system, ExpectedCore: &core, ObservedCore: &core}
	baseService := &fakeService{
		status:      protocol.Status{State: protocol.StateIdle},
		execution:   fogcast.ExecutionFPGANative,
		launch:      protocol.CachedLaunchResponse{Status: active},
		stopped:     protocol.Status{State: protocol.StateIdle},
		development: protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &core},
	}
	stopCalls := 0
	baseService.stopHook = func(context.Context) (protocol.Status, error) {
		stopCalls++
		if stopCalls > 1 {
			return protocol.Status{}, targetclient.ErrKitLeaseLost
		}
		return protocol.Status{State: protocol.StateIdle}, nil
	}
	service := &leasedService{fakeService: baseService}
	handler := hostapi.New(service, hostapi.WithRemoteInput(&fakeRemoteInput{}), hostapi.WithMediaSession(&fakeMediaSession{}))
	if launch := launchSession(t, handler, gameID); launch.Code != http.StatusOK {
		t.Fatalf("launch = %d %s", launch.Code, launch.Body.String())
	}
	if stop := serve(t, handler, http.MethodPost, "/api/v1/session/stop"); stop.Code != http.StatusOK ||
		!strings.Contains(stop.Body.String(), `"execution":"fpga_native"`) || !strings.Contains(stop.Body.String(), `"media":"stopped"`) {
		t.Fatalf("stop = %d %s", stop.Code, stop.Body.String())
	}
	if service.releases != 1 || stopCalls != 1 {
		t.Fatalf("stop calls=%d lease releases=%d", stopCalls, service.releases)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/session/development-rbf", strings.NewReader("rbf"))
	request.Host = "127.0.0.1"
	request.Header.Set("Content-Type", "application/octet-stream")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK || service.developmentCalls != 1 {
		t.Fatalf("development replacement = %d %s calls=%d", response.Code, response.Body.String(), service.developmentCalls)
	}
	if stopCalls != 1 || service.releases != 1 {
		t.Fatalf("development replacement changed stopped ownership: stop calls=%d lease releases=%d", stopCalls, service.releases)
	}
}

func TestSessionReplaceNativeGameNeverReadsDevelopmentBodyWithoutConfirmedIdle(t *testing.T) {
	for _, test := range []struct {
		name    string
		stopped protocol.Status
		stopErr error
		cancel  bool
	}{
		{name: "stop error", stopErr: errors.New("stop failed")},
		{name: "ambiguous stop deadline followed by idle", stopErr: context.DeadlineExceeded},
		{name: "non idle", stopped: protocol.Status{State: protocol.StateActive}},
		{name: "parent canceled", stopped: protocol.Status{State: protocol.StateIdle}, cancel: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			gameID, system, core := "megadrive-active", protocol.SystemMegaDrive, "MegaDrive"
			active := protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system, ExpectedCore: &core, ObservedCore: &core}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			service := &fakeService{
				status: active, execution: fogcast.ExecutionFPGANative,
				launch:  protocol.CachedLaunchResponse{Status: active},
				stopped: test.stopped, stopErr: test.stopErr,
				development: protocol.Status{State: protocol.StateActive, Development: true},
			}
			if errors.Is(test.stopErr, context.DeadlineExceeded) {
				statusCalls := 0
				service.statusHook = func(context.Context) (protocol.Status, error) {
					statusCalls++
					if statusCalls > 1 {
						return protocol.Status{State: protocol.StateIdle}, nil
					}
					return active, nil
				}
			}
			if test.cancel {
				service.stopHook = func(context.Context) (protocol.Status, error) {
					cancel()
					return protocol.Status{State: protocol.StateIdle}, nil
				}
			}
			handler := hostapi.New(service, hostapi.WithMediaSession(&fakeMediaSession{}))
			launchSession(t, handler, gameID)
			body := &forbiddenDevelopmentReader{}
			request := httptest.NewRequest(http.MethodPost, "/api/v1/session/development-rbf", body).WithContext(ctx)
			request.Host = "127.0.0.1"
			request.ContentLength = 3
			request.Header.Set("Content-Type", "application/octet-stream")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code == http.StatusOK || body.reads != 0 || service.developmentCalls != 0 {
				t.Fatalf("response=%d %s reads=%d uploads=%d", response.Code, response.Body.String(), body.reads, service.developmentCalls)
			}
		})
	}
}

func TestSessionReplaceDevelopmentIsBusyBeforeTeardownOrBodyRead(t *testing.T) {
	core := "DEVCORE"
	development := protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &core}
	service := &fakeService{status: development, development: development}
	order := []string{}
	remoteInput := &fakeRemoteInput{status: host.RemoteInputStatus{State: host.RemoteInputAttached, Ready: true}, order: &order}
	media := &fakeMediaSession{order: &order}
	body := &forbiddenDevelopmentReader{}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/session/development-rbf", body)
	request.Host = "127.0.0.1"
	request.ContentLength = 3
	request.Header.Set("Content-Type", "application/octet-stream")
	response := httptest.NewRecorder()
	hostapi.New(service, hostapi.WithRemoteInput(remoteInput), hostapi.WithMediaSession(media)).ServeHTTP(response, request)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"code":"BUSY"`) || body.reads != 0 || service.developmentCalls != 0 || len(order) != 0 {
		t.Fatalf("response=%d %s reads=%d uploads=%d order=%v", response.Code, response.Body.String(), body.reads, service.developmentCalls, order)
	}
}

func TestSessionReplaceReconstructedNativeGameStopsBeforeDevelopmentUpload(t *testing.T) {
	for _, test := range []struct {
		name    string
		stopped protocol.Status
		stopErr error
		cancel  bool
		wantOK  bool
	}{
		{name: "exact idle", stopped: protocol.Status{State: protocol.StateIdle}, wantOK: true},
		{name: "stop error", stopErr: errors.New("stop failed")},
		{name: "non idle", stopped: protocol.Status{State: protocol.StateActive}},
		{name: "parent canceled", stopped: protocol.Status{State: protocol.StateIdle}, cancel: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			gameID, system, core := "megadrive-reconstructed", protocol.SystemMegaDrive, "MegaDrive"
			order := []string{}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			service := &fakeService{
				status:                 protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system, ExpectedCore: &core, ObservedCore: &core},
				reconstructedExecution: fogcast.ExecutionFPGANative,
				stopped:                test.stopped,
				stopErr:                test.stopErr,
				development:            protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &core},
				order:                  &order,
			}
			if test.cancel {
				service.stopHook = func(context.Context) (protocol.Status, error) {
					cancel()
					return protocol.Status{State: protocol.StateIdle}, nil
				}
			}
			remoteInput := &fakeRemoteInput{status: host.RemoteInputStatus{State: host.RemoteInputAttached, Ready: true}, order: &order}
			var body io.Reader = strings.NewReader("rbf")
			forbidden := &forbiddenDevelopmentReader{}
			if !test.wantOK {
				body = forbidden
			}
			request := httptest.NewRequest(http.MethodPost, "/api/v1/session/development-rbf", body).WithContext(ctx)
			request.Host = "127.0.0.1"
			request.ContentLength = 3
			request.Header.Set("Content-Type", "application/octet-stream")
			response := httptest.NewRecorder()
			hostapi.New(service, hostapi.WithRemoteInput(remoteInput)).ServeHTTP(response, request)

			if test.wantOK {
				if response.Code != http.StatusOK || service.developmentCalls != 1 || string(service.developmentBody) != "rbf" {
					t.Fatalf("response=%d %s uploads=%d body=%q", response.Code, response.Body.String(), service.developmentCalls, service.developmentBody)
				}
				if got := strings.Join(order, ","); got != "input.detach,service.stop,development.upload" {
					t.Fatalf("replacement order = %q", got)
				}
				return
			}
			if response.Code == http.StatusOK || forbidden.reads != 0 || service.developmentCalls != 0 {
				t.Fatalf("response=%d %s reads=%d uploads=%d order=%v", response.Code, response.Body.String(), forbidden.reads, service.developmentCalls, order)
			}
		})
	}
}

func TestSessionDevelopmentRBFMustStopBeforeCatalogLaunch(t *testing.T) {
	for _, execution := range []string{fogcast.ExecutionFPGANative, fogcast.ExecutionHostOnly} {
		t.Run(execution, func(t *testing.T) {
			observed := "DEVCORE"
			development := protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &observed}
			service := &fakeService{
				development: development,
				status:      protocol.Status{State: protocol.StateIdle},
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
			service.status = development

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

func TestSessionUnsupportedOperationIsPublicBadRequest(t *testing.T) {
	t.Parallel()
	service := &fakeService{developmentErr: &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "private target wording"}}
	handler := hostapi.New(service)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/session/development-rbf", strings.NewReader("rbf"))
	request.Host = "127.0.0.1"
	request.Header.Set("Content-Type", "application/octet-stream")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"UNSUPPORTED_OPERATION"`) || !strings.Contains(response.Body.String(), `"message":"requested operation is unsupported"`) {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "private") {
		t.Fatalf("target detail leaked: %s", response.Body.String())
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

type busyTargetService struct {
	*fakeService
	conn fogcast.TargetConnection
}

func (s *busyTargetService) TargetConnection() fogcast.TargetConnection { return s.conn }

func TestSessionPlayHIDReachesAttachedInputAndFailClosedOnForeignLease(t *testing.T) {
	gameID := "zx81-j"
	core := "fes.zx81"
	service := &fakeService{
		status: protocol.Status{State: protocol.StateActive, GameID: &gameID, ObservedCore: &core,
			Development: true, CorePackage: &protocol.CorePackageStatus{
				PackageID: strings.Repeat("a", 64), Generation: 3,
				ABI: protocol.RuntimeContract{ID: "fes.simple-computer", Major: 1}, BuildID: strings.Repeat("b", 32),
				ActiveInterfaces: []protocol.RuntimeInterface{{ID: "fes.keyboard", Major: 1, Minor: 0}},
			}},
	}
	input := &fakeRemoteInput{status: host.RemoteInputStatus{State: host.RemoteInputAttached, Ready: true}}
	handler := hostapi.New(service, hostapi.WithRemoteInput(input))

	event := remoteinput.Event{Device: remoteinput.DeviceKeyboard, Kind: remoteinput.KindKey, Action: remoteinput.ActionPress, Code: zx81keys.Letter('J')}
	body, err := json.Marshal(map[string]any{"event": event})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/session/input/event", bytes.NewReader(body))
	req.Host = "127.0.0.1"
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || len(input.events) != 1 || input.events[0].Code != zx81keys.Letter('J') {
		t.Fatalf("play HID = %d %s events=%#v", rec.Code, rec.Body.String(), input.events)
	}

	input.status = host.RemoteInputStatus{State: host.RemoteInputDetached}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/session/input/event", bytes.NewReader(body))
	req.Host = "127.0.0.1"
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK || len(input.events) != 1 {
		t.Fatalf("detached HID = %d %s events=%d", rec.Code, rec.Body.String(), len(input.events))
	}

	foreign := &busyTargetService{fakeService: service, conn: fogcast.TargetConnection{State: "busy", Owner: "caster"}}
	input.status = host.RemoteInputStatus{State: host.RemoteInputAttached, Ready: true}
	handler = hostapi.New(foreign, hostapi.WithRemoteInput(input))
	req = httptest.NewRequest(http.MethodPost, "/api/v1/session/input/event", bytes.NewReader(body))
	req.Host = "127.0.0.1"
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "KIT_LEASE_DENIED") || len(input.events) != 1 {
		t.Fatalf("foreign HID = %d %s events=%d", rec.Code, rec.Body.String(), len(input.events))
	}

	recovery := &busyTargetService{fakeService: service, conn: fogcast.TargetConnection{State: "recovery-required"}}
	handler = hostapi.New(recovery, hostapi.WithRemoteInput(input))
	req = httptest.NewRequest(http.MethodPost, "/api/v1/session/input/event", bytes.NewReader(body))
	req.Host = "127.0.0.1"
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "KIT_LEASE_DENIED") || len(input.events) != 1 {
		t.Fatalf("recovery-required HID = %d %s events=%d", rec.Code, rec.Body.String(), len(input.events))
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

func TestStatusObservedMediaExitDoesNotReplayFailedHostServiceStop(t *testing.T) {
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
	if serviceStops != 1 {
		t.Fatalf("service stop attempts = %d, want one mutation; order=%#v", serviceStops, order)
	}
}

func TestExplicitStopDoesNotReplayServiceMutationAndPreservesFailure(t *testing.T) {
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
	if len(service.stopResults) != 1 || service.stopResults[0] != nil {
		t.Fatalf("service Stop was replayed: remaining results %#v", service.stopResults)
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

func TestAutonomousServiceFailureIsRecordedWithoutReplay(t *testing.T) {
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

func TestHealthIncludesHostIdentityAndTargetArtifacts(t *testing.T) {
	commit := strings.Repeat("1", 40)
	service := &fakeService{health: protocol.Health{Ready: true, Artifacts: &protocol.Artifacts{RuntimeCommit: commit}}}
	response := serve(t, hostapi.New(service), http.MethodGet, "/api/v1/health")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	var result struct {
		Ready bool `json:"ready"`
		Host  struct {
			Version string `json:"version"`
			OS      string `json:"os"`
			Arch    string `json:"arch"`
		} `json:"host"`
		Target struct {
			Ready     bool `json:"ready"`
			Artifacts *struct {
				RuntimeCommit string `json:"runtime_commit"`
			} `json:"artifacts"`
		} `json:"target"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Host.Version == "" || result.Host.OS == "" || result.Host.Arch == "" {
		t.Fatalf("host identity = %#v", result.Host)
	}
	if !result.Target.Ready || result.Target.Artifacts == nil || result.Target.Artifacts.RuntimeCommit != commit {
		t.Fatalf("target = %#v", result.Target)
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
