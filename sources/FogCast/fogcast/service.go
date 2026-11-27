package fogcast

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/internal/discovery"
	"github.com/DeanoC/FogCast/internal/kitpackages"
	"github.com/DeanoC/FogCast/internal/localcores"
	"github.com/DeanoC/FogCast/internal/meshcontent"
	"github.com/DeanoC/FogCast/internal/meshpref"
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

const (
	ExecutionFPGANative      = "fpga_native"
	ExecutionFPGADevelopment = "fpga_development"
	lostLaunchPollInterval   = 25 * time.Millisecond
	coreLoadReconcileTimeout = 2 * time.Second
)

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

type Service struct {
	targetReset       func(string)
	connectionMu      sync.Mutex
	connection        TargetConnection
	targetConnections map[string]targetConnectionState
	resolveTarget     func(context.Context, string) ([]string, error)
	meshMu            sync.Mutex
	meshNodes         []MeshNode
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
	corePackages        corePackageStore
	catalog             serviceCatalog
	scanner             serviceScanner
	preparer            servicePreparer
	roots               []catalog.Root
	rootsByID           map[string]catalog.Root
	configPath          string
	configWriteMu       sync.Mutex
	librarySources      []LibrarySourceConfig // guarded by libraryMu
	targets             []TargetConfig
	selectedTarget      string
	targetClients       map[string]serviceClient
	targetClientFactory func(TargetConfig) (serviceClient, error)
	// targetOrigin rebinds input/media to the selected target. The lease is
	// the kit grant already held for that target, and may be nil. Invoke hooks
	// only after dropping targetMu: bridge Dial reads this service under its
	// own locks, so targetMu -> bridge/input locks would invert that order.
	targetOrigin       func(TargetConfig, *targetclient.KitLease)
	targetMu           sync.RWMutex
	settingsTargetHook func() // guarded by targetMu; called after settings release it
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
	machineROM              MachineROMLinker
	metadataRoot            string
	metadataScope           string
	watchRoot               string
	folderWatchInterval     time.Duration
	folderWatchOff          bool
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

	// bundledFirmware is the imported default firmware, by core and ROM.
	bundledFirmwareMu sync.Mutex
	bundledFirmware   []protocol.BundledFirmware
	// bundledDisks are the imported disks, at most one per core.
	bundledDisks []protocol.BundledDisk
	// kitHost is [core_packages] kit: this host drives only the kit's own
	// agent at its loopback address. It never looks the agent up by mDNS
	// and refuses settings edits to its target.
	kitHost bool
	// Serialize local entry preparation through its optional ROM binding.
	kitHostEntryMu   sync.Mutex
	kitHostInventory func(context.Context) ([]localcores.Core, error)
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
		if config.CorePackages.Kit && target.Name == KitHostTargetName {
			ip := net.ParseIP(baseURL.Hostname())
			if ip == nil || !ip.IsLoopback() {
				return nil, errors.New("kit host target must use a loopback address")
			}
			lease = targetclient.NewLocalKitLease(baseURL, target.Agent, &operationClient)
		}
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
	if config.Firmware.BundledDir != "" {
		service.LoadBundledFirmware(ctx, config.Firmware.BundledDir)
	}
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
	if config.CorePackages.Kit {
		service.corePackages = kitpackages.NewView(openKitCorePackages, kitpackages.NewFactory(kitpackages.FactoryRoot))
	} else {
		packageRoot := paths.CorePackages
		if packageRoot == "" {
			packageRoot = filepath.Join(filepath.Dir(paths.Index), "core-packages")
		}
		packages, err := corepackage.NewStore(packageRoot)
		if err != nil {
			return fail("open installed core packages", err)
		}
		service.corePackages = packages
	}

	if err := service.retireSupersededLibraries(ctx); err != nil {
		if view, ok := service.corePackages.(*kitpackages.View); ok {
			view.Close()
		}
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
		attractIdle:              config.Library.AttractIdleSeconds,
		preferredRegions:         append([]string(nil), config.Library.PreferredRegions...),
		videoProfile:             defaultVideoProfile(config.Library.VideoProfile),
		librarySources:           append([]LibrarySourceConfig(nil), config.LibrarySources...),
		metadataRoot:             paths.MetadataRoot,
		metadataScope:            config.Metadata.ClientID,
		watchRoot:                strings.TrimSpace(config.Library.WatchRoot),
		folderWatchInterval:      config.FolderWatch.Interval,
		folderWatchOff:           config.FolderWatch.Off,
		kitHost:                  config.CorePackages.Kit,
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

func (s *Service) SetDebug(debug func(string)) {
	if scanner, ok := s.scanner.(debugServiceScanner); ok {
		scanner.SetDebug(debug)
	}
}

func (s *Service) Close() error {
	s.closeOnce.Do(func() {
		if view, ok := s.corePackages.(*kitpackages.View); ok {
			view.Close()
		}
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

func serviceTimeout(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, timeout)
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
	case protocol.CodeKitLeaseBusy:
		message = "Another session owns the kit; release it from that session before retrying."
	case protocol.CodeUnsupportedSystem:
		message = "game system is unsupported"
	case protocol.CodeUnsupportedOperation:
		message = "requested operation is unsupported"
	case protocol.CodeInvalidROMPath:
		message = "target ROM path is invalid"
	case protocol.CodeSourceUnavailable:
		message = "game source is unavailable"
	case protocol.CodeCorePackageStoreUnavailable:
		message = coreStoreUnavailableMessage
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
