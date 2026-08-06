package fogcast

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/DeanoC/FogCast-POC/catalog"
	"github.com/DeanoC/FogCast-POC/host"
	"github.com/DeanoC/FogCast-POC/internal/core"
	"github.com/DeanoC/FogCast-POC/internal/hostexec"
	"github.com/DeanoC/FogCast-POC/protocol"
	"github.com/DeanoC/FogCast-POC/romsource"
)

type Progress struct {
	Stage   string `json:"stage"`
	Message string `json:"message"`
}

type ProgressFunc func(Progress)

type serviceCatalog interface {
	Game(context.Context, string) (catalog.Game, error)
	Games(context.Context) ([]catalog.Game, error)
	Search(context.Context, string) ([]catalog.Game, error)
	GameMatchesRoot(context.Context, catalog.Game, catalog.Root) (bool, error)
	CompareAndSetContent(context.Context, catalog.Game, catalog.Root, catalog.Content) (bool, error)
	Close() error
}

type serviceScanner interface {
	Scan(context.Context, []catalog.Root) (catalog.ScanReport, error)
}

type debugServiceScanner interface {
	SetDebug(func(string))
}

type servicePreparer interface {
	Prepare(context.Context, catalog.Root, catalog.Game) (*romsource.Prepared, error)
}

type serviceClient interface {
	ProbeContent(context.Context, protocol.System, protocol.ContentIdentity) (protocol.CacheProbeResponse, error)
	UploadContent(context.Context, protocol.System, protocol.ContentIdentity, io.Reader) (protocol.CacheUploadResponse, error)
	LaunchContent(context.Context, protocol.CachedLaunchRequest) (protocol.CachedLaunchResponse, error)
	Health(context.Context) (protocol.Health, error)
	Status(context.Context) (protocol.Status, error)
	Stop(context.Context) (protocol.Status, error)
}

const (
	ExecutionFPGANative = "fpga_native"
	ExecutionHostOnly   = "host_only"
)

// ExecutionResolver is intentionally a service-level policy seam for POC5.
// Catalog schema v1 remains unchanged; persistence of execution metadata is
// deferred until it becomes catalog-owned product data.
type ExecutionResolver interface {
	Resolve(context.Context, catalog.Game) (string, error)
}

type ExecutionResolverFunc func(context.Context, catalog.Game) (string, error)

func (f ExecutionResolverFunc) Resolve(ctx context.Context, game catalog.Game) (string, error) {
	return f(ctx, game)
}

type defaultExecutionResolver struct{}

func (defaultExecutionResolver) Resolve(context.Context, catalog.Game) (string, error) {
	return ExecutionFPGANative, nil
}

type configuredExecutionResolver struct {
	systems map[protocol.System]struct{}
	host    hostexec.Adapter
}

// NewConfiguredExecutionResolver selects host execution only for configured
// systems and only when a host adapter is available. Otherwise it falls back
// to the native MiSTer execution path.
func NewConfiguredExecutionResolver(systems []protocol.System, host hostexec.Adapter) ExecutionResolver {
	configured := make(map[protocol.System]struct{}, len(systems))
	for _, system := range systems {
		configured[system] = struct{}{}
	}
	return configuredExecutionResolver{systems: configured, host: host}
}

func (r configuredExecutionResolver) Resolve(_ context.Context, game catalog.Game) (string, error) {
	if r.host != nil {
		if _, ok := r.systems[game.System]; ok {
			return ExecutionHostOnly, nil
		}
	}
	return ExecutionFPGANative, nil
}

type ExecutionPolicy struct {
	Resolver ExecutionResolver
	Host     hostexec.Adapter
}

type ServiceOption func(*Service)

func WithExecutionPolicy(policy ExecutionPolicy) ServiceOption {
	return func(service *Service) {
		if policy.Resolver != nil {
			service.executionResolver = policy.Resolver
		}
		service.hostExecutor = policy.Host
	}
}

type Service struct {
	catalog           serviceCatalog
	scanner           serviceScanner
	preparer          servicePreparer
	client            serviceClient
	roots             []catalog.Root
	rootsByID         map[string]catalog.Root
	requestTimeout    time.Duration
	uploadTimeout     time.Duration
	uploadReadDelay   time.Duration
	executionResolver ExecutionResolver
	hostExecutor      hostexec.Adapter
	activeExecution   string
	activeGameID      string
	activeSystem      protocol.System
	executionMu       sync.Mutex
	closeOnce         sync.Once
	closeErr          error
}

func Open(ctx context.Context, paths Paths, httpClient *http.Client) (*Service, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	config, err := LoadConfig(paths.Config)
	if err != nil {
		return nil, errors.New("load FogCast configuration")
	}
	if err := validatePrivateFilePath(paths.Index); err != nil {
		return nil, errors.New("prepare FogCast catalog location")
	}
	if err := ensurePrivateDirectory(filepath.Dir(paths.Index)); err != nil {
		return nil, errors.New("prepare FogCast catalog location")
	}
	store, err := catalog.OpenContext(ctx, paths.Index)
	if err != nil {
		return nil, safeOpenError("open FogCast catalog", err)
	}
	fail := func(message string, cause error) (*Service, error) {
		_ = store.Close()
		return nil, safeOpenError(message, cause)
	}
	if err := ensurePrivateRegularFile(paths.Index); err != nil {
		return fail("secure FogCast catalog", err)
	}
	if err := ensurePrivateDirectory(paths.Staging); err != nil {
		return fail("prepare FogCast staging", err)
	}
	baseURL, err := url.Parse(config.BaseURL)
	if err != nil {
		return fail("configure FogCast target", err)
	}
	registry := core.DefaultRegistry()
	scanner := &catalog.Scanner{Store: store, Registry: registry}
	preparer := &romsource.Preparer{StagingRoot: paths.Staging, MaxBytes: protocol.MaxContentBytes}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	operationClient := *httpClient
	operationClient.Timeout = 0
	client := host.NewClient(baseURL, config.Token, &operationClient)
	options := make([]ServiceOption, 0, 1)
	if config.HostEmulator.Binary != "" && config.HostEmulator.Core != "" {
		host := hostexec.NewRetroArchAdapter(config.HostEmulator.Binary, config.HostEmulator.Core, nil)
		options = append(options, WithExecutionPolicy(ExecutionPolicy{Resolver: NewConfiguredExecutionResolver(config.HostEmulator.Systems, host), Host: host}))
	}
	return newService(config, paths, store, scanner, preparer, client, options...), nil
}

func newService(config Config, _ Paths, store serviceCatalog, scanner serviceScanner, preparer servicePreparer, client serviceClient, options ...ServiceOption) *Service {
	roots := append([]catalog.Root(nil), config.Libraries...)
	rootsByID := make(map[string]catalog.Root, len(roots))
	for _, root := range roots {
		rootsByID[root.ID] = root
	}
	service := &Service{
		catalog: store, scanner: scanner, preparer: preparer, client: client,
		roots: roots, rootsByID: rootsByID,
		requestTimeout: config.RequestTimeout, uploadTimeout: config.UploadTimeout,
		executionResolver: defaultExecutionResolver{},
	}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	return service
}

// SessionExecution resolves the service-owned execution policy for one catalog game.
func (s *Service) SessionExecution(ctx context.Context, gameID string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := protocol.ValidateGameID(gameID); err != nil {
		return "", canonicalError(protocol.CodeBadRequest, nil)
	}
	game, err := s.catalog.Game(ctx, gameID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", canonicalError(protocol.CodeROMNotFound, nil)
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	execution, err := s.resolveExecution(ctx, game)
	if err != nil {
		return "", err
	}
	return execution, nil
}

func (s *Service) resolveExecution(ctx context.Context, game catalog.Game) (string, error) {
	execution, err := s.executionResolver.Resolve(ctx, game)
	if err != nil {
		return "", canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	if execution != ExecutionFPGANative && execution != ExecutionHostOnly {
		return "", canonicalError(protocol.CodeInternal, nil)
	}
	if execution == ExecutionHostOnly && s.hostExecutor == nil {
		return ExecutionFPGANative, nil
	}
	return execution, nil
}

func (s *Service) SetDebug(debug func(string)) {
	if scanner, ok := s.scanner.(debugServiceScanner); ok {
		scanner.SetDebug(debug)
	}
}

// SetUploadReadDelay adds a delay before each upload body read for deterministic
// operator-assisted interruption testing.
func (s *Service) SetUploadReadDelay(delay time.Duration) {
	if delay < 0 {
		delay = 0
	}
	s.uploadReadDelay = delay
}

func (s *Service) Launch(ctx context.Context, gameID string, progress ProgressFunc) (protocol.CachedLaunchResponse, error) {
	if err := ctx.Err(); err != nil {
		return protocol.CachedLaunchResponse{}, err
	}
	if err := protocol.ValidateGameID(gameID); err != nil {
		return protocol.CachedLaunchResponse{}, canonicalError(protocol.CodeBadRequest, nil)
	}
	for attempt := 0; attempt < 2; attempt++ {
		game, err := s.catalog.Game(ctx, gameID)
		if err != nil {
			switch {
			case errors.Is(err, sql.ErrNoRows):
				return protocol.CachedLaunchResponse{}, canonicalError(protocol.CodeROMNotFound, nil)
			case ctx.Err() != nil:
				return protocol.CachedLaunchResponse{}, ctx.Err()
			default:
				return protocol.CachedLaunchResponse{}, canonicalError(protocol.CodeInternal, safeContextError(err))
			}
		}
		response, retry, err := s.launchGame(ctx, game, progress)
		if !retry {
			return response, err
		}
	}
	return protocol.CachedLaunchResponse{}, canonicalError(protocol.CodeSourceUnavailable, nil)
}

func (s *Service) Close() error {
	s.closeOnce.Do(func() {
		s.closeErr = s.catalog.Close()
	})
	if s.closeErr != nil {
		return canonicalError(protocol.CodeInternal, nil)
	}
	return nil
}

func (s *Service) Scan(ctx context.Context) (catalog.ScanReport, error) {
	if err := ctx.Err(); err != nil {
		return catalog.ScanReport{}, err
	}
	report, err := s.scanner.Scan(ctx, append([]catalog.Root(nil), s.roots...))
	if err != nil {
		if ctx.Err() != nil {
			return catalog.ScanReport{}, ctx.Err()
		}
		return catalog.ScanReport{}, canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	return report, nil
}

func (s *Service) Games(ctx context.Context) ([]catalog.Game, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	games, err := s.catalog.Games(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	return games, nil
}

func (s *Service) Game(ctx context.Context, gameID string) (catalog.Game, error) {
	if err := ctx.Err(); err != nil {
		return catalog.Game{}, err
	}
	if err := protocol.ValidateGameID(gameID); err != nil {
		return catalog.Game{}, canonicalError(protocol.CodeBadRequest, nil)
	}
	game, err := s.catalog.Game(ctx, gameID)
	if err != nil {
		if ctx.Err() != nil {
			return catalog.Game{}, ctx.Err()
		}
		if errors.Is(err, sql.ErrNoRows) {
			return catalog.Game{}, canonicalError(protocol.CodeROMNotFound, nil)
		}
		return catalog.Game{}, canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	return game, nil
}

func (s *Service) Search(ctx context.Context, query string) ([]catalog.Game, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	games, err := s.catalog.Search(ctx, query)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	return games, nil
}

func (s *Service) Health(parent context.Context) (protocol.Health, error) {
	ctx, cancel := serviceTimeout(parent, s.requestTimeout)
	defer cancel()
	health, err := s.client.Health(ctx)
	if err != nil {
		return protocol.Health{}, canonicalRemoteError(err, protocol.CodeMiSTerUnavailable)
	}
	return health, nil
}

func (s *Service) Status(parent context.Context) (protocol.Status, error) {
	ctx, cancel := serviceTimeout(parent, s.requestTimeout)
	defer cancel()
	s.executionMu.Lock()
	hostOnly := s.activeExecution == ExecutionHostOnly
	gameID, system := s.activeGameID, s.activeSystem
	s.executionMu.Unlock()
	if hostOnly {
		if s.hostExecutor == nil {
			return protocol.Status{}, canonicalError(protocol.CodeInternal, nil)
		}
		status, err := s.hostExecutor.Status(ctx)
		if err != nil {
			return protocol.Status{}, canonicalError(protocol.CodeInternal, safeContextError(err))
		}
		if status.State == hostexec.Idle {
			s.executionMu.Lock()
			s.activeExecution, s.activeGameID, s.activeSystem = "", "", ""
			s.executionMu.Unlock()
			return protocol.Status{State: protocol.StateIdle}, nil
		}
		return protocol.Status{State: protocol.StateActive, GameID: stringPtr(gameID), System: systemPtr(system)}, nil
	}
	status, err := s.client.Status(ctx)
	if err != nil {
		return protocol.Status{}, canonicalRemoteError(err, protocol.CodeMiSTerUnavailable)
	}
	return status, nil
}

func (s *Service) Stop(parent context.Context) (protocol.Status, error) {
	ctx, cancel := serviceTimeout(parent, s.requestTimeout)
	defer cancel()
	s.executionMu.Lock()
	hostOnly := s.activeExecution == ExecutionHostOnly
	s.executionMu.Unlock()
	if hostOnly {
		if s.hostExecutor == nil {
			return protocol.Status{}, canonicalError(protocol.CodeInternal, nil)
		}
		if err := s.hostExecutor.Stop(ctx); err != nil {
			return protocol.Status{}, canonicalError(protocol.CodeInternal, safeContextError(err))
		}
		s.executionMu.Lock()
		s.activeExecution, s.activeGameID, s.activeSystem = "", "", ""
		s.executionMu.Unlock()
		return protocol.Status{State: protocol.StateIdle}, nil
	}
	status, err := s.client.Stop(ctx)
	if err != nil {
		return protocol.Status{}, canonicalRemoteError(err, protocol.CodeMiSTerUnavailable)
	}
	return status, nil
}

func (s *Service) launchGame(ctx context.Context, game catalog.Game, progress ProgressFunc) (protocol.CachedLaunchResponse, bool, error) {
	root, ok := s.rootsByID[game.LibraryID]
	if !ok || root.System != game.System {
		return protocol.CachedLaunchResponse{}, false, canonicalError(protocol.CodeSourceUnavailable, nil)
	}
	matchesRoot, err := s.catalog.GameMatchesRoot(ctx, game, root)
	if err != nil {
		return protocol.CachedLaunchResponse{}, false, canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	if !matchesRoot {
		return protocol.CachedLaunchResponse{}, false, canonicalError(protocol.CodeSourceUnavailable, nil)
	}
	execution, err := s.resolveExecution(ctx, game)
	if err != nil {
		return protocol.CachedLaunchResponse{}, false, err
	}
	if execution == ExecutionHostOnly {
		return s.launchHostOnly(ctx, game, root, progress)
	}
	if game.Content != nil {
		identity := contentIdentityFromCatalog(*game.Content)
		if err := protocol.ValidateContentIdentity(identity); err != nil {
			return protocol.CachedLaunchResponse{}, false, canonicalError(protocol.CodeInternal, nil)
		}
		emitProgress(progress, "cache", "checking target cache")
		probe, err := s.probe(ctx, game.System, identity)
		if err != nil {
			return protocol.CachedLaunchResponse{}, false, err
		}
		if probe.Present {
			emitProgress(progress, "cache", "content is already cached")
			response, err := s.launchContent(ctx, game, identity, progress)
			return response, false, err
		}
		emitProgress(progress, "cache", "content is not cached")
	}

	if game.State != catalog.SourceStateAvailable || !game.RootOnline {
		return protocol.CachedLaunchResponse{}, false, canonicalError(catalog.SourceErrorCode(game), nil)
	}
	emitProgress(progress, "prepare", "preparing source content")
	prepared, err := s.preparer.Prepare(ctx, root, game)
	if err != nil {
		return protocol.CachedLaunchResponse{}, false, canonicalPreparationError(err)
	}
	if prepared == nil {
		return protocol.CachedLaunchResponse{}, false, canonicalError(protocol.CodeInternal, nil)
	}
	return s.launchPrepared(ctx, game, prepared, progress)
}

func (s *Service) launchHostOnly(ctx context.Context, game catalog.Game, root catalog.Root, progress ProgressFunc) (response protocol.CachedLaunchResponse, retry bool, resultErr error) {
	if s.hostExecutor == nil {
		return protocol.CachedLaunchResponse{}, false, canonicalError(protocol.CodeInternal, nil)
	}
	if game.State != catalog.SourceStateAvailable || !game.RootOnline {
		return protocol.CachedLaunchResponse{}, false, canonicalError(catalog.SourceErrorCode(game), nil)
	}
	emitProgress(progress, "prepare", "preparing source content")
	prepared, err := s.preparer.Prepare(ctx, root, game)
	if err != nil {
		return protocol.CachedLaunchResponse{}, false, canonicalPreparationError(err)
	}
	if prepared == nil {
		return protocol.CachedLaunchResponse{}, false, canonicalError(protocol.CodeInternal, nil)
	}
	if err := protocol.ValidateContentIdentity(prepared.Content); err != nil {
		_ = prepared.Remove()
		return protocol.CachedLaunchResponse{}, false, canonicalError(protocol.CodeInternal, nil)
	}
	emitProgress(progress, "launch", "launching host content")
	content, err := prepared.Open()
	if err != nil {
		_ = prepared.Remove()
		return protocol.CachedLaunchResponse{}, false, canonicalError(protocol.CodeInternal, nil)
	}
	launchStatus, err := s.hostExecutor.Launch(ctx, content, prepared.Content)
	_ = content.Close()
	if err != nil {
		_ = prepared.Remove()
		return protocol.CachedLaunchResponse{}, false, canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	// The adapter owns the process as soon as Launch succeeds. Record that
	// ownership before cleanup so a degraded cleanup error cannot orphan it.
	s.executionMu.Lock()
	s.activeExecution = ExecutionHostOnly
	s.activeGameID, s.activeSystem = game.ID, game.System
	s.executionMu.Unlock()
	if err := prepared.Remove(); err != nil {
		return protocol.CachedLaunchResponse{}, false, canonicalError(protocol.CodeInternal, romsource.ErrCleanupRetained)
	}
	gameID, system := game.ID, game.System
	_ = launchStatus
	return protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system}, Content: prepared.Content}, false, nil
}

func stringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
func systemPtr(value protocol.System) *protocol.System {
	if value == "" {
		return nil
	}
	return &value
}

func (s *Service) launchPrepared(ctx context.Context, game catalog.Game, prepared *romsource.Prepared, progress ProgressFunc) (response protocol.CachedLaunchResponse, retry bool, resultErr error) {
	defer func() {
		if err := prepared.Remove(); err != nil {
			response = protocol.CachedLaunchResponse{}
			retry = false
			if resultErr == nil {
				resultErr = canonicalError(protocol.CodeInternal, romsource.ErrCleanupRetained)
			} else {
				resultErr = errors.Join(resultErr, romsource.ErrCleanupRetained)
			}
		}
	}()

	if err := protocol.ValidateContentIdentity(prepared.Content); err != nil {
		return protocol.CachedLaunchResponse{}, false, canonicalError(protocol.CodeInternal, nil)
	}
	content := catalog.Content{SHA256: prepared.Content.SHA256, Size: prepared.Content.Size, Extension: prepared.Content.Extension}
	root := s.rootsByID[game.LibraryID]
	updated, err := s.catalog.CompareAndSetContent(ctx, game, root, content)
	if err != nil {
		return protocol.CachedLaunchResponse{}, false, canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	if !updated {
		return protocol.CachedLaunchResponse{}, true, nil
	}

	emitProgress(progress, "cache", "checking target cache")
	probe, err := s.probe(ctx, game.System, prepared.Content)
	if err != nil {
		return protocol.CachedLaunchResponse{}, false, err
	}
	if probe.Present {
		emitProgress(progress, "cache", "content is already cached")
		response, err := s.launchContent(ctx, game, prepared.Content, progress)
		return response, false, err
	}
	emitProgress(progress, "cache", "content is not cached")
	if err := s.uploadPrepared(ctx, game.System, prepared, progress); err != nil {
		return protocol.CachedLaunchResponse{}, false, err
	}
	response, err = s.launchContent(ctx, game, prepared.Content, progress)
	return response, false, err
}

func (s *Service) uploadPrepared(parent context.Context, system protocol.System, prepared *romsource.Prepared, progress ProgressFunc) (resultErr error) {
	file, err := prepared.Open()
	if err != nil {
		return canonicalError(protocol.CodeTransferFailed, nil)
	}
	var uploadBody io.Reader = file
	if s.uploadReadDelay > 0 {
		uploadBody = &throttledReader{Reader: file, delay: s.uploadReadDelay}
	}
	body := &progressReader{Reader: uploadBody, onFirstRead: func() {
		emitProgress(progress, "upload-started", "upload body read")
	}}
	defer func() {
		if err := body.Close(); err != nil && resultErr == nil {
			resultErr = canonicalError(protocol.CodeTransferFailed, nil)
		}
	}()

	emitProgress(progress, "upload", "uploading prepared content")
	ctx, cancel := serviceTimeout(parent, s.uploadTimeout)
	defer cancel()
	response, err := s.client.UploadContent(ctx, system, prepared.Content, body)
	if err != nil {
		return canonicalRemoteError(err, protocol.CodeTransferFailed)
	}
	if (response.Result != protocol.CacheUploadPresent && response.Result != protocol.CacheUploadCreated) ||
		response.System != system || response.Content != prepared.Content {
		return canonicalError(protocol.CodeInternal, nil)
	}
	return nil
}

func (s *Service) probe(parent context.Context, system protocol.System, identity protocol.ContentIdentity) (protocol.CacheProbeResponse, error) {
	ctx, cancel := serviceTimeout(parent, s.requestTimeout)
	defer cancel()
	response, err := s.client.ProbeContent(ctx, system, identity)
	if err != nil {
		return protocol.CacheProbeResponse{}, canonicalRemoteError(err, protocol.CodeMiSTerUnavailable)
	}
	if !validServiceProbe(response, system, identity) {
		return protocol.CacheProbeResponse{}, canonicalError(protocol.CodeInternal, nil)
	}
	return response, nil
}

func (s *Service) launchContent(parent context.Context, game catalog.Game, identity protocol.ContentIdentity, progress ProgressFunc) (protocol.CachedLaunchResponse, error) {
	emitProgress(progress, "launch", "launching cached content")
	ctx, cancel := serviceTimeout(parent, s.requestTimeout)
	defer cancel()
	request := protocol.CachedLaunchRequest{GameID: game.ID, System: game.System, Content: identity}
	response, err := s.client.LaunchContent(ctx, request)
	if err != nil {
		return protocol.CachedLaunchResponse{}, canonicalRemoteError(err, protocol.CodeMiSTerUnavailable)
	}
	if !validServiceLaunch(response, request) {
		return protocol.CachedLaunchResponse{}, canonicalError(protocol.CodeInternal, nil)
	}
	return response, nil
}

func validServiceLaunch(response protocol.CachedLaunchResponse, request protocol.CachedLaunchRequest) bool {
	spec, ok := core.DefaultRegistry().Lookup(request.System)
	return ok && response.Content == request.Content &&
		response.Status.State == protocol.StateActive &&
		response.Status.GameID != nil && *response.Status.GameID == request.GameID &&
		response.Status.System != nil && *response.Status.System == request.System &&
		response.Status.ExpectedCore != nil && *response.Status.ExpectedCore == spec.ExpectedCore &&
		response.Status.ObservedCore != nil && *response.Status.ObservedCore == spec.ExpectedCore &&
		response.Status.LastError == nil
}

func validServiceProbe(response protocol.CacheProbeResponse, system protocol.System, identity protocol.ContentIdentity) bool {
	if !response.Present {
		return response.System == nil && response.Content == nil
	}
	return response.System != nil && *response.System == system && response.Content != nil && *response.Content == identity
}

func contentIdentityFromCatalog(content catalog.Content) protocol.ContentIdentity {
	return protocol.ContentIdentity{SHA256: content.SHA256, Size: content.Size, Extension: content.Extension}
}

func serviceTimeout(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, timeout)
}

func emitProgress(progress ProgressFunc, stage, message string) {
	if progress != nil {
		progress(Progress{Stage: stage, Message: message})
	}
}

type throttledReader struct {
	io.Reader
	delay time.Duration
}

func (r *throttledReader) Read(p []byte) (int, error) {
	if r.delay > 0 {
		time.Sleep(r.delay)
	}
	return r.Reader.Read(p)
}

func (r *throttledReader) Close() error {
	closer, ok := r.Reader.(io.Closer)
	if !ok {
		return nil
	}
	return closer.Close()
}

type progressReader struct {
	io.Reader
	onFirstRead func()
	once        sync.Once
	closeOnce   sync.Once
	closeErr    error
}

func (r *progressReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if n > 0 {
		r.once.Do(r.onFirstRead)
	}
	return n, err
}

func (r *progressReader) Close() error {
	r.closeOnce.Do(func() {
		closer, ok := r.Reader.(io.Closer)
		if ok {
			r.closeErr = closer.Close()
		}
	})
	return r.closeErr
}

func canonicalRemoteError(err error, fallback protocol.ErrorCode) error {
	var apiErr *protocol.APIError
	if errors.As(err, &apiErr) {
		return canonicalError(apiErr.Code, safeContextError(err))
	}
	return canonicalError(fallback, safeContextError(err))
}

func canonicalPreparationError(err error) error {
	var prepareErr *romsource.Error
	if errors.As(err, &prepareErr) {
		return canonicalError(prepareErr.Code, safePreparationCause(err))
	}
	return canonicalError(protocol.CodeSourceUnavailable, safePreparationCause(err))
}

func safePreparationCause(err error) error {
	causes := []error{safeContextError(err)}
	if errors.Is(err, romsource.ErrCleanupRetained) {
		causes = append(causes, romsource.ErrCleanupRetained)
	}
	return errors.Join(causes...)
}

func canonicalError(code protocol.ErrorCode, cause error) error {
	message := "FogCast operation failed"
	switch code {
	case protocol.CodeBadRequest:
		message = "FogCast request is invalid"
	case protocol.CodeUnauthorized:
		message = "target authentication failed"
	case protocol.CodeROMNotFound:
		message = "catalog game was not found"
	case protocol.CodeBusy:
		message = "another launch or stop transition is running"
	case protocol.CodeUnsupportedSystem:
		message = "game system is unsupported"
	case protocol.CodeInvalidROMPath:
		message = "target ROM path is invalid"
	case protocol.CodeSourceUnavailable:
		message = "game source is unavailable"
	case protocol.CodeInvalidArchive:
		message = "game archive is invalid"
	case protocol.CodeTransferFailed:
		message = "content transfer failed"
	case protocol.CodeDigestMismatch:
		message = "content digest does not match its identity"
	case protocol.CodeContentNotCached:
		message = "content is not present in the verified target cache"
	case protocol.CodeCacheFull:
		message = "target cache has insufficient safe capacity"
	case protocol.CodeMiSTerUnavailable:
		message = "MiSTer is unavailable"
	case protocol.CodeCoreTimeout:
		message = "core transition timed out"
	case protocol.CodeUnrecognizedCore:
		message = "active core is unrecognized"
	case protocol.CodeInternal:
		message = "FogCast operation failed internally"
	default:
		code = protocol.CodeInternal
		message = "FogCast operation failed internally"
	}
	apiErr := &protocol.APIError{Code: code, Message: message}
	if cause == nil {
		return apiErr
	}
	return errors.Join(apiErr, cause)
}

func safeContextError(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded
	default:
		return nil
	}
}

func validatePrivateFilePath(path string) error {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("state file path must be clean and absolute")
	}
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&fs.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("state file path is not a regular file")
	}
	return nil
}

func ensurePrivateDirectory(path string) error {
	if err := validatePrivateDirectoryPath(path); err != nil {
		return err
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&fs.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("state directory is not a real directory")
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return err
	}
	info, err = os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 || info.Mode().Perm() != 0o700 {
		return errors.Join(err, errors.New("state directory is not private"))
	}
	return nil
}

func validatePrivateDirectoryPath(path string) error {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("state directory path must be clean and absolute")
	}
	if filepath.Dir(path) == path {
		return errors.New("filesystem root cannot be used as a state directory")
	}
	return nil
}

func ensurePrivateRegularFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&fs.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("state file is not a regular file")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return err
	}
	info, err = os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&fs.ModeSymlink != 0 || info.Mode().Perm() != 0o600 {
		return errors.Join(err, errors.New("state file is not private"))
	}
	return nil
}

func safeOpenError(message string, err error) error {
	if cause := safeContextError(err); cause != nil {
		return errors.Join(errors.New(message), cause)
	}
	return errors.New(message)
}
