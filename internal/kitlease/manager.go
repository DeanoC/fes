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
	"time"
	"unicode"
)

var (
	ErrBusy    = errors.New("kit is owned or recovery is running")
	ErrLease   = errors.New("kit lease is missing, expired or superseded")
	ErrInvalid = errors.New("invalid kit lease request")
	ErrBlocked = errors.New("kit cleanup failed or agent is shutting down")
)

type Status struct {
	ExpiresInMS int64     `json:"expires_in_ms"`
	State       string    `json:"state"`
	Generation  string    `json:"generation"`
	Owner       string    `json:"owner,omitempty"`
	Purpose     string    `json:"purpose,omitempty"`
	ExpiresAt   time.Time `json:"expires_at,omitempty"`
	Reason      string    `json:"reason,omitempty"`
}
type ClaimRequest struct {
	RequestID string `json:"request_id"`
	Owner     string `json:"owner"`
	Purpose   string `json:"purpose"`
}
type TakeoverRequest struct {
	ClaimRequest
	ExpectedGeneration string `json:"expected_generation"`
	Reason             string `json:"reason"`
}
type Grant struct {
	Status Status `json:"status"`
	Token  string `json:"token"`
}

type Manager struct {
	mu           sync.Mutex
	ttl          time.Duration
	cleanup      func(context.Context) error
	status       Status
	claim        ClaimRequest
	token        string
	leaseContext context.Context
	cancel       context.CancelFunc
	timer        *time.Timer
	refs         int
	drained      chan struct{}
	pending      *TakeoverRequest
	pendingReady string
	takeover     *TakeoverRequest
	retired      map[string]bool
	finished     chan struct{}
	closed       bool
}

// New reconciles/cleans existing target state before allowing the first claim.
// The callback must return success only after input is neutral and hardware idle.
func New(ttl time.Duration, cleanup func(context.Context) error) *Manager {
	if ttl <= 0 {
		ttl = 90 * time.Second
	}
	m := &Manager{ttl: ttl, cleanup: cleanup, retired: make(map[string]bool), status: Status{State: "free", Generation: secret()}}
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
	m.leaseContext, m.cancel = context.WithCancel(context.Background())
	m.status = Status{State: "held", Generation: secret(), Owner: r.Owner, Purpose: r.Purpose}
	m.renewLocked()
	return Grant{Status: m.snapshotLocked(), Token: m.token}
}
func (m *Manager) renewLocked() {
	m.status.ExpiresAt = time.Now().Add(m.ttl)
	if m.timer != nil {
		m.timer.Stop()
	}
	generation := m.status.Generation
	m.timer = time.AfterFunc(m.ttl, func() {
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
		return Grant{m.snapshotLocked(), m.token}, nil
	}
	if m.status.State == "blocked" {
		return Grant{}, ErrBlocked
	}
	if m.status.State != "free" {
		return Grant{}, ErrBusy
	}
	m.pending = nil
	m.pendingReady = ""
	return m.grantLocked(r), nil
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
	return Grant{m.snapshotLocked(), m.token}, nil
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
		m.revokeLocked("owner released lease")
	}
	return m.snapshotLocked(), nil
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
	if m.retired[r.RequestID] {
		return Grant{}, ErrLease
	}
	if m.takeover != nil && m.takeover.RequestID == r.RequestID {
		if *m.takeover != r {
			return Grant{}, ErrInvalid
		}
		if m.status.State == "held" {
			return Grant{m.snapshotLocked(), m.token}, nil
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
		return g, nil
	}
	if m.claim.RequestID == r.RequestID {
		return Grant{}, ErrInvalid
	}
	m.pending = &r
	m.pendingReady = ""
	m.revokeLocked("operator takeover: " + r.Reason)
	return Grant{}, ErrBusy
}
func (m *Manager) revokeLocked(reason string) {
	if m.status.State == "revoking" {
		return
	}
	m.status.State = "revoking"
	m.status.Reason = reason
	if m.timer != nil {
		m.timer.Stop()
	}
	if m.cancel != nil {
		m.cancel()
	}
	drained := m.drained
	if m.refs == 0 {
		drained = make(chan struct{})
		close(drained)
	}
	m.finished = make(chan struct{})
	finished := m.finished
	go func() {
		defer close(finished)
		<-drained
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		var err error
		if m.cleanup == nil {
			err = ErrBlocked
		} else {
			err = m.cleanup(ctx)
		}
		cancel()
		m.mu.Lock()
		defer m.mu.Unlock()
		if err != nil || m.closed {
			m.status.State = "blocked"
			m.status.Reason = "kit cleanup failed or agent shutting down; operator recovery required"
			return
		}
		if m.claim.RequestID != "" {
			m.retired[m.claim.RequestID] = true
		}
		m.claim = ClaimRequest{}
		m.token = ""
		m.takeover = nil
		m.status = Status{State: "free", Generation: secret()}
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
