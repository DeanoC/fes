// SPDX-License-Identifier: GPL-3.0-or-later

package ota

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
)

const (
	// FormatVersion is the on-disk schema version for STATE.JSN and META.JSN.
	FormatVersion = 1
	// DefaultRootDir is the default FAT directory holding OTA state and slot images.
	DefaultRootDir = "FES/OTA"
	// DefaultMaxImageBytes caps staged firmware/image payloads at 16 MiB.
	DefaultMaxImageBytes = 16 << 20
	// DefaultMaxTries is the default trial boot attempt budget before auto-rollback.
	DefaultMaxTries = 3
	// MaxAllowedTries caps Config.MaxTries.
	MaxAllowedTries = 10

	maxStateBytes    = 16384
	maxManifestBytes = 8192
)

var (
	// ErrCorruptState indicates malformed or inconsistent on-disk OTA metadata.
	ErrCorruptState = errors.New("ota: corrupt metadata or state")
	// ErrInvalidPhase indicates an operation is not valid in the current A/B phase.
	ErrInvalidPhase = errors.New("ota: invalid state phase for operation")
	// ErrTargetMismatch indicates an image targets a different board/platform.
	ErrTargetMismatch = errors.New("ota: target board mismatch")
	// ErrDigestMismatch indicates a payload SHA-256 mismatch.
	ErrDigestMismatch = errors.New("ota: image digest mismatch")
	// ErrRollbackRejected indicates EnforceRollbackProtection rejected a lower epoch.
	ErrRollbackRejected = errors.New("ota: rollback epoch below active slot")
	// ErrInsufficientSpace indicates too little free FAT space for staging safely.
	ErrInsufficientSpace = errors.New("ota: insufficient storage space")
	// ErrSlotUnavailable indicates the requested fallback slot is empty, unhealthy, or invalid.
	ErrSlotUnavailable = errors.New("ota: target slot unavailable")
	// ErrSignatureInvalid indicates optional signature verification rejected the payload.
	ErrSignatureInvalid = errors.New("ota: invalid image signature")
)

// Slot identifies one of the two redundant A/B image partitions/directories.
type Slot string

const (
	SlotA Slot = "A"
	SlotB Slot = "B"
)

// Valid reports whether s is SlotA or SlotB.
func (s Slot) Valid() bool {
	return s == SlotA || s == SlotB
}

// Other returns the alternate A/B slot (or "" if s is invalid).
func (s Slot) Other() Slot {
	switch s {
	case SlotA:
		return SlotB
	case SlotB:
		return SlotA
	default:
		return ""
	}
}

// Dir returns the FAT directory path for slot s beneath root.
func (s Slot) Dir(root string) string {
	return path.Join(root, "SLOT"+string(s))
}

// ImagePath returns the image binary path for slot s beneath root.
func (s Slot) ImagePath(root string) string {
	return path.Join(s.Dir(root), "IMAGE.BIN")
}

// ManifestPath returns the slot metadata path for slot s beneath root.
func (s Slot) ManifestPath(root string) string {
	return path.Join(s.Dir(root), "META.JSN")
}

func statePath(root string) string {
	return path.Join(root, "STATE.JSN")
}

// Phase describes the A/B update lifecycle state stored in STATE.JSN.
type Phase string

const (
	// PhaseIdle means ActiveSlot is confirmed and no update is staged/pending.
	PhaseIdle Phase = "idle"
	// PhaseStaging is persisted before writing candidate IMAGE.BIN; a power loss
	// here recovers cleanly back to PhaseIdle on the next boot.
	PhaseStaging Phase = "staging"
	// PhaseStaged means candidate IMAGE.BIN and META.JSN are verified on disk,
	// waiting for explicit Activate before boot selection switches.
	PhaseStaged Phase = "staged"
	// PhasePending means Activate succeeded and the next SelectBoot will start a trial.
	PhasePending Phase = "pending"
	// PhaseTrial means a trial boot of PendingSlot is underway using TriesRemaining.
	PhaseTrial Phase = "trial"
)

// SlotManifest is persisted at <root>/SLOT{A,B}/META.JSN alongside IMAGE.BIN.
type SlotManifest struct {
	Format        int    `json:"format"`
	Slot          Slot   `json:"slot"`
	Version       string `json:"version"`
	Target        string `json:"target,omitempty"`
	Size          int    `json:"size"`
	SHA256        string `json:"sha256"`
	RollbackEpoch uint64 `json:"rollback_epoch"`
	StagedAtUnix  int64  `json:"staged_at_unix,omitempty"`
}

// Validate checks manifest fields and bounds.
func (m SlotManifest) Validate(maxImageBytes int) error {
	if m.Format != FormatVersion || !m.Slot.Valid() || strings.TrimSpace(m.Version) == "" ||
		m.Size <= 0 || (maxImageBytes > 0 && m.Size > maxImageBytes) || !isLowerHex64(m.SHA256) {
		return fmt.Errorf("%w: invalid slot manifest", ErrCorruptState)
	}
	return nil
}

// SlotRecord summarizes a slot's metadata inside STATE.JSN.
type SlotRecord struct {
	Version         string `json:"version"`
	Target          string `json:"target,omitempty"`
	Size            int    `json:"size"`
	SHA256          string `json:"sha256"`
	RollbackEpoch   uint64 `json:"rollback_epoch"`
	StagedAtUnix    int64  `json:"staged_at_unix,omitempty"`
	ConfirmedAtUnix int64  `json:"confirmed_at_unix,omitempty"`
	Healthy         bool   `json:"healthy"`
}

func (r *SlotRecord) clone() *SlotRecord {
	if r == nil {
		return nil
	}
	cp := *r
	return &cp
}

func (r *SlotRecord) validate() error {
	if r == nil {
		return nil
	}
	if strings.TrimSpace(r.Version) == "" || r.Size <= 0 || !isLowerHex64(r.SHA256) {
		return fmt.Errorf("%w: invalid slot record", ErrCorruptState)
	}
	return nil
}

func (r *SlotRecord) matchesManifest(m SlotManifest) bool {
	if r == nil {
		return false
	}
	return r.Version == m.Version && r.Target == m.Target && r.Size == m.Size &&
		r.SHA256 == m.SHA256 && r.RollbackEpoch == m.RollbackEpoch
}

// State is the durable A/B OTA state machine persisted at <root>/STATE.JSN.
type State struct {
	Format         int         `json:"format"`
	Generation     uint64      `json:"generation"`
	ActiveSlot     Slot        `json:"active_slot"`
	PendingSlot    Slot        `json:"pending_slot,omitempty"`
	Phase          Phase       `json:"phase"`
	TriesRemaining int         `json:"tries_remaining"`
	SlotA          *SlotRecord `json:"slot_a,omitempty"`
	SlotB          *SlotRecord `json:"slot_b,omitempty"`
	LastEvent      string      `json:"last_event,omitempty"`
	LastError      string      `json:"last_error,omitempty"`
}

// Record returns a deep copy of slot's record (or nil if unpopulated).
func (s State) Record(slot Slot) *SlotRecord {
	switch slot {
	case SlotA:
		return s.SlotA.clone()
	case SlotB:
		return s.SlotB.clone()
	default:
		return nil
	}
}

func (s *State) setRecord(slot Slot, rec *SlotRecord) {
	switch slot {
	case SlotA:
		s.SlotA = rec.clone()
	case SlotB:
		s.SlotB = rec.clone()
	}
}

// Clone returns an independent copy of s.
func (s State) Clone() State {
	cp := s
	cp.SlotA = s.SlotA.clone()
	cp.SlotB = s.SlotB.clone()
	return cp
}

// Validate checks internal state machine invariants.
func (s State) Validate() error {
	if s.Format != FormatVersion || s.Generation == 0 || !s.ActiveSlot.Valid() {
		return fmt.Errorf("%w: invalid state header", ErrCorruptState)
	}
	if err := s.SlotA.validate(); err != nil {
		return err
	}
	if err := s.SlotB.validate(); err != nil {
		return err
	}
	alt := s.ActiveSlot.Other()
	switch s.Phase {
	case PhaseIdle:
		if s.PendingSlot != "" || s.TriesRemaining != 0 {
			return fmt.Errorf("%w: idle state has pending fields", ErrCorruptState)
		}
	case PhaseStaging:
		if s.PendingSlot != alt || s.TriesRemaining != 0 {
			return fmt.Errorf("%w: invalid staging state", ErrCorruptState)
		}
	case PhaseStaged:
		if s.PendingSlot != alt || s.TriesRemaining != 0 || s.Record(alt) == nil {
			return fmt.Errorf("%w: invalid staged state", ErrCorruptState)
		}
	case PhasePending:
		if s.PendingSlot != alt || s.TriesRemaining < 1 || s.TriesRemaining > MaxAllowedTries || s.Record(alt) == nil {
			return fmt.Errorf("%w: invalid pending state", ErrCorruptState)
		}
	case PhaseTrial:
		if s.PendingSlot != alt || s.TriesRemaining < 0 || s.TriesRemaining > MaxAllowedTries || s.Record(alt) == nil {
			return fmt.Errorf("%w: invalid trial state", ErrCorruptState)
		}
	default:
		return fmt.Errorf("%w: unknown phase %q", ErrCorruptState, s.Phase)
	}
	return nil
}

func isLowerHex64(s string) bool {
	if len(s) != 64 || strings.ToLower(s) != s {
		return false
	}
	v, err := hex.DecodeString(s)
	return err == nil && len(v) == 32
}

func validateRootDir(root string) (string, error) {
	if root == "" {
		root = DefaultRootDir
	}
	if !fs.ValidPath(root) || root == "." {
		return "", fmt.Errorf("ota: invalid root directory %q", root)
	}
	return root, nil
}

func decodeStrictJSON(data []byte, maxBytes int, dst any) error {
	if len(data) == 0 || len(data) > maxBytes {
		return fmt.Errorf("%w: invalid metadata length %d", ErrCorruptState, len(data))
	}
	check := json.NewDecoder(bytes.NewReader(data))
	if err := checkUniqueKeys(check); err != nil {
		return fmt.Errorf("%w: %v", ErrCorruptState, err)
	}
	if _, err := check.Token(); err != io.EOF {
		return fmt.Errorf("%w: trailing JSON data", ErrCorruptState)
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return fmt.Errorf("%w: %v", ErrCorruptState, err)
	}
	if d.Decode(new(any)) != io.EOF {
		return fmt.Errorf("%w: trailing JSON value", ErrCorruptState)
	}
	return nil
}

func checkUniqueKeys(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]bool)
		for dec.More() {
			kTok, err := dec.Token()
			if err != nil {
				return err
			}
			key, ok := kTok.(string)
			if !ok || seen[key] {
				return fmt.Errorf("duplicate or invalid JSON field %v", kTok)
			}
			seen[key] = true
			if err := checkUniqueKeys(dec); err != nil {
				return err
			}
		}
		end, err := dec.Token()
		if err != nil || end != json.Delim('}') {
			return fmt.Errorf("unterminated JSON object")
		}
	case '[':
		for dec.More() {
			if err := checkUniqueKeys(dec); err != nil {
				return err
			}
		}
		end, err := dec.Token()
		if err != nil || end != json.Delim(']') {
			return fmt.Errorf("unterminated JSON array")
		}
	}
	return nil
}
