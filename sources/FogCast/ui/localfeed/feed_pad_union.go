package localfeed

import (
	"context"
	"errors"
	"sort"
	"sync/atomic"
	"time"

	"github.com/DeanoC/FogCast/internal/localhid"
	"github.com/DeanoC/FogCast/remoteinput"
)

type padButton struct {
	player uint8
	code   remoteinput.Code
}
type mappedPadKey struct {
	device localhid.DeviceID
	raw    uint16
}
type padContributorUnion struct {
	epoch     uint64
	fence     *PadWriteFence
	physical  map[padButton]bool
	mapped    map[mappedPadKey]padButton
	departure [2]bool
}

// PadWriteFence is a one-way, App-owned authority fence. Its zero value is
// live; each connection, mapping generation and device gets a fresh value.
type PadWriteFence struct{ revoked atomic.Bool }

func (f *PadWriteFence) Revoke() {
	if f != nil {
		f.revoked.Store(true)
	}
}
func (f *PadWriteFence) Revoked() bool { return f == nil || f.revoked.Load() }

func (f *Feed) padConnectionValidLocked() bool {
	return f.padUnion.fence == nil || !f.padUnion.fence.Revoked()
}
func (f *Feed) failRevokedPadLocked() error {
	f.closeLocked()
	return errors.New("pad authority changed; connection must drain")
}

func (u *padContributorUnion) held(button padButton) bool {
	if u.physical[button] {
		return true
	}
	for _, mapped := range u.mapped {
		if mapped == button {
			return true
		}
	}
	return false
}
func (u *padContributorUnion) mappedForPlayer(player uint8) bool {
	for _, mapped := range u.mapped {
		if mapped.player == player {
			return true
		}
	}
	return false
}
func (u *padContributorUnion) physicalForPlayer(player uint8) bool {
	for button, down := range u.physical {
		if down && button.player == player {
			return true
		}
	}
	return false
}
func padEvent(button padButton, down bool) remoteinput.Event {
	action := remoteinput.ActionRelease
	if down {
		action = remoteinput.ActionPress
	}
	return remoteinput.Event{Player: button.player, Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: action, Code: button.code}
}
func sortedPadButtons(buttons map[padButton]bool) []padButton {
	out := make([]padButton, 0, len(buttons))
	for button := range buttons {
		out = append(out, button)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].player != out[j].player {
			return out[i].player < out[j].player
		}
		return out[i].code < out[j].code
	})
	return out
}

// Once a contributor is changed, an error cannot leave the sender's union
// ahead of the target. Close this exact connection and let receiver cleanup
// and a new recovery establish the next source; never retry the event.
func (f *Feed) sendPadMutationLocked(ctx context.Context, event remoteinput.Event, now time.Time) error {
	if !f.padConnectionValidLocked() {
		return f.failRevokedPadLocked()
	}
	if err := f.sendLocked(ctx, event, now, nil); err != nil {
		f.closeLocked()
		return err
	}
	if !f.padConnectionValidLocked() {
		return f.failRevokedPadLocked()
	}
	return nil
}
func (f *Feed) finishPadMutationLocked(ctx context.Context) error {
	if !f.padConnectionValidLocked() {
		return f.failRevokedPadLocked()
	}
	if err := ctx.Err(); err != nil {
		f.closeLocked()
		return err
	}
	return nil
}

// sendPhysicalPadLocked merges physical gamepad buttons with keyboard-mapped
// contributors only on a recovered v2 connection. Legacy streams are not
// changed. Departures wait while a mapped contributor still holds that port.
func (f *Feed) sendPhysicalPadLocked(ctx context.Context, e remoteinput.Event, now time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !f.padConnectionValidLocked() {
		return f.failRevokedPadLocked()
	}
	if e.Player > 1 || (e.Action != remoteinput.ActionPress && e.Action != remoteinput.ActionRelease) {
		return errors.New("invalid physical pad event")
	}
	u := &f.padUnion
	if remoteinput.IsLocalPlayerDeparture(e) {
		buttons := map[padButton]bool{}
		for button := range u.physical {
			if button.player == e.Player {
				buttons[button] = true
				delete(u.physical, button)
			}
		}
		if !u.mappedForPlayer(e.Player) {
			u.departure[e.Player] = false
			return f.sendPadMutationLocked(ctx, e, now)
		}
		u.departure[e.Player] = true
		for _, button := range sortedPadButtons(buttons) {
			if !u.held(button) {
				if err := f.sendPadMutationLocked(ctx, padEvent(button, false), now); err != nil {
					return err
				}
			}
		}
		return f.finishPadMutationLocked(ctx)
	}
	if e.Code == 0 {
		return errors.New("invalid physical pad button")
	}
	if u.physical == nil {
		u.physical = make(map[padButton]bool)
	}
	u.departure[e.Player] = false // A later physical event supersedes an old departure.
	button := padButton{player: e.Player, code: e.Code}
	before := u.held(button)
	if e.Action == remoteinput.ActionPress {
		u.physical[button] = true
	} else {
		delete(u.physical, button)
	}
	if before == u.held(button) {
		return f.finishPadMutationLocked(ctx)
	}
	return f.sendPadMutationLocked(ctx, e, now)
}

// SendNativeMappedPad records one physical keyboard incarnation/code as a
// contributor to a gamepad button. The exact recovered handle is mandatory;
// no raw keyboard frame is sent and no ambiguous write is replayed.
func (f *Feed) SendNativeMappedPad(ctx context.Context, handle V2Handle, padEpoch uint64, mappingFence, deviceFence *PadWriteFence, device localhid.DeviceID, rawCode uint16, event remoteinput.Event) error {
	if f == nil || ctx == nil || device == (localhid.DeviceID{}) || rawCode == 0 || rawCode >= 256 || event.Player > 1 || event.Device != remoteinput.DeviceGamepad || event.Kind != remoteinput.KindButton || (event.Action != remoteinput.ActionPress && event.Action != remoteinput.ActionRelease) || event.Code < remoteinput.ButtonDPadUp || event.Code > remoteinput.ButtonR || event.Value != 0 {
		return ErrUnavailable
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if f.v2 == nil || f.v2 != handle.transport || f.recovering || padEpoch == 0 || f.padUnion.epoch != padEpoch || !f.padConnectionValidLocked() || mappingFence.Revoked() || deviceFence.Revoked() {
		return ErrUnavailable
	}
	u := &f.padUnion
	if u.mapped == nil {
		u.mapped = make(map[mappedPadKey]padButton)
	}
	key := mappedPadKey{device: device, raw: rawCode}
	button := padButton{player: event.Player, code: event.Code}
	old, exists := u.mapped[key]
	if exists && old != button {
		return errors.New("mapped pad key changed while held")
	}
	before := u.held(button)
	if event.Action == remoteinput.ActionPress {
		if !exists && len(u.mapped) >= 256 {
			return ErrUnavailable
		}
		u.mapped[key] = button
	} else {
		delete(u.mapped, key)
	}
	if before != u.held(button) {
		if err := f.sendPadMutationLocked(ctx, padEvent(button, event.Action == remoteinput.ActionPress), time.Time{}); err != nil {
			return err
		}
	}
	if mappingFence.Revoked() || deviceFence.Revoked() || !f.padConnectionValidLocked() {
		return f.failRevokedPadLocked()
	}
	if err := f.finishPadDepartureLocked(ctx, button.player); err != nil {
		return err
	}
	return f.finishPadMutationLocked(ctx)
}

// RetireNativeMappedPad releases only one keyboard incarnation, or all mapped
// incarnations for the zero device, without clearing physical pad contributors.
func (f *Feed) RetireNativeMappedPad(ctx context.Context, handle V2Handle, padEpoch uint64, device localhid.DeviceID) error {
	if f == nil || ctx == nil {
		return ErrUnavailable
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if f.v2 == nil || f.v2 != handle.transport || f.recovering || padEpoch == 0 || f.padUnion.epoch != padEpoch || !f.padConnectionValidLocked() {
		return ErrUnavailable
	}
	u := &f.padUnion
	buttons := map[padButton]bool{}
	for key, button := range u.mapped {
		if device == (localhid.DeviceID{}) || key.device == device {
			buttons[button] = true
			delete(u.mapped, key)
		}
	}
	for _, button := range sortedPadButtons(buttons) {
		if !u.held(button) {
			if err := f.sendPadMutationLocked(ctx, padEvent(button, false), time.Time{}); err != nil {
				return err
			}
		}
	}
	for player := uint8(0); player < 2; player++ {
		if err := f.finishPadDepartureLocked(ctx, player); err != nil {
			return err
		}
	}
	return f.finishPadMutationLocked(ctx)
}

func (f *Feed) finishPadDepartureLocked(ctx context.Context, player uint8) error {
	u := &f.padUnion
	if !u.departure[player] || u.mappedForPlayer(player) || u.physicalForPlayer(player) {
		return nil
	}
	u.departure[player] = false
	return f.sendPadMutationLocked(ctx, remoteinput.LocalPlayerDeparture(player), time.Time{})
}
