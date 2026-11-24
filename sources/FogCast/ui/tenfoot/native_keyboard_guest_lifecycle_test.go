package tenfoot

import (
	"errors"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/internal/localhid"
	"github.com/DeanoC/FogCast/protocol"
)

func nativeGuestTestSource(t *testing.T) (*nativeGuestLifecycle, nativeGuestBinding, localhid.DeviceID) {
	t.Helper()
	id := localhid.DeviceID{1}
	binding := nativeGuestBinding{Runtime: protocol.RuntimeInputIdentity{Kind: protocol.RuntimeInputDevelopment, RuntimeInstance: strings.Repeat("a", 32), Generation: 9}, TargetID: "kit", BootID: "boot", InputAgentInstance: strings.Repeat("e", 32), LeaseGeneration: strings.Repeat("b", 64), Publisher: [16]byte{1}, QueueEpoch: 3, AuthorityGeneration: 1}
	s := &nativeGuestLifecycle{}
	_, err := s.begin(binding, nativeRawBoundary{Publisher: binding.Publisher, Sequence: 10, Devices: []nativeRawDevice{{ID: id, Held: []uint16{1, 2}}}}, func(code uint16) uint8 {
		if code == 1 {
			return 0xe1
		}
		if code == 3 {
			return 4
		}
		return 0
	})
	if err != nil {
		t.Fatal(err)
	}
	return s, binding, id
}

func TestNativeGuestLifecycleBuffersBeforeBeginAndNumbersMappedEdges(t *testing.T) {
	s, b, id := nativeGuestTestSource(t)
	for _, edge := range []nativeRawEdge{{Device: id, Sequence: 11, Code: 2, Down: false}, {Device: id, Sequence: 12, Code: 1, Down: false}, {Device: id, Sequence: 13, Code: 3, Down: true}} {
		if err := s.rawEdge(b, edge); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok := s.nextEdge(b); ok {
		t.Fatal("sent edge before BEGIN ACK")
	}
	token := strings.Repeat("c", 64)
	s.beginResult(s.operation, b, localhid.Reply{Op: "begin", Status: "ready", Token: token}, nil)
	req, ok := s.nextEdge(b)
	if !ok || req.EventSeq != 1 || req.Usage != 0xe1 || req.Down {
		t.Fatalf("buffered held release: %+v", req)
	}
	if _, ok := s.nextEdge(b); ok {
		t.Fatal("more than one in-flight edge")
	}
	s.edgeResult(s.operation, b, localhid.Reply{Op: "edge", Status: "accepted", Token: token}, nil)
	req, ok = s.nextEdge(b)
	if !ok || req.EventSeq != 2 || req.Usage != 4 || !req.Down {
		t.Fatalf("mapped sequence: %+v", req)
	}
}

func TestNativeGuestLifecycleLateAckRequiresReset(t *testing.T) {
	s, b, id := nativeGuestTestSource(t)
	if err := s.rawEdge(b, nativeRawEdge{Device: id, Sequence: 11, Code: 1, Down: false}); err != nil {
		t.Fatal(err)
	}
	s.invalidate()
	token := strings.Repeat("c", 64)
	s.beginResult(s.operation, b, localhid.Reply{Op: "begin", Status: "ready", Token: token}, nil)
	if _, ok := s.nextEdge(b); ok {
		t.Fatal("late begin enabled invalidated source")
	}
	reset, ok := s.reset()
	if !ok || reset.Token != token || reset.LastEventSeq != 0 {
		t.Fatal("missing exact token reset")
	}
	s.resetResult(s.operation, localhid.Reply{Op: "reset", Status: "neutral-confirmed", Token: token}, nil)
	if s.phase != nativeGuestIdle {
		t.Fatal("confirmed reset did not retire source")
	}
}

func TestNativeGuestLifecycleAmbiguityAndOverflowFailClosed(t *testing.T) {
	s, b, id := nativeGuestTestSource(t)
	token := strings.Repeat("c", 64)
	s.beginResult(s.operation, b, localhid.Reply{Op: "begin", Status: "ready", Token: token}, nil)
	if err := s.rawEdge(b, nativeRawEdge{Device: id, Sequence: 11, Code: 1, Down: false}); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.nextEdge(b); !ok {
		t.Fatal("missing edge")
	}
	s.edgeResult(s.operation, b, localhid.Reply{}, errors.New("lost ACK"))
	if _, ok := s.reset(); ok || s.phase != nativeGuestUncertain {
		t.Fatal("ambiguous edge was reset or replayed")
	}
	s, b, id = nativeGuestTestSource(t)
	for i := 0; i < nativeGuestQueueLimit; i++ {
		if err := s.rawEdge(b, nativeRawEdge{Device: id, Sequence: uint64(11 + i), Code: 3, Down: i%2 == 0}); err != nil {
			t.Fatal(err)
		}
	}
	if s.rawEdge(b, nativeRawEdge{Device: id, Sequence: 11 + nativeGuestQueueLimit, Code: 3, Down: true}) == nil || !s.invalidated {
		t.Fatal("overflow accepted")
	}
}

func TestNativeGuestLifecycleAuthorityChangeCannotRebind(t *testing.T) {
	s, b, id := nativeGuestTestSource(t)
	changed := b
	changed.Runtime.RuntimeInstance = strings.Repeat("d", 32)
	if s.rawEdge(changed, nativeRawEdge{Device: id, Sequence: 11, Code: 1, Down: false}) == nil {
		t.Fatal("new runtime rebound old input")
	}
	token := strings.Repeat("c", 64)
	s.beginResult(s.operation, changed, localhid.Reply{Op: "begin", Status: "ready", Token: token}, nil)
	if s.phase != nativeGuestNeedsReset {
		t.Fatal("new authority did not quarantine old token")
	}
}

func TestNativeGuestLifecycleRequiresSeparateAgentIncarnation(t *testing.T) {
	s, b, id := nativeGuestTestSource(t)
	changed := b
	changed.InputAgentInstance = strings.Repeat("f", 32)
	if s.rawEdge(changed, nativeRawEdge{Device: id, Sequence: 11, Code: 1, Down: false}) == nil {
		t.Fatal("changed agent rebound old source")
	}
	missing := b
	missing.InputAgentInstance = ""
	s = &nativeGuestLifecycle{}
	if _, err := s.begin(missing, nativeRawBoundary{Publisher: b.Publisher, Devices: []nativeRawDevice{{ID: id}}}, func(uint16) uint8 { return 4 }); err == nil {
		t.Fatal("missing agent admitted")
	}
}

func TestNativeGuestLifecycleQuarantinedEdgeResetsAttemptedSequence(t *testing.T) {
	s, b, id := nativeGuestTestSource(t)
	token := strings.Repeat("c", 64)
	s.beginResult(s.operation, b, localhid.Reply{Op: "begin", Status: "ready", Token: token}, nil)
	if err := s.rawEdge(b, nativeRawEdge{Device: id, Sequence: 11, Code: 1, Down: false}); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.nextEdge(b); !ok {
		t.Fatal("missing edge")
	}
	s.edgeResult(s.operation, b, localhid.Reply{Op: "edge", Status: "quarantined", Token: token}, localhid.ErrQuarantined)
	req, ok := s.reset()
	if !ok || req.LastEventSeq != 1 {
		t.Fatal("quarantined attempted sequence was not reset")
	}
	s.resetResult(s.operation, localhid.Reply{Op: "reset", Status: "unconfirmed", Token: token}, nil)
	if s.phase != nativeGuestUncertain {
		t.Fatal("unconfirmed reset granted new source")
	}
}

func TestNativeGuestLifecycleRejectsReservedRawCode(t *testing.T) {
	s, b, id := nativeGuestTestSource(t)
	if s.rawEdge(b, nativeRawEdge{Device: id, Sequence: 11, Code: 0, Down: true}) == nil || !s.invalidated {
		t.Fatal("reserved raw edge accepted")
	}
	s = &nativeGuestLifecycle{}
	_, err := s.begin(b, nativeRawBoundary{Publisher: b.Publisher, Devices: []nativeRawDevice{{ID: id, Held: []uint16{0}}}}, func(uint16) uint8 { return 0 })
	if err == nil {
		t.Fatal("reserved raw snapshot accepted")
	}
}

func TestNativeGuestLifecycleRejectsStaleCompletionsAcrossEdgesAndCycles(t *testing.T) {
	s, b, id := nativeGuestTestSource(t)
	token := strings.Repeat("c", 64)
	oldBegin := s.operation
	s.beginResult(oldBegin, b, localhid.Reply{Op: "begin", Status: "ready", Token: token}, nil)
	if err := s.rawEdge(b, nativeRawEdge{Device: id, Sequence: 11, Code: 1, Down: false}); err != nil {
		t.Fatal(err)
	}
	if err := s.rawEdge(b, nativeRawEdge{Device: id, Sequence: 12, Code: 3, Down: true}); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.nextEdge(b); !ok {
		t.Fatal("missing first edge")
	}
	oldEdge := s.operation
	s.edgeResult(oldEdge, b, localhid.Reply{Op: "edge", Status: "accepted", Token: token}, nil)
	if _, ok := s.nextEdge(b); !ok {
		t.Fatal("missing second edge")
	}
	s.edgeResult(oldEdge, b, localhid.Reply{Op: "edge", Status: "accepted", Token: token}, nil)
	if s.pending == nil || s.wireSeq != 1 {
		t.Fatal("duplicate completion advanced second edge")
	}
	s.edgeResult(s.operation, b, localhid.Reply{Op: "edge", Status: "accepted", Token: token}, nil)
	s.invalidate()
	if _, ok := s.reset(); !ok {
		t.Fatal("missing reset")
	}
	oldReset := s.operation
	s.resetResult(oldReset, localhid.Reply{Op: "reset", Status: "neutral-confirmed", Token: token}, nil)
	b.AuthorityGeneration++
	if _, err := s.begin(b, nativeRawBoundary{Publisher: b.Publisher, Sequence: 20, Devices: []nativeRawDevice{{ID: id}}}, func(code uint16) uint8 { return 4 }); err != nil {
		t.Fatal(err)
	}
	newBegin := s.operation
	s.beginResult(oldBegin, b, localhid.Reply{Op: "begin", Status: "ready", Token: token}, nil)
	s.resetResult(oldReset, localhid.Reply{Op: "reset", Status: "neutral-confirmed", Token: token}, nil)
	if s.phase != nativeGuestBeginning || s.operation != newBegin {
		t.Fatal("stale completion affected new cycle")
	}
}
