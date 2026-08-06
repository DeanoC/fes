package protocol

import (
	"bytes"
	"testing"
)

func TestRemoteInputRoundTripAndBounds(t *testing.T) {
	frame := InputFrame{Header: InputHeader{Type: InputTypeInput, Session: 42}, Seq: 7, Player: 1, Device: 1, Kind: 1, Action: 1, Code: 104, Value: -3}
	wire, err := EncodeInputFrame(frame)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeInputFrame(bytes.NewReader(wire), 1024)
	if err != nil {
		t.Fatal(err)
	}
	if got != frame {
		t.Fatalf("got %#v want %#v", got, frame)
	}
	if _, err := DecodeInputFrame(bytes.NewReader(wire), uint32(len(wire)-5)); err == nil {
		t.Fatal("oversize frame accepted")
	}
}

func TestRemoteInputControlFrameRoundTrip(t *testing.T) {
	frame := InputFrame{Header: InputHeader{Type: InputTypePing, Session: 42}, Seq: 8}
	wire, err := EncodeInputFrame(frame)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeInputFrame(bytes.NewReader(wire), 1024)
	if err != nil || got != frame {
		t.Fatalf("got %#v err=%v", got, err)
	}
}

func TestRemoteInputRejectsHeaderAndValues(t *testing.T) {
	frame := InputFrame{Header: InputHeader{Type: InputTypeInput, Session: 1}, Seq: 1, Device: 9}
	if _, err := EncodeInputFrame(frame); err == nil {
		t.Fatal("invalid device accepted")
	}
	wire, err := EncodeInputFrame(InputFrame{Header: InputHeader{Type: InputTypeInput, Session: 1}, Seq: 1, Device: 0, Kind: 0, Action: 1, Code: 1})
	if err != nil {
		t.Fatal(err)
	}
	wire[0] = 'X'
	if _, err := DecodeInputFrame(bytes.NewReader(wire), 1024); err == nil {
		t.Fatal("bad magic accepted")
	}
}

func TestSequenceTrackerRejectsStaleAndAcceptsDuplicateIdempotently(t *testing.T) {
	var tracker SequenceTracker
	if !tracker.Accept(10) {
		t.Fatal("first sequence rejected")
	}
	if !tracker.Accept(10) {
		t.Fatal("duplicate should be idempotent")
	}
	if tracker.Accept(9) {
		t.Fatal("stale sequence accepted")
	}
	if !tracker.Accept(11) {
		t.Fatal("next sequence rejected")
	}
}

func TestSessionGuardRejectsWrongTokenSessionAndVersion(t *testing.T) {
	guard := SessionGuard{Version: InputVersion, Session: 9, Token: []byte("secret")}
	for name, got := range map[string]SessionHello{
		"version": {Version: 2, Session: 9, Token: []byte("secret")},
		"session": {Version: InputVersion, Session: 8, Token: []byte("secret")},
		"token":   {Version: InputVersion, Session: 9, Token: []byte("wrong")},
	} {
		t.Run(name, func(t *testing.T) {
			if err := guard.Validate(got); err == nil {
				t.Fatal("invalid hello accepted")
			}
		})
	}
}

func TestStateRecoverySnapshot(t *testing.T) {
	r := NewInputState()
	if err := r.Apply(InputFrame{Header: InputHeader{Type: InputTypeInput, Session: 1}, Seq: 1, Device: 0, Kind: 0, Action: 1, Code: 1}); err != nil {
		t.Fatal(err)
	}
	if !r.Pressed(1) {
		t.Fatal("press missing")
	}
	r.ApplySnapshot(StateSnapshot{Pressed: []uint16{2}})
	if r.Pressed(1) || !r.Pressed(2) {
		t.Fatal("snapshot not authoritative")
	}
}
