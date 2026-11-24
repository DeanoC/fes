package bridge

import (
	"context"
	"io"
	"sync"
	"testing"

	"github.com/DeanoC/FogCast/protocol"
)

type timedTestSink struct {
	testSink
	muTap sync.Mutex
	taps  []protocol.InputFrame
}

func (s *timedTestSink) Tap(_ context.Context, f protocol.InputFrame) error {
	s.muTap.Lock()
	defer s.muTap.Unlock()
	s.taps = append(s.taps, f)
	return nil
}
func (s *timedTestSink) countTap() int { s.muTap.Lock(); defer s.muTap.Unlock(); return len(s.taps) }

func tapProbe(t *testing.T, c io.Writer, r io.Reader) protocol.InputFrame {
	t.Helper()
	f := protocol.InputFrame{Header: protocol.InputHeader{Type: protocol.InputTypePing, Flags: protocol.InputFlagTapProbe, Session: 9}, ClientMonoNS: 100}
	if err := WriteFrame(c, f); err != nil {
		t.Fatal(err)
	}
	got, err := protocol.DecodeInputFrame(r, MaxFrameBytes)
	if err != nil {
		t.Fatal(err)
	}
	if got.Header.Type != protocol.InputTypeReceipt || got.ClientMonoNS != 100 {
		t.Fatalf("bad probe: %+v", got)
	}
	return got
}
func validTap() protocol.InputFrame {
	return protocol.InputFrame{Header: protocol.InputHeader{Type: protocol.InputTypeTap, Session: 9}, Seq: 1, ClientMonoNS: 101, ServerMonoNS: 7, Action: 1, Code: 0x1004, Value: protocol.InputTapHoldMS}
}

func TestTapProbeAndReceiptUseActualBridge(t *testing.T) {
	sink := &timedTestSink{}
	c, r := receiptFixture(t, sink)
	if got := tapProbe(t, c, r); got.Header.Flags != protocol.InputFlagTapSupported {
		t.Fatalf("unsupported: %+v", got)
	}
	f := validTap()
	if err := WriteFrame(c, f); err != nil {
		t.Fatal(err)
	}
	got, err := protocol.DecodeInputFrame(r, MaxFrameBytes)
	if err != nil {
		t.Fatal(err)
	}
	if got.Header.Type != protocol.InputTypeReceipt || got.Header.Flags != 0 || got.Seq != f.Seq || got.ClientMonoNS != f.ClientMonoNS || sink.countTap() != 1 {
		t.Fatalf("tap receipt %+v taps=%d", got, sink.countTap())
	}
	// Tap requests are transient: replay of the same sequence must not
	// reapply a key, even though ordinary held-state frames are idempotent.
	if err := WriteFrame(c, f); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ReadByte(); err == nil {
		t.Fatal("duplicate tap did not close stream")
	}
	if sink.countTap() != 1 {
		t.Fatal("duplicate tap applied")
	}
}

func TestTapProbeWithoutSinkCapabilityDoesNotDispatch(t *testing.T) {
	sink := &testSink{}
	c, r := receiptFixture(t, sink)
	if got := tapProbe(t, c, r); got.Header.Flags != 0 {
		t.Fatalf("capability advertised: %+v", got)
	}
	if err := WriteFrame(c, validTap()); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ReadByte(); err == nil {
		t.Fatal("unnegotiated tap did not close stream")
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.events) != 0 {
		t.Fatalf("unnegotiated tap dispatched: %+v", sink.events)
	}
}

func TestChordProbeDoesNotSendInputToTapOnlySink(t *testing.T) {
	sink := &timedTestSink{}
	c, r := receiptFixture(t, sink)
	f := protocol.InputFrame{Header: protocol.InputHeader{Type: protocol.InputTypePing, Flags: protocol.InputFlagTapChordProbe, Session: 9}, ClientMonoNS: 100}
	if err := WriteFrame(c, f); err != nil {
		t.Fatal(err)
	}
	got, err := protocol.DecodeInputFrame(r, MaxFrameBytes)
	if err != nil || got.Header.Type != protocol.InputTypeReceipt || got.Header.Flags != 0 {
		t.Fatalf("chord capability reply %+v: %v", got, err)
	}
	f = validTap()
	f.Header.Type = protocol.InputTypeTapChord
	f.Reserved = 2
	if err := WriteFrame(c, f); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ReadByte(); err == nil {
		t.Fatal("unnegotiated chord did not close stream")
	}
	if sink.countTap() != 0 {
		t.Fatal("chord dispatched without capability")
	}
}
