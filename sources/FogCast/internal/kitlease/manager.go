// Package kitlease owns admission to one target's hardware session. It does not
// program hardware: revocation drains admitted requests before invoking the
// existing target coordinator's cleanup path.
package kitlease

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/DeanoC/FogCast/internal/flightdiag"
	contract "github.com/DeanoC/FogCast/kitlease"
)

type admittedContextKey struct{}
type leaseLifetimeContextKey struct{}

// WithLeaseLifetime carries the target-owned execution lease lifetime through
// an admitted request. It is not a credential or an admission by itself.
func WithLeaseLifetime(ctx, lifetime context.Context) context.Context {
	return context.WithValue(ctx, leaseLifetimeContextKey{}, lifetime)
}

// LeaseLifetime returns the execution lease lifetime associated with a target
// admission, independently of the shorter HTTP request context.
func LeaseLifetime(ctx context.Context) context.Context {
	lifetime, _ := ctx.Value(leaseLifetimeContextKey{}).(context.Context)
	return lifetime
}

type generationContextKey struct{}
type inputCleanupReceiptKey struct{}
type inputCleanupMarkerKey struct{}

// inputCleanupReceipt belongs to one lease incarnation and is published only
// after its admitted operations and explicitly confirmed cleanup have drained.
type inputCleanupReceipt struct{ confirmed atomic.Bool }
type inputCleanupMarker struct{ confirmed atomic.Bool }

// InputCleanupConfirmed reports the cleanup result for the exact lease
// lifetime context returned by BeginLocalInput. Revocation alone is not proof.
func InputCleanupConfirmed(leaseCtx context.Context) bool {
	if leaseCtx == nil {
		return false
	}
	receipt, _ := leaseCtx.Value(inputCleanupReceiptKey{}).(*inputCleanupReceipt)
	return receipt != nil && receipt.confirmed.Load()
}

// ConfirmInputCleanup marks the manager-provided cleanup context only after
// the configured cleanup has actually confirmed neutral input and idle core.
// The manager still checks the callback result and deadline before publishing.
func ConfirmInputCleanup(ctx context.Context) {
	if ctx == nil || ctx.Err() != nil {
		return
	}
	marker, _ := ctx.Value(inputCleanupMarkerKey{}).(*inputCleanupMarker)
	if marker != nil {
		marker.confirmed.Store(true)
	}
}

func inputCleanupSucceeded(ctx context.Context, marker *inputCleanupMarker, err error) bool {
	return ctx != nil && ctx.Err() == nil && err == nil && marker != nil && marker.confirmed.Load()
}

// WithAdmission marks an operation whose execution lease was admitted by the
// target HTTP guard. Its context is also cancelled when that lease ends.
func WithAdmission(ctx context.Context) context.Context {
	return context.WithValue(ctx, admittedContextKey{}, true)
}

// WithGeneration preserves the exact generation admitted by the HTTP guard.
func WithGeneration(ctx context.Context, generation string) context.Context {
	return context.WithValue(ctx, generationContextKey{}, generation)
}

func Generation(ctx context.Context) string {
	generation, _ := ctx.Value(generationContextKey{}).(string)
	return generation
}

// Admitted reports whether the target lease guard admitted this operation.
func Admitted(ctx context.Context) bool {
	admitted, _ := ctx.Value(admittedContextKey{}).(bool)
	return admitted && ctx.Err() == nil
}

var (
	ErrBusy    = errors.New("kit is owned or recovery is running")
	ErrLease   = errors.New("kit lease is missing, expired or superseded")
	ErrInvalid = errors.New("invalid kit lease request")
	ErrBlocked = errors.New("kit cleanup failed or agent is shutting down")
	// ErrRuntimeUnreachable means cleanup could not open the runtime socket.
	// Revocation frees the lease: there is no live runtime session to hold.
	ErrRuntimeUnreachable = errors.New("runtime socket is not open")
)

type Status = contract.Status
type ClaimRequest = contract.ClaimRequest
type TakeoverRequest = contract.TakeoverRequest
type Grant = contract.Grant

type Manager struct {
	mu                          sync.Mutex
	ttl                         time.Duration
	cleanupTimeout              time.Duration
	localHost                   bool
	cleanup                     func(context.Context) error
	status                      Status
	claim                       ClaimRequest
	token                       string
	leaseContext                context.Context
	cancel                      context.CancelFunc
	timer                       *time.Timer
	refs                        int
	drained                     chan struct{}
	pending                     *TakeoverRequest
	pendingReady                string
	takeover                    *TakeoverRequest
	retired                     map[string]bool
	finished                    chan struct{}
	closed                      bool
	maintenanceStore            *maintenanceStore
	maintenance                 *maintenanceRecord
	maintenanceLoaded           bool
	maintenanceFault            bool
	queuedMaintenance           *ClaimRequest
	queuedMaintenanceCredential string
	// executionFence blocks new execution claims while a software cancellation
	// has removed its queue but durable and volatile state have not reconciled.
	executionFence bool
	events         flightdiag.Sink
}

type Option func(*Manager)

func WithEventSink(sink flightdiag.Sink) Option {
	return func(manager *Manager) { manager.events = sink }
}

// New reconciles/cleans existing target state before allowing the first claim.
// The callback must return success only after input is neutral and hardware idle.
func New(ttl time.Duration, cleanup func(context.Context) error, options ...Option) *Manager {
	if ttl <= 0 {
		ttl = 90 * time.Second
	}
	m := &Manager{ttl: ttl, cleanupTimeout: 30 * time.Second, cleanup: cleanup, retired: make(map[string]bool), status: Status{State: "free", Generation: secret()}}
	for _, option := range options {
		if option != nil {
			option(m)
		}
	}
	m.mu.Lock()
	m.revokeLocked("agent startup reconciliation")
	m.mu.Unlock()
	return m
}
func secret() string { b := make([]byte, 32); _, _ = rand.Read(b); return hex.EncodeToString(b) }
func label(s string) bool {
	return len(s) > 0 && len(s) <= 160 && strings.TrimSpace(s) == s && !strings.ContainsFunc(s, unicode.IsControl)
}
func valid(r ClaimRequest) bool {
	if !label(r.Owner) || !label(r.Purpose) || len(r.RequestID) < 32 || len(r.RequestID) > 128 {
		return false
	}
	_, e := hex.DecodeString(r.RequestID)
	return e == nil
}

// SetExecutionFence closes or opens new execution claims. An existing owner's
// renewal still works. The software supervisor closes the fence before dropping
// a queued intent and opens it only after the durable cancellation is stored,
// or after the queue has been restored.
func (m *Manager) SetExecutionFence(closed bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrBlocked
	}
	m.executionFence = closed
	return nil
}
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expireLocked()
	return m.snapshotLocked()
}
func (m *Manager) expireLocked() {
	if m.status.State == "held" && !time.Now().Before(m.status.ExpiresAt) {
		m.revokeLocked("lease expired")
	}
}
func (m *Manager) grantLocked(r ClaimRequest) Grant {
	m.claim = r
	m.token = secret()
	m.takeover = nil
	m.status = Status{State: "held", Generation: secret(), Owner: r.Owner, Purpose: r.Purpose}
	leaseCtx := context.WithValue(WithGeneration(context.Background(), m.status.Generation), inputCleanupReceiptKey{}, &inputCleanupReceipt{})
	m.leaseContext, m.cancel = context.WithCancel(leaseCtx)
	m.renewLocked()
	return Grant{Status: m.snapshotLocked(), Token: m.token}
}

func (m *Manager) recordLocked(kind, severity string, detail map[string]any) {
	if m.events == nil {
		return
	}
	m.events.Append(flightdiag.Event{
		LeaseGen: m.status.Generation,
		Layer:    flightdiag.LayerTarget,
		Kind:     kind,
		Severity: severity,
		Detail:   detail,
	})
}
func (m *Manager) renewLocked() {
	ttl := m.ttl
	if m.localHost && ttl < 10*time.Minute {
		ttl = 10 * time.Minute
	}
	m.status.ExpiresAt = time.Now().Add(ttl)
	if m.timer != nil {
		m.timer.Stop()
	}
	generation := m.status.Generation
	m.timer = time.AfterFunc(ttl, func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.status.Generation == generation {
			m.expireLocked()
		}
	})
}
func (m *Manager) Claim(r ClaimRequest) (Grant, error) {
	if !valid(r) {
		return Grant{}, ErrInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expireLocked()
	if m.closed {
		return Grant{}, ErrBlocked
	}
	if m.retired[r.RequestID] {
		return Grant{}, ErrLease
	}
	if m.claim.RequestID == r.RequestID && m.claim != r {
		return Grant{}, ErrInvalid
	}
	if m.status.State == "held" && m.claim == r {
		return Grant{Status: m.snapshotLocked(), Token: m.token}, nil
	}
	if m.executionFence {
		return Grant{}, ErrBlocked
	}
	if m.queuedMaintenance != nil {
		return Grant{}, ErrBusy
	}
	if m.status.State == "blocked" {
		return Grant{}, ErrBlocked
	}
	if m.status.State != "free" {
		return Grant{}, ErrBusy
	}
	m.pending = nil
	m.pendingReady = ""
	grant := m.grantLocked(r)
	m.recordLocked(flightdiag.KindLeaseClaim, "ok", map[string]any{
		"owner": r.Owner, "purpose": r.Purpose, "request_id": r.RequestID,
	})
	return grant, nil
}

// ClaimLocal grants the authenticated kit-local host a lease that can be
// recovered after the host process restarts. Remote owners use Claim instead.
func (m *Manager) ClaimLocal() (Grant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expireLocked()
	if m.closed || m.executionFence || m.status.State == "blocked" {
		return Grant{}, ErrBlocked
	}
	if m.status.State == "held" && m.localHost {
		m.renewLocked()
		return Grant{Status: m.snapshotLocked(), Token: m.token}, nil
	}
	if m.status.State != "free" || m.queuedMaintenance != nil {
		return Grant{}, ErrBusy
	}
	m.pending, m.pendingReady = nil, ""
	m.localHost = true
	grant := m.grantLocked(ClaimRequest{RequestID: secret(), Owner: "fogcast@kit", Purpose: "local host session"})
	m.recordLocked(flightdiag.KindLeaseClaim, "ok", map[string]any{"owner": m.claim.Owner, "purpose": m.claim.Purpose})
	return grant, nil
}

// ResumeLocal returns only a still-held local-host grant; it never claims a
// free kit or returns a remote owner's token.
func (m *Manager) ResumeLocal() (Grant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expireLocked()
	if m.closed || m.status.State != "held" || !m.localHost {
		return Grant{}, ErrLease
	}
	m.renewLocked()
	return Grant{Status: m.snapshotLocked(), Token: m.token}, nil
}

func (m *Manager) ownsLocked(token string) bool {
	return token != "" && m.token != "" && subtle.ConstantTimeCompare([]byte(token), []byte(m.token)) == 1
}
func (m *Manager) Renew(token string) (Grant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expireLocked()
	if m.closed || m.status.State != "held" || !m.ownsLocked(token) {
		return Grant{}, ErrLease
	}
	m.renewLocked()
	m.recordLocked(flightdiag.KindLeaseRenew, "ok", map[string]any{"owner": m.claim.Owner})
	return Grant{Status: m.snapshotLocked(), Token: m.token}, nil
}

// Begin retains admission until done, which is idempotent. Its context is
// canceled at revocation; an already-dispatched physical operation must finish
// on the coordinator's process context before invoking done.
func (m *Manager) Begin(token string) (context.Context, func(), error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expireLocked()
	if m.closed || m.status.State != "held" || !m.ownsLocked(token) {
		return nil, nil, ErrLease
	}
	return m.beginLocked()
}

// BeginLocalInput binds a kit-local physical-input operation to the exact
// execution generation observed by the launcher. The generation is not a
// credential: callers must themselves be restricted to the root-owned local
// input transport. Revocation cancels and drains the operation just as it
// does a token-admitted host operation.
func (m *Manager) BeginLocalInput(generation string) (context.Context, func(), error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expireLocked()
	if m.closed || m.status.State != "held" || generation == "" || m.status.Generation != generation {
		return nil, nil, ErrLease
	}
	return m.beginLocked()
}

// LocalHostGeneration identifies the supervisor's own kit host without
// inferring authority from an owner label supplied by a remote claimant.
func (m *Manager) LocalHostGeneration(generation string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expireLocked()
	return !m.closed && m.status.State == "held" && m.localHost && generation != "" && m.status.Generation == generation
}

func (m *Manager) beginLocked() (context.Context, func(), error) {
	if m.refs == 0 {
		m.drained = make(chan struct{})
	}
	m.refs++
	var once sync.Once
	done := func() {
		once.Do(func() {
			m.mu.Lock()
			defer m.mu.Unlock()
			m.refs--
			if m.refs == 0 {
				close(m.drained)
			}
		})
	}
	return m.leaseContext, done, nil
}
func (m *Manager) Release(token string) (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expireLocked()
	if m.closed || !m.ownsLocked(token) || (m.status.State != "held" && m.status.State != "revoking") {
		return m.snapshotLocked(), ErrLease
	}
	if m.status.State == "held" {
		m.recordLocked(flightdiag.KindLeaseRelease, "ok", map[string]any{"owner": m.claim.Owner})
		m.revokeLocked("owner released lease")
	}
	return m.snapshotLocked(), nil
}

// Settled waits until an in-flight revocation finishes. Release and takeover
// return while cleanup is still running and the state is revoking; a grant
// cannot be claimed again until this returns. A lease that is not revoking
// returns immediately. Cancelling ctx abandons the wait and leaves cleanup
// running.
func (m *Manager) Settled(ctx context.Context) (Status, error) {
	if m == nil {
		return Status{}, ErrLease
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		m.mu.Lock()
		if m.status.State != "revoking" {
			status := m.snapshotLocked()
			m.mu.Unlock()
			return status, nil
		}
		finished := m.finished
		m.mu.Unlock()
		if finished == nil {
			return Status{}, ErrLease
		}
		select {
		case <-ctx.Done():
			return Status{}, ctx.Err()
		case <-finished:
		}
	}
}
func (m *Manager) Takeover(r TakeoverRequest) (Grant, error) {
	if !valid(r.ClaimRequest) || !label(r.Reason) || r.ExpectedGeneration == "" {
		return Grant{}, ErrInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expireLocked()
	if m.closed {
		return Grant{}, ErrBlocked
	}
	if m.executionFence {
		return Grant{}, ErrBlocked
	}
	if m.maintenanceFault {
		return Grant{}, ErrBlocked
	}
	if m.maintenanceActiveLocked() {
		return Grant{}, ErrBusy
	}
	if m.queuedMaintenance != nil {
		return Grant{}, ErrBusy
	}
	if m.retired[r.RequestID] {
		return Grant{}, ErrLease
	}
	if m.takeover != nil && m.takeover.RequestID == r.RequestID {
		if *m.takeover != r {
			return Grant{}, ErrInvalid
		}
		if m.status.State == "held" {
			return Grant{Status: m.snapshotLocked(), Token: m.token}, nil
		}
	}
	if m.pending != nil && m.pending.RequestID == r.RequestID {
		if *m.pending != r {
			return Grant{}, ErrInvalid
		}
		if m.status.State == "free" && m.pendingReady == m.status.Generation {
			g := m.grantLocked(r.ClaimRequest)
			m.takeover = &r
			m.pending = nil
			m.pendingReady = ""
			m.recordLocked(flightdiag.KindLeaseTakeover, "ok", map[string]any{
				"owner": r.Owner, "purpose": r.Purpose, "reason": r.Reason,
			})
			return g, nil
		}
		if m.status.State == "revoking" {
			return Grant{}, ErrBusy
		}
	}
	if m.status.Generation != r.ExpectedGeneration {
		return Grant{}, ErrLease
	}
	if m.status.State == "revoking" {
		return Grant{}, ErrBusy
	}
	if m.status.State == "free" {
		g := m.grantLocked(r.ClaimRequest)
		m.takeover = &r
		m.recordLocked(flightdiag.KindLeaseTakeover, "ok", map[string]any{
			"owner": r.Owner, "purpose": r.Purpose, "reason": r.Reason,
		})
		return g, nil
	}
	if m.claim.RequestID == r.RequestID {
		return Grant{}, ErrInvalid
	}
	m.pending = &r
	m.pendingReady = ""
	m.recordLocked(flightdiag.KindLeaseTakeover, "warn", map[string]any{
		"owner": r.Owner, "purpose": r.Purpose, "reason": r.Reason, "state": "pending",
	})
	m.revokeLocked("operator takeover: " + r.Reason)
	return Grant{}, ErrBusy
}
func (m *Manager) revokeLocked(reason string) {
	if m.status.State == "revoking" {
		return
	}
	m.localHost = false
	m.status.State = "revoking"
	m.status.Reason = reason
	m.recordLocked(flightdiag.KindFenceOwnership, "warn", map[string]any{"reason": reason})
	if m.timer != nil {
		m.timer.Stop()
	}
	if m.cancel != nil {
		m.cancel()
	}
	var receipt *inputCleanupReceipt
	if m.leaseContext != nil {
		receipt, _ = m.leaseContext.Value(inputCleanupReceiptKey{}).(*inputCleanupReceipt)
	}
	drained := m.drained
	if m.refs == 0 {
		drained = make(chan struct{})
		close(drained)
	}
	m.finished = make(chan struct{})
	finished := m.finished
	cleanupTimeout := m.cleanupTimeout
	go func() {
		defer close(finished)
		<-drained
		ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
		defer cancel()
		marker := &inputCleanupMarker{}
		ctx = context.WithValue(ctx, inputCleanupMarkerKey{}, marker)
		// A replacement agent may start while the root-only executor holds
		// the maintenance lock. Serve health while loading asynchronously,
		// with admission still revoking until the reservation is reconciled.
		if m.maintenanceStore != nil && !m.maintenanceLoaded {
			record, loadErr := m.maintenanceStore.reconcileStartup(ctx)
			m.mu.Lock()
			m.maintenanceLoaded = true
			m.maintenance = record
			m.maintenanceFault = loadErr != nil
			if loadErr != nil {
				m.status.State = "blocked"
				m.status.Reason = "maintenance reservation could not be reconciled; operator recovery required"
				m.mu.Unlock()
				cancel()
				return
			}
			m.mu.Unlock()
		}
		var err error
		if m.cleanup == nil {
			err = ErrBlocked
		} else {
			err = m.cleanup(ctx)
		}
		confirmed := inputCleanupSucceeded(ctx, marker, err)
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.maintenanceFault {
			m.status.State = "blocked"
			m.status.Reason = "maintenance reservation could not be read; operator recovery required"
			return
		}
		if m.maintenanceActiveLocked() {
			m.reconcileMaintenanceLocked(err)
			return
		}
		if m.closed || (err != nil && !errors.Is(err, ErrRuntimeUnreachable)) {
			m.status.State = "blocked"
			m.status.Reason = "kit cleanup failed or agent shutting down; operator recovery required"
			return
		}
		if receipt != nil && confirmed && ctx.Err() == nil {
			receipt.confirmed.Store(true)
		}
		if m.claim.RequestID != "" {
			m.retired[m.claim.RequestID] = true
		}
		m.claim = ClaimRequest{}
		m.token = ""
		m.takeover = nil
		m.status = Status{State: "free", Generation: secret()}
		m.recordLocked(flightdiag.KindFenceHandoff, "ok", map[string]any{"reason": reason})
		if m.pending != nil {
			m.pendingReady = m.status.Generation
		}
	}()
}

// Close waits for admitted operations and cleanup before target dependencies
// are destroyed, bounded by the same cleanup deadline used during revocation.
func (m *Manager) Close() {
	m.mu.Lock()
	if !m.closed {
		m.closed = true
		if m.timer != nil {
			m.timer.Stop()
		}
		if m.status.State == "held" {
			m.revokeLocked("agent shutdown")
		} else if m.cancel != nil {
			m.cancel()
		}
	}
	finished := m.finished
	m.mu.Unlock()
	if finished != nil {
		timer := time.NewTimer(30 * time.Second)
		defer timer.Stop()
		select {
		case <-finished:
		case <-timer.C:
		}
	}
}

// Remaining duration lets clients use their own monotonic clocks even when the
// target has no RTC. Absolute expiry is informational only.
func (m *Manager) snapshotLocked() Status {
	s := m.status
	if s.State == "held" {
		s.ExpiresInMS = time.Until(s.ExpiresAt).Milliseconds()
		if s.ExpiresInMS < 1 {
			s.ExpiresInMS = 1
		}
	}
	return s
}
