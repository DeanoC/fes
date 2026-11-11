package localcores

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

const (
	// warmListBound is how long a kit-local list waits for the startup
	// catalog warm. The kit client gives up after 3s, so this stays well
	// under a second. A warm that is still running answers "warming"
	// instead of holding the request for the scan.
	warmListBound = 200 * time.Millisecond
)

var errWarming = errors.New("warming")

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

// Service lists installed cores for the kit-local socket.
type Service struct {
	roots         Roots
	inventory     func() ([]Core, error)
	listCtx       context.Context
	listCancel    context.CancelFunc
	listMu        sync.Mutex
	factoryKey    string
	factoryHits   []Core
	factoryReady  bool
	factoryFlight *factoryFlight

	// warmStarted is set by Warm before its goroutine runs, so a list cannot
	// begin a second scan between startup and the first request. warming is
	// true until that goroutine has finished the durable verification and the
	// factory catalog. warmDone closes after warming is cleared.
	warmStarted bool
	warming     bool
	warmDone    chan struct{}
}

func New(roots Roots) *Service {
	ctx, cancel := context.WithCancel(context.Background())
	return &Service{roots: roots, listCtx: ctx, listCancel: cancel}
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
	if err := s.waitWarm(ctx); err != nil {
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
// serving requests; changes in selection are reread for each list.
func (s *Service) SetInventory(read func() ([]Core, error)) { s.inventory = read }

// Warm starts the factory catalog scan and the durable verification pass in
// the background. It returns immediately, so agent startup can listen before
// either cache is filled. Call it after SetInventory and only when the durable
// store opened: an unavailable store must not read factory package bytes.
// One scan runs at a time. Close cancels the warm through the service list
// context. A list that arrives while the warm is running waits at most
// warmListBound, then returns errWarming without starting another scan.
func (s *Service) Warm() {
	if s == nil {
		return
	}
	s.listMu.Lock()
	if s.warmStarted {
		s.listMu.Unlock()
		return
	}
	s.warmStarted = true
	s.warming = true
	done := make(chan struct{})
	s.warmDone = done
	s.listMu.Unlock()
	go s.runWarm(done)
}

func (s *Service) runWarm(done chan struct{}) {
	defer close(done)
	defer func() {
		s.listMu.Lock()
		s.warming = false
		s.listMu.Unlock()
	}()
	// Durable verification and the factory scan both read package bytes.
	// Running them one after another keeps a single payload in flight.
	if s.inventory != nil {
		if _, err := s.inventory(); err != nil {
			return
		}
	}
	ctx := s.listCtx
	if ctx == nil {
		ctx = context.Background()
	}
	_, _ = s.factoryCores(ctx)
}

// waitWarm bounds a list that arrives during startup warm-up. A finished
// warm, or a service that was not warmed, continues into the cached list.
// Cancellation of the request ends the wait and leaves the warm running.
func (s *Service) waitWarm(ctx context.Context) error {
	s.listMu.Lock()
	if !s.warming {
		s.listMu.Unlock()
		return nil
	}
	done := s.warmDone
	s.listMu.Unlock()
	timer := time.NewTimer(warmListBound)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return nil
	case <-timer.C:
		return errWarming
	}
}

// Close cancels outstanding inventory work.
func (s *Service) Close() {
	if s != nil && s.listCancel != nil {
		s.listCancel()
	}
}
