// SPDX-License-Identifier: GPL-3.0-or-later

// Package ota implements crash-safe A/B update staging, activation, trial-boot
// selection, and automatic rollback over FAT32 volumes such as storagepolicy.Volume.
package ota

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"sync"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/fat"
)

const minFreeHeadroomBytes = 64 << 10

// FileSystem is the minimal durable FAT interface needed by Manager.
// *fat.Volume and *storagepolicy.Volume both satisfy it directly.
type FileSystem interface {
	ReadFile(name string) ([]byte, error)
	CreateFile(ctx context.Context, name string, data []byte) error
	ReplaceFile(ctx context.Context, name string, data []byte) error
	RemoveFile(ctx context.Context, name string) error
	MkdirAll(ctx context.Context, name string) error
}

// StatFSProvider optionally reports free space so Stage can fail before mutating storage.
type StatFSProvider interface {
	StatFS(ctx context.Context) (fat.FSStats, error)
}

// Flusher optionally flushes durable state writes to the backing media.
type Flusher interface {
	Flush() error
}

// SignatureVerifier validates candidate payload digests before disk mutation.
type SignatureVerifier func(manifest SlotManifest, digest [32]byte, signature []byte) error

// Config parameterizes an A/B OTA Manager.
type Config struct {
	RootDir                   string
	Board                     string
	InitialSlot               Slot
	MaxImageBytes             int
	MaxTries                  int
	EnforceRollbackProtection bool
	VerifySignature           SignatureVerifier
	Now                       func() time.Time
}

func (c Config) normalized() (Config, error) {
	root, err := validateRootDir(c.RootDir)
	if err != nil {
		return c, err
	}
	c.RootDir = root
	if c.InitialSlot == "" {
		c.InitialSlot = SlotA
	}
	if !c.InitialSlot.Valid() {
		return c, fmt.Errorf("ota: invalid initial slot %q", c.InitialSlot)
	}
	if c.MaxImageBytes == 0 {
		c.MaxImageBytes = DefaultMaxImageBytes
	}
	if c.MaxImageBytes < 512 {
		return c, fmt.Errorf("ota: invalid MaxImageBytes %d", c.MaxImageBytes)
	}
	if c.MaxTries == 0 {
		c.MaxTries = DefaultMaxTries
	}
	if c.MaxTries < 1 || c.MaxTries > MaxAllowedTries {
		return c, fmt.Errorf("ota: MaxTries %d outside [1, %d]", c.MaxTries, MaxAllowedTries)
	}
	return c, nil
}

// StageRequest supplies a candidate payload to SeedActiveSlot or Stage.
type StageRequest struct {
	Version        string
	Target         string
	Payload        []byte
	ExpectedSHA256 string
	RollbackEpoch  uint64
	Signature      []byte
}

// BootDecision describes the boot slot selected by SelectBoot on startup.
type BootDecision struct {
	Slot                      Slot
	IsTrial                   bool
	TriesRemaining            int
	RolledBack                bool
	RecoveredInterruptedStage bool
	Reason                    string
	Manifest                  *SlotManifest
}

// Manager coordinates crash-safe A/B slot transitions over a FileSystem.
type Manager struct {
	mu        sync.Mutex
	fs        FileSystem
	cfg       Config
	state     State
	persisted bool
}

// Open reads and validates <root>/STATE.JSN if present, or initializes clean in-memory state.
func Open(ctx context.Context, filesystem FileSystem, cfg Config) (*Manager, error) {
	if ctx == nil || filesystem == nil {
		return nil, fmt.Errorf("ota: context and filesystem required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	norm, err := cfg.normalized()
	if err != nil {
		return nil, err
	}
	m := &Manager{fs: filesystem, cfg: norm}
	raw, err := filesystem.ReadFile(statePath(norm.RootDir))
	if errors.Is(err, fs.ErrNotExist) {
		m.state = State{
			Format:     FormatVersion,
			Generation: 1,
			ActiveSlot: norm.InitialSlot,
			Phase:      PhaseIdle,
			LastEvent:  fmt.Sprintf("initialized on slot %s", norm.InitialSlot),
		}
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	var st State
	if err := decodeStrictJSON(raw, maxStateBytes, &st); err != nil {
		return nil, err
	}
	if err := st.Validate(); err != nil {
		return nil, err
	}
	m.state = st
	m.persisted = true
	return m, nil
}

// Status returns a deep copy of the current A/B state.
func (m *Manager) Status() State {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state.Clone()
}

// Initialize creates the OTA directory tree and persists initial STATE.JSN if absent.
func (m *Manager) Initialize(ctx context.Context) (State, error) {
	if ctx == nil {
		return State{}, fmt.Errorf("ota: context required")
	}
	if err := ctx.Err(); err != nil {
		return State{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ensureDirsLocked(ctx); err != nil {
		return State{}, err
	}
	if !m.persisted {
		if err := m.persistStateLocked(ctx, m.state); err != nil {
			return State{}, err
		}
	}
	return m.state.Clone(), nil
}

// SeedActiveSlot installs an initial confirmed payload into the currently active slot
// when that slot has no metadata record yet.
func (m *Manager) SeedActiveSlot(ctx context.Context, req StageRequest) (SlotRecord, error) {
	if ctx == nil {
		return SlotRecord{}, fmt.Errorf("ota: context required")
	}
	if err := ctx.Err(); err != nil {
		return SlotRecord{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state.Phase != PhaseIdle {
		return SlotRecord{}, fmt.Errorf("%w: SeedActiveSlot requires idle phase, got %s", ErrInvalidPhase, m.state.Phase)
	}
	slot := m.state.ActiveSlot
	if m.state.Record(slot) != nil {
		return SlotRecord{}, fmt.Errorf("%w: active slot %s already seeded", ErrInvalidPhase, slot)
	}
	manifest, _, err := m.validateRequestLocked(ctx, slot, req, false)
	if err != nil {
		return SlotRecord{}, err
	}
	if err := m.ensureDirsLocked(ctx); err != nil {
		return SlotRecord{}, err
	}
	if err := m.writeAndVerifySlotLocked(ctx, slot, req.Payload, manifest); err != nil {
		return SlotRecord{}, err
	}
	nowUnix := manifest.StagedAtUnix
	rec := SlotRecord{
		Version:         manifest.Version,
		Target:          manifest.Target,
		Size:            manifest.Size,
		SHA256:          manifest.SHA256,
		RollbackEpoch:   manifest.RollbackEpoch,
		StagedAtUnix:    manifest.StagedAtUnix,
		ConfirmedAtUnix: nowUnix,
		Healthy:         true,
	}
	next := m.state.Clone()
	next.Generation++
	next.setRecord(slot, &rec)
	next.LastEvent = fmt.Sprintf("seeded active slot %s (%s)", slot, rec.Version)
	next.LastError = ""
	if err := m.persistStateLocked(ctx, next); err != nil {
		return SlotRecord{}, err
	}
	return rec, nil
}

// Stage writes and verifies a candidate update into the inactive slot without changing ActiveSlot.
func (m *Manager) Stage(ctx context.Context, req StageRequest) (SlotRecord, error) {
	if ctx == nil {
		return SlotRecord{}, fmt.Errorf("ota: context required")
	}
	if err := ctx.Err(); err != nil {
		return SlotRecord{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state.Phase != PhaseIdle && m.state.Phase != PhaseStaging && m.state.Phase != PhaseStaged {
		return SlotRecord{}, fmt.Errorf("%w: cannot stage while phase is %s", ErrInvalidPhase, m.state.Phase)
	}
	target := m.state.ActiveSlot.Other()
	manifest, _, err := m.validateRequestLocked(ctx, target, req, true)
	if err != nil {
		return SlotRecord{}, err
	}
	if err := m.ensureDirsLocked(ctx); err != nil {
		return SlotRecord{}, err
	}

	// Step 1: Mark PhaseStaging before mutating candidate IMAGE.BIN so interrupted writes
	// recover cleanly back to PhaseIdle on next SelectBoot.
	staging := m.state.Clone()
	staging.Generation++
	staging.Phase = PhaseStaging
	staging.PendingSlot = target
	staging.TriesRemaining = 0
	staging.LastEvent = fmt.Sprintf("staging slot %s (%s)", target, manifest.Version)
	staging.LastError = ""
	if err := m.persistStateLocked(ctx, staging); err != nil {
		return SlotRecord{}, err
	}

	// Step 2-4: Write candidate payload via copy-on-write ReplaceFile, read back & verify SHA-256, then META.JSN.
	if err := m.writeAndVerifySlotLocked(ctx, target, req.Payload, manifest); err != nil {
		return SlotRecord{}, err
	}

	// Step 5: Promote state to PhaseStaged (candidate still inactive until Activate + trial ConfirmBoot).
	rec := SlotRecord{
		Version:       manifest.Version,
		Target:        manifest.Target,
		Size:          manifest.Size,
		SHA256:        manifest.SHA256,
		RollbackEpoch: manifest.RollbackEpoch,
		StagedAtUnix:  manifest.StagedAtUnix,
		Healthy:       false,
	}
	staged := m.state.Clone()
	staged.Generation++
	staged.Phase = PhaseStaged
	staged.PendingSlot = target
	staged.TriesRemaining = 0
	staged.setRecord(target, &rec)
	staged.LastEvent = fmt.Sprintf("staged slot %s (%s)", target, rec.Version)
	staged.LastError = ""
	if err := m.persistStateLocked(ctx, staged); err != nil {
		return SlotRecord{}, err
	}
	return rec, nil
}

// Activate re-verifies the staged candidate slot on disk and arms a trial boot budget.
func (m *Manager) Activate(ctx context.Context) (State, error) {
	if ctx == nil {
		return State{}, fmt.Errorf("ota: context required")
	}
	if err := ctx.Err(); err != nil {
		return State{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state.Phase != PhaseStaged || m.state.PendingSlot != m.state.ActiveSlot.Other() {
		return State{}, fmt.Errorf("%w: Activate requires staged phase, got %s", ErrInvalidPhase, m.state.Phase)
	}
	if _, err := m.verifySlotLocked(ctx, m.state.PendingSlot); err != nil {
		return State{}, err
	}
	next := m.state.Clone()
	next.Generation++
	next.Phase = PhasePending
	next.TriesRemaining = m.cfg.MaxTries
	next.LastEvent = fmt.Sprintf("activated slot %s for %d trial boots", next.PendingSlot, next.TriesRemaining)
	next.LastError = ""
	if err := m.persistStateLocked(ctx, next); err != nil {
		return State{}, err
	}
	return m.state.Clone(), nil
}

// SelectBoot decides which slot to boot next, decrementing trial attempts or automatically
// rolling back to ActiveSlot when trial boots are exhausted or candidate verification fails.
func (m *Manager) SelectBoot(ctx context.Context) (BootDecision, error) {
	if ctx == nil {
		return BootDecision{}, fmt.Errorf("ota: context required")
	}
	if err := ctx.Err(); err != nil {
		return BootDecision{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	switch m.state.Phase {
	case PhaseIdle, PhaseStaged:
		return BootDecision{
			Slot:   m.state.ActiveSlot,
			Reason: "booting active slot",
		}, nil

	case PhaseStaging:
		next := m.state.Clone()
		interrupted := next.PendingSlot
		next.Generation++
		next.Phase = PhaseIdle
		next.PendingSlot = ""
		next.TriesRemaining = 0
		next.LastEvent = fmt.Sprintf("recovered interrupted staging on slot %s", interrupted)
		next.LastError = ""
		if err := m.persistStateLocked(ctx, next); err != nil {
			return BootDecision{}, err
		}
		return BootDecision{
			Slot:                      m.state.ActiveSlot,
			RecoveredInterruptedStage: true,
			Reason:                    next.LastEvent,
		}, nil

	case PhasePending, PhaseTrial:
		cand := m.state.PendingSlot
		if m.state.TriesRemaining <= 0 {
			reason := fmt.Sprintf("trial boot attempts exhausted on slot %s", cand)
			if err := m.rollbackPendingLocked(ctx, reason); err != nil {
				return BootDecision{}, err
			}
			return BootDecision{
				Slot:       m.state.ActiveSlot,
				RolledBack: true,
				Reason:     reason,
			}, nil
		}
		manifest, err := m.verifySlotLocked(ctx, cand)
		if err != nil {
			reason := fmt.Sprintf("candidate slot %s verification failed: %v", cand, err)
			if rbErr := m.rollbackPendingLocked(ctx, reason); rbErr != nil {
				return BootDecision{}, rbErr
			}
			return BootDecision{
				Slot:       m.state.ActiveSlot,
				RolledBack: true,
				Reason:     reason,
			}, nil
		}
		next := m.state.Clone()
		next.Generation++
		next.Phase = PhaseTrial
		next.TriesRemaining--
		next.LastEvent = fmt.Sprintf("trial boot slot %s (%d tries left)", cand, next.TriesRemaining)
		next.LastError = ""
		if err := m.persistStateLocked(ctx, next); err != nil {
			return BootDecision{}, err
		}
		cp := manifest
		return BootDecision{
			Slot:           cand,
			IsTrial:        true,
			TriesRemaining: next.TriesRemaining,
			Reason:         next.LastEvent,
			Manifest:       &cp,
		}, nil

	default:
		return BootDecision{}, fmt.Errorf("%w: unexpected phase %s", ErrCorruptState, m.state.Phase)
	}
}

// ConfirmBoot promotes a successful trial boot into the confirmed active slot.
// Calling ConfirmBoot when already idle on bootedSlot is an idempotent no-op.
func (m *Manager) ConfirmBoot(ctx context.Context, bootedSlot Slot) (State, error) {
	if ctx == nil {
		return State{}, fmt.Errorf("ota: context required")
	}
	if err := ctx.Err(); err != nil {
		return State{}, err
	}
	if !bootedSlot.Valid() {
		return State{}, fmt.Errorf("%w: invalid slot %q", ErrSlotUnavailable, bootedSlot)
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.state.Phase == PhaseIdle && bootedSlot == m.state.ActiveSlot {
		return m.state.Clone(), nil
	}
	if m.state.Phase != PhaseTrial || bootedSlot != m.state.PendingSlot {
		return State{}, fmt.Errorf("%w: cannot confirm slot %s in phase %s (pending=%s)", ErrInvalidPhase, bootedSlot, m.state.Phase, m.state.PendingSlot)
	}
	next := m.state.Clone()
	rec := next.Record(bootedSlot)
	if rec == nil {
		return State{}, fmt.Errorf("%w: missing candidate record for slot %s", ErrCorruptState, bootedSlot)
	}
	rec.Healthy = true
	if now := m.nowUnixLocked(); now != 0 {
		rec.ConfirmedAtUnix = now
	}
	next.Generation++
	next.ActiveSlot = bootedSlot
	next.PendingSlot = ""
	next.Phase = PhaseIdle
	next.TriesRemaining = 0
	next.setRecord(bootedSlot, rec)
	next.LastEvent = fmt.Sprintf("confirmed slot %s (%s)", bootedSlot, rec.Version)
	next.LastError = ""
	if err := m.persistStateLocked(ctx, next); err != nil {
		return State{}, err
	}
	return m.state.Clone(), nil
}

// Rollback reverts an unconfirmed pending/trial update or switches back from Idle
// to the alternate slot when that slot holds a verified healthy image.
func (m *Manager) Rollback(ctx context.Context, reason string) (State, error) {
	if ctx == nil {
		return State{}, fmt.Errorf("ota: context required")
	}
	if err := ctx.Err(); err != nil {
		return State{}, err
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "operator requested rollback"
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	switch m.state.Phase {
	case PhasePending, PhaseTrial:
		if err := m.rollbackPendingLocked(ctx, reason); err != nil {
			return State{}, err
		}
		return m.state.Clone(), nil

	case PhaseIdle:
		prev := m.state.ActiveSlot.Other()
		rec := m.state.Record(prev)
		if rec == nil || !rec.Healthy {
			return State{}, fmt.Errorf("%w: fallback slot %s is not healthy", ErrSlotUnavailable, prev)
		}
		if _, err := m.verifySlotLocked(ctx, prev); err != nil {
			return State{}, fmt.Errorf("%w: %v", ErrSlotUnavailable, err)
		}
		next := m.state.Clone()
		next.Generation++
		next.ActiveSlot = prev
		next.LastEvent = fmt.Sprintf("rolled back active slot to %s (%s): %s", prev, rec.Version, reason)
		next.LastError = ""
		if err := m.persistStateLocked(ctx, next); err != nil {
			return State{}, err
		}
		return m.state.Clone(), nil

	default:
		return State{}, fmt.Errorf("%w: Rollback invalid in phase %s", ErrInvalidPhase, m.state.Phase)
	}
}

// CancelPending aborts an inactive Staging, Staged, or Pending update back to Idle.
func (m *Manager) CancelPending(ctx context.Context) (State, error) {
	if ctx == nil {
		return State{}, fmt.Errorf("ota: context required")
	}
	if err := ctx.Err(); err != nil {
		return State{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	switch m.state.Phase {
	case PhaseIdle:
		return m.state.Clone(), nil
	case PhaseStaging, PhaseStaged, PhasePending:
		cand := m.state.PendingSlot
		next := m.state.Clone()
		next.Generation++
		next.Phase = PhaseIdle
		next.PendingSlot = ""
		next.TriesRemaining = 0
		next.LastEvent = fmt.Sprintf("canceled pending update on slot %s", cand)
		next.LastError = ""
		if err := m.persistStateLocked(ctx, next); err != nil {
			return State{}, err
		}
		return m.state.Clone(), nil
	default:
		return State{}, fmt.Errorf("%w: cannot cancel update in phase %s", ErrInvalidPhase, m.state.Phase)
	}
}

// VerifySlot reads and verifies slot's META.JSN and IMAGE.BIN on disk against STATE.JSN.
func (m *Manager) VerifySlot(ctx context.Context, slot Slot) (SlotManifest, error) {
	if ctx == nil {
		return SlotManifest{}, fmt.Errorf("ota: context required")
	}
	if err := ctx.Err(); err != nil {
		return SlotManifest{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.verifySlotLocked(ctx, slot)
}

// ResetToActive explicitly repairs or resets on-disk STATE.JSN back to an idle active slot.
func ResetToActive(ctx context.Context, filesystem FileSystem, cfg Config, active Slot) (*Manager, error) {
	if ctx == nil || filesystem == nil {
		return nil, fmt.Errorf("ota: context and filesystem required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !active.Valid() {
		return nil, fmt.Errorf("%w: invalid active slot %q", ErrSlotUnavailable, active)
	}
	norm, err := cfg.normalized()
	if err != nil {
		return nil, err
	}
	m := &Manager{
		fs:  filesystem,
		cfg: norm,
		state: State{
			Format:     FormatVersion,
			Generation: 1,
			ActiveSlot: active,
			Phase:      PhaseIdle,
			LastEvent:  fmt.Sprintf("operator reset state to slot %s", active),
		},
	}
	if err := m.ensureDirsLocked(ctx); err != nil {
		return nil, err
	}
	if err := m.persistStateLocked(ctx, m.state); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *Manager) nowUnixLocked() int64 {
	if m.cfg.Now == nil {
		return 0
	}
	return m.cfg.Now().UTC().Unix()
}

func (m *Manager) validateRequestLocked(ctx context.Context, target Slot, req StageRequest, checkRollback bool) (SlotManifest, [32]byte, error) {
	var zero [32]byte
	version := strings.TrimSpace(req.Version)
	if version == "" {
		return SlotManifest{}, zero, fmt.Errorf("%w: image version required", ErrCorruptState)
	}
	if m.cfg.Board != "" && req.Target != m.cfg.Board {
		return SlotManifest{}, zero, fmt.Errorf("%w: got %q, want %q", ErrTargetMismatch, req.Target, m.cfg.Board)
	}
	if len(req.Payload) == 0 || len(req.Payload) > m.cfg.MaxImageBytes {
		return SlotManifest{}, zero, fmt.Errorf("%w: payload size %d outside [1, %d]", ErrCorruptState, len(req.Payload), m.cfg.MaxImageBytes)
	}
	sum := sha256.Sum256(req.Payload)
	sumHex := fmt.Sprintf("%x", sum)
	if req.ExpectedSHA256 != "" && strings.ToLower(req.ExpectedSHA256) != sumHex {
		return SlotManifest{}, zero, fmt.Errorf("%w: expected %s, got %s", ErrDigestMismatch, req.ExpectedSHA256, sumHex)
	}
	if checkRollback && m.cfg.EnforceRollbackProtection {
		if activeRec := m.state.Record(m.state.ActiveSlot); activeRec != nil && req.RollbackEpoch < activeRec.RollbackEpoch {
			return SlotManifest{}, zero, fmt.Errorf("%w: candidate epoch %d < active %d", ErrRollbackRejected, req.RollbackEpoch, activeRec.RollbackEpoch)
		}
	}
	if sp, ok := m.fs.(StatFSProvider); ok {
		stats, err := sp.StatFS(ctx)
		if err != nil {
			return SlotManifest{}, zero, err
		}
		need := uint64(len(req.Payload)) + minFreeHeadroomBytes
		if stats.FreeBytes < need {
			return SlotManifest{}, zero, fmt.Errorf("%w: have %d free bytes, need %d", ErrInsufficientSpace, stats.FreeBytes, need)
		}
	}
	manifest := SlotManifest{
		Format:        FormatVersion,
		Slot:          target,
		Version:       version,
		Target:        req.Target,
		Size:          len(req.Payload),
		SHA256:        sumHex,
		RollbackEpoch: req.RollbackEpoch,
		StagedAtUnix:  m.nowUnixLocked(),
	}
	if m.cfg.VerifySignature != nil {
		if err := m.cfg.VerifySignature(manifest, sum, req.Signature); err != nil {
			return SlotManifest{}, zero, fmt.Errorf("%w: %v", ErrSignatureInvalid, err)
		}
	}
	return manifest, sum, nil
}

func (m *Manager) ensureDirsLocked(ctx context.Context) error {
	for _, dir := range []string{m.cfg.RootDir, SlotA.Dir(m.cfg.RootDir), SlotB.Dir(m.cfg.RootDir)} {
		if err := m.fs.MkdirAll(ctx, dir); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) writeAndVerifySlotLocked(ctx context.Context, slot Slot, payload []byte, manifest SlotManifest) error {
	imgPath := slot.ImagePath(m.cfg.RootDir)
	if err := m.fs.ReplaceFile(ctx, imgPath, payload); err != nil {
		return err
	}
	readback, err := m.fs.ReadFile(imgPath)
	if err != nil {
		return err
	}
	if len(readback) != len(payload) || fmt.Sprintf("%x", sha256.Sum256(readback)) != manifest.SHA256 {
		return fmt.Errorf("%w: slot %s readback verification failed", ErrDigestMismatch, slot)
	}
	metaBytes, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	return m.fs.ReplaceFile(ctx, slot.ManifestPath(m.cfg.RootDir), metaBytes)
}

func (m *Manager) verifySlotLocked(ctx context.Context, slot Slot) (SlotManifest, error) {
	if err := ctx.Err(); err != nil {
		return SlotManifest{}, err
	}
	if !slot.Valid() {
		return SlotManifest{}, fmt.Errorf("%w: invalid slot %q", ErrSlotUnavailable, slot)
	}
	rec := m.state.Record(slot)
	if rec == nil {
		return SlotManifest{}, fmt.Errorf("%w: slot %s is empty", ErrSlotUnavailable, slot)
	}
	rawMeta, err := m.fs.ReadFile(slot.ManifestPath(m.cfg.RootDir))
	if err != nil {
		return SlotManifest{}, fmt.Errorf("%w: slot %s manifest: %v", ErrCorruptState, slot, err)
	}
	var manifest SlotManifest
	if err := decodeStrictJSON(rawMeta, maxManifestBytes, &manifest); err != nil {
		return SlotManifest{}, err
	}
	if err := manifest.Validate(m.cfg.MaxImageBytes); err != nil {
		return SlotManifest{}, err
	}
	if manifest.Slot != slot {
		return SlotManifest{}, fmt.Errorf("%w: slot manifest claims %s in slot %s", ErrCorruptState, manifest.Slot, slot)
	}
	if !rec.matchesManifest(manifest) {
		return SlotManifest{}, fmt.Errorf("%w: slot %s manifest diverges from STATE.JSN", ErrCorruptState, slot)
	}
	if m.cfg.Board != "" && manifest.Target != m.cfg.Board {
		return SlotManifest{}, fmt.Errorf("%w: slot %s target %q != %q", ErrTargetMismatch, slot, manifest.Target, m.cfg.Board)
	}
	payload, err := m.fs.ReadFile(slot.ImagePath(m.cfg.RootDir))
	if err != nil {
		return SlotManifest{}, fmt.Errorf("%w: slot %s image: %v", ErrCorruptState, slot, err)
	}
	if len(payload) != manifest.Size {
		return SlotManifest{}, fmt.Errorf("%w: slot %s image size %d != %d", ErrDigestMismatch, slot, len(payload), manifest.Size)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(payload)); got != manifest.SHA256 {
		return SlotManifest{}, fmt.Errorf("%w: slot %s image SHA-256 mismatch", ErrDigestMismatch, slot)
	}
	return manifest, nil
}

func (m *Manager) rollbackPendingLocked(ctx context.Context, reason string) error {
	cand := m.state.PendingSlot
	next := m.state.Clone()
	if rec := next.Record(cand); rec != nil {
		rec.Healthy = false
		next.setRecord(cand, rec)
	}
	next.Generation++
	next.Phase = PhaseIdle
	next.PendingSlot = ""
	next.TriesRemaining = 0
	next.LastEvent = fmt.Sprintf("rolled back candidate slot %s to active slot %s", cand, next.ActiveSlot)
	next.LastError = reason
	return m.persistStateLocked(ctx, next)
}

func (m *Manager) persistStateLocked(ctx context.Context, next State) error {
	if err := next.Validate(); err != nil {
		return err
	}
	raw, err := json.Marshal(next)
	if err != nil {
		return err
	}
	if len(raw) > maxStateBytes {
		return fmt.Errorf("%w: encoded state size %d exceeds limit", ErrCorruptState, len(raw))
	}
	if err := m.fs.ReplaceFile(ctx, statePath(m.cfg.RootDir), raw); err != nil {
		return err
	}
	if fl, ok := m.fs.(Flusher); ok {
		if err := fl.Flush(); err != nil {
			return err
		}
	}
	m.state = next
	m.persisted = true
	return nil
}
