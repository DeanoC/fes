// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"sync"

	"github.com/DeanoC/FogCast/ui/tenfoot"
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
)

// ProgramPreferences serves a private RAM snapshot. The zero value is volatile.
// An explicitly selected storage checkpoint reports Pending until an owned SD
// handoff has completed its filesystem write/flush. UI methods never access SD.
type ProgramPreferences struct {
	mu                        sync.Mutex
	data                      []byte
	store                     *packagestore.Store
	pending, attached, sealed bool
	failure                   error
}

// LoadProgramPreferences runs before menu construction, while SD is active.
// persistent is a trusted platform choice requiring writable media; it grants
// no write permission itself. A read-only/missing file uses volatile defaults.
func LoadProgramPreferences(ctx context.Context, store *packagestore.Store, persistent bool) (*ProgramPreferences, error) {
	if store == nil || ctx == nil {
		return nil, ErrProgramSession
	}
	data, err := store.ReadPreferences(ctx)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) || persistent {
			return nil, err
		}
		// Missing optional settings cannot hide a simultaneously changed lease.
		if _, leaseErr := store.ActiveReceipt(); leaseErr != nil {
			return nil, errors.Join(err, leaseErr)
		}
	}
	p := &ProgramPreferences{data: bytes.Clone(data)}
	if persistent {
		p.store = store
	}
	return p, nil
}

func (p *ProgramPreferences) ReadPreferences() ([]byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.data == nil {
		return []byte(`{"home":"library","attract_enabled":false}`), nil
	}
	return bytes.Clone(p.data), nil
}

func (p *ProgramPreferences) WritePreferences(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(data) > packagestore.MaxPreferencesBytes || len(trimmed) == 0 || trimmed[0] != '{' || !json.Valid(data) {
		return errors.New("native program UI: invalid bounded preferences")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.sealed {
		return errors.New("native program UI: preferences owner has closed")
	}
	if bytes.Equal(p.data, data) && !p.pending && p.failure == nil {
		return nil
	}
	p.data = bytes.Clone(data)
	if p.store == nil {
		return nil
	}
	p.pending = true
	if p.failure != nil {
		return p.failure
	}
	return tenfoot.ErrPreferencesPending
}

func (p *ProgramPreferences) PreferenceSaveState() tenfoot.PreferenceSaveState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return tenfoot.PreferenceSaveState{Pending: p.pending, Failed: p.failure != nil}
}

func (p *ProgramPreferences) seal(cause error) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sealed = true
	if p.pending && cause != nil {
		p.failure = cause
	}
}

// AttachPreferences binds checkpoints before frontend ownership is transferred.
// Persistent checkpoints require the paired menu lifecycle and this exact store.
func (s *ProgramService) AttachPreferences(prefs *ProgramPreferences) error {
	if prefs == nil {
		return ErrProgramSession
	}
	p, view, err := s.begin(context.Background())
	if err != nil {
		return err
	}
	defer p.operation.Unlock()
	prefs.mu.Lock()
	defer prefs.mu.Unlock()
	if view.State != ProgramReady || view.Generation != 0 || p.backendClaimed || p.preferences != nil || prefs.attached || prefs.sealed ||
		prefs.store != nil && (prefs.store != p.store || p.menu == nil) {
		return ErrProgramSession
	}
	prefs.attached = true
	p.preferences = prefs
	return nil
}

// Caller holds programService.operation in a verified active-storage window.
// Preference memory is unlocked during IO so newer UI changes are not lost.
func (p *programService) checkpointPreferences(ctx context.Context) error {
	prefs := p.preferences
	if prefs == nil {
		return nil
	}
	prefs.mu.Lock()
	if prefs.store == nil || !prefs.pending || prefs.failure != nil {
		prefs.mu.Unlock()
		return nil
	}
	data := bytes.Clone(prefs.data)
	prefs.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	err := p.store.WritePreferences(ctx, data)
	prefs.mu.Lock()
	if err == nil && bytes.Equal(prefs.data, data) {
		prefs.pending = false
	}
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		prefs.failure = err
	}
	prefs.mu.Unlock()
	if err != nil {
		// A settings-capacity/protection refusal does not prevent playing a
		// core. Unknown IO failures retire the store and therefore the runtime.
		if _, storageErr := p.store.ActiveReceipt(); storageErr != nil {
			return errors.Join(err, storageErr)
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
	}
	return nil
}
