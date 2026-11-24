package input

import (
	"context"
	"errors"
	"time"

	"github.com/DeanoC/FogCast/internal/hidkeys"
	"github.com/DeanoC/FogCast/internal/kitlease"
	"github.com/DeanoC/FogCast/internal/localhid"
	"github.com/DeanoC/FogCast/internal/playhid"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

// delegatedV2Backend is owned by one local reader. It holds no manager lease
// reference across calls; each operation pins and drains its own admission.
type delegatedV2Backend struct{ controller *TargetController }
type delegatedV2Binding struct {
	identity localDelegation
	matrix   bool
	leaseCtx context.Context
}

const delegatedV2Wait = time.Second
const delegatedV2Operation = 2 * time.Second

// withCore keeps the lease admission until observation and operation finish.
// Transitions publish intent before acquiring lifecycle, so a waiting source
// cannot overtake Stop, focus or a reconstructed same-generation binding.
func (b delegatedV2Backend) withCore(ctx context.Context, identity localDelegation, requireGrant bool, fn func(context.Context, CoreObservation, func() error) error) error {
	c := b.controller
	if c == nil || ctx == nil || c.localLeases == nil || c.ports == nil {
		return errNoCore
	}
	leaseCtx, done, err := c.localLeases.BeginLocalInput(identity.LeaseGeneration)
	if err != nil {
		return err
	}
	defer done()
	op, cancel := context.WithTimeout(ctx, delegatedV2Operation)
	defer cancel()
	stop := context.AfterFunc(leaseCtx, cancel)
	defer stop()
	epoch := c.transitionEpoch.Load()
	if c.transitionPending.Load() != 0 {
		return errNoCore
	}
	wait, cancelWait := context.WithTimeout(op, delegatedV2Wait)
	defer cancelWait()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		if wait.Err() != nil || leaseCtx.Err() != nil || c.transitionPending.Load() != 0 || c.transitionEpoch.Load() != epoch {
			return errNoCore
		}
		if c.lifecycle.TryLock() {
			if wait.Err() != nil || leaseCtx.Err() != nil {
				c.lifecycle.Unlock()
				return errNoCore
			}
			break
		}
		select {
		case <-wait.Done():
			return errNoCore
		case <-tick.C:
		}
	}
	defer c.lifecycle.Unlock()
	if op.Err() != nil || leaseCtx.Err() != nil || c.transitionPending.Load() != 0 || c.transitionEpoch.Load() != epoch {
		return errNoCore
	}
	c.mu.Lock()
	closed, observe := c.closed, c.observe
	c.mu.Unlock()
	if closed || observe == nil || (requireGrant && !c.localKeyboardAdmitted(identity)) {
		return errNoCore
	}
	probe, cancelProbe := context.WithTimeout(op, localCoreObserveTimeout)
	obs, err := observe(probe)
	if err == nil && probe.Err() != nil {
		err = probe.Err()
	}
	cancelProbe()
	if err != nil {
		return err
	}
	if op.Err() != nil || leaseCtx.Err() != nil || c.transitionPending.Load() != 0 || c.transitionEpoch.Load() != epoch {
		return errNoCore
	}
	guard := func() error {
		if op.Err() != nil || leaseCtx.Err() != nil || c.transitionPending.Load() != 0 || c.transitionEpoch.Load() != epoch {
			return errNoCore
		}
		c.mu.Lock()
		closed := c.closed
		c.mu.Unlock()
		if closed || (requireGrant && !c.localKeyboardAdmitted(identity)) {
			return errNoCore
		}
		return nil
	}
	return fn(op, obs, guard)
}

func (b delegatedV2Backend) Open(ctx context.Context, req localhid.Request) (any, error) {
	if b.controller == nil || b.controller.localLeases == nil {
		return nil, errNoCore
	}
	instance, err := b.controller.LocalAgentInstance()
	if err != nil || req.AgentInstance != instance {
		return nil, errNoCore
	}
	id := localDelegation{LeaseGeneration: req.LeaseGeneration, RuntimeInstance: req.RuntimeInstance, PackageID: req.PackageID, CoreGeneration: req.CoreGeneration, Epoch: req.QueueEpoch}
	leaseCtx, done, err := b.controller.localLeases.BeginLocalInput(id.LeaseGeneration)
	if err != nil {
		return nil, err
	}
	defer done()
	bound := delegatedV2Binding{identity: id, leaseCtx: leaseCtx}
	err = b.withCore(ctx, id, true, func(ctx context.Context, obs CoreObservation, guard func() error) error {
		if !obs.Active || obs.RuntimeInstance != id.RuntimeInstance || obs.PackageID != id.PackageID || obs.Generation != id.CoreGeneration {
			return errNoCore
		}
		if err := guard(); err != nil {
			return err
		}
		if !b.controller.reserveLocalQueue(id.LeaseGeneration, req.Publisher, id.Epoch) {
			return errNoCore
		}
		if obs.KeyboardHID == nil {
			if !obs.Keyboard || !b.controller.localLeases.LocalHostGeneration(id.LeaseGeneration) || b.controller.ports.keys == nil {
				return errNoCore
			}
			if err := b.controller.ports.setObservationContext(ctx, obs); err != nil {
				return err
			}
			if err := b.checkMatrix(ctx, id, guard, true); err != nil {
				return err
			}
			bound.matrix = true
			return nil
		}
		if b.controller.ports.hid == nil || obs.KeyboardHID.RuntimeInstance != id.RuntimeInstance || obs.KeyboardHID.PackageID != id.PackageID || obs.KeyboardHID.Generation != id.CoreGeneration {
			return errNoCore
		}
		// An old/ambiguous local contribution cannot be adopted by a new token.
		hid := b.controller.ports.hid
		if err := hid.lockTap(ctx); err != nil {
			return err
		}
		defer hid.mu.Unlock()
		if err := guard(); err != nil {
			return err
		}
		if hid.binding == nil || *hid.binding != *obs.KeyboardHID || hid.poster == nil || hid.focused || hid.focusFence.Load() || hid.unconfirmed || hid.localV2Uncertain != nil ||
			len(hid.pressed[sourceLocal]) != 0 || len(hid.blocked[sourceLocal]) != 0 || hid.shiftAmbiguous[sourceLocal] {
			return errNoCore
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return bound, nil
}

// checkMatrix validates target-owned local-host authority and adapter health
// while the controller lifecycle and matrix sink remain held.
func (b delegatedV2Backend) checkMatrix(ctx context.Context, id localDelegation, guard func() error, fresh bool) error {
	c := b.controller
	if c.ports.keys == nil || !c.localLeases.LocalHostGeneration(id.LeaseGeneration) {
		return errNoCore
	}
	sink := c.ports.keys
	if err := lockInputMutex(ctx, &sink.mu); err != nil {
		return err
	}
	defer sink.mu.Unlock()
	if err := guard(); err != nil {
		return err
	}
	expected := MatrixBinding{RuntimeInstance: id.RuntimeInstance, PackageID: id.PackageID, Generation: id.CoreGeneration}
	if sink.binding == nil || *sink.binding != expected || sink.poster == nil || sink.sinkID == ([16]byte{}) || sink.nextSequence >= ^uint64(0)-1 || sink.focused || sink.uncertain != nil || (fresh && len(sink.pressed[sourceLocal]) != 0) {
		return errNoCore
	}
	c.mu.Lock()
	admit := c.matrixAdmission
	c.mu.Unlock()
	if admit == nil {
		return errNoCore
	}
	if err := admit(ctx, id.RuntimeInstance, id.PackageID, id.CoreGeneration, func(context.Context) error { return guard() }); err != nil {
		return err
	}
	if fresh {
		// SourceSession installed the authoritative snapshot first; it keeps
		// snapshot-held usages suppressed until their physical release.
		sink.blocked[sourceLocal] = nil
	}
	return nil
}

func (b delegatedV2Backend) Edge(ctx context.Context, witness any, delta *localhid.Delta) error {
	bound, ok := witness.(delegatedV2Binding)
	if !ok {
		return errNoCore
	}
	return b.withCore(ctx, bound.identity, true, func(ctx context.Context, obs CoreObservation, guard func() error) error {
		if bound.matrix {
			id := bound.identity
			if !obs.Active || !obs.Keyboard || obs.KeyboardHID != nil || obs.RuntimeInstance != id.RuntimeInstance || obs.PackageID != id.PackageID || obs.Generation != id.CoreGeneration {
				return errNoCore
			}
			if delta == nil {
				return b.checkMatrix(ctx, id, guard, false)
			}
			code, ok := hidkeys.Code(delta.Usage)
			if !ok {
				return errors.New("invalid local HID usage")
			}
			action := remoteinput.ActionRelease
			if delta.Down {
				action = remoteinput.ActionPress
			}
			mapped, ok := playhid.StreamEvent(remoteinput.Event{Device: remoteinput.DeviceKeyboard, Kind: remoteinput.KindKey, Action: action, Code: code}, playhid.MatrixKeys)
			if !ok {
				return b.checkMatrix(ctx, id, guard, false)
			}
			frame := protocol.InputFrame{Device: uint8(mapped.Device), Kind: uint8(mapped.Kind), Action: uint8(mapped.Action), Code: uint16(mapped.Code)}
			expected := MatrixBinding{RuntimeInstance: id.RuntimeInstance, PackageID: id.PackageID, Generation: id.CoreGeneration}
			write, cancel := context.WithTimeout(ctx, localCoreWriteTimeout)
			defer cancel()
			return b.controller.ports.keys.applyFromBound(write, sourceLocal, frame, &expected, func(context.Context) error { return guard() })
		}
		if !obs.Active || obs.RuntimeInstance != bound.identity.RuntimeInstance || obs.KeyboardHID == nil || obs.KeyboardHID.RuntimeInstance != bound.identity.RuntimeInstance || obs.KeyboardHID.PackageID != bound.identity.PackageID || obs.KeyboardHID.Generation != bound.identity.CoreGeneration {
			return errNoCore
		}
		hid := b.controller.ports.hid
		checked := func() error {
			if err := guard(); err != nil {
				return err
			}
			if hid.binding == nil || *hid.binding != *obs.KeyboardHID || hid.poster == nil || hid.focused || hid.focusFence.Load() || hid.unconfirmed {
				return errNoCore
			}
			return nil
		}
		if delta == nil {
			if err := hid.lockTap(ctx); err != nil {
				return err
			}
			defer hid.mu.Unlock()
			return checked()
		}
		code, ok := hidkeys.Code(delta.Usage)
		if !ok {
			return errors.New("invalid local HID usage")
		}
		action := remoteinput.ActionRelease
		if delta.Down {
			action = remoteinput.ActionPress
		}
		frame := protocol.InputFrame{Header: protocol.InputHeader{Type: protocol.InputTypeInput, Session: 1}, Device: uint8(remoteinput.DeviceKeyboard), Kind: uint8(remoteinput.KindKey), Action: uint8(action), Code: uint16(code)}
		write, cancel := context.WithTimeout(ctx, localCoreWriteTimeout)
		defer cancel()
		return hid.applyFromGuarded(write, sourceLocal, frame, nil, false, checked, true)
	})
}

func (b delegatedV2Backend) Barrier(ctx context.Context, witness any) error {
	return b.Edge(ctx, witness, nil)
}

func (b delegatedV2Backend) Neutral(ctx context.Context, witness any) (string, error) {
	bound, ok := witness.(delegatedV2Binding)
	if !ok {
		return "unconfirmed", errNoCore
	}
	// A completed, explicitly confirmed cleanup belongs to this exact lease;
	// unlike revocation alone it proves the old source was neutralized.
	if ctx != nil && ctx.Err() == nil && kitlease.InputCleanupConfirmed(bound.leaseCtx) {
		return "neutral-confirmed", nil
	}
	status := "unconfirmed"
	err := b.withCore(ctx, bound.identity, false, func(ctx context.Context, obs CoreObservation, guard func() error) error {
		if err := guard(); err != nil {
			return err
		}
		if bound.matrix {
			id := bound.identity
			sink := b.controller.ports.keys
			if sink == nil {
				return errNoCore
			}
			expected := MatrixBinding{RuntimeInstance: id.RuntimeInstance, PackageID: id.PackageID, Generation: id.CoreGeneration}
			validate := func(context.Context) error { return guard() }
			if matrixObservationRetires(obs, id.RuntimeInstance, id.PackageID, id.CoreGeneration) {
				b.controller.mu.Lock()
				observe := b.controller.observe
				b.controller.mu.Unlock()
				if err := sink.retireBindingObserved(ctx, expected, observe, validate); err != nil {
					return err
				}
				status = "old-generation-retired"
				return nil
			}
			if !obs.Active || !obs.Keyboard || obs.KeyboardHID != nil || obs.RuntimeInstance != id.RuntimeInstance || obs.PackageID != id.PackageID || obs.Generation != id.CoreGeneration {
				return errNoCore
			}
			if err := sink.releaseSourcesBound(ctx, &expected, validate, sourceLocal); err != nil {
				return err
			}
			status = "neutral-confirmed"
			return nil
		}
		idle := obs.IdleConfirmed && !obs.Active && obs.PackageID == "" && obs.Generation == 0
		active := obs.Active && !obs.IdleConfirmed && localIdentity.MatchString(obs.PackageID) && obs.Generation != 0
		if validObservedMatrixInstance(obs.RuntimeInstance) && (idle || active && (obs.RuntimeInstance != bound.identity.RuntimeInstance || obs.PackageID != bound.identity.PackageID || obs.Generation != bound.identity.CoreGeneration)) {
			hid := b.controller.ports.hid
			if err := hid.lockTap(ctx); err != nil {
				return err
			}
			defer hid.mu.Unlock()
			if err := guard(); err != nil {
				return err
			}
			expected := KeyboardHIDBinding{RuntimeInstance: bound.identity.RuntimeInstance, PackageID: bound.identity.PackageID, Generation: bound.identity.CoreGeneration}
			if hid.localV2Uncertain != nil {
				if *hid.localV2Uncertain != expected {
					return errNoCore
				}
				hid.localV2Uncertain = nil
			}
			status = "old-generation-retired"
			return nil
		}
		if !obs.Active || obs.RuntimeInstance != bound.identity.RuntimeInstance || obs.KeyboardHID == nil || obs.KeyboardHID.RuntimeInstance != bound.identity.RuntimeInstance || obs.KeyboardHID.PackageID != bound.identity.PackageID || obs.KeyboardHID.Generation != bound.identity.CoreGeneration {
			return errNoCore
		}
		hid := b.controller.ports.hid
		if hid.focusFence.Load() {
			return errNoCore
		}
		expected := &KeyboardHIDBinding{RuntimeInstance: bound.identity.RuntimeInstance, PackageID: bound.identity.PackageID, Generation: bound.identity.CoreGeneration}
		if err := hid.lockTap(ctx); err != nil {
			return err
		}
		same := hid.binding != nil && *hid.binding == *expected && hid.poster != nil && !hid.focused && !hid.focusFence.Load() && guard() == nil
		hid.mu.Unlock()
		if !same {
			return errNoCore
		}
		if err := hid.neutralDelegatedSource(ctx, expected, func() error {
			if err := guard(); err != nil {
				return err
			}
			if hid.binding == nil || *hid.binding != *expected || hid.poster == nil || hid.focused || hid.focusFence.Load() {
				return errNoCore
			}
			return nil
		}); err != nil {
			return err
		}
		if err := hid.lockTap(ctx); err != nil {
			return err
		}
		confirmed := hid.binding != nil && *hid.binding == *expected && hid.poster != nil && !hid.unconfirmed && hid.localV2Uncertain == nil && len(hid.pressed[sourceLocal]) == 0 && !hid.focused && !hid.focusFence.Load()
		hid.mu.Unlock()
		if !confirmed {
			return errNoCore
		}
		status = "neutral-confirmed"
		return nil
	})
	return status, err
}

// delegatedV2RawAllowed is an allowlist for unrelated raw traffic while the
// delegated source is active or quarantined. In particular KindKey is never a
// pad event, even if a sender disguises it with DeviceGamepad. The ordinary
// separately authorized raw-local-owner route is not governed by this gate.
func delegatedV2RawAllowed(f protocol.InputFrame) bool {
	if f.Header.Type != protocol.InputTypeInput {
		return false
	}
	if f.Device == uint8(remoteinput.DeviceKeyboard) || f.Kind == uint8(remoteinput.KindKey) {
		return false
	}
	switch remoteinput.Device(f.Device) {
	case remoteinput.DeviceGamepad:
		if f.Player > 1 {
			return false
		}
		switch remoteinput.Kind(f.Kind) {
		case remoteinput.KindButton:
			return f.Action == uint8(remoteinput.ActionPress) || f.Action == uint8(remoteinput.ActionRelease)
		case remoteinput.KindAxis:
			return f.Action == uint8(remoteinput.ActionAbsolute)
		}
	case remoteinput.DeviceMouse:
		return f.Player == 0 && f.Kind == uint8(remoteinput.KindRelative) && f.Action == uint8(remoteinput.ActionRelative) && f.Code <= 3
	}
	return false
}
