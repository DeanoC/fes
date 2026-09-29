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
type Runtime interface {
	LoadCore(ctx context.Context, path, packageID string) error
	Stop(ctx context.Context) error
}

// LeaseGate is the kit lease operations local control may use.
// Takeover is not part of this surface.
type LeaseGate interface {
	Status() contract.Status
	Claim(contract.ClaimRequest) (contract.Grant, error)
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

	mu    sync.Mutex
	token string
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
// blocked and makes no runtime call. A failed load releases the lease.
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
	if err != nil || grant.Token == "" || !contract.LocalCoreSession(grant.Status) {
		return Core{}, errInUse
	}
	leaseCtx, done, err := s.leases.Begin(grant.Token)
	if err != nil {
		_, _ = s.leases.Release(grant.Token)
		return Core{}, errInUse
	}
	loadErr := s.load(ctx, leaseCtx, core)
	done()
	if loadErr != nil {
		_, _ = s.leases.Release(grant.Token)
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
	return nil
}

func (s *Service) load(request, lease context.Context, core Core) error {
	ctx, cancel := context.WithTimeout(lease, loadTimeout)
	defer cancel()
	if request != nil {
		stop := context.AfterFunc(request, cancel)
		defer stop()
	}
	return s.runtime.LoadCore(ctx, core.installPath, core.PackageID)
}

func (s *Service) stop(request, lease context.Context) error {
	ctx, cancel := context.WithTimeout(lease, stopTimeout)
	defer cancel()
	if request != nil {
		stop := context.AfterFunc(request, cancel)
		defer stop()
	}
	return s.runtime.Stop(ctx)
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
