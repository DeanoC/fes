package localcores

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	contract "github.com/DeanoC/FogCast/kitlease"
)

const (
	loadTimeout = 60 * time.Second
	stopTimeout = 30 * time.Second
)

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

// Service lists, launches and stops installed cores for the kit-local socket.
type Service struct {
	leases  LeaseGate
	runtime Runtime
	roots   Roots

	mu          sync.Mutex
	token       string
	closed      bool
	renewCancel context.CancelFunc
	renewDone   chan struct{}
}

func New(leases LeaseGate, runtime Runtime, roots Roots) *Service {
	return &Service{leases: leases, runtime: runtime, roots: roots}
}

func (s *Service) List() []Core {
	if s == nil {
		return []Core{}
	}
	return ReadInstalledCores(s.roots.Selections, s.roots.Packages)
}

// Launch claims owner kit-hostless purpose kit-local-core only when the
// lease is free. A held, busy, blocked or recovery lease is in_use and
// makes no runtime call. A package that still needs media or firmware is
// blocked and makes no runtime call. Renewal starts at the claim, before
// load. A failed load releases the lease; revocation stops the runtime.
func (s *Service) Launch(ctx context.Context, packageID string) (Core, error) {
	if s == nil || s.leases == nil || s.runtime == nil {
		return Core{}, errUnavailable
	}
	if !sha256Hex(packageID) {
		return Core{}, errNotFound
	}
	var core Core
	found := false
	for _, candidate := range s.List() {
		if candidate.PackageID == packageID {
			core = candidate
			found = true
			break
		}
	}
	if !found {
		return Core{}, errNotFound
	}
	if core.Needs != "none" || !core.Launchable {
		return Core{}, errBlocked
	}
	if !leaseFree(s.leases.Status()) {
		return Core{}, errInUse
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
	// succeeds, so Stop cannot interrupt the in-flight program.
	s.startRenew(grant.Token, time.Duration(grant.Status.ExpiresInMS)*time.Millisecond)
	loadErr := s.load(ctx, leaseCtx, core)
	done()
	if loadErr != nil {
		_, _ = s.leases.Release(grant.Token)
		s.stopRenew()
		return Core{}, errUnavailable
	}
	s.mu.Lock()
	s.token = grant.Token
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
	s.mu.Unlock()
	if token == "" || !contract.LocalCoreSession(s.leases.Status()) {
		return errInUse
	}
	leaseCtx, done, err := s.leases.Begin(token)
	if err != nil {
		return errInUse
	}
	stopErr := s.stop(ctx, leaseCtx)
	done()
	if stopErr != nil {
		return errUnavailable
	}
	if _, err := s.leases.Release(token); err != nil {
		return errUnavailable
	}
	s.mu.Lock()
	if s.token == token {
		s.token = ""
	}
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
	s.mu.Unlock()
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

func (s *Service) startRenew(token string, remaining time.Duration) {
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
	// Publish the loop before unlocking so Close cannot miss it.
	go func() {
		defer close(done)
		timer := time.NewTimer(interval)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				if ctx.Err() != nil {
					return
				}
				grant, err := s.leases.Renew(token)
				if err != nil {
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

func (s *Service) load(request, lease context.Context, core Core) error {
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
