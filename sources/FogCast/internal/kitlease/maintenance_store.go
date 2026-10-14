package kitlease

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	contract "github.com/DeanoC/FogCast/kitlease"
	"golang.org/x/sys/unix"
)

// This file is also read by the root-only software replacement executor. Keep
// public identity fields at the top level; credentials never leave the agent.
type maintenanceRecord struct {
	Schema int `json:"schema"`
	contract.MaintenanceStatus
	Token      string `json:"token"`
	StartToken string `json:"start_token"`
}

type maintenanceStore struct{ path, lockPath, bootID string }

// WithMaintenanceFile enables target-side reservations. /run storage survives
// agent replacement but is cleared by boot; BootID rejects copied stale records.
// Loading happens inside startup reconciliation, before any claim is admitted.
func WithMaintenanceFile(path, lockPath, bootID string) Option {
	return func(m *Manager) { m.maintenanceStore = &maintenanceStore{path, lockPath, bootID} }
}

func validBootID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
			continue
		}
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func validMaintenanceRecord(r *maintenanceRecord) bool {
	if r.Schema != 1 || !validBootID(r.BootID) || !valid(ClaimRequest{RequestID: r.OperationID, Owner: r.Owner, Purpose: r.Purpose}) || !hexIdentity(r.OperationID, len(r.OperationID)) || !hexIdentity(r.Generation, 64) || !hexIdentity(r.Token, 64) || !hexIdentity(r.StartToken, 64) {
		return false
	}
	switch r.State {
	case "preparing", "ready", "blocked", "finishing", "completed":
		return true
	}
	return false
}
func hexIdentity(s string, n int) bool {
	if len(s) != n {
		return false
	}
	return strings.IndexFunc(s, func(c rune) bool { return !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') }) < 0
}

// All marker transactions take the executor's same lock. Acquisition is
// cancellable so a failed/hung executor cannot indefinitely stall admission.
func (s *maintenanceStore) locked(ctx context.Context, fn func() error) error {
	if !validBootID(s.bootID) || s.path == "" || s.lockPath == "" {
		return ErrInvalid
	}
	if err := os.MkdirAll(filepath.Dir(s.lockPath), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(s.lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	for {
		err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	defer unix.Flock(int(f.Fd()), unix.LOCK_UN)
	return fn()
}
func (s *maintenanceStore) read() (*maintenanceRecord, error) {
	f, err := os.Open(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 8192 {
		return nil, ErrInvalid
	}
	decoder := json.NewDecoder(io.LimitReader(f, 8193))
	decoder.DisallowUnknownFields()
	var r maintenanceRecord
	if decoder.Decode(&r) != nil || decoder.Decode(&struct{}{}) != io.EOF || !validMaintenanceRecord(&r) {
		return nil, ErrInvalid
	}
	return &r, nil
}

// reconcileStartup fences the on-disk reservation before cleanup begins. The
// executor cannot observe ready between reading the marker and fencing it.
func (s *maintenanceStore) reconcileStartup(ctx context.Context) (*maintenanceRecord, error) {
	var r *maintenanceRecord
	err := s.locked(ctx, func() error {
		var err error
		r, err = s.read()
		if err != nil || r == nil || r.State == "completed" {
			return err
		}
		r.State = "blocked"
		r.Reason = "agent restarted during maintenance; explicit finish required"
		return s.write(*r)
	})
	return r, err
}

// save compares the previous generation under the lock. A late old process
// cannot retire or overwrite a different reservation.
func (s *maintenanceStore) save(ctx context.Context, r maintenanceRecord, expected string) error {
	return s.locked(ctx, func() error {
		old, err := s.read()
		if err != nil {
			return err
		}
		if old == nil && expected != "" || old != nil && old.Generation != expected {
			return ErrLease
		}
		return s.write(r)
	})
}

// write is called only while holding the executor's lock.
func (s *maintenanceStore) write(r maintenanceRecord) error {
	if !validMaintenanceRecord(&r) {
		return ErrInvalid
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.path), ".kit-maintenance-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = json.NewEncoder(f).Encode(r); err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	if err = os.Rename(f.Name(), s.path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(s.path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
