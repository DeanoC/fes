package input

import (
	"errors"
	"sort"
	"sync"

	"github.com/DeanoC/FogCast/internal/bridge"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

// inputSource identifies one feed into the shared sink. Local and remote
// sources keep separate state and merge onto a player; neither replaces the
// other.
type inputSource uint8

const (
	sourceRemote inputSource = iota
	sourceLocal
	sourceCount
)

const controllerPortCount = 2

func frameEvent(f protocol.InputFrame) remoteinput.Event {
	return remoteinput.Event{
		Player: f.Player,
		Device: remoteinput.Device(f.Device),
		Kind:   remoteinput.Kind(f.Kind),
		Action: remoteinput.Action(f.Action),
		Code:   remoteinput.Code(f.Code),
		Value:  f.Value,
	}
}

func axisValue(snapshot remoteinput.Snapshot, code remoteinput.Code) int16 {
	if snapshot.Axes == nil {
		return 0
	}
	return snapshot.Axes[code]
}

// mergeAxis keeps the larger deflection. An equal magnitude prefers the local
// source so a centred or opposing remote stick cannot cancel a local hold.
func mergeAxis(local, remote int16) int16 {
	if deflection(local) >= deflection(remote) {
		return local
	}
	return remote
}

func deflection(value int16) int32 {
	if value < 0 {
		return -int32(value)
	}
	return int32(value)
}

func mergeSnapshots(local, remote remoteinput.Snapshot) remoteinput.Snapshot {
	pressed := make(map[remoteinput.Code]struct{}, len(local.Pressed)+len(remote.Pressed))
	for _, code := range local.Pressed {
		pressed[code] = struct{}{}
	}
	for _, code := range remote.Pressed {
		pressed[code] = struct{}{}
	}
	codes := make([]remoteinput.Code, 0, len(pressed))
	for code := range pressed {
		codes = append(codes, code)
	}
	sort.Slice(codes, func(i, j int) bool { return codes[i] < codes[j] })
	axes := map[remoteinput.Code]int16{
		remoteinput.AxisLeftX: mergeAxis(axisValue(local, remoteinput.AxisLeftX), axisValue(remote, remoteinput.AxisLeftX)),
		remoteinput.AxisLeftY: mergeAxis(axisValue(local, remoteinput.AxisLeftY), axisValue(remote, remoteinput.AxisLeftY)),
	}
	return remoteinput.Snapshot{Pressed: codes, Axes: axes}
}

// remotePort maps a remote sender player onto the next port a local pad is
// not using. Local pads keep P1 and P2; Coleco's sink has two ports.
func remotePort(player uint8, claim [controllerPortCount]bool) (uint8, bool) {
	var seen uint8
	for port := uint8(0); port < controllerPortCount; port++ {
		if claim[port] {
			continue
		}
		if seen == player {
			return port, true
		}
		seen++
	}
	return 0, false
}

func remotePlayerForPort(port uint8, claim [controllerPortCount]bool) (uint8, bool) {
	var remotePlayer uint8
	for candidate := uint8(0); candidate < controllerPortCount; candidate++ {
		if claim[candidate] {
			continue
		}
		if candidate == port {
			return remotePlayer, true
		}
		remotePlayer++
	}
	return 0, false
}

func buttonFrame(code remoteinput.Code, action remoteinput.Action) protocol.InputFrame {
	return protocol.InputFrame{
		Device: uint8(remoteinput.DeviceGamepad),
		Kind:   uint8(remoteinput.KindButton),
		Action: uint8(action),
		Code:   uint16(code),
	}
}

func axisFrame(code remoteinput.Code, value int16) protocol.InputFrame {
	return protocol.InputFrame{
		Device: uint8(remoteinput.DeviceGamepad),
		Kind:   uint8(remoteinput.KindAxis),
		Action: uint8(remoteinput.ActionAbsolute),
		Code:   uint16(code),
		Value:  int32(value),
	}
}

func sendSnapshotDiff(inner bridge.Sink, prev, next remoteinput.Snapshot) error {
	if inner == nil {
		return nil
	}
	prevPressed := make(map[remoteinput.Code]bool, len(prev.Pressed))
	for _, code := range prev.Pressed {
		prevPressed[code] = true
	}
	nextPressed := make(map[remoteinput.Code]bool, len(next.Pressed))
	for _, code := range next.Pressed {
		nextPressed[code] = true
	}
	for code := range nextPressed {
		if !prevPressed[code] {
			if err := inner.Apply(buttonFrame(code, remoteinput.ActionPress)); err != nil {
				return err
			}
		}
	}
	for code := range prevPressed {
		if !nextPressed[code] {
			if err := inner.Apply(buttonFrame(code, remoteinput.ActionRelease)); err != nil {
				return err
			}
		}
	}
	for _, code := range []remoteinput.Code{remoteinput.AxisLeftX, remoteinput.AxisLeftY} {
		if axisValue(prev, code) != axisValue(next, code) {
			if err := inner.Apply(axisFrame(code, axisValue(next, code))); err != nil {
				return err
			}
		}
	}
	return nil
}

// padMerge is the legacy single-pad path. Both sources collapse onto player 0
// and the virtual gamepad sees only the merged snapshot.
type padMerge struct {
	mu     sync.Mutex
	inner  bridge.Sink
	states [sourceCount]remoteinput.State
	last   remoteinput.Snapshot
}

func newPadMerge(inner bridge.Sink) *padMerge {
	return &padMerge{inner: inner}
}

func (p *padMerge) Apply(f protocol.InputFrame) error {
	return p.apply(sourceRemote, f)
}

func (p *padMerge) ApplyFrom(source inputSource, f protocol.InputFrame) error {
	return p.apply(source, f)
}

func (p *padMerge) apply(source inputSource, f protocol.InputFrame) error {
	if source >= sourceCount {
		source = sourceRemote
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	event := frameEvent(f)
	event.Player = 0
	if err := p.states[source].Apply(event); err != nil {
		return err
	}
	next := p.mergedLocked()
	if err := sendSnapshotDiff(p.inner, p.last, next); err != nil {
		return err
	}
	p.last = next
	return nil
}

func (p *padMerge) mergedLocked() remoteinput.Snapshot {
	return mergeSnapshots(p.states[sourceLocal].SnapshotForPlayer(0), p.states[sourceRemote].SnapshotForPlayer(0))
}

func (p *padMerge) ReleaseSource(source inputSource) error {
	if source >= sourceCount {
		source = sourceRemote
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.states[source].ReleaseAll()
	next := p.mergedLocked()
	if err := sendSnapshotDiff(p.inner, p.last, next); err != nil {
		return err
	}
	p.last = next
	return nil
}

func (p *padMerge) ReleaseAll() error {
	p.mu.Lock()
	p.states[sourceRemote].ReleaseAll()
	p.states[sourceLocal].ReleaseAll()
	p.last = remoteinput.Snapshot{}
	inner := p.inner
	p.mu.Unlock()
	if inner == nil {
		return nil
	}
	return inner.ReleaseAll()
}

func (p *padMerge) Close() error {
	err := p.ReleaseAll()
	if p.inner == nil {
		return err
	}
	return errors.Join(err, p.inner.Close())
}
