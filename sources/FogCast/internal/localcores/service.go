package localcores

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"sync"
	"time"

	contract "github.com/DeanoC/FogCast/kitlease"
)

const (
	loadTimeout = 60 * time.Second
	stopTimeout = 30 * time.Second

	phaseIdle      = "idle"
	phaseLaunching = "launching"
	phaseRunning   = "running"
	phaseStopping  = "stopping"
)

// RunStatus is GET /v1/local/status. Running is true only after load has
// published the kit-local lease. Phase is idle, launching, running, or stopping.
// CoreID is the installed core id, such as fes.sms, while a phase is in flight.
type RunStatus struct {
	Phase     string `json:"phase"`
	PackageID string `json:"package_id"`
	CoreID    string `json:"core_id,omitempty"`
	Running   bool   `json:"running"`
}

var (
	errNotFound    = errors.New("not_found")
	errBlocked     = errors.New("blocked")
	errInUse       = errors.New("in_use")
	errUnavailable = errors.New("unavailable")
)

// Runtime loads an installed directory and returns the kit to the menu.
// admission is the caller, checked before dispatch. operation is the lease
// context and is not canceled when the caller disconnects.
type Runtime interface {
	LoadCore(admission, operation context.Context, path, packageID string) error
	Stop(admission, operation context.Context) error
}

// CartridgeRuntime programs an installed core with cartridge bytes. The
// local socket type-asserts this. A runtime that only implements Runtime
// refuses the cartridge and does not call LoadCore in its place.
type CartridgeRuntime interface {
	LoadCartridge(admission, operation context.Context, installPath, packageID string, rom []byte) error
}

// LeaseGate is the kit lease operations local control may use.
// Takeover is not part of this surface.
type LeaseGate interface {
	Status() contract.Status
	Claim(contract.ClaimRequest) (contract.Grant, error)
	Renew(token string) (contract.Grant, error)
	Release(token string) (contract.Status, error)
	Begin(token string) (context.Context, func(), error)
}

// Roots are the installed selection directory and the core-packages directory.
type Roots struct {
	Selections string
	Packages   string
}

type factoryFlight struct {
	key   string
	done  chan struct{}
	cores []Core
	err   error
}

// Service lists, launches and stops installed cores for the kit-local socket.
type Service struct {
	leases    LeaseGate
	runtime   Runtime
	roots     Roots
	inventory func() ([]Core, error)
	admission func(context.Context) (context.Context, func(), error)

	mu          sync.Mutex
	token       string
	phase       string
	runPackage  string
	runCore     string
	closed      bool
	renewCancel context.CancelFunc
	renewDone   chan struct{}

	listCtx       context.Context
	listCancel    context.CancelFunc
	listMu        sync.Mutex
	factoryKey    string
	factoryHits   []Core
	factoryReady  bool
	factoryFlight *factoryFlight
}

// RunStatus reports the in-flight or running kit-local core. A nil service
// is idle. The list payload is unchanged; this is a separate read.
func (s *Service) RunStatus() RunStatus {
	if s == nil {
		return RunStatus{Phase: phaseIdle}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	phase := s.phase
	if phase == "" {
		phase = phaseIdle
	}
	return RunStatus{
		Phase:     phase,
		PackageID: s.runPackage,
		CoreID:    s.runCore,
		Running:   phase == phaseRunning && s.token != "",
	}
}

func New(leases LeaseGate, runtime Runtime, roots Roots) *Service {
	ctx, cancel := context.WithCancel(context.Background())
	return &Service{leases: leases, runtime: runtime, roots: roots, listCtx: ctx, listCancel: cancel}
}

func (s *Service) List() []Core {
	if s == nil {
		return []Core{}
	}
	cores, err := s.list(context.Background())
	if err != nil || cores == nil {
		return []Core{}
	}
	return cores
}

func (s *Service) list(ctx context.Context) ([]Core, error) {
	if s == nil {
		return []Core{}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// An unreadable durable library, including a missing FESDATA3 volume, is
	// an empty list. Factory packages stay unread. Bearer REST reports the
	// same failure as HTTP 503 CORE_PACKAGE_STORE_UNAVAILABLE.
	var selected []Core
	if s.inventory != nil {
		var err error
		selected, err = s.inventory()
		if err != nil {
			return []Core{}, nil
		}
	}
	cores, err := s.factoryCores(ctx)
	if err != nil {
		return nil, err
	}
	if s.inventory == nil {
		return cores, nil
	}
	byCore := make(map[string]Core, len(cores)+len(selected))
	for _, core := range cores {
		byCore[core.CoreID] = core
	}
	for _, core := range selected {
		byCore[core.CoreID] = core
	}
	cores = make([]Core, 0, len(byCore))
	for _, core := range byCore {
		cores = append(cores, core)
	}
	sort.Slice(cores, func(i, j int) bool { return cores[i].CoreID < cores[j].CoreID })
	return cores, nil
}

// factoryCores returns the factory catalog. One scan streams every package
// and caches the result when the fingerprint is still current at the end.
// The caller stops waiting when ctx ends. Another caller shares that scan,
// including when its fingerprint differs: a second scan starts only after
// the first finishes, and only if the tree still does not match the cache.
func (s *Service) factoryCores(ctx context.Context) ([]Core, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		key := factoryFingerprint(s.roots.Selections, s.roots.Packages)
		s.listMu.Lock()
		if s.factoryReady && s.factoryKey == key {
			cores := append([]Core(nil), s.factoryHits...)
			s.listMu.Unlock()
			return cores, nil
		}
		flight := s.factoryFlight
		if flight == nil {
			flight = &factoryFlight{key: key, done: make(chan struct{})}
			s.factoryFlight = flight
			go s.runFactory(flight)
		}
		done := flight.done
		waited := flight.key
		s.listMu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-done:
		}
		if flight.err != nil && factoryFingerprint(s.roots.Selections, s.roots.Packages) == waited {
			return nil, flight.err
		}
	}
}

func (s *Service) runFactory(flight *factoryFlight) {
	defer close(flight.done)
	cores, err := s.scanFactory()
	var keyNow string
	if err == nil {
		keyNow = factoryFingerprint(s.roots.Selections, s.roots.Packages)
	}
	s.listMu.Lock()
	flight.cores, flight.err = cores, err
	if s.factoryFlight == flight {
		if err == nil && keyNow == flight.key {
			s.factoryKey = flight.key
			s.factoryHits = append([]Core(nil), cores...)
			s.factoryReady = true
		}
		s.factoryFlight = nil
	}
	s.listMu.Unlock()
}

// factoryListGate is a same-package test seam. Production leaves it nil.
// Tests block here to show a client cancellation ends the handler while one
// shared catalog scan is still in flight.
var (
	factoryListGateMu sync.Mutex
	factoryListGate   func(context.Context) error
)

func runFactoryListGate(ctx context.Context) error {
	factoryListGateMu.Lock()
	fn := factoryListGate
	factoryListGateMu.Unlock()
	if fn == nil {
		return nil
	}
	return fn(ctx)
}

func (s *Service) scanFactory() ([]Core, error) {
	ctx := s.listCtx
	if ctx == nil {
		ctx = context.Background()
	}
	if err := runFactoryListGate(ctx); err != nil {
		return nil, err
	}
	return readInstalledCores(ctx, s.roots.Selections, s.roots.Packages)
}

// SetInventory adds explicitly selected durable packages. Configure it before
// serving requests; changes in selection are reread for each list and launch.
func (s *Service) SetInventory(read func() ([]Core, error)) { s.inventory = read }

// SetAdmission connects local launches to the appliance update drain. Configure
// it before serving requests. Stop remains available for ownership cleanup.
func (s *Service) SetAdmission(begin func(context.Context) (context.Context, func(), error)) {
	s.admission = begin
}

// Launch claims owner kit-hostless purpose kit-local-core only when the
// lease is free. A held, busy, blocked or recovery lease is in_use and
// makes no runtime call. A package that still needs media or firmware is
// blocked and makes no runtime call. Renewal starts at the claim, before
// load. A failed load releases the lease; revocation stops the runtime.
// A failed renew, or cancellation of that lease context, clears the
// session so RunStatus is not running.
func (s *Service) Launch(ctx context.Context, packageID string) (Core, error) {
	return s.launch(ctx, packageID, nil)
}

// LaunchCartridge is Launch for a core that needs media. rom is the
// cartridge bytes already read from a local file. An empty body, a core
// that does not need media, or a runtime with no cartridge link is refused.
// This does not dial a host.
func (s *Service) LaunchCartridge(ctx context.Context, packageID string, rom []byte) (Core, error) {
	if len(rom) == 0 || len(rom) > maxCartridgeBytes {
		return Core{}, errUnavailable
	}
	return s.launch(ctx, packageID, rom)
}

func (s *Service) launch(ctx context.Context, packageID string, rom []byte) (Core, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if s == nil || s.leases == nil || s.runtime == nil {
		return Core{}, errUnavailable
	}
	if !sha256Hex(packageID) {
		return Core{}, errNotFound
	}
	listed, err := s.list(ctx)
	if err != nil {
		return Core{}, errUnavailable
	}
	var core Core
	found := false
	for _, candidate := range listed {
		if candidate.PackageID == packageID {
			core = candidate
			found = true
			break
		}
	}
	if !found {
		return Core{}, errNotFound
	}
	if rom == nil {
		if core.Needs != "none" || !core.Launchable {
			return Core{}, errBlocked
		}
	} else if core.Needs != "media" {
		return Core{}, errBlocked
	}
	if !leaseFree(s.leases.Status()) {
		return Core{}, errInUse
	}
	var updateCtx context.Context
	if s.admission != nil {
		var done func()
		var err error
		// Client disconnects do not cancel programming; an appliance update
		// still cancels and drains this admitted operation before reboot.
		updateCtx, done, err = s.admission(context.WithoutCancel(ctx))
		if err != nil {
			return Core{}, errInUse
		}
		defer done()
	}
	requestID, err := newRequestID()
	if err != nil {
		return Core{}, errUnavailable
	}
	grant, err := s.leases.Claim(contract.ClaimRequest{
		RequestID: requestID,
		Owner:     contract.HostlessOwner,
		Purpose:   contract.LocalCorePurpose,
	})
	if err != nil || grant.Token == "" {
		return Core{}, errInUse
	}
	if !contract.LocalCoreSession(grant.Status) {
		_, _ = s.leases.Release(grant.Token)
		return Core{}, errInUse
	}
	leaseCtx, done, err := s.leases.Begin(grant.Token)
	if err != nil {
		_, _ = s.leases.Release(grant.Token)
		return Core{}, errInUse
	}
	// Renew during programming. The service token stays empty until load
	// succeeds, so Stop cannot interrupt the in-flight program. Phase is
	// published first so a revoke during startRenew cannot miss the session.
	s.mu.Lock()
	s.phase = phaseLaunching
	s.runPackage = core.PackageID
	s.runCore = core.CoreID
	s.mu.Unlock()
	s.startRenew(grant.Token, leaseCtx, time.Duration(grant.Status.ExpiresInMS)*time.Millisecond)
	loadCtx := leaseCtx
	if updateCtx != nil {
		merged, cancel := context.WithCancel(leaseCtx)
		stop := context.AfterFunc(updateCtx, cancel)
		defer func() { stop(); cancel() }()
		loadCtx = merged
	}
	loadErr := s.load(ctx, loadCtx, core, rom)
	done()
	if loadErr != nil {
		_, _ = s.leases.Release(grant.Token)
		s.mu.Lock()
		if s.token == "" || s.token == grant.Token {
			s.token = ""
			s.phase = phaseIdle
			s.runPackage = ""
			s.runCore = ""
		}
		s.mu.Unlock()
		s.stopRenew()
		return Core{}, errUnavailable
	}
	s.mu.Lock()
	// The renew loop may already have dropped a revoked session.
	if s.phase != phaseLaunching || loadCtx.Err() != nil {
		s.token = ""
		s.phase = phaseIdle
		s.runPackage = ""
		s.runCore = ""
		s.mu.Unlock()
		_, _ = s.leases.Release(grant.Token)
		s.stopRenew()
		return Core{}, errUnavailable
	}
	s.token = grant.Token
	s.phase = phaseRunning
	s.runPackage = core.PackageID
	s.runCore = core.CoreID
	s.mu.Unlock()
	return core, nil
}

// Stop runs only while this process holds the kit-local lease. Any other
// owner, including a host, is refused before the runtime is called.
// A successful stop returns the kit to the menu and releases the lease.
func (s *Service) Stop(ctx context.Context) error {
	if s == nil || s.leases == nil || s.runtime == nil {
		return errUnavailable
	}
	s.mu.Lock()
	token := s.token
	markedStopping := false
	if token != "" && s.phase == phaseRunning {
		s.phase = phaseStopping
		markedStopping = true
	}
	s.mu.Unlock()
	if token == "" || !contract.LocalCoreSession(s.leases.Status()) {
		if markedStopping {
			s.mu.Lock()
			if s.phase == phaseStopping {
				s.phase = phaseRunning
			}
			s.mu.Unlock()
		}
		return errInUse
	}
	leaseCtx, done, err := s.leases.Begin(token)
	if err != nil {
		return errInUse
	}
	stopErr := s.stop(ctx, leaseCtx)
	done()
	if stopErr != nil {
		s.mu.Lock()
		if s.token == token && s.phase == phaseStopping {
			s.phase = phaseRunning
		}
		s.mu.Unlock()
		return errUnavailable
	}
	if _, err := s.leases.Release(token); err != nil {
		return errUnavailable
	}
	s.mu.Lock()
	if s.token == token {
		s.token = ""
	}
	s.phase = phaseIdle
	s.runPackage = ""
	s.runCore = ""
	s.mu.Unlock()
	s.stopRenew()
	return nil
}

// Close stops lease renewal. It does not release the grant. Process shutdown
// calls this before closing the lease manager so the loop cannot outlive the process.
func (s *Service) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.closed = true
	cancel := s.listCancel
	s.listCancel = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.stopRenew()
}

// renewInterval is one quarter of the time still remaining. A 90s production
// lease renews about every 20s, and a shorter remainder still renews before expiry.
func renewInterval(remaining time.Duration) time.Duration {
	interval := remaining / 4
	if interval < time.Millisecond {
		interval = time.Millisecond
	}
	return interval
}

func (s *Service) startRenew(token string, leaseCtx context.Context, remaining time.Duration) {
	s.stopRenew()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	interval := renewInterval(remaining)
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		cancel()
		return
	}
	s.renewCancel = cancel
	s.renewDone = done
	var leaseDone <-chan struct{}
	if leaseCtx != nil {
		leaseDone = leaseCtx.Done()
	}
	// Publish the loop before unlocking so Close cannot miss it.
	// leaseDone is the context revokeLocked cancels. Waiting for the next
	// renew tick would leave RunStatus running after takeover or expiry.
	go func() {
		defer close(done)
		timer := time.NewTimer(interval)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				if leaseLost(leaseCtx) {
					s.clearLostSession(token)
				}
				return
			case <-leaseDone:
				s.clearLostSession(token)
				return
			case <-timer.C:
				if ctx.Err() != nil && !leaseLost(leaseCtx) {
					return
				}
				grant, err := s.leases.Renew(token)
				// Close and Stop cancel ctx without a revoked lease. A revoke
				// cancels leaseCtx even when this loop is also stopping.
				if ctx.Err() != nil && !leaseLost(leaseCtx) {
					return
				}
				if err != nil || leaseLost(leaseCtx) || !contract.LocalCoreSession(grant.Status) || !contract.LocalCoreSession(s.leases.Status()) {
					s.clearLostSession(token)
					return
				}
				s.mu.Lock()
				current := s.token
				s.mu.Unlock()
				// Empty while load is still in flight. A different token means
				// this loop's grant is no longer the one Stop or Launch published.
				if current != "" && current != token {
					return
				}
				next := renewInterval(time.Duration(grant.Status.ExpiresInMS) * time.Millisecond)
				timer.Reset(next)
			}
		}
	}()
	s.mu.Unlock()
}

func leaseLost(leaseCtx context.Context) bool {
	return leaseCtx != nil && leaseCtx.Err() != nil
}

// clearLostSession forgets a session after renewal fails or the lease
// context is revoked. It does not release the grant or stop the runtime.
func (s *Service) clearLostSession(token string) {
	if token == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token != "" && s.token != token {
		return
	}
	// An empty token is the in-flight launch. Anything else already moved on.
	if s.token == "" && s.phase != phaseLaunching {
		return
	}
	s.token = ""
	s.phase = phaseIdle
	s.runPackage = ""
	s.runCore = ""
}

func (s *Service) stopRenew() {
	s.mu.Lock()
	cancel := s.renewCancel
	done := s.renewDone
	s.renewCancel = nil
	s.renewDone = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

func (s *Service) load(request, lease context.Context, core Core, rom []byte) error {
	if request == nil {
		request = context.Background()
	}
	if lease == nil {
		return errUnavailable
	}
	// The operation follows the lease, not the unix client. Disconnecting the
	// client must not abort an in-flight program. Revocation still cancels it.
	operation, cancel := context.WithTimeout(lease, loadTimeout)
	defer cancel()
	if len(rom) > 0 {
		cart, ok := s.runtime.(CartridgeRuntime)
		if !ok {
			return errUnavailable
		}
		return cart.LoadCartridge(request, operation, core.installPath, core.PackageID, rom)
	}
	return s.runtime.LoadCore(request, operation, core.installPath, core.PackageID)
}

func (s *Service) stop(request, lease context.Context) error {
	if request == nil {
		request = context.Background()
	}
	if lease == nil {
		return errUnavailable
	}
	operation, cancel := context.WithTimeout(lease, stopTimeout)
	defer cancel()
	return s.runtime.Stop(request, operation)
}

func leaseFree(status contract.Status) bool {
	return status.State == "free"
}

func newRequestID() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}
