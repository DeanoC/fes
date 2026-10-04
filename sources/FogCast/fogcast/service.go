package fogcast

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"io/fs"
	"net"
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
	"github.com/DeanoC/FogCast/internal/discovery"
	"github.com/DeanoC/FogCast/internal/hostexec"
	"github.com/DeanoC/FogCast/internal/meshcontent"
	"github.com/DeanoC/FogCast/internal/meshplace"
	"github.com/DeanoC/FogCast/internal/meshpref"
	"github.com/DeanoC/FogCast/internal/systems"
	"github.com/DeanoC/FogCast/kitlease"
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
	targetReset   func()
	connectionMu  sync.Mutex
	connection    TargetConnection
	resolveTarget func(context.Context, string) ([]string, error)
	meshMu        sync.Mutex
	meshNodes     []MeshNode
	// meshNodesRetained is true while meshNodes are the rows kept from an
	// earlier window after a browse error. Guarded by meshMu.
	meshNodesRetained bool
	meshExecute       MeshExecuteSession
	// meshPlacementAsk is the placement request Launch reads. Nil means
	// the launch is not asking, and bind stays on the existing path.
	meshPlacementAsk *MeshPlacementAsk
	// meshPlacementExecutors optionally supplies the executor installed
	// when placement rebinds to that FPGA node and ensure is already on.
	// Production leaves it nil and dials the configured kit.
	meshPlacementExecutors map[string]meshcontent.Executor
	// meshPlacementConfig is [mesh] placement as Open read it.
	// EnableMeshPlacement turns meshPlacement on when it is enabled.
	// With meshPlacement on and no installed ask, Launch and the games
	// list build the ask from the node inventory.
	meshPlacementConfig MeshPlacementConfig
	meshPlacement       bool
	// placementNodes caches the node document reads placement uses for
	// fpga_native eligibility. The key is the node id.
	placementNodes map[string]placementNodeRead
	// libraryNodes caches node-document reads GET /api/v1/library/titles
	// made at the configured [[targets]] address. It is not placement's
	// cache. The key is the node id.
	libraryNodes map[string]placementNodeRead
	// libraryPins is the process-lifetime IP pin for a configured
	// library hostname. The key is the lowercased hostname. It is not
	// persisted; a restart re-pins. Persisting the pin is #396.
	libraryPins map[string]net.IP
	// libraryResolve looks up a configured hostname for that pin. Nil
	// uses the process resolver. Tests inject it.
	libraryResolve   func(context.Context, string) ([]net.IP, error)
	meshEnsureConfig bool
	meshEnsure       bool
	meshHTTP         *http.Client
	meshDialAt       time.Time
	// meshDialID is the selected-target identity of the last dial attempt.
	// meshInstalled is the identity that installed meshExecute. A different
	// selected target drops that executor and dials the new endpoint.
	meshDialID     meshTargetIdentity
	meshInstalled  meshTargetIdentity
	collectNodes   func(context.Context) ([]discovery.ObservedNode, error)
	lookupCancel   context.CancelFunc
	monitorCancel  context.CancelFunc
	monitorDone    chan struct{}
	nextLookup     time.Time
	lookupFailures uint

	// Grants captured by Stop and not yet released. Idle settings may drop
	// the owning client; explicit ReleaseKitLease still frees each one.
	// Invalidating one target removes only that client's grant. A release
	// that fails stays here until a later attempt succeeds. A grant that
	// still backs a remaining play stays here and is not released.
	stoppedKitLeases       []*targetclient.KitLease
	stoppedKitLeaseTargets map[*targetclient.KitLease]string
	// placementHolds counts in-flight launches that share one placement
	// claim. The key is the target client. A failed launch releases the
	// grant only when it is the last in-flight holder and the session did
	// not already hold it. Execution start moves that hold to the session.
	placementHoldMu     sync.Mutex
	placementHolds      map[serviceClient]*placementClaimRecord
	closeKitLeases      func(context.Context) error
	coreCatalogPath     string
	coreLibrarySourceID string
	corePackages        *corepackage.Store
	catalog             serviceCatalog
	scanner             serviceScanner
	preparer            servicePreparer
	roots               []catalog.Root
	rootsByID           map[string]catalog.Root
	configPath          string
	configWriteMu       sync.Mutex
	targets             []TargetConfig
	selectedTarget      string
	targetClients       map[string]serviceClient
	targetClientFactory func(TargetConfig) (serviceClient, error)
	// targetOrigin rebinds input/media to the selected target. The lease is
	// the kit grant already held for that target, and may be nil. The hook
	// must not call back into Service (same rule as targetReset).
	targetOrigin func(TargetConfig, *targetclient.KitLease)
	targetMu     sync.RWMutex
	// pairedTargetMu protects the independent paired-target lookup snapshot and
	// lazy client cache. Lock order is targetMu then pairedTargetMu when both
	// are needed; paired reads take only pairedTargetMu and release it before
	// network I/O, so they never wait for a foreground launch's targetMu.
	pairedTargetMu           sync.Mutex
	pairedTargetConfigs      []TargetConfig
	pairedTargetClients      map[string]serviceClient
	requestTimeout           time.Duration
	uploadTimeout            time.Duration
	meshCheckingTimeout      time.Duration
	coreLoadReconcileTimeout time.Duration
	uploadReadDelay          time.Duration
	executionResolver        ExecutionResolver
	hostExecutor             hostexec.Adapter
	hostCastClaimsKitDisplay bool
	users                    *libraryuser.Store
	media                    *librarymedia.Index
	libraryOverlayPath       string
	librarySettingsOnce      sync.Once
	librarySettingsAdmission chan struct{}
	libraryMu                sync.RWMutex
	// lastScan is the latest successful catalog scan. LocalShelfNotice
	// reads it so an unfiltered games list can explain an empty shelf
	// without dialing a remote target. Guarded by libraryMu.
	lastScan                catalog.ScanReport
	lastScanOK              bool
	attractIdle             int
	preferredRegions        []string
	videoProfile            string
	hostEmulator            HostEmulatorConfig
	machineROM              MachineROMLinker
	metadataRoot            string
	metadataScope           string
	watchRoot               string
	folderWatchInterval     time.Duration
	folderWatchFailureMu    sync.Mutex
	folderWatchFailureCount int
	catalogAdmission        chan struct{}
	scanMu                  sync.Mutex
	scanWG                  sync.WaitGroup
	closing                 bool
	catalogCloseWait        time.Duration
	activeExecution         string
	boundKitTarget          string
	activeTarget            string
	activeGameID            string
	activeSystem            protocol.System
	plays                   map[string]targetPlay
	// displayMemory is host process memory for the household display
	// preference and the last play DisplaySink. Open seeds the
	// preference from [mesh] display_preference. The last sink is not
	// written to config.
	displayMemory            *meshpref.Memory
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

	romCacheMu    sync.Mutex
	romCacheSnaps map[string]romCacheSnapshot
}

const catalogCloseScanTimeout = 2 * time.Second

var (
	errCatalogClosing            = errors.New("catalog is closing")
	errFolderWatchRootUnresolved = errors.New("folder watch root is unresolved")
)

func Open(ctx context.Context, paths Paths, httpClient *http.Client) (*Service, error) {
	return OpenWithHostProcessStarter(ctx, paths, httpClient, nil)
}

// OpenWithHostProcessStarter opens the normal configured service while letting
// callers provide the process boundary used by the host emulator. Production
// callers should use Open; the explicit starter keeps end-to-end host session
// tests on the real configured service without starting an emulator binary.
func OpenWithHostProcessStarter(ctx context.Context, paths Paths, httpClient *http.Client, starter hostexec.StartProcess) (*Service, error) {
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
		pins := make(map[string]string, len(config.HostEmulator.Cores))
		for _, entry := range config.HostEmulator.Cores {
			cores[entry.Platform] = entry.Core
			if entry.SHA256 != "" {
				pins[entry.Core] = entry.SHA256
			}
		}
		host := hostexec.NewRetroArchAdapterConfigured(config.HostEmulator.Binary, config.HostEmulator.Core, cores, hostexec.Options{Args: config.HostEmulator.Args, Env: config.HostEmulator.Env, SHA256: pins}, starter)
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
	service.coreCatalogPath = config.CoreCatalogPath
	service.coreLibrarySourceID = config.CoreLibrarySourceID
	service.meshEnsureConfig = config.MeshEnsure
	service.meshPlacementConfig = config.MeshPlacement
	service.SetDisplayPreference(config.MeshPlacement.DisplayPreference)
	service.meshHTTP = &http.Client{}
	if config.ZX81MachineROM.Script != "" {
		service.SetMachineROMLinker(PythonMachineROM{
			Python: config.ZX81MachineROM.Python, Script: config.ZX81MachineROM.Script,
			Image: config.ZX81MachineROM.Image, MistralCV: config.ZX81MachineROM.MistralCV,
		})
	}
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
		targetClients:       make(map[string]serviceClient),
		pairedTargetClients: make(map[string]serviceClient),
		plays:               make(map[string]targetPlay),
		displayMemory:       meshpref.New(),
		requestTimeout:      config.RequestTimeout, uploadTimeout: config.UploadTimeout,
		coreLoadReconcileTimeout: coreLoadReconcileTimeout,
		executionResolver:        defaultExecutionResolver{},
		// cmd/fogcast-api composes a kit cast only for the managed sender path;
		// its mjpeg preview and ffplay receiver are host-local (ARCHITECTURE.md).
		hostCastClaimsKitDisplay: hostCastClaimsKitDisplay(config),
		attractIdle:              config.Library.AttractIdleSeconds,
		preferredRegions:         append([]string(nil), config.Library.PreferredRegions...),
		videoProfile:             defaultVideoProfile(config.Library.VideoProfile),
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
		service.pairedTargetClients[service.selectedTarget] = client
	}
	service.pairedTargetConfigs = append([]TargetConfig(nil), service.targets...)
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

// hostCastClaimsKitDisplay mirrors cmd/fogcast-api composition: only the
// managed sender with a target cast places host-only playback on a kit display.
func hostCastClaimsKitDisplay(config Config) bool {
	return config.Media.Enabled && config.Media.Decoder != "mjpeg" && config.Media.Decoder != "ffplay"
}

func (s *Service) selectedClientSnapshot() (serviceClient, bool) {
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	return s.selectedClientLocked()
}

func (s *Service) pairedTargetClient(targetID string) (serviceClient, bool) {
	s.pairedTargetMu.Lock()
	defer s.pairedTargetMu.Unlock()
	for _, cfg := range s.pairedTargetConfigs {
		if !cfg.Enabled || cfg.TargetID != targetID {
			continue
		}
		client := s.pairedTargetClients[cfg.Name]
		if client == nil && s.targetClientFactory != nil {
			var err error
			client, err = s.targetClientFactory(cfg)
			if err != nil || client == nil {
				return nil, false
			}
			s.pairedTargetClients[cfg.Name] = client
		}
		return client, client != nil
	}
	return nil, false
}

func (s *Service) selectedClientLocked() (serviceClient, bool) {
	client := s.targetClients[s.sessionTargetNameLocked()]
	return client, client != nil
}

func (s *Service) clientForSessionContextLocked(ctx context.Context) (serviceClient, bool) {
	name := SessionTargetFromContext(ctx)
	if name == "" {
		return s.selectedClientLocked()
	}
	client := s.targetClients[name]
	return client, client != nil
}

// SessionTargetName reports the target used by an unscoped host session
// operation. A live foreground target wins; otherwise the selected target.
func (s *Service) SessionTargetName() string {
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	return s.sessionTargetNameLocked()
}

// ForegroundSessionTargetName reports only a target already bound to the
// foreground FPGA session. An idle selected kit does not scope host-only reads.
func (s *Service) ForegroundSessionTargetName() string {
	s.executionMu.Lock()
	defer s.executionMu.Unlock()
	if s.activeExecution == ExecutionHostOnly {
		return ""
	}
	return s.activeTarget
}

// HostOnlyIdleStopSafe reports that an unbound Stop can be answered locally
// when a host emulator is configured, without probing the selected kit.
func (s *Service) HostOnlyIdleStopSafe() bool { return s.hostEmulator.Binary != "" }

// Caller holds targetMu. Admission and transport must resolve the same binding.
// Host-only play does not own a kit, so a host marker is ignored. A launch
// that has already bound a configured target still uses that name while the
// previous execution is host-only. Otherwise the load falls through to the
// selected kit.
func (s *Service) sessionTargetNameLocked() string {
	name := s.selectedTarget
	s.executionMu.Lock()
	active := s.activeTarget
	execution := s.activeExecution
	bound := s.boundKitTarget
	s.executionMu.Unlock()
	if s.configuredTargetLocked(bound) {
		return bound
	}
	if active == "" {
		return name
	}
	if execution != ExecutionHostOnly && s.configuredTargetLocked(active) {
		return active
	}
	return name
}

// Caller holds targetMu.
func (s *Service) configuredTargetLocked(name string) bool {
	_, ok := s.targetClients[name]
	return ok
}

// Caller holds targetMu. The name is a kit bind that host-only cleanup must
// not replace with the selected target.
func (s *Service) kitBindToKeepLocked() string {
	s.executionMu.Lock()
	active := s.activeTarget
	execution := s.activeExecution
	bound := s.boundKitTarget
	s.executionMu.Unlock()
	if s.configuredTargetLocked(bound) {
		return bound
	}
	if execution != ExecutionHostOnly && s.configuredTargetLocked(active) {
		return active
	}
	return ""
}

type targetPlay struct {
	execution         string
	gameID            string
	system            protocol.System
	packageID         string
	packageGeneration uint64
	packageRejection  *protocol.APIError
}

type PlaySession struct {
	Target    string
	TargetID  string
	Execution string
	GameID    string
	System    protocol.System
}

// selectedKitPlayLocked returns the authoritative lifecycle record for the
// kit bound to the current operation. Root activeExecution is host-only.
func (s *Service) selectedKitPlayLocked() (string, targetPlay) {
	name := s.selectedTarget
	if s.boundKitTarget != "" {
		name = s.boundKitTarget
	} else if s.activeExecution != ExecutionHostOnly && s.activeTarget != "" {
		name = s.activeTarget
	}
	return name, s.plays[name]
}

func (s *Service) clearNonHostForegroundLocked() {
	if s.activeExecution == ExecutionHostOnly {
		return
	}
	s.activeExecution, s.activeGameID, s.activeSystem = "", "", ""
}

// bindPlayTargetLocked saves the current kit play and restores the requested
// kit's record. Caller holds executionMu and targetMu.
func (s *Service) bindPlayTargetLocked(name string) {
	s.boundKitTarget = name
	if s.activeTarget == name {
		return
	}
	if s.activeExecution == ExecutionHostOnly {
		return
	}
	s.activeTarget = name
}

// notePlayDisplaySink records the last play DisplaySink once a library
// FPGA launch has finished, including its firmware and media. It takes
// targetMu and then executionMu. A launch that did not leave FPGA play
// active does not record.
func (s *Service) notePlayDisplaySink() {
	if s == nil {
		return
	}
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	s.executionMu.Lock()
	defer s.executionMu.Unlock()
	_, play := s.selectedKitPlayLocked()
	if play.execution != ExecutionFPGANative {
		return
	}
	s.notePlayDisplaySinkLocked()
}

// notePlayDisplaySinkLocked records the bound target's node id as the
// last play DisplaySink. Caller holds executionMu and targetMu.
// An empty node id is unset and does not replace a remembered sink.
// Development loads, status reads, stops, and host-only play do not
// call this.
func (s *Service) notePlayDisplaySinkLocked() {
	if s.displayMemory == nil {
		return
	}
	name := s.boundKitTarget
	if name == "" {
		name = s.activeTarget
	}
	if name == "" {
		name = s.selectedTarget
	}
	s.displayMemory.NotePlayStarted(targetByName(s.targets, name).NodeID())
}

// SetDisplayPreference stores the household display preference node id.
// Empty clears it. The value stays in host memory.
func (s *Service) SetDisplayPreference(nodeID string) {
	if s == nil || s.displayMemory == nil {
		return
	}
	s.displayMemory.SetDisplayPreference(nodeID)
}

// PlaceOptions copies the household display preference and last play
// DisplaySink into meshplace.Options. Empty stays empty. Override and
// a missing composition slot stay unset.
func (s *Service) PlaceOptions() meshplace.Options {
	if s == nil || s.displayMemory == nil {
		return meshplace.Options{}
	}
	return s.displayMemory.PlaceOptions()
}

func (s *Service) PlaySessions() []PlaySession {
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	s.executionMu.Lock()
	defer s.executionMu.Unlock()
	out := make([]PlaySession, 0, len(s.plays))
	for name, play := range s.plays {
		if play.execution == "" || play.packageRejection != nil {
			continue
		}
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
	if s.activeExecution == ExecutionHostOnly {
		// Keep root host play unbound; a single kit's independent play remains in its map.
		if _, ok := s.plays[s.boundKitTarget]; !ok {
			s.boundKitTarget = ""
		}
		return
	}
	if _, ok := s.plays[s.activeTarget]; !ok {
		s.activeTarget = ""
	}
	if _, ok := s.plays[s.boundKitTarget]; !ok {
		s.boundKitTarget = ""
	}
}

// launchPinnedTargetBoundHook observes the name bind stored. Tests move
// selectedTarget after Ensure and read this name. It is nil outside tests
// and must not call back into Service.
var launchPinnedTargetBoundHook func(name string)

// bindLiveLaunchTarget resolves the selected target under targetMu at
// bind time. LaunchOn uses it when no mesh session is installed, so a
// settings change that selects another target or replaces its client
// is the endpoint this launch binds.
func (s *Service) bindLiveLaunchTarget(target string) error {
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
			s.bindPlayTargetLocked(name)
			s.executionMu.Unlock()
			if launchPinnedTargetBoundHook != nil {
				launchPinnedTargetBoundHook(name)
			}
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
	s.bindPlayTargetLocked(name)
	s.executionMu.Unlock()
	if s.targetOrigin != nil && explicit && name != s.selectedTarget {
		s.targetOrigin(cfg, kitLeaseOf(s.targetClients[name]))
	}
	if launchPinnedTargetBoundHook != nil {
		launchPinnedTargetBoundHook(name)
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
	_, play := s.selectedKitPlayLocked()
	return play.packageID != ""
}

func (s *Service) ActivePackageOwnedForTarget(target string) bool {
	s.executionMu.Lock()
	defer s.executionMu.Unlock()
	return s.plays[target].packageID != ""
}

// DevelopmentSessionState returns both development admission and execution
// ownership reconstructed by the same authoritative target observation.
func (s *Service) DevelopmentSessionState(ctx context.Context) (bool, string, error) {
	scopedTarget := SessionTargetFromContext(ctx)
	s.executionMu.Lock()
	execution := s.activeExecution
	if scopedTarget != "" {
		execution = s.plays[scopedTarget].execution
		if execution == "" && s.activeExecution == ExecutionHostOnly {
			execution = ExecutionHostOnly
		}
	} else if execution != ExecutionHostOnly {
		_, play := s.selectedKitPlayLocked()
		execution = play.execution
	}
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
	if scopedTarget != "" {
		execution = s.plays[scopedTarget].execution
		if execution == "" && s.activeExecution == ExecutionHostOnly {
			execution = ExecutionHostOnly
		}
	} else if s.activeExecution == ExecutionHostOnly {
		execution = ExecutionHostOnly
	} else {
		_, play := s.selectedKitPlayLocked()
		execution = play.execution
	}
	s.executionMu.Unlock()
	return execution == ExecutionFPGADevelopment, execution, nil
}

// RecognizedPlayABI reports whether a format-2 package ABI is a normal FES
// play profile. Unknown or empty ABIs stay on the Diagnostic development path.
func RecognizedPlayABI(id string, major, minor int64) bool {
	switch id {
	case "fes.simple-computer", "fes.simple-game", "fes.application", "fes.computer":
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
	// Admit before Ensure so a canceled or malformed request never
	// starts a pull. The same two checks run before the core-entry
	// lookup when the seam is off, so that request returns before
	// catalog access. With the seam off, bind still resolves the
	// selected target under targetMu at bind time. A nil executor
	// leaves Phase 0 and Phase 1 launch unchanged. Rooms and
	// GET /api/v1/games read ReadyHere only when this seam is installed.
	if err := ctx.Err(); err != nil {
		return protocol.CachedLaunchResponse{}, err
	}
	if err := protocol.ValidateGameID(gameID); err != nil {
		return protocol.CachedLaunchResponse{}, canonicalError(protocol.CodeBadRequest, nil)
	}
	snap, err := s.captureLaunchSnapshot(gameID, target)
	if err != nil {
		return protocol.CachedLaunchResponse{}, err
	}
	// A placement request runs Place before Ensure and before bind. A
	// selection of the bound executor is recorded. A selection of
	// another FPGA kit claims that kit and rebinds this snapshot onto
	// it. Any other selection of a different node returns
	// ErrUnboundNode and does not move the session. No request leaves
	// this path unchanged.
	snap, err = s.applyMatchingPlacement(ctx, gameID, snap)
	// A kit lease claimed for this placement is held only while this
	// launch reaches execution. Ensure failure releases it and settles
	// the flag only when that release succeeds. A failed release stays
	// unsettled so this defer can retry it. A grant the session already
	// held is not placementClaimed. A rebind that does not start
	// execution restores the previous session unless a later launch has
	// already moved it, and leaves the previous kit's lease untouched.
	// A rebind that starts execution releases that previous lease through
	// the kit lease API (#281). The release runs after claim cleanup,
	// with no Service lock held.
	var placementSettled bool
	var placementKept bool
	if snap.placementClaimed || snap.placementUndo.installedName != "" {
		if snap.placementClaimed {
			snap.placementClaimSettled = &placementSettled
		}
		if snap.placementUndo.installedName != "" {
			snap.placementKept = &placementKept
		}
		defer func() {
			if snap.placementKept != nil && !placementKept {
				s.restorePlacementSession(snap.placementUndo)
			}
			if snap.placementClaimed && !placementSettled {
				if s.releaseClaimedContentLease(snap) {
					placementSettled = true
				}
			}
			if snap.placementKept != nil && placementKept {
				s.releaseLeftPlacementLease(snap.placementUndo)
			}
		}()
	}
	if err != nil {
		return protocol.CachedLaunchResponse{}, err
	}
	if err := s.meshEnsureBeforeExecute(ctx, snap); err != nil {
		return protocol.CachedLaunchResponse{}, err
	}
	if store, ok := s.catalog.(coreEntryCatalog); ok {
		if _, err := store.CoreEntry(ctx, gameID); err == nil {
			// A core entry executes on an FPGA kit. Deny only the kit whose
			// connection is already held elsewhere; another named target does
			// not use that lease. Generation takeover stays on the kit lease API.
			if s.launchUsesForeignKit(snap, ExecutionFPGANative) {
				return protocol.CachedLaunchResponse{}, canonicalError(protocol.CodeKitLeaseDenied, nil)
			}
			return s.launchCoreEntry(ctx, gameID, snap)
		} else if !errors.Is(err, catalog.ErrCoreEntryNotFound) {
			return protocol.CachedLaunchResponse{}, mapCoreEntryError(err)
		}
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
	// Resolve execution before the lease decision, on the same path that skips
	// target admission for host-only play. A foreign holder blocks only an
	// FPGA launch aimed at that kit. Host-emulator play and a different named
	// target do not claim or take over the lease.
	execution, err := s.resolveExecution(ctx, admittedGame)
	if err != nil {
		return protocol.CachedLaunchResponse{}, err
	}
	if s.launchUsesForeignKit(snap, execution) {
		return protocol.CachedLaunchResponse{}, canonicalError(protocol.CodeKitLeaseDenied, nil)
	}
	if err := s.nativeCatalogAdmission(ctx, admittedGame); err != nil {
		return protocol.CachedLaunchResponse{}, err
	}

	if err := s.revalidateLaunchSnapshot(snap); err != nil {
		return protocol.CachedLaunchResponse{}, err
	}
	defer s.clearUnstartedSessionTarget()
	if s.protocolAdmissionEnabled() && execution != ExecutionHostOnly {
		if _, err := s.refreshTargetAdmission(ctx); err != nil {
			return protocol.CachedLaunchResponse{}, canonicalRemoteError(err, protocol.CodeMiSTerUnavailable)
		}
	}
	if execution != ExecutionHostOnly {
		if err := s.incompatibleTargetError(); err != nil {
			return protocol.CachedLaunchResponse{}, err
		}
	}

	// A managed host cast places host-only playback on the requested kit's display,
	// so preserve package replacement there; without it RetroArch is runner-local
	// and does not claim a kit display (ARCHITECTURE.md, host capture sender).
	if execution != ExecutionHostOnly || s.hostCastClaimsKitDisplay {
		if err := s.stopPackageOwnedForCatalogLaunch(ctx, snap.name); err != nil {
			return protocol.CachedLaunchResponse{}, err
		}
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
					name := snap.name
					if name == "" {
						name = s.selectedTarget
					}
					s.plays[name] = targetPlay{execution: ExecutionFPGANative, gameID: game.ID, system: game.System}
					if s.displayMemory != nil {
						s.displayMemory.NotePlayStarted(targetByName(s.targets, name).NodeID())
					}
					s.settlePlacementClaim(snap)
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
	s.targetMu.RLock()
	target, available := s.clientForSessionContextLocked(ctx)
	s.targetMu.RUnlock()
	client, ok := target.(castClient)
	if !available || !ok {
		return targetclient.CastStatus{}, errors.New("target cast control is unavailable")
	}
	return client.CastStart(ctx, session, token, generation)
}

func (s *Service) CastStartWithMedia(ctx context.Context, session, token string, generation uint64, media protocol.CastMediaSet) (targetclient.CastStatus, error) {
	s.targetMu.RLock()
	target, available := s.clientForSessionContextLocked(ctx)
	s.targetMu.RUnlock()
	client, ok := target.(mediaCastClient)
	if !available || !ok {
		return targetclient.CastStatus{}, errors.New("target cast control is unavailable")
	}
	return client.CastStartWithMedia(ctx, session, token, generation, media)
}

func (s *Service) CastStop(ctx context.Context, session string, generation uint64) (targetclient.CastStatus, error) {
	s.targetMu.RLock()
	target, available := s.clientForSessionContextLocked(ctx)
	s.targetMu.RUnlock()
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
	s.rememberScan(report)
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
	s.rememberScan(report)
	return report, nil
}

func (s *Service) rememberScan(report catalog.ScanReport) {
	copied := catalog.ScanReport{Roots: append([]catalog.RootReport(nil), report.Roots...)}
	s.libraryMu.Lock()
	s.lastScan = copied
	s.lastScanOK = true
	s.libraryMu.Unlock()
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
	if targetID, _ := parent.Value(pairedTargetContextKey{}).(string); targetID != "" {
		ctx, cancel := serviceTimeout(parent, s.requestTimeout)
		defer cancel()
		client, ok := s.pairedTargetClient(targetID)
		if !ok {
			return protocol.Health{}, errors.New("paired target unavailable")
		}
		return client.Health(ctx)
	}
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

// SoftwareBackends describes configured host execution after checking each core file.
func (s *Service) SoftwareBackends() []SoftwareBackend {
	return s.SoftwareBackendsContext(context.Background())
}

func (s *Service) SoftwareBackendsContext(ctx context.Context) []SoftwareBackend {
	config := s.hostEmulator
	if config.Binary == "" {
		return []SoftwareBackend{}
	}
	binaryInfo, binaryErr := os.Stat(config.Binary)
	binaryAvailable := binaryErr == nil && binaryInfo.Mode().IsRegular() && binaryInfo.Mode().Perm()&0o111 != 0
	entries := append([]HostEmulatorCore(nil), config.Cores...)
	if len(entries) == 0 {
		for _, system := range config.Systems {
			entries = append(entries, HostEmulatorCore{Platform: system, Core: config.Core})
		}
	}
	out := make([]SoftwareBackend, 0, len(entries))
	for _, entry := range entries {
		item := SoftwareBackend{Execution: meshcontent.ExecuteNativeEmu, Emulator: "retroarch", CoreID: entry.ID, CoreVersion: entry.Version, System: string(entry.Platform), Available: false}
		if item.CoreID == "" {
			item.CoreID = filepath.Base(entry.Core)
		}
		if digest, err := coreFileDigest(ctx, entry.Core); err == nil {
			item.CoreSHA256 = digest
			item.Available = binaryAvailable && (entry.SHA256 == "" || item.CoreSHA256 == entry.SHA256)
		}
		out = append(out, item)
	}
	return out
}

type SoftwareBackend struct {
	Execution   string `json:"execution"`
	Emulator    string `json:"emulator"`
	CoreID      string `json:"core_id"`
	CoreVersion string `json:"core_version,omitempty"` // Configured version label; not queried from RetroArch.
	CoreSHA256  string `json:"core_sha256,omitempty"`
	System      string `json:"system"`
	Available   bool   `json:"available"`
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
	s.executionMu.Lock()
	kitTarget, kitPlay := s.selectedKitPlayLocked()
	s.executionMu.Unlock()
	if kitPlay.packageRejection != nil {
		if status, err := s.stopRejectedKitCore(ctx, kitTarget); err != nil {
			return status, err
		}
	}
	if s.protocolAdmissionEnabled() {
		if _, err := s.refreshTargetAdmission(ctx); err != nil {
			return protocol.Status{}, canonicalRemoteError(err, protocol.CodeMiSTerUnavailable)
		}
	}
	if err := s.incompatibleTargetError(); err != nil {
		return protocol.Status{}, err
	}

	// A development RBF replaces the kit display. It only replaces host-only
	// playback when the configured host pipeline casts onto that display.
	if s.hostCastClaimsKitDisplay {
		if err := s.stopHostOnlyIfActive(ctx); err != nil {
			return protocol.Status{}, err
		}
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
	playTarget := s.sessionTargetNameLocked()
	s.executionMu.Lock()
	if s.configuredTargetLocked(playTarget) {
		s.plays[playTarget] = targetPlay{execution: ExecutionFPGADevelopment}
	}
	s.clearNonHostForegroundLocked()
	if s.activeExecution != ExecutionHostOnly {
		s.activeTarget = playTarget
	}
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
func (s *Service) loadCoreLocked(ctx, parent context.Context, source func(context.Context) (coreLoadSource, error)) (result protocol.Status, resultErr error) {
	// Resolve and validate the requested immutable package/media before even a
	// recovery Stop: an invalid next launch must preserve the retained owner.
	selected, err := source(ctx)
	if err != nil {
		return protocol.Status{}, corePackageRequestFailure(err)
	}
	s.executionMu.Lock()
	kitTarget, kitPlay := s.selectedKitPlayLocked()
	pendingRejection := kitPlay.packageRejection != nil
	s.executionMu.Unlock()
	if pendingRejection {
		if status, err := s.stopRejectedKitCore(ctx, kitTarget); err != nil {
			return status, err
		}
		if s.hostCastClaimsKitDisplay {
			if err := s.stopHostOnlyIfActive(ctx); err != nil {
				return protocol.Status{}, &protocol.APIError{Code: protocol.CodeInternal, Message: "host cleanup failed after core package rejection", Phase: "recovery"}
			}
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
	// Core mutations may claim the kit lease inside the target client. If
	// this operation created that grant and then failed (including a target
	// UPDATE_CONFLICT), release only that newly acquired grant. A grant that
	// existed before this launch may back another live play and must survive.
	lease := kitLeaseOf(client)
	leaseWasHeld := lease != nil && lease.Held()
	defer func() {
		resultErr = releaseFailedLaunchLease(resultErr, leaseWasHeld, lease, s.kitLeaseBacksPlay(lease))
	}()
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
		if selected.partsComposition != nil {
			parts, ok := client.(interface {
				LoadLibraryPartsCore(context.Context, int64, io.Reader, string) (protocol.Status, error)
			})
			if !ok {
				return protocol.Status{}, corePackageRequestFailure(canonicalError(protocol.CodeUnsupportedOperation, nil))
			}
			status, err = parts.LoadLibraryPartsCore(ctx, size, content, selected.entry.PackageID)
		} else if selected.composition != nil || (selected.slotComposition != nil && selected.romID == "") {
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

	if selected.entry != nil && (status.CorePackage.PackageID != selected.entry.PackageID || !selected.matchesLoadedIdentity(status.CorePackage)) {
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

		keepTarget := s.kitBindToKeepLocked()
		if keepTarget == "" {
			keepTarget = s.selectedTarget
		}
		var hostCleanupErr error
		if s.hostCastClaimsKitDisplay {
			hostCleanupErr = s.stopHostOnlyIfActive(ctx)
		}
		s.executionMu.Lock()
		s.plays[keepTarget] = targetPlay{execution: ExecutionFPGADevelopment, packageRejection: rejection}
		if s.activeExecution != ExecutionHostOnly {
			s.activeTarget = keepTarget
		}
		s.executionMu.Unlock()
		status.LastError = rejection
		if hostCleanupErr != nil {
			return status, &protocol.APIError{Code: protocol.CodeInternal, Message: "host cleanup failed after core package rejection", Phase: "recovery"}
		}
		return status, rejection
	}
	// Keep the previous host owner until its executor is confirmed stopped.
	// A successful target activation can still require recovery of both owners.
	keepTarget := s.kitBindToKeepLocked()
	if keepTarget == "" {
		keepTarget = s.selectedTarget
	}
	var hostCleanupErr error
	if s.hostCastClaimsKitDisplay {
		hostCleanupErr = s.stopHostOnlyIfActive(ctx)
	}
	if hostCleanupErr != nil {
		cleanupErr := &protocol.APIError{Code: protocol.CodeInternal, Message: "host cleanup failed after core package activation", Phase: "recovery"}
		s.executionMu.Lock()
		play := targetPlay{execution: ExecutionFPGADevelopment, packageRejection: cleanupErr}
		if selected.entry != nil && recognizedPlayContract(status.CorePackage.ABI) {
			play.execution = ExecutionFPGANative
			play.gameID, play.system = selected.entry.GameID, catalog.CorePlatform
			play.packageID, play.packageGeneration = selected.entry.PackageID, status.CorePackage.Generation
		}
		s.plays[keepTarget] = play
		if s.activeExecution != ExecutionHostOnly {
			s.activeTarget = keepTarget
		}
		s.executionMu.Unlock()
		status.GameID, status.System = nil, nil
		status.LastError = cleanupErr
		return status, cleanupErr
	}
	s.executionMu.Lock()
	// Kit ownership is always per target; root activeExecution is host-only.
	play := targetPlay{execution: ExecutionFPGADevelopment}
	if selected.entry != nil && recognizedPlayContract(status.CorePackage.ABI) {
		play.execution = ExecutionFPGANative
		play.gameID, play.system = selected.entry.GameID, catalog.CorePlatform
		play.packageID, play.packageGeneration = selected.entry.PackageID, status.CorePackage.Generation
	}
	s.plays[keepTarget] = play
	s.clearNonHostForegroundLocked()
	if s.activeExecution != ExecutionHostOnly {
		s.activeTarget = keepTarget
	}
	if selected.entry != nil && status.CorePackage.PackageID == selected.entry.PackageID {
		status.GameID = stringPtr(selected.entry.GameID)
		status.System = systemPtr(catalog.CorePlatform)
	}
	s.executionMu.Unlock()
	return status, nil
}

type failedLaunchLease interface {
	Held() bool
	Release(context.Context) error
}

func releaseFailedLaunchLease(launchErr error, wasHeld bool, lease failedLaunchLease, backsLivePlay bool) error {
	if launchErr == nil || wasHeld || lease == nil || !lease.Held() || backsLivePlay {
		return launchErr
	}
	releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return errors.Join(launchErr, lease.Release(releaseCtx))
}

func (s *Service) retireExecutionAfterConfirmedIdleCorePackageFailure(ctx context.Context, status protocol.Status, loadErr error) (protocol.Status, error) {
	s.executionMu.Lock()
	kitTarget, _ := s.selectedKitPlayLocked()
	delete(s.plays, kitTarget)
	s.clearNonHostForegroundLocked()
	if s.activeExecution != ExecutionHostOnly && s.activeTarget == kitTarget {
		s.activeTarget = ""
	}
	if s.boundKitTarget == kitTarget {
		s.boundKitTarget = ""
	}
	if kitTarget == s.selectedTarget {
		s.selectedTargetReconciled = true
		s.selectedTargetRepairAllowed = false
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
func (s *Service) stopPackageOwnedForCatalogLaunch(ctx context.Context, requestedTarget string) error {
	s.executionMu.Lock()
	boundTarget := requestedTarget
	if boundTarget == "" {
		boundTarget = s.selectedTarget
	}
	play := s.plays[boundTarget]
	development := play.execution == ExecutionFPGADevelopment && play.packageRejection == nil
	packageOwned := play.packageID != ""
	s.executionMu.Unlock()
	if development {
		return canonicalError(protocol.CodeBusy, nil)
	}
	if !packageOwned || !packageReplacementApplies(requestedTarget, boundTarget) {
		return nil
	}
	timeout := s.uploadTimeout
	stopCtx, cancel := serviceTimeout(ctx, timeout)
	defer cancel()
	stopped, err := s.stopLocked(stopCtx, ctx, timeout, boundTarget)
	if err != nil {
		return err
	}
	if !validRecoveredDevelopmentStatus(stopped) {
		return canonicalError(protocol.CodeMiSTerUnavailable, nil)
	}
	return nil
}

func (s *Service) Status(parent context.Context) (protocol.Status, error) {
	if targetID := PairedTargetFromContext(parent); targetID != "" {
		return s.statusPairedTarget(parent, targetID)
	}
	if name := SessionTargetFromContext(parent); name != "" {
		return s.StatusTarget(parent, name)
	}
	return s.statusTarget(parent, "")
}

func (s *Service) statusPairedTarget(parent context.Context, targetID string) (protocol.Status, error) {
	ctx, cancel := serviceTimeout(parent, s.requestTimeout)
	defer cancel()
	name := s.pairedTargetName(targetID)
	if name == "" {
		return protocol.Status{}, canonicalError(protocol.CodeBadRequest, nil)
	}
	client, ok := s.pairedTargetClient(targetID)
	if !ok {
		return protocol.Status{}, canonicalError(protocol.CodeMiSTerUnavailable, nil)
	}
	status, err := client.Status(ctx)
	if err != nil {
		return protocol.Status{}, canonicalRemoteError(err, protocol.CodeMiSTerUnavailable)
	}
	s.executionMu.Lock()
	play, hasPlay := s.plays[name]
	matchingPackage := status.CorePackage == nil && play.packageID == "" && play.packageGeneration == 0
	if status.CorePackage != nil {
		matchingPackage = status.CorePackage.PackageID != "" && status.CorePackage.Generation != 0 &&
			status.CorePackage.PackageID == play.packageID && status.CorePackage.Generation == play.packageGeneration
	}
	if hasPlay && matchingPackage && (status.State == protocol.StateActive || status.State == protocol.StateStopping) {
		if status.GameID == nil && play.gameID != "" {
			status.GameID = stringPtr(play.gameID)
		}
		if status.System == nil && play.system != "" {
			status.System = systemPtr(play.system)
		}
		if status.LastError == nil {
			status.LastError = play.packageRejection
		}
	}
	s.executionMu.Unlock()
	s.adoptObservedKit(name, status)
	return status, nil
}

func (s *Service) pairedTargetName(targetID string) string {
	s.pairedTargetMu.Lock()
	defer s.pairedTargetMu.Unlock()
	for _, target := range s.pairedTargetConfigs {
		if target.Enabled && target.TargetID == targetID {
			return target.Name
		}
	}
	return ""
}

// StatusTarget observes one configured target without changing which target
// unscoped browser Status considers foreground.
func (s *Service) StatusTarget(parent context.Context, target string) (protocol.Status, error) {
	if strings.TrimSpace(target) == "" {
		return protocol.Status{}, canonicalError(protocol.CodeBadRequest, nil)
	}
	return s.statusTarget(parent, target)
}

// HostExecutionStatus observes the local software runner for session
// admission before coordinator-owned media is changed.
func (s *Service) HostExecutionStatus(parent context.Context) (hostexec.Status, error) {
	if s.hostExecutor == nil {
		return hostexec.Status{}, errors.New("host executor is unavailable")
	}
	ctx, cancel := serviceTimeout(parent, s.requestTimeout)
	defer cancel()
	return s.hostExecutor.Status(ctx)
}

func (s *Service) statusTarget(parent context.Context, target string) (protocol.Status, error) {
	ctx, cancel := serviceTimeout(parent, s.requestTimeout)
	defer cancel()
	releaseLifecycle, err := s.acquireLifecycle(ctx)
	if err != nil {
		return protocol.Status{}, err
	}
	defer releaseLifecycle()
	if target != "" {
		s.targetMu.RLock()
		cfg := targetByName(s.targets, target)
		s.targetMu.RUnlock()
		if cfg.Name == "" || !cfg.Enabled {
			return protocol.Status{}, canonicalError(protocol.CodeBadRequest, nil)
		}
	}
	s.executionMu.Lock()
	localExecution := target == "" && s.activeExecution == ExecutionHostOnly
	_, selectedPlay := s.selectedKitPlayLocked()
	knownFPGA := target != "" || selectedPlay.execution != "" || selectedPlay.packageRejection != nil
	s.executionMu.Unlock()
	if !localExecution && s.discoveryEnabled() {
		if _, err := s.refreshTargetConnection(ctx); err != nil {
			if target == "" && s.hostEmulator.Binary != "" && (localExecution || !knownFPGA) {
				return protocol.Status{State: protocol.StateIdle}, nil
			}
			return protocol.Status{}, canonicalRemoteError(err, protocol.CodeMiSTerUnavailable)
		}
	}
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	s.executionMu.Lock()
	hostOnly := target == "" && s.activeExecution == ExecutionHostOnly
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
			s.executionMu.Unlock()
			return protocol.Status{State: protocol.StateIdle}, nil
		}
		return protocol.Status{State: protocol.StateActive, GameID: stringPtr(gameID), System: systemPtr(system)}, nil
	}
	var client serviceClient
	var ok bool
	if target != "" {
		client, ok = s.targetClients[target]
		ok = client != nil
	} else {
		client, ok = s.selectedClientLocked()
	}
	if !ok {
		s.executionMu.Lock()
		_, selectedPlay := s.selectedKitPlayLocked()
		knownFPGA := target != "" || selectedPlay.execution != "" || selectedPlay.packageRejection != nil
		localExecution := target == "" && s.activeExecution == ExecutionHostOnly
		s.executionMu.Unlock()
		if target == "" && s.hostEmulator.Binary != "" && (localExecution || !knownFPGA) {
			return protocol.Status{State: protocol.StateIdle}, nil
		}
		s.allowSelectedTargetRepair(parent)
		return protocol.Status{}, canonicalError(protocol.CodeMiSTerUnavailable, nil)
	}
	status, err := client.Status(ctx)
	if err != nil {
		if target == "" && s.hostEmulator.Binary != "" && (localExecution || !knownFPGA) {
			return protocol.Status{State: protocol.StateIdle}, nil
		}
		s.allowSelectedTargetRepair(parent)
		return protocol.Status{}, canonicalRemoteError(err, protocol.CodeMiSTerUnavailable)
	}
	{
		// A non-cast kit play can coexist with the root host foreground, so
		// project its catalog identity from the target-owned record directly.
		s.executionMu.Lock()
		kitTarget := target
		if kitTarget == "" {
			kitTarget, _ = s.selectedKitPlayLocked()
		}
		play, hasPlay := s.plays[kitTarget]
		matchingPackage := status.CorePackage == nil && play.packageID == "" && play.packageGeneration == 0
		if status.CorePackage != nil {
			matchingPackage = status.CorePackage.PackageID != "" && status.CorePackage.Generation != 0 &&
				status.CorePackage.PackageID == play.packageID && status.CorePackage.Generation == play.packageGeneration
		}
		if hasPlay && matchingPackage && (status.State == protocol.StateActive || status.State == protocol.StateStopping) {
			if status.GameID == nil && play.gameID != "" {
				status.GameID = stringPtr(play.gameID)
			}
			if status.System == nil && play.system != "" {
				status.System = systemPtr(play.system)
			}
			if status.LastError == nil {
				status.LastError = play.packageRejection
			}
		}
		s.executionMu.Unlock()
	}
	kitStatusTarget := target
	if kitStatusTarget == "" {
		s.executionMu.Lock()
		kitStatusTarget, _ = s.selectedKitPlayLocked()
		s.executionMu.Unlock()
	}
	s.adoptObservedKit(kitStatusTarget, status)
	s.executionMu.Lock()
	if target == "" && status.State != protocol.StateIdle && s.activeExecution != ExecutionHostOnly {
		s.activeTarget = kitStatusTarget
	}
	if rejection := s.plays[kitStatusTarget].packageRejection; rejection != nil {
		status.LastError = rejection
		status.GameID = nil
		status.System = nil
	}
	s.executionMu.Unlock()
	return status, nil
}

func (s *Service) adoptObservedKit(target string, status protocol.Status) {
	s.executionMu.Lock()
	defer s.executionMu.Unlock()
	play := s.plays[target]
	if status.State == protocol.StateIdle {
		if target == s.selectedTarget {
			s.selectedTargetReconciled = true
			s.selectedTargetRepairAllowed = false
		}
		if play.packageRejection != nil {
			s.plays[target] = targetPlay{packageRejection: play.packageRejection}
		} else {
			delete(s.plays, target)
			if s.activeExecution != ExecutionHostOnly && s.activeTarget == target {
				s.activeTarget = ""
			}
			if s.boundKitTarget == target {
				s.boundKitTarget = ""
			}
		}
		return
	}
	if status.State == protocol.StateFailed && !status.Development {
		if play.packageRejection != nil {
			s.plays[target] = targetPlay{packageRejection: play.packageRejection}
		} else {
			delete(s.plays, target)
			if s.activeExecution != ExecutionHostOnly && s.activeTarget == target {
				s.activeTarget = ""
			}
			if s.boundKitTarget == target {
				s.boundKitTarget = ""
			}
		}
		if target == s.selectedTarget {
			s.selectedTargetReconciled = false
			s.selectedTargetRepairAllowed = false
		}
		return
	}
	if target == s.selectedTarget {
		s.selectedTargetReconciled = false
		s.selectedTargetRepairAllowed = false
	}
	if status.Development {
		play.execution = ExecutionFPGADevelopment
		if status.CorePackage != nil {
			if play.packageID != status.CorePackage.PackageID || play.packageGeneration != status.CorePackage.Generation {
				play.gameID, play.system = "", ""
			}
			play.packageID = status.CorePackage.PackageID
			play.packageGeneration = status.CorePackage.Generation
			if recognizedPlayContract(status.CorePackage.ABI) {
				play.execution = ExecutionFPGANative
			}
		} else {
			play.packageID, play.packageGeneration = "", 0
		}
	} else if status.State == protocol.StateActive {
		play.execution = ExecutionFPGANative
	}
	if status.GameID != nil {
		play.gameID = *status.GameID
	}
	if status.System != nil {
		play.system = *status.System
	}
	s.plays[target] = play
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
	if name := SessionTargetFromContext(parent); name != "" {
		return s.StopTarget(parent, name)
	}
	if targetID := PairedTargetFromContext(parent); targetID != "" {
		if name := s.targetNameForID(targetID); name != "" {
			return s.StopTarget(parent, name)
		}
	}
	return s.stopExpected(parent, nil, nil, "")
}

func (s *Service) targetNameForID(targetID string) string {
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	for _, target := range s.targets {
		if target.Enabled && target.TargetID == targetID {
			return target.Name
		}
	}
	return ""
}

// TargetNameForID resolves a configured paired target identity.
func (s *Service) TargetNameForID(targetID string) string { return s.targetNameForID(targetID) }

func (s *Service) TargetIDForName(name string) string {
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	return targetByName(s.targets, name).TargetID
}

// StopTarget stops one configured target's play while preserving the default
// foreground selection used by unscoped Stop and Status calls.
func (s *Service) StopTarget(parent context.Context, target string) (protocol.Status, error) {
	if strings.TrimSpace(target) == "" {
		return protocol.Status{}, canonicalError(protocol.CodeBadRequest, nil)
	}
	return s.stopExpected(parent, nil, nil, target)
}

// StopExpected preserves the ordinary Stop/save lifecycle while refusing to
// stop a different foreground play from the one the caller observed.
func (s *Service) StopExpected(parent context.Context, expected SessionStopBinding) (protocol.Status, error) {
	return s.stopExpected(parent, &expected, nil, "")
}

func (s *Service) StopExpectedTargetWithPreparation(parent context.Context, expected SessionStopBinding, prepare func(context.Context), target string) (protocol.Status, error) {
	return s.stopExpected(parent, &expected, prepare, target)
}

// StopExpectedWithPreparation admits the bound play before coordinator input
// and media teardown. Preparation must not call a lifecycle service method:
// lifecycle admission stays held through preparation and the physical Stop.
func (s *Service) StopExpectedWithPreparation(parent context.Context, expected SessionStopBinding, prepare func(context.Context)) (protocol.Status, error) {
	return s.stopExpected(parent, &expected, prepare, "")
}

func (s *Service) stopExpected(parent context.Context, expected *SessionStopBinding, prepare func(context.Context), target string) (protocol.Status, error) {
	s.executionMu.Lock()
	activeExecution := s.activeExecution
	kitTarget := target
	if kitTarget == "" {
		kitTarget, _ = s.selectedKitPlayLocked()
	}
	kitPlay := s.plays[kitTarget]
	pendingRejection := !(target == "" && activeExecution == ExecutionHostOnly) && kitPlay.packageRejection != nil
	s.executionMu.Unlock()
	timeout := s.requestTimeout
	if activeExecution == ExecutionFPGANative || activeExecution == ExecutionFPGADevelopment || kitPlay.execution == ExecutionFPGANative || kitPlay.execution == ExecutionFPGADevelopment || pendingRejection {
		timeout = s.uploadTimeout
	}
	ctx, cancel := serviceTimeout(parent, timeout)
	defer cancel()
	releaseLifecycle, err := s.acquireLifecycle(ctx)
	if err != nil {
		return protocol.Status{}, WithStopStage(err, "lifecycle")
	}
	defer releaseLifecycle()
	if target != "" {
		s.targetMu.RLock()
		valid := targetByName(s.targets, target).Name != "" && targetByName(s.targets, target).Enabled
		s.targetMu.RUnlock()
		if !valid {
			return protocol.Status{}, canonicalError(protocol.CodeBadRequest, nil)
		}
	}
	if expected != nil && !s.matchesStopBinding(*expected) {
		return protocol.Status{}, ErrSessionChanged
	}
	if prepare != nil {
		prepare(ctx)
	}
	return s.stopLocked(ctx, parent, timeout, target)
}

// Caller holds lifecycle admission.
func (s *Service) stopLocked(ctx, parent context.Context, timeout time.Duration, scopedTarget string) (result protocol.Status, resultErr error) {
	stage := "admission"
	defer func() { resultErr = WithStopStage(resultErr, stage) }()
	s.executionMu.Lock()
	activeExecution := s.activeExecution
	kitTarget := scopedTarget
	if kitTarget == "" {
		kitTarget, _ = s.selectedKitPlayLocked()
	}
	pendingRejection := !(scopedTarget == "" && activeExecution == ExecutionHostOnly) && s.plays[kitTarget].packageRejection != nil
	s.executionMu.Unlock()
	if pendingRejection {
		stage = "development_recovery"
		status, err := s.stopRejectedKitCoreWithAdmission(ctx, true, kitTarget)
		if err == nil {
			s.executionMu.Lock()
			if s.activeExecution != ExecutionHostOnly && s.activeTarget == kitTarget {
				s.activeTarget = ""
			}
			if s.boundKitTarget == kitTarget {
				s.boundKitTarget = ""
			}
			s.executionMu.Unlock()
		}
		return status, err
	}
	if activeExecution == "" && s.hostEmulator.Binary != "" {
		s.executionMu.Lock()
		_, selectedPlay := s.selectedKitPlayLocked()
		noBoundPlay := s.activeTarget == "" && selectedPlay.execution == "" && selectedPlay.packageRejection == nil
		s.executionMu.Unlock()
		if noBoundPlay {
			return protocol.Status{State: protocol.StateIdle}, nil
		}
	}

	protocolClient := s.protocolAdmissionEnabled()
	if scopedTarget != "" {
		s.targetMu.RLock()
		_, protocolClient = s.targetClients[scopedTarget].(*targetclient.Client)
		s.targetMu.RUnlock()
	}
	if activeExecution != ExecutionHostOnly && protocolClient {
		admit := s.refreshStopAdmission
		admitCtx := ctx
		if scopedTarget != "" && scopedTarget != s.SessionTargetName() {
			admit = s.refreshTargetAdmission
			admitCtx = WithSessionTarget(ctx, scopedTarget)
		}
		if _, err := admit(admitCtx); err != nil {
			return protocol.Status{}, stopAdmissionError(err)
		}
	}

	hostOnly := activeExecution == ExecutionHostOnly && scopedTarget == ""
	if hostOnly {
		stage = "host_stop"
		if err := s.stopHostOnlyIfActive(ctx); err != nil {
			return protocol.Status{}, err
		}
		return protocol.Status{State: protocol.StateIdle}, nil
	}
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	var client serviceClient
	var ok bool
	if scopedTarget != "" {
		client, ok = s.targetClients[scopedTarget]
		ok = client != nil
	} else {
		client, ok = s.selectedClientLocked()
	}
	if !ok {
		return protocol.Status{}, canonicalError(protocol.CodeMiSTerUnavailable, nil)
	}
	// Preserve the exact ownership used for this Stop even when its response
	// is lost and the public session layer later reconciles idle via Status.
	// Keep earlier grants too: an idle selected-target change rebuilds
	// targetClients and would otherwise drop the only reference a later
	// explicit release uses.
	s.executionMu.Lock()
	if leased, ok := client.(interface{ KitLease() *targetclient.KitLease }); ok {
		lease := leased.KitLease()
		s.stoppedKitLeases = retainStoppedKitLease(s.stoppedKitLeases, lease)
		if lease != nil {
			if s.stoppedKitLeaseTargets == nil {
				s.stoppedKitLeaseTargets = make(map[*targetclient.KitLease]string)
			}
			s.stoppedKitLeaseTargets[lease] = kitTarget
		}
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
		recovered, rebootErr := recoveryClient.RebootDevelopment(ctx)
		// An agent error means recover_idle failed closed and no reboot was
		// armed. Return it now. A lost transport reply can still mean a reboot
		// is in flight, so that case keeps waiting for a new boot id.
		var closed *protocol.APIError
		if rebootErr != nil && errors.As(rebootErr, &closed) {
			return protocol.Status{}, canonicalRemoteError(rebootErr, protocol.CodeMiSTerUnavailable)
		}
		if validRecoveredDevelopmentStatus(recovered) {
			status = recovered
		} else {
			status, err = waitForDevelopmentRecovery(ctx, client, health.BootID, s.developmentRecoveryHealth(client, health.BootID))
			if err != nil {
				return protocol.Status{}, err
			}
		}
	}
	s.executionMu.Lock()
	if status.State == protocol.StateIdle {
		stoppedTarget := kitTarget
		if stoppedTarget != "" {
			delete(s.plays, stoppedTarget)
		}
		s.clearNonHostForegroundLocked()
		if s.activeTarget == stoppedTarget && s.activeExecution != ExecutionHostOnly {
			s.activeTarget = ""
		}
		if s.boundKitTarget == stoppedTarget {
			s.boundKitTarget = ""
		}
	}
	if kitTarget == s.selectedTarget {
		s.selectedTargetReconciled = status.State == protocol.StateIdle
		s.selectedTargetRepairAllowed = false
	}
	s.executionMu.Unlock()
	if s.targetOrigin != nil {
		s.executionMu.Lock()
		stoppedTarget := scopedTarget
		s.executionMu.Unlock()
		if stoppedTarget == "" {
			stoppedTarget = s.selectedTarget
		}
		s.targetOrigin(targetByName(s.targets, stoppedTarget), kitLeaseOf(s.targetClients[stoppedTarget]))
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
		if errors.Is(err, hostexec.ErrBusy) {
			return protocol.CachedLaunchResponse{}, false, canonicalError(protocol.CodeBusy, nil)
		}
		if errors.Is(err, hostexec.ErrUnavailable) {
			return protocol.CachedLaunchResponse{}, false, canonicalError(protocol.CodeUnavailable, safeContextError(err))
		}
		return protocol.CachedLaunchResponse{}, false, canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	// The adapter owns the process as soon as Launch succeeds. Record that
	// ownership before cleanup so a degraded cleanup error cannot orphan it.
	s.executionMu.Lock()
	s.activeExecution = ExecutionHostOnly
	s.activeTarget = ""
	s.boundKitTarget = ""
	s.activeGameID, s.activeSystem = game.ID, game.System
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
			if errors.Is(err, hostexec.ErrBusy) {
				return protocol.CachedLaunchResponse{}, false, canonicalError(protocol.CodeBusy, nil)
			}
			if errors.Is(err, hostexec.ErrUnavailable) {
				return protocol.CachedLaunchResponse{}, false, canonicalError(protocol.CodeUnavailable, safeContextError(err))
			}
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
	s.boundKitTarget = ""
	s.activeGameID, s.activeSystem = game.ID, game.System
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
	case protocol.CodeUnavailable:
		message = "configured software emulator core is unavailable"
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
// The lease argument is the kit grant for that target and may be nil. The
// callback must not call back into Service or send network cleanup requests.
func (s *Service) SetTargetOrigin(hook func(TargetConfig, *targetclient.KitLease)) {
	s.targetMu.Lock()
	s.targetOrigin = hook
	s.targetMu.Unlock()
}

// kitLeaseOf is the kit grant attached to client. A client without one
// returns nil. The caller supplies a client it already holds.
func kitLeaseOf(client serviceClient) *targetclient.KitLease {
	leased, ok := client.(interface{ KitLease() *targetclient.KitLease })
	if !ok || leased == nil {
		return nil
	}
	return leased.KitLease()
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

// TargetConfigForName returns an immutable copy of one configured target.
func (s *Service) TargetConfigForName(name string) TargetConfig {
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	return targetByName(s.targets, name)
}

// PairedTargetKitLeaseStatus reads only the lease for an enabled target ID.
// Callers on the paired listener must supply the ID authenticated by pairing.
func (s *Service) PairedTargetKitLeaseStatus(ctx context.Context, targetID string) (kitlease.Status, error) {
	status, _, _, err := s.PairedTargetStatus(ctx, targetID)
	return status, err
}

// PairedTargetStatus reads lease and health for one enabled target ID.
func (s *Service) PairedTargetStatus(ctx context.Context, targetID string) (kitlease.Status, bool, bool, error) {
	client, ok := s.pairedTargetClient(targetID)
	if !ok {
		return kitlease.Status{}, false, false, errors.New("paired target unavailable")
	}
	health, healthErr := client.Health(ctx)
	provider, ok := client.(interface {
		KitLeaseStatus(context.Context) (kitlease.Status, error)
	})
	if !ok {
		return kitlease.Status{}, false, false, errors.New("paired target lease status unavailable")
	}
	status, err := provider.KitLeaseStatus(ctx)
	if err != nil {
		return kitlease.Status{}, false, false, err
	}
	if healthErr != nil {
		return status, false, false, nil
	}
	return status, true, health.Ready, nil
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

// KitLeaseForTarget returns the application-owned lease for a named target.
func (s *Service) KitLeaseForTarget(name string) *targetclient.KitLease {
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	client := s.targetClients[name]
	if leased, ok := client.(interface{ KitLease() *targetclient.KitLease }); ok {
		return leased.KitLease()
	}
	return nil
}

// ShutdownCleanupRequired reports whether this process still has local
// ownership that permits shutdown cleanup. It never contacts or mutates a
// target: host-only execution is locally owned, while target execution needs
// a currently held grant on any target with a retained play.
func (s *Service) ShutdownCleanupRequired() bool {
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	s.executionMu.Lock()
	activeExecution := s.activeExecution
	if activeExecution == ExecutionHostOnly {
		s.executionMu.Unlock()
		return true
	}
	for name := range s.plays {
		client := s.targetClients[name]
		owner, ok := client.(interface{ HasKitGrant() bool })
		if ok && owner.HasKitGrant() {
			s.executionMu.Unlock()
			return true
		}
	}
	s.executionMu.Unlock()
	client := s.targetClients[s.sessionTargetNameLocked()]
	if client == nil {
		return false
	}
	owner, ok := client.(interface{ HasKitGrant() bool })
	return ok && owner.HasKitGrant()
}

// ReleaseKitLease is for an explicit user Stop after input/media cleanup.
// It releases every grant captured by Stop since the previous release,
// including a kit dropped from targetClients by an idle selected-target
// change. A grant that still backs a remaining PlaySession is left held:
// Soft-stop keeps the lease in stoppedKitLeases, and relaunch does not
// drop that entry, so stopping a different foreground target must not
// revoke the live session. A failed explicit Stop still calls this when
// the coordinator was already idle, so idle grants from earlier Soft-stops
// are released while the surviving play's grant stays held. Sofa Soft-stop
// (session stop with retain_lease) does not call it. A failed release stays
// in stoppedKitLeases.
// KitLease.Release keeps that grant renewing so a later Stop can retry it;
// only a successful release drops the entry. Replacement Stop retains
// ownership so the next launch uses the same grant.
func (s *Service) ReleaseKitLease(ctx context.Context) error {
	return s.releaseKitLease(ctx, "")
}

// ReleaseKitLeaseTarget releases retained grants belonging to one target.
func (s *Service) ReleaseKitLeaseTarget(ctx context.Context, target string) error {
	return s.releaseKitLease(ctx, target)
}

func (s *Service) releaseKitLease(ctx context.Context, target string) error {
	s.executionMu.Lock()
	pending := append([]*targetclient.KitLease(nil), s.stoppedKitLeases...)
	targets := make(map[*targetclient.KitLease]string, len(s.stoppedKitLeaseTargets))
	for lease, name := range s.stoppedKitLeaseTargets {
		targets[lease] = name
	}
	s.executionMu.Unlock()
	var first error
	for _, lease := range pending {
		if target != "" && targets[lease] != target {
			continue
		}
		if s.kitLeaseBacksPlay(lease) {
			continue
		}
		if err := lease.Release(ctx); err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		s.executionMu.Lock()
		s.stoppedKitLeases = dropStoppedKitLease(s.stoppedKitLeases, lease)
		delete(s.stoppedKitLeaseTargets, lease)
		s.executionMu.Unlock()
	}
	return first
}

// kitLeaseBacksPlay reports whether lease is the grant of a target that
// still has a play. Caller must not hold targetMu or executionMu.
func (s *Service) kitLeaseBacksPlay(lease *targetclient.KitLease) bool {
	if lease == nil {
		return false
	}
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	s.executionMu.Lock()
	defer s.executionMu.Unlock()
	for name := range s.plays {
		leased, ok := s.targetClients[name].(interface{ KitLease() *targetclient.KitLease })
		if ok && leased.KitLease() == lease {
			return true
		}
	}
	return false
}

func retainStoppedKitLease(leases []*targetclient.KitLease, lease *targetclient.KitLease) []*targetclient.KitLease {
	if lease == nil {
		return leases
	}
	for _, existing := range leases {
		if existing == lease {
			return leases
		}
	}
	return append(leases, lease)
}

func dropStoppedKitLease(leases []*targetclient.KitLease, lease *targetclient.KitLease) []*targetclient.KitLease {
	if lease == nil || len(leases) == 0 {
		return leases
	}
	kept := make([]*targetclient.KitLease, 0, len(leases))
	for _, existing := range leases {
		if existing != lease {
			kept = append(kept, existing)
		}
	}
	if len(kept) == len(leases) {
		return leases
	}
	if len(kept) == 0 {
		return nil
	}
	return kept
}
