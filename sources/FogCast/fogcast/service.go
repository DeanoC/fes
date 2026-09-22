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
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"

	"github.com/DeanoC/FogCast/internal/hostexec"
	"github.com/DeanoC/FogCast/internal/systems"
	"github.com/DeanoC/FogCast/librarymedia"
	"github.com/DeanoC/FogCast/libraryuser"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/romsource"
	"github.com/DeanoC/FogCast/targetclient"
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
	QueryGames(context.Context, catalog.Query) (catalog.Page, error)
	Platforms(context.Context) ([]catalog.PlatformInfo, error)
	GamesByIDs(context.Context, []string) ([]catalog.Game, error)
	GameMatchesRoot(context.Context, catalog.Game, catalog.Root) (bool, error)
	Close() error
}

type serviceScanner interface {
	Scan(context.Context, []catalog.Root) (catalog.ScanReport, error)
	SetAdmissionGate(func(context.Context) (func(), error))
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
	Health(context.Context) (protocol.Health, error)
	Status(context.Context) (protocol.Status, error)
	Stop(context.Context) (protocol.Status, error)
}

type developmentRBFClient interface {
	LoadDevelopmentRBF(context.Context, int64, io.Reader) (protocol.Status, error)
}

type corePackageClient interface {
	LoadCore(context.Context, int64, io.Reader) (protocol.Status, error)
}

type developmentRecoveryClient interface {
	RebootDevelopment(context.Context) (protocol.Status, error)
}

type castClient interface {
	CastStart(context.Context, string, string, uint64) (targetclient.CastStatus, error)
	CastStop(context.Context, string, uint64) (targetclient.CastStatus, error)
}

type mediaCastClient interface {
	castClient
	CastStartWithMedia(context.Context, string, string, uint64, protocol.CastMediaSet) (targetclient.CastStatus, error)
}

const (
	ExecutionFPGANative      = "fpga_native"
	ExecutionFPGADevelopment = "fpga_development"
	ExecutionHostOnly        = string(systems.CapabilityHostOnly)
	lostLaunchPollInterval   = 25 * time.Millisecond
	coreLoadReconcileTimeout = 2 * time.Second
)

// ExecutionResolver is a service-level policy seam.
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

func WithUserLibrary(store *libraryuser.Store) ServiceOption {
	return func(service *Service) { service.users = store }
}

func WithLibraryOverlayPath(path string) ServiceOption {
	return func(service *Service) { service.libraryOverlayPath = path }
}

func WithConfigPath(path string) ServiceOption {
	return func(service *Service) { service.configPath = path }
}

func withTargetClientFactory(factory func(TargetConfig) (serviceClient, error)) ServiceOption {
	return func(service *Service) { service.targetClientFactory = factory }
}

func WithLibraryMedia(index *librarymedia.Index) ServiceOption {
	return func(service *Service) { service.media = index }
}

func WithExecutionPolicy(policy ExecutionPolicy) ServiceOption {
	return func(service *Service) {
		if policy.Resolver != nil {
			service.executionResolver = policy.Resolver
		}
		service.hostExecutor = policy.Host
	}
}

type Service struct {
	targetReset    func()
	connectionMu   sync.Mutex
	connection     TargetConnection
	resolveTarget  func(context.Context, string) ([]string, error)
	lookupCancel   context.CancelFunc
	monitorCancel  context.CancelFunc
	monitorDone    chan struct{}
	nextLookup     time.Time
	lookupFailures uint

	stoppedKitLease         *targetclient.KitLease
	closeKitLeases          func(context.Context) error
	corePackages            *corepackage.Store
	activePackageID         string
	activePackageGeneration uint64
	packageRejection        *protocol.APIError
	catalog                 serviceCatalog
	scanner                 serviceScanner
	preparer                servicePreparer
	roots                   []catalog.Root
	rootsByID               map[string]catalog.Root
	configPath              string
	configWriteMu           sync.Mutex
	targets                 []TargetConfig
	selectedTarget          string
	targetClients           map[string]serviceClient
	targetClientFactory     func(TargetConfig) (serviceClient, error)
	// targetOrigin rebinds input/media to the selected target. It must not call
	// back into Service (same rule as targetReset).
	targetOrigin             func(TargetConfig)
	targetMu                 sync.RWMutex
	requestTimeout           time.Duration
	uploadTimeout            time.Duration
	coreLoadReconcileTimeout time.Duration
	uploadReadDelay          time.Duration
	executionResolver        ExecutionResolver
	hostExecutor             hostexec.Adapter
	users                    *libraryuser.Store
	media                    *librarymedia.Index
	libraryOverlayPath       string
	librarySettingsOnce      sync.Once
	librarySettingsAdmission chan struct{}
	libraryMu                sync.RWMutex
	attractIdle              int
	preferredRegions         []string
	hostEmulator             HostEmulatorConfig
	metadataRoot             string
	metadataScope            string
	watchRoot                string
	folderWatchInterval      time.Duration
	folderWatchFailureMu     sync.Mutex
	folderWatchFailureCount  int
	catalogAdmission         chan struct{}
	scanMu                   sync.Mutex
	scanWG                   sync.WaitGroup
	closing                  bool
	catalogCloseWait         time.Duration
	activeExecution          string
	activeTarget             string
	activeGameID             string
	activeSystem             protocol.System
	plays                    map[string]targetPlay
	selectedTargetReconciled bool
	// selectedTargetRepairAllowed permits one same-target connection repair after status is unreachable.
	selectedTargetRepairAllowed bool
	lifecycleOnce               sync.Once
	lifecycleAdmission          chan struct{}
	executionMu                 sync.Mutex
	closeOnce                   sync.Once
	catalogCloseOnce            sync.Once
	catalogCloseErr             error
	closeErr                    error

	romCacheMu   sync.Mutex
	romCacheSnap romCacheSnapshot
}

const catalogCloseScanTimeout = 2 * time.Second

var (
	errCatalogClosing            = errors.New("catalog is closing")
	errFolderWatchRootUnresolved = errors.New("folder watch root is unresolved")
)

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
	var users *libraryuser.Store
	var media *librarymedia.Index
	fail := func(message string, cause error) (*Service, error) {
		if media != nil {
			_ = media.Close()
			media = nil
		}
		if users != nil {
			_ = users.Close()
			users = nil
		}
		_ = store.Close()
		return nil, safeOpenError(message, cause)
	}
	if err := ensurePrivateRegularFile(paths.Index); err != nil {
		return fail("secure FogCast catalog", err)
	}
	if err := ensurePrivateDirectory(paths.Staging); err != nil {
		return fail("prepare FogCast staging", err)
	}
	scanner := &catalog.Scanner{Store: store, Platforms: catalog.DefaultPlatforms()}
	preparer := &romsource.Preparer{StagingRoot: paths.Staging, MaxBytes: protocol.MaxContentBytes}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	operationClient := *httpClient
	operationClient.Timeout = 0
	var leaseMu sync.Mutex
	var leases []*targetclient.KitLease
	targetClientFactory := func(target TargetConfig) (serviceClient, error) {
		if !target.Enabled {
			return nil, nil
		}
		baseURL, err := url.Parse(target.Address)
		if err != nil {
			return nil, err
		}
		owner, _ := os.Hostname()
		lease := targetclient.NewKitLease(baseURL, target.Agent, &operationClient, "fogcast@"+owner, "interactive game/development session")
		leaseMu.Lock()
		leases = append(leases, lease)
		leaseMu.Unlock()
		return targetclient.NewClient(baseURL, target.Agent, &operationClient).WithKitLease(lease), nil
	}
	selectedTarget := targetByName(config.Targets, config.SelectedTarget)
	client, err := targetClientFactory(selectedTarget)
	if err != nil {
		return fail("configure FogCast target", err)
	}
	options := []ServiceOption{WithConfigPath(paths.Config), withTargetClientFactory(targetClientFactory), func(s *Service) {
		s.closeKitLeases = func(ctx context.Context) error {
			leaseMu.Lock()
			defer leaseMu.Unlock()
			var errs []error
			for _, lease := range leases {
				errs = append(errs, lease.Close(ctx))
			}
			return errors.Join(errs...)
		}
	}}
	if config.HostEmulator.Binary != "" {
		cores := make(map[protocol.System]string, len(config.HostEmulator.Cores))
		for _, entry := range config.HostEmulator.Cores {
			cores[entry.Platform] = entry.Core
		}
		host := hostexec.NewRetroArchAdapterWithCores(config.HostEmulator.Binary, config.HostEmulator.Core, cores, nil)
		options = append(options, WithExecutionPolicy(ExecutionPolicy{Resolver: NewConfiguredExecutionResolver(config.HostEmulator.LaunchPlatforms(), host), Host: host}))
	}
	if paths.UserLibrary != "" {
		if err := validatePrivateFilePath(paths.UserLibrary); err != nil {
			return fail("prepare FogCast user library location", err)
		}
		if err := ensurePrivateDirectory(filepath.Dir(paths.UserLibrary)); err != nil {
			return fail("prepare FogCast user library location", err)
		}
		openedUsers, err := libraryuser.OpenContext(ctx, paths.UserLibrary)
		if err != nil {
			return fail("open FogCast user library", err)
		}
		users = openedUsers
		if err := ensurePrivateRegularFile(paths.UserLibrary); err != nil {
			return fail("secure FogCast user library", err)
		}
		options = append(options, WithUserLibrary(users))
	}
	if overlayPath := libraryOverlayPath(paths); overlayPath != "" {
		if err := validatePrivateFilePath(overlayPath); err != nil {
			return fail("prepare FogCast library settings location", err)
		}
		if err := ensurePrivateDirectory(filepath.Dir(overlayPath)); err != nil {
			return fail("prepare FogCast library settings location", err)
		}
		if _, err := os.Lstat(overlayPath); err == nil {
			if err := ensurePrivateRegularFile(overlayPath); err != nil {
				return fail("secure FogCast library settings", err)
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			return fail("prepare FogCast library settings location", err)
		}
		options = append(options, WithLibraryOverlayPath(overlayPath))
	}
	if paths.MediaIndex != "" && len(config.LibraryMedia) > 0 {
		if err := validatePrivateFilePath(paths.MediaIndex); err != nil {
			return fail("prepare FogCast media index location", err)
		}
		if err := ensurePrivateDirectory(filepath.Dir(paths.MediaIndex)); err != nil {
			return fail("prepare FogCast media index location", err)
		}
		if paths.MediaCache != "" {
			if err := ensurePrivateDirectory(paths.MediaCache); err != nil {
				return fail("prepare FogCast media cache", err)
			}
		}
		openedMedia, err := librarymedia.Open(ctx, paths.MediaIndex, paths.MediaCache, config.LibraryMedia)
		if err != nil {
			return fail("open FogCast media index", err)
		}
		media = openedMedia
		options = append(options, WithLibraryMedia(media))
	}
	service := newService(config, paths, store, scanner, preparer, client, options...)
	packageRoot := paths.CorePackages
	if packageRoot == "" {
		packageRoot = filepath.Join(filepath.Dir(paths.Index), "core-packages")
	}
	service.corePackages, err = corepackage.NewStore(packageRoot)
	if err != nil {
		return fail("open installed core packages", err)
	}

	if err := service.retireSupersededLibraries(ctx); err != nil {
		return fail("retire superseded mapped libraries", err)
	}
	service.startTargetMonitor()
	return service, nil
}

func newService(config Config, paths Paths, store serviceCatalog, scanner serviceScanner, preparer servicePreparer, client serviceClient, options ...ServiceOption) *Service {
	roots := append([]catalog.Root(nil), config.Libraries...)
	rootsByID := make(map[string]catalog.Root, len(roots))
	for _, root := range roots {
		rootsByID[root.ID] = root
	}
	service := &Service{
		catalog: store, scanner: scanner, preparer: preparer,
		roots: roots, rootsByID: rootsByID,
		targets: append([]TargetConfig(nil), config.Targets...), selectedTarget: config.SelectedTarget,
		targetClients:  make(map[string]serviceClient),
		plays:          make(map[string]targetPlay),
		requestTimeout: config.RequestTimeout, uploadTimeout: config.UploadTimeout,
		coreLoadReconcileTimeout: coreLoadReconcileTimeout,
		executionResolver:        defaultExecutionResolver{},
		attractIdle:              config.Library.AttractIdleSeconds,
		preferredRegions:         append([]string(nil), config.Library.PreferredRegions...),
		hostEmulator:             config.HostEmulator,
		metadataRoot:             paths.MetadataRoot,
		metadataScope:            config.Metadata.ClientID,
		watchRoot:                strings.TrimSpace(config.Library.WatchRoot),
		catalogAdmission:         make(chan struct{}, 1),
	}
	if len(service.targets) == 0 {
		enabled := strings.TrimSpace(config.BaseURL) != "" && strings.TrimSpace(config.Token) != ""
		service.targets = []TargetConfig{{Name: "dev", Enabled: enabled, Address: config.BaseURL, Agent: config.Token}}
		service.selectedTarget = "dev"
	}
	if client != nil {
		service.targetClients[service.selectedTarget] = client
	}
	service.catalogAdmission <- struct{}{}
	scanner.SetAdmissionGate(service.acquireCatalogAdmission)
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	service.selectedTargetReconciled = !targetByName(service.targets, service.selectedTarget).Enabled
	service.applyPersistedLibraryOverlay()
	return service
}

func (s *Service) selectedClientSnapshot() (serviceClient, bool) {
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	return s.selectedClientLocked()
}

func (s *Service) selectedClientLocked() (serviceClient, bool) {
	client := s.targetClients[s.sessionTargetNameLocked()]
	return client, client != nil
}

// Caller holds targetMu. Admission and transport must resolve the same binding.
func (s *Service) sessionTargetNameLocked() string {
	name := s.selectedTarget
	s.executionMu.Lock()
	if s.activeTarget != "" && s.activeExecution != ExecutionHostOnly {
		name = s.activeTarget
	}
	s.executionMu.Unlock()
	return name
}

type targetPlay struct {
	execution string
	gameID    string
	system    protocol.System
}

type PlaySession struct {
	Target    string
	TargetID  string
	Execution string
	GameID    string
	System    protocol.System
}

func (s *Service) retainSessionTargetLocked() {
	if s.activeTarget == "" {
		s.activeTarget = s.selectedTarget
	}
	if s.activeExecution != ExecutionFPGANative && s.activeExecution != ExecutionFPGADevelopment {
		return
	}
	if s.plays == nil {
		s.plays = make(map[string]targetPlay)
	}
	s.plays[s.activeTarget] = targetPlay{execution: s.activeExecution, gameID: s.activeGameID, system: s.activeSystem}
}

func (s *Service) clearForegroundPlayLocked() {
	if s.activeTarget != "" {
		delete(s.plays, s.activeTarget)
	}
	s.activeExecution, s.activeTarget, s.activeGameID, s.activeSystem = "", "", "", ""
	s.packageRejection = nil
	s.activePackageID, s.activePackageGeneration = "", 0
	for name, play := range s.plays {
		s.activeTarget = name
		s.activeExecution = play.execution
		s.activeGameID = play.gameID
		s.activeSystem = play.system
		break
	}
}

func (s *Service) PlaySessions() []PlaySession {
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	s.executionMu.Lock()
	defer s.executionMu.Unlock()
	out := make([]PlaySession, 0, len(s.plays))
	for name, play := range s.plays {
		out = append(out, PlaySession{
			Target:    name,
			TargetID:  targetByName(s.targets, name).TargetID,
			Execution: play.execution,
			GameID:    play.gameID,
			System:    play.system,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Target < out[j].Target })
	return out
}

func (s *Service) clearUnstartedSessionTarget() {
	s.executionMu.Lock()
	defer s.executionMu.Unlock()
	if s.activeExecution == "" {
		s.activeTarget = ""
		return
	}
	if _, ok := s.plays[s.activeTarget]; ok {
		return
	}
	s.activeTarget = ""
	for name, play := range s.plays {
		s.activeTarget = name
		s.activeExecution = play.execution
		s.activeGameID = play.gameID
		s.activeSystem = play.system
		break
	}
}

func (s *Service) bindLaunchTarget(target string) error {
	s.targetMu.Lock()
	defer s.targetMu.Unlock()
	explicit := strings.TrimSpace(target) != ""
	name := strings.TrimSpace(target)
	if name == "" {
		name = s.selectedTarget
	}
	cfg := targetByName(s.targets, name)
	if explicit && (strings.TrimSpace(cfg.Name) == "" || !cfg.Enabled) {
		return canonicalError(protocol.CodeBadRequest, nil)
	}
	if _, ok := s.targetClients[name]; !ok {
		if !explicit {
			s.executionMu.Lock()
			s.activeTarget = name
			s.executionMu.Unlock()
			return nil
		}
		if s.targetClientFactory == nil {
			return canonicalError(protocol.CodeInternal, nil)
		}
		client, err := s.targetClientFactory(cfg)
		if err != nil || client == nil {
			return canonicalError(protocol.CodeBadRequest, nil)
		}
		s.targetClients[name] = client
	}
	s.executionMu.Lock()
	s.activeTarget = name
	s.executionMu.Unlock()
	if s.targetOrigin != nil && explicit && name != s.selectedTarget {
		s.targetOrigin(cfg)
	}
	return nil
}

func (s *Service) libraryRootsSnapshot() []catalog.Root {
	s.libraryMu.RLock()
	defer s.libraryMu.RUnlock()
	return append([]catalog.Root(nil), s.roots...)
}

func (s *Service) libraryRoot(id string) (catalog.Root, bool) {
	s.libraryMu.RLock()
	defer s.libraryMu.RUnlock()
	root, ok := s.rootsByID[id]
	return root, ok
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

// DevelopmentActive reconstructs development ownership from the selected
// target after a host restart, when no local execution marker exists yet.
func (s *Service) DevelopmentActive(ctx context.Context) (bool, error) {
	development, _, err := s.DevelopmentSessionState(ctx)
	return development, err
}

func (s *Service) ActivePackageOwned() bool {
	s.executionMu.Lock()
	defer s.executionMu.Unlock()
	return s.activePackageID != ""
}

// DevelopmentSessionState returns both development admission and execution
// ownership reconstructed by the same authoritative target observation.
func (s *Service) DevelopmentSessionState(ctx context.Context) (bool, string, error) {
	s.executionMu.Lock()
	execution := s.activeExecution
	s.executionMu.Unlock()
	if execution == ExecutionFPGADevelopment {
		return true, execution, nil
	}
	if execution != "" {
		return false, execution, nil
	}
	if _, err := s.Status(ctx); err != nil {
		return false, "", err
	}
	s.executionMu.Lock()
	execution = s.activeExecution
	s.executionMu.Unlock()
	return execution == ExecutionFPGADevelopment, execution, nil
}

// RecognizedPlayABI reports whether a format-2 package ABI is a normal FES
// play profile. Unknown or empty ABIs stay on the Diagnostic development path.
func RecognizedPlayABI(id string, major, minor int64) bool {
	switch id {
	case "fes.simple-computer", "fes.simple-game", "fes.application":
		return major == 1 && minor == 0
	default:
		return false
	}
}

func recognizedPlayContract(abi protocol.RuntimeContract) bool {
	return RecognizedPlayABI(abi.ID, int64(abi.Major), int64(abi.Minor))
}

func (s *Service) resolveExecution(ctx context.Context, game catalog.Game) (string, error) {
	if game.Kind == catalog.SourceKindCorePackage {
		if s.corePackageHasRecognizedPlayABI(ctx, game.ID) {
			return ExecutionFPGANative, nil
		}
		return ExecutionFPGADevelopment, nil
	}
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

func (s *Service) corePackageHasRecognizedPlayABI(ctx context.Context, gameID string) bool {
	if s.corePackages == nil {
		return false
	}
	store, ok := s.catalog.(coreEntryCatalog)
	if !ok {
		return false
	}
	entry, err := store.CoreEntry(ctx, gameID)
	if err != nil {
		return false
	}
	inspection, _, err := s.corePackages.Read(ctx, entry.PackageID)
	if err != nil {
		return false
	}
	abi := inspection.Descriptor.ABI
	return RecognizedPlayABI(abi.ID, abi.Major, abi.Minor)
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
	return s.LaunchOn(ctx, gameID, "", progress)
}

func (s *Service) LaunchOn(ctx context.Context, gameID, target string, progress ProgressFunc) (protocol.CachedLaunchResponse, error) {
	if store, ok := s.catalog.(coreEntryCatalog); ok {
		if _, err := store.CoreEntry(ctx, gameID); err == nil {
			return s.launchCoreEntry(ctx, gameID, target)
		} else if !errors.Is(err, catalog.ErrCoreEntryNotFound) {
			return protocol.CachedLaunchResponse{}, mapCoreEntryError(err)
		}
	}

	if err := ctx.Err(); err != nil {
		return protocol.CachedLaunchResponse{}, err
	}
	if err := protocol.ValidateGameID(gameID); err != nil {
		return protocol.CachedLaunchResponse{}, canonicalError(protocol.CodeBadRequest, nil)
	}
	releaseLifecycle, err := s.acquireLifecycle(ctx)
	if err != nil {
		return protocol.CachedLaunchResponse{}, err
	}
	defer releaseLifecycle()
	// Reject retired FPGA requests before stopping or rebinding a live package.
	admittedGame, admissionErr := s.catalog.Game(ctx, gameID)
	if admissionErr != nil {
		if errors.Is(admissionErr, sql.ErrNoRows) {
			return protocol.CachedLaunchResponse{}, canonicalError(protocol.CodeROMNotFound, nil)
		}
		if ctx.Err() != nil {
			return protocol.CachedLaunchResponse{}, ctx.Err()
		}
		return protocol.CachedLaunchResponse{}, canonicalError(protocol.CodeInternal, safeContextError(admissionErr))
	}
	if err := s.nativeCatalogAdmission(ctx, admittedGame); err != nil {
		return protocol.CachedLaunchResponse{}, err
	}

	s.executionMu.Lock()
	packageOwnerTarget := ""
	if s.activePackageID != "" {
		packageOwnerTarget = s.activeTarget
	}
	s.executionMu.Unlock()
	if err := s.bindLaunchTarget(target); err != nil {
		return protocol.CachedLaunchResponse{}, err
	}
	defer s.clearUnstartedSessionTarget()
	if s.protocolAdmissionEnabled() {
		execution, err := s.SessionExecution(ctx, gameID)
		if err != nil {
			return protocol.CachedLaunchResponse{}, err
		}
		if execution != ExecutionHostOnly {
			if _, err := s.refreshTargetAdmission(ctx); err != nil {
				return protocol.CachedLaunchResponse{}, canonicalRemoteError(err, protocol.CodeMiSTerUnavailable)
			}
		}
	}
	if err := s.incompatibleTargetError(); err != nil {
		return protocol.CachedLaunchResponse{}, err
	}

	if err := s.stopPackageOwnedForCatalogLaunch(ctx, packageOwnerTarget); err != nil {
		return protocol.CachedLaunchResponse{}, err
	}
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	for attempt := 0; attempt < 2; attempt++ {
		game := admittedGame
		var err error
		if attempt > 0 {
			game, err = s.catalog.Game(ctx, gameID)
		}
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
			if err == nil && response.Status.State == protocol.StateActive {
				s.executionMu.Lock()
				if s.activeExecution != ExecutionHostOnly {
					s.activeExecution = ExecutionFPGANative
					s.activeGameID, s.activeSystem = game.ID, game.System
					s.retainSessionTargetLocked()
					s.packageRejection = nil
					s.activePackageID, s.activePackageGeneration = "", 0
				}
				s.executionMu.Unlock()
			}
			return response, err
		}
	}
	return protocol.CachedLaunchResponse{}, canonicalError(protocol.CodeSourceUnavailable, nil)
}

func (s *Service) Close() error {
	s.closeOnce.Do(func() {
		if s.monitorCancel != nil {
			s.monitorCancel()
		}
		s.cancelTargetLookup()
		if s.monitorDone != nil {
			<-s.monitorDone
		}
		s.scanMu.Lock()
		s.closing = true
		s.scanMu.Unlock()
		var first error
		if s.closeKitLeases != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			first = s.closeKitLeases(ctx)
			cancel()
		}
		if s.media != nil {
			if err := s.media.Close(); first == nil {
				first = err
			}
		}
		if s.users != nil {
			if err := s.users.Close(); first == nil {
				first = err
			}
		}
		if s.waitForCatalogScan() {
			if err := s.closeCatalog(); first == nil {
				first = err
			}
		} else {
			go func() {
				s.scanWG.Wait()
				_ = s.closeCatalog()
			}()
		}
		s.closeErr = first
	})
	if s.closeErr != nil {
		return canonicalError(protocol.CodeInternal, nil)
	}
	return nil
}

func (s *Service) catalogCloseTimeout() time.Duration {
	if s.catalogCloseWait > 0 {
		return s.catalogCloseWait
	}
	return catalogCloseScanTimeout
}

func (s *Service) beginCatalogScan() bool {
	s.scanMu.Lock()
	defer s.scanMu.Unlock()
	if s.closing {
		return false
	}
	s.scanWG.Add(1)
	return true
}

func (s *Service) endCatalogScan() {
	s.scanWG.Done()
}

func (s *Service) closeCatalog() error {
	s.catalogCloseOnce.Do(func() {
		s.catalogCloseErr = s.catalog.Close()
	})
	return s.catalogCloseErr
}

func (s *Service) waitForCatalogScan() bool {
	done := make(chan struct{})
	go func() {
		s.scanWG.Wait()
		close(done)
	}()
	timer := time.NewTimer(s.catalogCloseTimeout())
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	}
}

func (s *Service) CastStart(ctx context.Context, session, token string, generation uint64) (targetclient.CastStatus, error) {
	target, available := s.selectedClientSnapshot()
	client, ok := target.(castClient)
	if !available || !ok {
		return targetclient.CastStatus{}, errors.New("target cast control is unavailable")
	}
	return client.CastStart(ctx, session, token, generation)
}

func (s *Service) CastStartWithMedia(ctx context.Context, session, token string, generation uint64, media protocol.CastMediaSet) (targetclient.CastStatus, error) {
	target, available := s.selectedClientSnapshot()
	client, ok := target.(mediaCastClient)
	if !available || !ok {
		return targetclient.CastStatus{}, errors.New("target cast control is unavailable")
	}
	return client.CastStartWithMedia(ctx, session, token, generation, media)
}

func (s *Service) CastStop(ctx context.Context, session string, generation uint64) (targetclient.CastStatus, error) {
	target, available := s.selectedClientSnapshot()
	client, ok := target.(castClient)
	if !available || !ok {
		return targetclient.CastStatus{}, errors.New("target cast control is unavailable")
	}
	return client.CastStop(ctx, session, generation)
}

func (s *Service) Scan(ctx context.Context) (catalog.ScanReport, error) {
	releaseSettings, err := s.acquireLibrarySettings(ctx)
	if err != nil {
		return catalog.ScanReport{}, err
	}
	defer releaseSettings()
	return s.scanLocked(ctx)
}

// scanLocked scans the published library roots while library settings admission is held.
func (s *Service) scanLocked(ctx context.Context) (catalog.ScanReport, error) {
	if err := ctx.Err(); err != nil {
		return catalog.ScanReport{}, err
	}
	if !s.beginCatalogScan() {
		return catalog.ScanReport{}, canonicalError(protocol.CodeInternal, errCatalogClosing)
	}
	defer s.endCatalogScan()
	if err := s.retireSupersededLibrariesWithAdmission(ctx); err != nil {
		if ctx.Err() != nil {
			return catalog.ScanReport{}, ctx.Err()
		}
		return catalog.ScanReport{}, canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	report, err := s.scanner.Scan(ctx, s.libraryRootsSnapshot())
	if err != nil {
		if ctx.Err() != nil {
			return catalog.ScanReport{}, ctx.Err()
		}
		return catalog.ScanReport{}, canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	_ = s.ScanMedia(ctx)
	return report, nil
}

// FolderWatchRoot returns the configured SMB share source of truth.
func (s *Service) FolderWatchRoot() string {
	return s.watchRoot
}

func (s *Service) folderWatchRoots() []catalog.Root {
	return FolderWatchRoots(s.libraryRootsSnapshot())
}

// ReconcileFolderWatch updates the host catalog from every configured root
// mapped by the system table. It does not scan unmapped systems or media.
func (s *Service) ReconcileFolderWatch(ctx context.Context) (catalog.ScanReport, error) {
	releaseSettings, err := s.acquireLibrarySettings(ctx)
	if err != nil {
		return catalog.ScanReport{}, err
	}
	defer releaseSettings()
	return s.reconcileFolderWatchLocked(ctx)
}

// reconcileFolderWatchLocked scans mapped roots while library settings admission is held.
func (s *Service) reconcileFolderWatchLocked(ctx context.Context) (catalog.ScanReport, error) {
	if err := ctx.Err(); err != nil {
		return catalog.ScanReport{}, err
	}
	roots := s.folderWatchRoots()
	if !s.beginCatalogScan() {
		return catalog.ScanReport{}, canonicalError(protocol.CodeInternal, errCatalogClosing)
	}
	defer s.endCatalogScan()
	if err := s.retireSupersededLibrariesWithAdmission(ctx); err != nil {
		if ctx.Err() != nil {
			return catalog.ScanReport{}, ctx.Err()
		}
		return catalog.ScanReport{}, canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	if len(roots) == 0 {
		return catalog.ScanReport{}, canonicalError(protocol.CodeInternal, errFolderWatchRootUnresolved)
	}
	report, err := s.scanner.Scan(ctx, roots)
	if err != nil {
		if ctx.Err() != nil {
			return catalog.ScanReport{}, ctx.Err()
		}
		return catalog.ScanReport{}, canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	return report, nil
}

type catalogLibraryRetirer interface {
	Libraries(context.Context) ([]catalog.Root, error)
	RebindLibrary(context.Context, catalog.Root) error
	RetireLibrary(context.Context, string) error
}

type catalogLibraryRootReleaser interface {
	ReleaseLibraryRoot(context.Context, catalog.Root) error
}

func (s *Service) retireSupersededLibraries(ctx context.Context) error {
	retirer, ok := s.catalog.(catalogLibraryRetirer)
	if !ok {
		return nil
	}
	keep := make(map[string]catalog.Root)
	keepByID := make(map[string]catalog.Root)
	keepByPath := make(map[string]catalog.Root)
	for _, root := range s.libraryRootsSnapshot() {
		keepByPath[root.Path] = root
		if strings.TrimSpace(root.ID) != "" {
			keepByID[root.ID] = root
			keep[root.ID] = root
		}
	}
	libraries, err := retirer.Libraries(ctx)
	if err != nil {
		return err
	}
	releaser, supportsRelease := retirer.(catalogLibraryRootReleaser)
	for _, library := range libraries {
		root, adopted := keepByPath[library.Path]
		if !adopted || library.ID == root.ID && library.System == root.System {
			root, adopted = keepByID[library.ID]
			adopted = adopted && library.System != root.System
		}
		if !adopted {
			continue
		}
		if !supportsRelease {
			return errors.New("catalog library does not support releasing an adopted root")
		}
		if err := releaser.ReleaseLibraryRoot(ctx, root); err != nil {
			return err
		}
	}
	for _, library := range libraries {
		root, ok := keep[library.ID]
		if !ok || library.Path == root.Path {
			continue
		}
		if err := retirer.RebindLibrary(ctx, root); err != nil {
			return err
		}
	}
	for _, library := range libraries {
		if _, ok := keep[library.ID]; ok {
			continue
		}
		if _, adopted := keepByPath[library.Path]; adopted {
			continue
		}
		if err := retirer.RetireLibrary(ctx, library.ID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) retireSupersededLibrariesWithAdmission(ctx context.Context) error {
	if _, ok := s.catalog.(catalogLibraryRetirer); !ok {
		return nil
	}
	release, err := s.acquireCatalogAdmission(ctx)
	if err != nil {
		return err
	}
	defer release()
	return s.retireSupersededLibraries(ctx)
}

// RunFolderWatch reconciles every configured table-mapped root immediately
// and then on a poll interval until ctx is cancelled. Polling is SMB-safe.
func (s *Service) RunFolderWatch(ctx context.Context) error {
	interval := s.folderWatchInterval
	if interval <= 0 {
		interval = catalog.DefaultFolderWatchInterval
	}
	watcher := catalog.FolderWatcher{
		Scan: func(ctx context.Context, _ []catalog.Root) (catalog.ScanReport, error) {
			return s.ReconcileFolderWatch(ctx)
		},
		Roots:    s.folderWatchRoots,
		Interval: interval,
		OnError:  s.noteFolderWatchReconcileFailure,
	}
	return watcher.Run(ctx)
}

func (s *Service) noteFolderWatchReconcileFailure() {
	s.folderWatchFailureMu.Lock()
	s.folderWatchFailureCount++
	s.folderWatchFailureMu.Unlock()
}

func (s *Service) acquireCatalogAdmission(ctx context.Context) (func(), error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.catalogAdmission:
		return func() { s.catalogAdmission <- struct{}{} }, nil
	}
}

func (s *Service) acquireLibrarySettings(ctx context.Context) (func(), error) {
	s.librarySettingsOnce.Do(func() {
		s.librarySettingsAdmission = make(chan struct{}, 1)
		s.librarySettingsAdmission <- struct{}{}
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.librarySettingsAdmission:
		return func() { s.librarySettingsAdmission <- struct{}{} }, nil
	}
}

// FolderWatchReconcileFailures returns how many non-cancel reconcile
// failures the poller has seen. Transient SMB errors increment this and
// the watcher keeps retrying.
func (s *Service) FolderWatchReconcileFailures() int {
	s.folderWatchFailureMu.Lock()
	defer s.folderWatchFailureMu.Unlock()
	return s.folderWatchFailureCount
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

func (s *Service) QueryGames(ctx context.Context, query catalog.Query) (catalog.Page, error) {
	if err := ctx.Err(); err != nil {
		return catalog.Page{}, err
	}
	if len(query.PreferredRegions) == 0 {
		query.PreferredRegions = s.currentPreferredRegions()
	}
	switch query.Collection {
	case "favorites":
		ids, err := s.favoriteIDs(ctx)
		if err != nil {
			return catalog.Page{}, err
		}
		query.Restrict = true
		query.RestrictIDs = ids
	case "recents":
		return s.queryRecents(ctx, query)
	case "continue":
		return s.queryContinue(ctx, query)
	case "unplayed":
		ids, err := s.playedIDs(ctx)
		if err != nil {
			return catalog.Page{}, err
		}
		query.ExcludeIDs = ids
	case "recently_added":
		if query.Sort == "" || query.Sort == catalog.SortTitle {
			query.Sort = catalog.SortAdded
		}
	case "":
	default:
		return s.queryCustomCollection(ctx, query)
	}
	page, err := s.catalog.QueryGames(ctx, query)
	if err != nil {
		if ctx.Err() != nil {
			return catalog.Page{}, ctx.Err()
		}
		if errors.Is(err, catalog.ErrInvalidQuery) {
			return catalog.Page{}, canonicalError(protocol.CodeBadRequest, nil)
		}
		return catalog.Page{}, canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	return page, nil
}

func (s *Service) PlatformLaunchable(system protocol.System) bool {
	if system == catalog.CorePlatform {
		return true
	}
	execution, err := s.resolveExecution(context.Background(), catalog.Game{System: system})
	if err == nil && execution == ExecutionHostOnly {
		return true
	}
	return false
}

func (s *Service) Platforms(ctx context.Context) ([]catalog.PlatformInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	platforms, err := s.catalog.Platforms(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	for index := range platforms {
		platforms[index].Launchable = s.PlatformLaunchable(platforms[index].ID)
	}
	return platforms, nil
}

func (s *Service) hostLaunchable(system protocol.System) bool {
	if s.hostExecutor == nil {
		return false
	}
	if s.hostEmulator.CoreFor(system) != "" {
		return true
	}
	execution, err := s.executionResolver.Resolve(context.Background(), catalog.Game{System: system})
	return err == nil && execution == ExecutionHostOnly
}

func (s *Service) Health(parent context.Context) (protocol.Health, error) {
	ctx, cancel := serviceTimeout(parent, s.requestTimeout)
	defer cancel()
	release, err := s.acquireLifecycle(ctx)
	if err != nil {
		return protocol.Health{}, err
	}
	defer release()
	health, err := s.refreshTargetConnection(ctx)
	if err != nil {
		return protocol.Health{}, canonicalRemoteError(err, protocol.CodeMiSTerUnavailable)
	}
	return health, nil
}

func (s *Service) LoadDevelopmentRBF(parent context.Context, size int64, content io.Reader) (protocol.Status, error) {
	if size <= 0 || size > protocol.MaxDevelopmentRBFBytes || content == nil {
		return protocol.Status{}, canonicalError(protocol.CodeBadRequest, nil)
	}
	ctx, cancel := serviceTimeout(parent, s.uploadTimeout)
	defer cancel()
	releaseLifecycle, err := s.acquireLifecycle(ctx)
	if err != nil {
		return protocol.Status{}, err
	}
	defer releaseLifecycle()
	if s.protocolAdmissionEnabled() {
		if _, err := s.refreshTargetAdmission(ctx); err != nil {
			return protocol.Status{}, canonicalRemoteError(err, protocol.CodeMiSTerUnavailable)
		}
	}
	if err := s.incompatibleTargetError(); err != nil {
		return protocol.Status{}, err
	}

	if err := s.stopHostOnlyIfActive(ctx); err != nil {
		return protocol.Status{}, err
	}
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	client, ok := s.selectedClientLocked()
	if !ok {
		return protocol.Status{}, canonicalError(protocol.CodeMiSTerUnavailable, nil)
	}
	developmentClient, ok := client.(developmentRBFClient)
	if !ok {
		return protocol.Status{}, canonicalError(protocol.CodeInternal, nil)
	}
	status, err := developmentClient.LoadDevelopmentRBF(ctx, size, content)
	if err != nil {
		targetDeadlineExpired := errors.Is(ctx.Err(), context.DeadlineExceeded) && parent.Err() == nil
		if targetDeadlineExpired && ambiguousTargetMutationError(err) {
			status, err = s.reconcileLostDevelopmentLoad(parent, client, err)
			if err != nil {
				return protocol.Status{}, err
			}
		} else {
			return protocol.Status{}, canonicalRemoteError(err, protocol.CodeTransferFailed)
		}
	}
	if !validServiceDevelopmentStatus(status) {
		return protocol.Status{}, canonicalError(protocol.CodeInternal, nil)
	}
	s.executionMu.Lock()
	s.activeExecution = ExecutionFPGADevelopment
	s.retainSessionTargetLocked()
	s.activeGameID, s.activeSystem = "", ""
	s.activePackageID, s.activePackageGeneration = "", 0
	s.packageRejection = nil
	s.selectedTargetReconciled = false
	s.selectedTargetRepairAllowed = false
	s.executionMu.Unlock()
	return status, nil
}

// LoadCore sends one package mutation without tearing down the current session
// first. Target admission therefore preserves a prior game/input session on a
// stale lease, invalid archive, or compatibility rejection.
func (s *Service) LoadCore(parent context.Context, size int64, content io.Reader) (protocol.Status, error) {
	if size <= 0 || size > corepackage.MaxArchiveSize || content == nil {
		return protocol.Status{}, corePackageRequestFailure(canonicalError(protocol.CodeBadRequest, nil))
	}
	return s.loadCore(parent, func(context.Context) (coreLoadSource, error) { return coreLoadSource{size: size, body: content}, nil })
}

func (s *Service) loadCore(parent context.Context, source func(context.Context) (coreLoadSource, error)) (protocol.Status, error) {
	ctx, cancel := serviceTimeout(parent, s.uploadTimeout)
	defer cancel()
	releaseLifecycle, err := s.acquireLifecycle(ctx)
	if err != nil {
		return protocol.Status{}, corePackageRequestFailure(err)
	}
	defer releaseLifecycle()

	return s.loadCoreLocked(ctx, parent, source)
}

// Caller holds lifecycle admission through package and optional media delivery.
func (s *Service) loadCoreLocked(ctx, parent context.Context, source func(context.Context) (coreLoadSource, error)) (protocol.Status, error) {
	// Resolve and validate the requested immutable package/media before even a
	// recovery Stop: an invalid next launch must preserve the retained owner.
	selected, err := source(ctx)
	if err != nil {
		return protocol.Status{}, corePackageRequestFailure(err)
	}
	s.executionMu.Lock()
	pendingRejection := s.packageRejection != nil
	s.executionMu.Unlock()
	if pendingRejection {
		if status, err := s.stopRejectedCore(ctx); err != nil {
			return status, err
		}
	}
	size, content := selected.size, selected.body
	if s.protocolAdmissionEnabled() {
		if _, err := s.refreshTargetAdmission(ctx); err != nil {
			return protocol.Status{}, corePackageRequestFailure(canonicalRemoteError(err, protocol.CodeMiSTerUnavailable))
		}
	}
	if err := s.incompatibleTargetError(); err != nil {
		return protocol.Status{}, corePackageRequestFailure(err)
	}
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	client, ok := s.selectedClientLocked()
	if !ok {
		return protocol.Status{}, corePackageRequestFailure(canonicalError(protocol.CodeMiSTerUnavailable, nil))
	}
	loader, ok := client.(corePackageClient)
	if !ok {
		return protocol.Status{}, corePackageRequestFailure(canonicalError(protocol.CodeUnsupportedOperation, nil))
	}
	prior, err := client.Status(ctx)
	if err != nil {
		return protocol.Status{}, corePackageRequestFailure(canonicalRemoteError(err, protocol.CodeMiSTerUnavailable))
	}
	var status protocol.Status
	if selected.entry != nil {
		library, ok := client.(libraryCoreClient)
		if !ok {
			return protocol.Status{}, corePackageRequestFailure(canonicalError(protocol.CodeUnsupportedOperation, nil))
		}
		if retainedIdleLaunchRecoveryCandidate(prior) {
			if err := s.validateLibraryCoreBeforeRecovery(ctx, selected.entry.PackageID, client); err != nil {
				return protocol.Status{}, err
			}
		}
		prior, err = s.recoverIdleLaunchError(ctx, client, prior)
		if err != nil {
			return protocol.Status{}, err
		}
		if selected.composition != nil {
			composed, ok := client.(interface {
				LoadComposedCore(context.Context, int64, io.Reader, string) (protocol.Status, error)
			})
			if !ok {
				return protocol.Status{}, corePackageRequestFailure(canonicalError(protocol.CodeUnsupportedOperation, nil))
			}
			status, err = composed.LoadComposedCore(ctx, size, content, selected.entry.PackageID)
		} else {
			status, err = library.LoadLibraryCore(ctx, size, content, selected.entry.PackageID)
		}
	} else {
		status, err = loader.LoadCore(ctx, size, content)
	}
	if err != nil {
		if corePackagePreMutationFailure(err) {
			return protocol.Status{}, preserveCorePackageError(retainedAdmissionDiagnostic(prior, err))
		}
		status, err = s.reconcileLostCoreLoad(parent, client, prior, err)
		if err != nil {
			if confirmedIdleCorePackageFailure(status, err) {
				return s.retireExecutionAfterConfirmedIdleCorePackageFailure(ctx, status, err)
			}
			return status, err
		}
	}
	if !validServiceCorePackageStatus(status) {
		return protocol.Status{}, canonicalError(protocol.CodeInternal, nil)
	}

	if selected.entry != nil && (status.CorePackage.PackageID != selected.entry.PackageID || !reflect.DeepEqual(status.CorePackage.Composition, selected.composition)) {
		rejection := &protocol.APIError{Code: protocol.CodeUnrecognizedCore, Message: "activated package differs from selected library package", Phase: "identity", Expected: selected.entry.PackageID, Observed: status.CorePackage.PackageID}
		recoveryCtx, recoveryCancel := serviceTimeout(parent, s.coreLoadReconcileTimeout)
		recovered, stopErr := client.Stop(recoveryCtx)
		if stopErr != nil || !validRecoveredDevelopmentStatus(recovered) || recovered.CorePackage != nil {
			recovered, stopErr = client.Status(recoveryCtx)
		}
		recoveryCancel()
		if stopErr == nil && validRecoveredDevelopmentStatus(recovered) && recovered.CorePackage == nil {
			recovered.LastError = rejection
			return s.retireExecutionAfterConfirmedIdleCorePackageFailure(ctx, recovered, rejection)
		}

		hostCleanupErr := s.stopHostOnlyIfActive(ctx)
		s.executionMu.Lock()
		if hostCleanupErr == nil {
			s.activeExecution = ExecutionFPGADevelopment
			s.retainSessionTargetLocked()
			s.activeGameID, s.activeSystem = "", ""
			s.activePackageID, s.activePackageGeneration = "", 0
		}
		s.packageRejection = rejection
		s.executionMu.Unlock()
		status.LastError = rejection
		if hostCleanupErr != nil {
			return status, &protocol.APIError{Code: protocol.CodeInternal, Message: "host cleanup failed after core package rejection", Phase: "recovery"}
		}
		return status, rejection
	}
	// Keep the previous host owner until its executor is confirmed stopped.
	// A successful target activation can still require recovery of both owners.
	if hostCleanupErr := s.stopHostOnlyIfActive(ctx); hostCleanupErr != nil {
		cleanupErr := &protocol.APIError{Code: protocol.CodeInternal, Message: "host cleanup failed after core package activation", Phase: "recovery"}
		s.executionMu.Lock()
		s.packageRejection = cleanupErr
		s.executionMu.Unlock()
		status.GameID, status.System = nil, nil
		status.LastError = cleanupErr
		return status, cleanupErr
	}
	s.executionMu.Lock()
	s.activeExecution = ExecutionFPGADevelopment
	if selected.entry != nil && recognizedPlayContract(status.CorePackage.ABI) {
		s.activeExecution = ExecutionFPGANative
	}
	s.retainSessionTargetLocked()
	s.activeGameID, s.activeSystem = "", ""
	s.activePackageID, s.activePackageGeneration = "", 0
	s.packageRejection = nil
	if selected.entry != nil && status.CorePackage.PackageID == selected.entry.PackageID {
		s.activeGameID, s.activeSystem = selected.entry.GameID, catalog.CorePlatform
		s.activePackageID, s.activePackageGeneration = selected.entry.PackageID, status.CorePackage.Generation
		status.GameID = stringPtr(s.activeGameID)
		status.System = systemPtr(s.activeSystem)
	}

	s.selectedTargetReconciled = false
	s.selectedTargetRepairAllowed = false
	s.executionMu.Unlock()
	return status, nil
}

func (s *Service) retireExecutionAfterConfirmedIdleCorePackageFailure(ctx context.Context, status protocol.Status, loadErr error) (protocol.Status, error) {
	s.executionMu.Lock()
	previousHostOnly := s.activeExecution == ExecutionHostOnly
	s.selectedTargetReconciled = true
	s.selectedTargetRepairAllowed = false
	if s.activeExecution == ExecutionFPGANative || s.activeExecution == ExecutionFPGADevelopment {
		s.activeExecution, s.activeTarget, s.activeGameID, s.activeSystem = "", "", "", ""
		s.packageRejection = nil
		s.activePackageID, s.activePackageGeneration = "", 0
	}
	s.executionMu.Unlock()
	if !previousHostOnly {
		return status, loadErr
	}
	if s.hostExecutor == nil || s.hostExecutor.Stop(ctx) != nil {
		return status, &protocol.APIError{Code: protocol.CodeInternal, Message: "host cleanup failed after core package rejection", Phase: "recovery"}
	}
	s.executionMu.Lock()
	if s.activeExecution == ExecutionHostOnly {
		s.activeExecution, s.activeTarget, s.activeGameID, s.activeSystem = "", "", "", ""
		s.packageRejection = nil
		s.activePackageID, s.activePackageGeneration = "", 0
	}
	s.executionMu.Unlock()
	return status, loadErr
}

func corePackageRequestFailure(err error) error {
	var apiErr *protocol.APIError
	if errors.As(err, &apiErr) {
		copy := *apiErr
		copy.Phase = "request"
		return &copy
	}
	return &protocol.APIError{Code: protocol.CodeInternal, Message: "core package request failed before dispatch", Phase: "request"}
}

func corePackagePreMutationFailure(err error) bool {
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	if apiErr.Phase == "recovery" {
		return false
	}
	if apiErr.Code == protocol.CodeBusy {
		return true
	}
	switch apiErr.Phase {
	case "request", "admission", "compatibility", "save":
		return true
	default:
		return false
	}
}

func (s *Service) reconcileLostCoreLoad(parent context.Context, client serviceClient, prior protocol.Status, loadErr error) (protocol.Status, error) {
	reconcileContext, cancelReconcile := context.WithTimeout(parent, s.coreLoadReconcileTimeout)
	defer cancelReconcile()
	for {
		if err := reconcileContext.Err(); err != nil {
			return protocol.Status{}, &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "core package activation outcome is unavailable", Phase: "recovery"}
		}
		statusContext, cancel := serviceTimeout(reconcileContext, s.requestTimeout)
		status, err := client.Status(statusContext)
		cancel()
		if err != nil {
			return protocol.Status{}, &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "core package activation outcome is unavailable", Phase: "recovery"}
		}
		if validServiceCorePackageStatus(status) && !reflect.DeepEqual(status, prior) {
			return status, nil
		}
		if confirmedIdleCorePackageFailure(status, loadErr) {
			return status, preserveCorePackageError(loadErr)
		}
		if reflect.DeepEqual(status, prior) {
			preserved := preserveCorePackageError(loadErr)
			var apiErr *protocol.APIError
			if errors.As(preserved, &apiErr) {
				apiErr.Phase = "request"
			}
			return protocol.Status{}, preserved
		}
		if status.State != protocol.StateLaunching || !status.Development || status.LastError != nil || status.Recovery != "" {
			return protocol.Status{}, &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "core package activation outcome is inconsistent", Phase: "recovery"}
		}
		timer := time.NewTimer(lostLaunchPollInterval)
		select {
		case <-reconcileContext.Done():
			timer.Stop()
			return protocol.Status{}, &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "core package activation outcome is unavailable", Phase: "recovery"}
		case <-timer.C:
		}
	}
}

func confirmedIdleCorePackageFailure(status protocol.Status, loadErr error) bool {
	if ambiguousTargetMutationError(loadErr) {
		return false
	}
	var dispatched *protocol.APIError
	if !errors.As(loadErr, &dispatched) || status.LastError == nil {
		return false
	}
	return status.State == protocol.StateIdle && status.GameID == nil && status.System == nil &&
		status.ExpectedCore == nil && status.ObservedCore == nil && !status.Development &&
		status.Recovery == "" && status.CorePackage == nil &&
		status.LastError.Code == dispatched.Code && status.LastError.Phase == dispatched.Phase &&
		status.LastError.Expected == dispatched.Expected && status.LastError.Observed == dispatched.Observed
}

func preserveCorePackageError(err error) error {
	var apiErr *protocol.APIError
	if errors.As(err, &apiErr) {
		copy := *apiErr
		return &copy
	}
	return canonicalRemoteError(err, protocol.CodeTransferFailed)
}

func packageReplacementApplies(requestedTarget, packageOwnerTarget string) bool {
	requestedTarget = strings.TrimSpace(requestedTarget)
	packageOwnerTarget = strings.TrimSpace(packageOwnerTarget)
	if requestedTarget == "" || packageOwnerTarget == "" {
		return true
	}
	return requestedTarget == packageOwnerTarget
}

// Caller holds lifecycle admission and must not hold targetMu.
func (s *Service) stopPackageOwnedForCatalogLaunch(ctx context.Context, packageOwnerTarget string) error {
	s.executionMu.Lock()
	development := s.activeExecution == ExecutionFPGADevelopment
	packageOwned := s.activePackageID != ""
	boundTarget := s.activeTarget
	s.executionMu.Unlock()
	if development {
		return canonicalError(protocol.CodeBusy, nil)
	}
	if !packageOwned || !packageReplacementApplies(boundTarget, packageOwnerTarget) {
		return nil
	}
	timeout := s.uploadTimeout
	stopCtx, cancel := serviceTimeout(ctx, timeout)
	defer cancel()
	stopped, err := s.stopLocked(stopCtx, ctx, timeout)
	if err != nil {
		return err
	}
	if !validRecoveredDevelopmentStatus(stopped) {
		return canonicalError(protocol.CodeMiSTerUnavailable, nil)
	}
	return nil
}

func (s *Service) adoptObservedForeground(status *protocol.Status) {
	s.executionMu.Lock()
	defer s.executionMu.Unlock()
	if status.Development && status.State != protocol.StateIdle {
		s.selectedTargetReconciled = false
		s.selectedTargetRepairAllowed = false
		confirmedPackage := status.State == protocol.StateActive || status.State == protocol.StateStopping
		if s.activeExecution == "" {
			if confirmedPackage && status.CorePackage != nil && recognizedPlayContract(status.CorePackage.ABI) {
				s.activeExecution = ExecutionFPGANative
			} else {
				s.activeExecution = ExecutionFPGADevelopment
			}
			s.retainSessionTargetLocked()
			s.activeGameID, s.activeSystem = "", ""
			if confirmedPackage && status.CorePackage != nil {
				s.activePackageID, s.activePackageGeneration = status.CorePackage.PackageID, status.CorePackage.Generation
			} else {
				s.activePackageID, s.activePackageGeneration = "", 0
			}
		}
		if confirmedPackage {
			if status.CorePackage != nil && s.activePackageID != "" && s.activePackageID == status.CorePackage.PackageID && s.activePackageGeneration == status.CorePackage.Generation {
				if s.activeGameID != "" {
					status.GameID = stringPtr(s.activeGameID)
					status.System = systemPtr(s.activeSystem)
				}
			} else {
				s.activePackageID, s.activePackageGeneration = "", 0
				if s.activeExecution != ExecutionHostOnly {
					s.activeGameID, s.activeSystem = "", ""
				}
			}
		}
		if s.packageRejection != nil {
			rejection := *s.packageRejection
			status.LastError = &rejection
			status.GameID = nil
			status.System = nil
		}
		return
	}
	if status.State == protocol.StateActive {
		s.selectedTargetReconciled = false
		s.selectedTargetRepairAllowed = false
		if s.activeExecution == "" {
			s.activeExecution = ExecutionFPGANative
			s.retainSessionTargetLocked()
			if status.GameID != nil {
				s.activeGameID = *status.GameID
			}
			if status.System != nil {
				s.activeSystem = *status.System
			}
		}
		return
	}
	if status.State == protocol.StateIdle {
		s.selectedTargetReconciled = true
		s.selectedTargetRepairAllowed = false
		if s.activeExecution == ExecutionFPGANative || s.activeExecution == ExecutionFPGADevelopment {
			s.clearForegroundPlayLocked()
		}
		return
	}
	s.selectedTargetReconciled = false
	s.selectedTargetRepairAllowed = false
}

func (s *Service) Status(parent context.Context) (protocol.Status, error) {
	ctx, cancel := serviceTimeout(parent, s.requestTimeout)
	defer cancel()
	releaseLifecycle, err := s.acquireLifecycle(ctx)
	if err != nil {
		return protocol.Status{}, err
	}
	defer releaseLifecycle()
	s.executionMu.Lock()
	localExecution := s.activeExecution == ExecutionHostOnly && s.packageRejection == nil
	s.executionMu.Unlock()
	if !localExecution && s.discoveryEnabled() {
		if _, err := s.refreshTargetConnection(ctx); err != nil {
			return protocol.Status{}, canonicalRemoteError(err, protocol.CodeMiSTerUnavailable)
		}
	}
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	s.executionMu.Lock()
	hostOnly := s.activeExecution == ExecutionHostOnly && s.packageRejection == nil
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
			s.activeExecution, s.activeTarget, s.activeGameID, s.activeSystem = "", "", "", ""
			s.packageRejection = nil
			s.activePackageID, s.activePackageGeneration = "", 0
			s.executionMu.Unlock()
			return protocol.Status{State: protocol.StateIdle}, nil
		}
		return protocol.Status{State: protocol.StateActive, GameID: stringPtr(gameID), System: systemPtr(system)}, nil
	}
	client, ok := s.selectedClientLocked()
	if !ok {
		s.allowSelectedTargetRepair(parent)
		return protocol.Status{}, canonicalError(protocol.CodeMiSTerUnavailable, nil)
	}
	status, err := client.Status(ctx)
	if err != nil {
		s.allowSelectedTargetRepair(parent)
		return protocol.Status{}, canonicalRemoteError(err, protocol.CodeMiSTerUnavailable)
	}
	s.adoptObservedForeground(&status)
	s.executionMu.Lock()
	if s.packageRejection != nil {
		rejection := *s.packageRejection
		status.LastError = &rejection
		status.GameID = nil
		status.System = nil
	}
	s.executionMu.Unlock()
	return status, nil
}

func (s *Service) allowSelectedTargetRepair(parent context.Context) {
	if parent.Err() != nil {
		return
	}
	s.executionMu.Lock()
	s.selectedTargetReconciled = false
	s.selectedTargetRepairAllowed = s.activeExecution == ""
	s.executionMu.Unlock()
}

func (s *Service) Stop(parent context.Context) (protocol.Status, error) {
	s.executionMu.Lock()
	activeExecution := s.activeExecution
	pendingRejection := s.packageRejection != nil
	s.executionMu.Unlock()
	timeout := s.requestTimeout
	if activeExecution == ExecutionFPGANative || activeExecution == ExecutionFPGADevelopment || pendingRejection {
		timeout = s.uploadTimeout
	}
	ctx, cancel := serviceTimeout(parent, timeout)
	defer cancel()
	releaseLifecycle, err := s.acquireLifecycle(ctx)
	if err != nil {
		return protocol.Status{}, WithStopStage(err, "lifecycle")
	}
	defer releaseLifecycle()
	return s.stopLocked(ctx, parent, timeout)
}

// Caller holds lifecycle admission.
func (s *Service) stopLocked(ctx, parent context.Context, timeout time.Duration) (result protocol.Status, resultErr error) {
	stage := "admission"
	defer func() { resultErr = WithStopStage(resultErr, stage) }()
	s.executionMu.Lock()
	activeExecution := s.activeExecution
	pendingRejection := s.packageRejection != nil
	s.executionMu.Unlock()
	if pendingRejection {
		stage = "development_recovery"
		return s.stopRejectedCoreWithAdmission(ctx, true)
	}

	if activeExecution != ExecutionHostOnly && s.protocolAdmissionEnabled() {
		if _, err := s.refreshStopAdmission(ctx); err != nil {
			return protocol.Status{}, stopAdmissionError(err)
		}
	}

	hostOnly := activeExecution == ExecutionHostOnly
	if hostOnly {
		stage = "host_stop"
		if err := s.stopHostOnlyIfActive(ctx); err != nil {
			return protocol.Status{}, err
		}
		return protocol.Status{State: protocol.StateIdle}, nil
	}
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	client, ok := s.selectedClientLocked()
	if !ok {
		return protocol.Status{}, canonicalError(protocol.CodeMiSTerUnavailable, nil)
	}
	// Preserve the exact ownership used for this Stop even when its response
	// is lost and the public session layer later reconciles idle via Status.
	s.executionMu.Lock()
	s.stoppedKitLease = nil
	if leased, ok := client.(interface{ KitLease() *targetclient.KitLease }); ok {
		s.stoppedKitLease = leased.KitLease()
	}
	s.executionMu.Unlock()
	stage = "target_stop"
	var status protocol.Status
	var err error
	idleWithoutLease := false
	if leased, ok := client.(interface{ KitLease() *targetclient.KitLease }); ok {
		if lease := leased.KitLease(); lease != nil && !lease.Held() {
			// A launch rejected before dispatch has no grant to stop with.
			// Confirm clean idle without claiming the kit or mutating a peer's
			// session. Unreachable, active or recovery states still fail closed.
			status, err = client.Status(ctx)
			idleWithoutLease = err == nil && validRecoveredDevelopmentStatus(status) && status.CorePackage == nil
		}
	}
	if !idleWithoutLease {
		status, err = client.Stop(ctx)
	}
	if err != nil {
		targetDeadlineExpired := errors.Is(ctx.Err(), context.DeadlineExceeded) && parent.Err() == nil
		if targetDeadlineExpired && ambiguousTargetMutationError(err) && activeExecution != ExecutionFPGADevelopment {
			stage = "reconcile_stop"
			status, err = s.reconcileLostStop(parent, client, err, timeout)
			if err != nil {
				return protocol.Status{}, err
			}
		} else {
			return protocol.Status{}, canonicalRemoteError(err, protocol.CodeMiSTerUnavailable)
		}
	}
	if status.State == protocol.StateStopping && status.Development && status.Recovery == protocol.RecoveryRebootRequired {
		stage = "development_recovery"
		recoveryClient, ok := client.(developmentRecoveryClient)
		if !ok {
			return protocol.Status{}, canonicalError(protocol.CodeInternal, nil)
		}
		health, healthErr := client.Health(ctx)
		if healthErr != nil || health.BootID == "" {
			return protocol.Status{}, canonicalRemoteError(healthErr, protocol.CodeMiSTerUnavailable)
		}
		_, _ = recoveryClient.RebootDevelopment(ctx)
		status, err = waitForDevelopmentRecovery(ctx, client, health.BootID, s.developmentRecoveryHealth(client, health.BootID))
		if err != nil {
			return protocol.Status{}, err
		}
	}
	s.executionMu.Lock()
	if s.activeExecution == ExecutionFPGANative || s.activeExecution == ExecutionFPGADevelopment {
		s.clearForegroundPlayLocked()
	}
	s.selectedTargetReconciled = status.State == protocol.StateIdle
	s.selectedTargetRepairAllowed = false
	s.executionMu.Unlock()
	if s.targetOrigin != nil {
		s.targetOrigin(targetByName(s.targets, s.selectedTarget))
	}
	return status, nil
}

func (s *Service) reconcileLostStop(parent context.Context, client serviceClient, stopErr error, timeout time.Duration) (protocol.Status, error) {
	ctx, cancel := serviceTimeout(parent, timeout)
	defer cancel()
	for {
		status, err := client.Status(ctx)
		if err != nil {
			return protocol.Status{}, canonicalRemoteError(errors.Join(stopErr, err), protocol.CodeMiSTerUnavailable)
		}
		if validRecoveredDevelopmentStatus(status) {
			return status, nil
		}
		if !provisionalLostStop(status) {
			if status.LastError != nil {
				return protocol.Status{}, canonicalRemoteError(errors.Join(stopErr, status.LastError), protocol.CodeMiSTerUnavailable)
			}
			return protocol.Status{}, canonicalRemoteError(stopErr, protocol.CodeMiSTerUnavailable)
		}
		timer := time.NewTimer(lostLaunchPollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return protocol.Status{}, canonicalRemoteError(errors.Join(stopErr, ctx.Err()), protocol.CodeMiSTerUnavailable)
		case <-timer.C:
		}
	}
}

func provisionalLostStop(status protocol.Status) bool {
	return (status.State == protocol.StateActive || status.State == protocol.StateStopping) &&
		status.LastError == nil && !status.Development && status.Recovery == ""
}

func waitForDevelopmentRecovery(ctx context.Context, client serviceClient, previousBootID string, poll ...func(context.Context) (protocol.Health, error)) (protocol.Status, error) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	healthPoll := client.Health
	if len(poll) > 0 {
		healthPoll = poll[0]
	}
	for {
		health, healthErr := healthPoll(ctx)
		if healthErr == nil && health.Ready && health.BootID != "" && health.BootID != previousBootID {
			status, statusErr := client.Status(ctx)
			if statusErr == nil && validRecoveredDevelopmentStatus(status) {
				return status, nil
			}
		}
		select {
		case <-ctx.Done():
			return protocol.Status{}, canonicalError(protocol.CodeMiSTerUnavailable, safeContextError(ctx.Err()))
		case <-ticker.C:
		}
	}
}

func validRecoveredDevelopmentStatus(status protocol.Status) bool {
	return status.State == protocol.StateIdle && !status.Development && status.Recovery == "" &&
		status.GameID == nil && status.System == nil && status.ExpectedCore == nil && status.ObservedCore == nil && status.LastError == nil
}

func (s *Service) acquireLifecycle(ctx context.Context) (func(), error) {
	s.lifecycleOnce.Do(func() {
		s.lifecycleAdmission = make(chan struct{}, 1)
		s.lifecycleAdmission <- struct{}{}
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.lifecycleAdmission:
		return func() { s.lifecycleAdmission <- struct{}{} }, nil
	}
}

func (s *Service) launchGame(ctx context.Context, game catalog.Game, progress ProgressFunc) (protocol.CachedLaunchResponse, bool, error) {
	if err := s.nativeCatalogAdmission(ctx, game); err != nil {
		return protocol.CachedLaunchResponse{}, false, err
	}
	root, ok := s.libraryRoot(game.LibraryID)
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
	return s.launchHostOnly(ctx, game, root, progress)
}

func (s *Service) launchHostOnly(ctx context.Context, game catalog.Game, root catalog.Root, progress ProgressFunc) (response protocol.CachedLaunchResponse, retry bool, resultErr error) {
	if s.hostExecutor == nil {
		return protocol.CachedLaunchResponse{}, false, canonicalError(protocol.CodeInternal, nil)
	}
	if game.State != catalog.SourceStateAvailable || !game.RootOnline {
		return protocol.CachedLaunchResponse{}, false, canonicalError(catalog.SourceErrorCode(game), nil)
	}
	if hostPathLaunch(game) {
		return s.launchHostPath(ctx, game, root, progress)
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
	launchStatus, err := s.launchHostPrepared(ctx, game.System, content, prepared.Content)
	_ = content.Close()
	if err != nil {
		_ = prepared.Remove()
		return protocol.CachedLaunchResponse{}, false, canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	// The adapter owns the process as soon as Launch succeeds. Record that
	// ownership before cleanup so a degraded cleanup error cannot orphan it.
	s.executionMu.Lock()
	s.activeExecution = ExecutionHostOnly
	s.activeTarget = ""
	s.activeGameID, s.activeSystem = game.ID, game.System
	s.packageRejection = nil
	s.activePackageID, s.activePackageGeneration = "", 0
	s.executionMu.Unlock()
	if err := prepared.Remove(); err != nil {
		return protocol.CachedLaunchResponse{}, false, canonicalError(protocol.CodeInternal, romsource.ErrCleanupRetained)
	}
	gameID, system := game.ID, game.System
	_ = launchStatus
	return protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system}, Content: prepared.Content}, false, nil
}

func hostPathLaunch(game catalog.Game) bool {
	extension := strings.ToLower(filepath.Ext(game.RelativePath))
	switch extension {
	case ".cue", ".gdi", ".chd":
		return true
	}
	return game.Fingerprint.SourceSize > protocol.MaxContentBytes
}

type hostPlatformLauncher interface {
	LaunchFor(context.Context, protocol.System, io.Reader, protocol.ContentIdentity) (hostexec.Status, error)
}

type hostPathLauncher interface {
	LaunchPath(context.Context, protocol.System, string) (hostexec.Status, error)
}

type hostOwnedPathLauncher interface {
	LaunchOwnedPath(context.Context, protocol.System, string, func()) (hostexec.Status, error)
}

func (s *Service) launchHostPrepared(ctx context.Context, system protocol.System, content io.Reader, identity protocol.ContentIdentity) (hostexec.Status, error) {
	if launcher, ok := s.hostExecutor.(hostPlatformLauncher); ok {
		return launcher.LaunchFor(ctx, system, content, identity)
	}
	return s.hostExecutor.Launch(ctx, content, identity)
}

func (s *Service) launchHostPath(ctx context.Context, game catalog.Game, root catalog.Root, progress ProgressFunc) (protocol.CachedLaunchResponse, bool, error) {
	launcher, ok := s.hostExecutor.(hostPathLauncher)
	if !ok {
		return protocol.CachedLaunchResponse{}, false, canonicalError(protocol.CodeInvalidArchive, nil)
	}
	launchPath, cleanup, err := materializeConfinedLibrary(ctx, root.Path, game.RelativePath, game.Fingerprint)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return protocol.CachedLaunchResponse{}, false, err
		}
		if errors.Is(err, catalog.ErrEscapingMediaReference) || errors.Is(err, catalog.ErrUnvalidatedMediaSheet) {
			return protocol.CachedLaunchResponse{}, false, canonicalError(protocol.CodeInvalidArchive, nil)
		}
		return protocol.CachedLaunchResponse{}, false, canonicalError(protocol.CodeSourceUnavailable, nil)
	}
	handedOff := false
	defer func() {
		if !handedOff && cleanup != nil {
			cleanup()
		}
	}()
	emitProgress(progress, "launch", "launching host content")
	if owned, ok := s.hostExecutor.(hostOwnedPathLauncher); ok {
		if _, err := owned.LaunchOwnedPath(ctx, game.System, launchPath, cleanup); err != nil {
			return protocol.CachedLaunchResponse{}, false, canonicalError(protocol.CodeInternal, safeContextError(err))
		}
		handedOff = true
	} else {
		if _, err := launcher.LaunchPath(ctx, game.System, launchPath); err != nil {
			return protocol.CachedLaunchResponse{}, false, canonicalError(protocol.CodeInternal, safeContextError(err))
		}
	}
	s.executionMu.Lock()
	s.activeExecution = ExecutionHostOnly
	s.activeTarget = ""
	s.activeGameID, s.activeSystem = game.ID, game.System
	s.packageRejection = nil
	s.activePackageID, s.activePackageGeneration = "", 0
	s.executionMu.Unlock()
	gameID, system := game.ID, game.System
	return protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system}}, false, nil
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

func ambiguousTargetMutationError(err error) bool {
	var ambiguous interface{ AmbiguousMutation() bool }
	if errors.As(err, &ambiguous) && ambiguous.AmbiguousMutation() {
		return true
	}
	var apiErr *protocol.APIError
	if errors.As(err, &apiErr) {
		return false
	}
	var transportErr *url.Error
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) ||
		errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.As(err, &transportErr)
}

func validServiceDevelopmentStatus(status protocol.Status) bool {
	return status.State == protocol.StateActive && status.Development && status.GameID == nil && status.System == nil &&
		status.ExpectedCore == nil && (status.ObservedCore == nil || *status.ObservedCore != "") && status.LastError == nil && status.Recovery == ""
}

func validServiceCorePackageStatus(status protocol.Status) bool {
	return status.State == protocol.StateActive && status.Development && status.GameID == nil &&
		status.System == nil && status.ExpectedCore == nil && status.LastError == nil &&
		status.Recovery == "" && status.CorePackage != nil &&
		status.CorePackage.PackageID != "" && status.CorePackage.Generation != 0 &&
		status.CorePackage.ABI.ID != "" && status.CorePackage.ABI.Major != 0 &&
		status.CorePackage.BuildID != ""
}

func (s *Service) reconcileLostDevelopmentLoad(parent context.Context, client serviceClient, loadErr error) (protocol.Status, error) {
	for {
		if err := parent.Err(); err != nil {
			return protocol.Status{}, canonicalError(protocol.CodeMiSTerUnavailable, safeContextError(err))
		}
		statusContext, cancelStatus := serviceTimeout(parent, s.requestTimeout)
		status, err := client.Status(statusContext)
		cancelStatus()
		if err != nil {
			return protocol.Status{}, canonicalError(protocol.CodeMiSTerUnavailable, safeContextError(err))
		}
		if validServiceDevelopmentStatus(status) {
			return status, nil
		}
		if !provisionalLostDevelopmentLoad(status) {
			if status.LastError != nil {
				return protocol.Status{}, canonicalRemoteError(errors.Join(status.LastError, loadErr), protocol.CodeMiSTerUnavailable)
			}
			return protocol.Status{}, canonicalError(protocol.CodeMiSTerUnavailable, safeContextError(loadErr))
		}
		timer := time.NewTimer(lostLaunchPollInterval)
		select {
		case <-parent.Done():
			timer.Stop()
			return protocol.Status{}, canonicalError(protocol.CodeMiSTerUnavailable, safeContextError(parent.Err()))
		case <-timer.C:
		}
	}
}

func provisionalLostDevelopmentLoad(status protocol.Status) bool {
	if validRecoveredDevelopmentStatus(status) {
		return true
	}
	return status.State == protocol.StateLaunching && status.Development && status.GameID == nil && status.System == nil &&
		status.ExpectedCore == nil && status.ObservedCore == nil && status.LastError == nil && status.Recovery == ""
}

func (s *Service) stopHostOnlyIfActive(ctx context.Context) error {
	s.executionMu.Lock()
	hostOnly := s.activeExecution == ExecutionHostOnly
	s.executionMu.Unlock()
	if !hostOnly {
		return nil
	}
	if s.hostExecutor == nil {
		return canonicalError(protocol.CodeInternal, nil)
	}
	if err := s.hostExecutor.Stop(ctx); err != nil {
		return canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	s.executionMu.Lock()
	if s.activeExecution == ExecutionHostOnly {
		s.activeExecution, s.activeTarget, s.activeGameID, s.activeSystem = "", "", "", ""
		s.packageRejection = nil
		s.activePackageID, s.activePackageGeneration = "", 0
	}
	s.executionMu.Unlock()
	return nil
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
		fallback = apiErr.Code
	}
	result := canonicalError(fallback, nil).(*protocol.APIError)
	applyRemoteDiagnostic(result, apiErr, err)
	if cause := safeContextError(err); cause != nil {
		return errors.Join(result, cause)
	}
	return result
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
	case protocol.CodeKitLeaseDenied:
		message = "Another session owns the target; release it from that session before retrying."
	case protocol.CodeUnsupportedSystem:
		message = "game system is unsupported"
	case protocol.CodeUnsupportedOperation:
		message = "requested operation is unsupported"
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
	case protocol.CodeCorruptData:
		message = "stored core data is corrupt"
	case protocol.CodeIncompatibleData:
		message = "stored or selected core data is incompatible"
	case protocol.CodeStaleRevision:
		message = "core package selection or data revision changed; refresh before retrying"
	case protocol.CodeSaveFailed:
		message = "core data could not be durably written; inspect status before retrying"
	case protocol.CodeVersionMismatch:
		message = "target API version is missing or unsupported; expected v1"
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

// SetTargetOrigin registers a hook that follows selected-target identity changes.
// The callback must not call back into Service or send network cleanup requests.
func (s *Service) SetTargetOrigin(hook func(TargetConfig)) {
	s.targetMu.Lock()
	s.targetOrigin = hook
	s.targetMu.Unlock()
}

// SessionTarget is the FPGA target bound to the host session: the active
// execution target, or the selected configured target when idle.
func (s *Service) SessionTarget() (name, targetID string) {
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	s.executionMu.Lock()
	name = s.activeTarget
	s.executionMu.Unlock()
	if name == "" {
		name = s.selectedTarget
	}
	return name, targetByName(s.targets, name).TargetID
}

// SelectedTargetConfig returns the configured selected target.
func (s *Service) SelectedTargetConfig() TargetConfig {
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	return targetByName(s.targets, s.selectedTarget)
}

// KitLease returns the application-owned lease for the selected session target.
func (s *Service) KitLease() *targetclient.KitLease {
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	client, ok := s.selectedClientLocked()
	if !ok {
		return nil
	}
	if leased, ok := client.(interface{ KitLease() *targetclient.KitLease }); ok {
		return leased.KitLease()
	}
	return nil
}

// ShutdownCleanupRequired reports whether this process still has local
// ownership that permits shutdown cleanup. It never contacts or mutates a
// target: host-only execution is locally owned, while target execution needs
// a currently held grant on the foreground target.
func (s *Service) ShutdownCleanupRequired() bool {
	s.executionMu.Lock()
	activeExecution := s.activeExecution
	s.executionMu.Unlock()
	if activeExecution == ExecutionHostOnly {
		return true
	}
	client, ok := s.selectedClientSnapshot()
	if !ok {
		return false
	}
	owner, ok := client.(interface{ HasKitGrant() bool })
	return ok && owner.HasKitGrant()
}

// ReleaseKitLease is for an explicit user Stop after input/media cleanup.
// Replacement Stop retains ownership so the next launch uses the same grant.
func (s *Service) ReleaseKitLease(ctx context.Context) error {
	s.executionMu.Lock()
	lease := s.stoppedKitLease
	s.stoppedKitLease = nil
	s.executionMu.Unlock()
	return lease.Release(ctx)
}
