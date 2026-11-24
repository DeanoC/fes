package tenfoot

import (
	"encoding/hex"
	"errors"

	"github.com/DeanoC/FogCast/internal/localhid"
	"github.com/DeanoC/FogCast/protocol"
)

// This is inert App bookkeeping, not a transport or source grant. Callers
// serialize it with App state. It never opens an input device or performs I/O.
const nativeGuestQueueLimit = 256

var errNativeGuestState = errors.New("native guest source unavailable")

type nativeGuestBinding struct {
	Runtime             protocol.RuntimeInputIdentity
	TargetID            string
	BootID              string
	InputAgentInstance  string
	LeaseGeneration     string
	Publisher           [16]byte
	QueueEpoch          uint64
	AuthorityGeneration uint64
}

// Operation cookies are captured with the request under App.mu. The App
// owns this lifecycle for its lifetime and must never reconstruct it while
// callbacks from an earlier instance can still arrive.
type nativeGuestOperation struct {
	generation, serial uint64
	op                 string
}

type nativeGuestPhase uint8

const (
	nativeGuestIdle nativeGuestPhase = iota
	nativeGuestBeginning
	nativeGuestReady
	nativeGuestNeedsReset
	nativeGuestResetting
	nativeGuestUncertain
)

type nativeGuestLifecycle struct {
	phase                  nativeGuestPhase
	binding                nativeGuestBinding
	rawSeq, wireSeq        uint64
	token                  string
	devices                map[localhid.DeviceID]map[uint16]bool
	queue                  []nativeRawEdge
	pending                *localhid.Request
	translate              func(uint16) uint8
	invalidated            bool
	lastGeneration, serial uint64
	operation              nativeGuestOperation
}

// The caller must provide a freshly captured authoritative boundary after
// every confirmed retirement; this inert helper cannot prove that ordering.
func (s *nativeGuestLifecycle) begin(binding nativeGuestBinding, b nativeRawBoundary, translate func(uint16) uint8) (localhid.Request, error) {
	if s.phase != nativeGuestIdle || binding.AuthorityGeneration == 0 || binding.AuthorityGeneration <= s.lastGeneration || s.serial == ^uint64(0) || binding.Runtime.Validate() != nil || !protocol.ValidInputAgentInstance(binding.InputAgentInstance) || binding.TargetID == "" || binding.BootID == "" || b.Publisher != binding.Publisher || translate == nil || len(b.Devices) == 0 || len(b.Devices) > localhid.MaxDevices {
		return localhid.Request{}, errNativeGuestState
	}
	req := localhid.Request{Version: 2, Op: "begin", Request: 1, Publisher: hex.EncodeToString(binding.Publisher[:]), QueueEpoch: binding.QueueEpoch, LeaseGeneration: binding.LeaseGeneration, AgentInstance: binding.InputAgentInstance, RuntimeInstance: binding.Runtime.RuntimeInstance, PackageID: binding.Runtime.PackageID, CoreGeneration: binding.Runtime.Generation, Devices: make([]localhid.DeviceSnapshot, 0, len(b.Devices))}
	devices := make(map[localhid.DeviceID]map[uint16]bool, len(b.Devices))
	for _, device := range b.Devices {
		if device.ID == (localhid.DeviceID{}) || devices[device.ID] != nil || len(device.Held) > 256 {
			return localhid.Request{}, errNativeGuestState
		}
		raw, usages := make(map[uint16]bool), make(map[uint8]bool)
		d := localhid.DeviceSnapshot{ID: device.ID, Held: []uint8{}}
		for _, code := range device.Held {
			if code == 0 || code >= 256 || raw[code] {
				return localhid.Request{}, errNativeGuestState
			}
			raw[code] = true
			if usage := translate(code); usage != 0 {
				if usages[usage] {
					return localhid.Request{}, errNativeGuestState
				}
				usages[usage] = true
				d.Held = append(d.Held, usage)
			}
		}
		devices[device.ID] = raw
		req.Devices = append(req.Devices, d)
	}
	if _, err := localhid.EncodeRequest(req); err != nil {
		return localhid.Request{}, errNativeGuestState
	}
	s.phase, s.binding, s.rawSeq, s.wireSeq = nativeGuestBeginning, binding, b.Sequence, 0
	s.lastGeneration = binding.AuthorityGeneration
	s.newOperation("begin")
	s.devices, s.queue, s.translate, s.invalidated = devices, nil, translate, false
	return req, nil
}

// rawEdge is called even for UI-consumed releases and unmapped keys. It
// buffers while an ACK is pending and cannot turn an unknown state into up.
func (s *nativeGuestLifecycle) rawEdge(binding nativeGuestBinding, edge nativeRawEdge) error {
	if binding != s.binding || (s.phase != nativeGuestBeginning && s.phase != nativeGuestReady) || s.invalidated || s.rawSeq == ^uint64(0) || edge.Sequence != s.rawSeq+1 || edge.Code == 0 || edge.Code >= 256 || len(s.queue) >= nativeGuestQueueLimit {
		s.invalidate()
		return errNativeGuestState
	}
	raw := s.devices[edge.Device]
	if raw == nil || raw[edge.Code] == edge.Down {
		s.invalidate()
		return errNativeGuestState
	}
	if usage := s.translate(edge.Code); edge.Down && usage != 0 {
		for code, held := range raw {
			if held && code != edge.Code && s.translate(code) == usage {
				s.invalidate()
				return errNativeGuestState
			}
		}
	}
	raw[edge.Code] = edge.Down
	s.rawSeq = edge.Sequence
	s.queue = append(s.queue, edge)
	return nil
}

// beginResult is fed only a transport-correlated ACK. Loss or ambiguity must
// use uncertain, which cannot be reset or silently replaced on this source.
func (s *nativeGuestLifecycle) beginResult(operation nativeGuestOperation, binding nativeGuestBinding, reply localhid.Reply, err error) {
	if operation != s.operation || operation.op != "begin" {
		return
	}
	s.operation = nativeGuestOperation{}
	if s.phase != nativeGuestBeginning || err != nil || reply.Op != "begin" || reply.Status != "ready" || reply.Token == "" {
		s.uncertain()
		return
	}
	s.token = reply.Token
	if s.invalidated || binding != s.binding {
		s.phase = nativeGuestNeedsReset
		s.queue = nil
		return
	}
	s.phase = nativeGuestReady
}

// nextEdge assigns wire sequence only to mapped records. One edge remains
// pending until its correlated ACK; no ambiguous write is ever replayed.
func (s *nativeGuestLifecycle) nextEdge(binding nativeGuestBinding) (localhid.Request, bool) {
	if binding != s.binding {
		s.invalidate()
		return localhid.Request{}, false
	}
	if s.phase != nativeGuestReady || s.pending != nil {
		return localhid.Request{}, false
	}
	for len(s.queue) != 0 {
		e := s.queue[0]
		s.queue = s.queue[1:]
		usage := s.translate(e.Code)
		if usage == 0 {
			continue
		}
		if s.wireSeq >= ^uint64(0)-1 {
			s.invalidate()
			return localhid.Request{}, false
		}
		req := localhid.Request{Op: "edge", Token: s.token, EventSeq: s.wireSeq + 1, Device: e.Device, Usage: usage, Down: e.Down}
		if !s.newOperation("edge") {
			s.uncertain()
			return localhid.Request{}, false
		}
		s.pending = &req
		return req, true
	}
	return localhid.Request{}, false
}

func (s *nativeGuestLifecycle) edgeResult(operation nativeGuestOperation, binding nativeGuestBinding, reply localhid.Reply, err error) {
	if operation != s.operation || operation.op != "edge" {
		return
	}
	s.operation = nativeGuestOperation{}
	quarantined := reply.Status == "quarantined" && errors.Is(err, localhid.ErrQuarantined)
	if s.pending == nil || s.phase != nativeGuestReady || (err != nil && !quarantined) || reply.Op != "edge" || reply.Token != s.token || (reply.Status != "accepted" && reply.Status != "quarantined") {
		s.uncertain()
		return
	}
	s.wireSeq = s.pending.EventSeq
	s.pending = nil
	if reply.Status == "quarantined" || s.invalidated || binding != s.binding {
		s.phase = nativeGuestNeedsReset
		s.queue = nil
	}
}

func (s *nativeGuestLifecycle) invalidate() {
	s.invalidated = true
	s.queue = nil
	if s.phase == nativeGuestReady && s.pending == nil {
		s.phase = nativeGuestNeedsReset
	}
}

func (s *nativeGuestLifecycle) uncertain() {
	s.phase = nativeGuestUncertain
	s.queue = nil
	s.pending = nil
	s.invalidated = true
	s.operation = nativeGuestOperation{}
}

func (s *nativeGuestLifecycle) newOperation(op string) bool {
	if s.serial == ^uint64(0) {
		return false
	}
	s.serial++
	s.operation = nativeGuestOperation{generation: s.binding.AuthorityGeneration, serial: s.serial, op: op}
	return true
}

func (s *nativeGuestLifecycle) reset() (localhid.Request, bool) {
	if s.phase != nativeGuestNeedsReset || s.token == "" {
		return localhid.Request{}, false
	}
	if !s.newOperation("reset") {
		s.uncertain()
		return localhid.Request{}, false
	}
	s.phase = nativeGuestResetting
	return localhid.Request{Op: "reset", Token: s.token, LastEventSeq: s.wireSeq}, true
}

func (s *nativeGuestLifecycle) resetResult(operation nativeGuestOperation, reply localhid.Reply, err error) {
	if operation != s.operation || operation.op != "reset" {
		return
	}
	s.operation = nativeGuestOperation{}
	if s.phase != nativeGuestResetting || err != nil || reply.Op != "reset" || reply.Token != s.token || (reply.Status != "neutral-confirmed" && reply.Status != "old-generation-retired") {
		s.uncertain()
		return
	}
	lastGeneration, serial := s.lastGeneration, s.serial
	*s = nativeGuestLifecycle{lastGeneration: lastGeneration, serial: serial}
}
