package remotemedia

import (
	"errors"
	"testing"
)

func TestReceiverAuthenticatesControlAndDepacketizesMarkerDelimitedAccessUnit(t *testing.T) {
	r, err := NewReceiver(ReceiverConfig{Session: "s", Generation: 7, Token: "t", SSRC: 42})
	if err != nil {
		t.Fatalf("new receiver: %v", err)
	}
	defer r.Close()
	if err := r.AcceptControl(ControlMessage{Type: ControlMediaHello, Session: "s", Generation: 7, Token: "t", Body: []byte(`{"ssrc":42}`)}); err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	packets := []RTPPacket{
		{PayloadType: 96, SSRC: 42, Sequence: 10, Timestamp: 100, Payload: []byte{0x67, 1}},
		{PayloadType: 96, SSRC: 42, Sequence: 11, Timestamp: 100, Payload: []byte{0x68, 2}},
		{PayloadType: 96, SSRC: 42, Sequence: 12, Timestamp: 100, Marker: true, Payload: []byte{0x65, 3}},
	}
	var got []AccessUnit
	for _, p := range packets {
		u, err := r.Ingest(p.Marshal())
		if err != nil {
			t.Fatalf("ingest: %v", err)
		}
		got = append(got, u...)
	}
	if len(got) != 1 || len(got[0].NALs) != 3 || !got[0].Keyframe {
		t.Fatalf("access units = %#v", got)
	}
	report := r.Report()
	if report.Packets != 3 || report.AccessUnits != 1 || report.SPS != 1 || report.PPS != 1 || report.IDR != 1 {
		t.Fatalf("report = %#v", report)
	}
}

func TestReceiverReassemblesFUAAndCountsSequenceGaps(t *testing.T) {
	r, err := NewReceiver(ReceiverConfig{Session: "s", Generation: 1, Token: "t", SSRC: 9})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.AcceptControl(ControlMessage{Type: ControlMediaHello, Session: "s", Generation: 1, Token: "t", Body: []byte(`{"ssrc":9}`)}); err != nil {
		t.Fatal(err)
	}
	parts := [][]byte{{0x7c, 0x85, 'a'}, {0x7c, 0x05, 'b'}, {0x7c, 0x45, 'c'}}
	var units []AccessUnit
	for i, payload := range parts {
		u, err := r.Ingest(RTPPacket{PayloadType: 96, SSRC: 9, Sequence: uint16(20 + i), Timestamp: 3, Marker: i == 2, Payload: payload}.Marshal())
		if err != nil {
			t.Fatal(err)
		}
		units = append(units, u...)
	}
	if len(units) != 1 || len(units[0].NALs) != 1 || string(units[0].NALs[0]) != "\x65abc" {
		t.Fatalf("units = %#v", units)
	}
	if _, err := r.Ingest(RTPPacket{PayloadType: 96, SSRC: 9, Sequence: 25, Timestamp: 4, Marker: true, Payload: []byte{0x65, 9}}.Marshal()); err != nil {
		t.Fatal(err)
	}
	if r.Report().SequenceGaps != 2 {
		t.Fatalf("gaps = %d", r.Report().SequenceGaps)
	}
}

func TestReceiverRejectsInvalidRTPAndIdentityAndStopsCleanly(t *testing.T) {
	r, err := NewReceiver(ReceiverConfig{Session: "s", Generation: 2, Token: "t", SSRC: 8})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.AcceptControl(ControlMessage{Type: ControlMediaHello, Session: "wrong", Generation: 2, Token: "t"}); err == nil {
		t.Fatal("wrong session accepted")
	}
	if _, err := r.Ingest([]byte{0x40}); err == nil {
		t.Fatal("short RTP accepted")
	}
	if err := r.AcceptControl(ControlMessage{Type: ControlMediaHello, Session: "s", Generation: 2, Token: "t", Body: []byte(`{"ssrc":8}`)}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []RTPPacket{{PayloadType: 97, SSRC: 8, Sequence: 1, Payload: []byte{0x65}}, {PayloadType: 96, SSRC: 99, Sequence: 2, Payload: []byte{0x65}}} {
		if _, err := r.Ingest(p.Marshal()); err == nil {
			t.Fatalf("invalid packet accepted: %#v", p)
		}
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Ingest((&RTPPacket{PayloadType: 96, SSRC: 8, Payload: []byte{0x65}}).Marshal()); !errors.Is(err, ErrReceiverClosed) {
		t.Fatalf("close error = %v", err)
	}
}

func TestReceiverRejectsBackwardSequenceAndPreAuthControl(t *testing.T) {
	r, err := NewReceiver(ReceiverConfig{Session: "s", Generation: 1, Token: "t", SSRC: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.AcceptControl(ControlMessage{Type: ControlMediaReport, Session: "s", Generation: 1, Token: "t"}); err == nil {
		t.Fatal("pre-auth report accepted")
	}
	if err := r.AcceptControl(ControlMessage{Type: ControlMediaHello, Session: "s", Generation: 1, Token: "t", Body: []byte(`{"ssrc":1}`)}); err != nil {
		t.Fatal(err)
	}
	first := RTPPacket{PayloadType: RTPPayloadTypeH264, SSRC: 1, Sequence: 100, Timestamp: 1, Marker: true, Payload: []byte{0x61, 1}}
	if _, err := r.Ingest(first.Marshal()); err != nil {
		t.Fatal(err)
	}
	backward := RTPPacket{PayloadType: RTPPayloadTypeH264, SSRC: 1, Sequence: 99, Timestamp: 1, Marker: true, Payload: []byte{0x61, 2}}
	if _, err := r.Ingest(backward.Marshal()); err == nil {
		t.Fatal("backward RTP sequence accepted")
	}
	if got := r.Report().Malformed; got != 1 {
		t.Fatalf("malformed = %d, want 1", got)
	}
}

func TestReceiverRejectsMalformedFUAndFlushesOnlyOnMarker(t *testing.T) {
	r, _ := NewReceiver(ReceiverConfig{Session: "s", Generation: 1, Token: "t", SSRC: 1})
	defer r.Close()
	_ = r.AcceptControl(ControlMessage{Type: ControlMediaHello, Session: "s", Generation: 1, Token: "t", Body: []byte(`{"ssrc":1}`)})
	if _, err := r.Ingest(RTPPacket{PayloadType: 96, SSRC: 1, Sequence: 1, Payload: []byte{28}}.Marshal()); err == nil {
		t.Fatal("malformed FU accepted")
	}
	u, err := r.Ingest(RTPPacket{PayloadType: 96, SSRC: 1, Sequence: 2, Timestamp: 4, Payload: []byte{0x65, 1}}.Marshal())
	if err != nil || len(u) != 0 {
		t.Fatalf("non-marker result = %#v, %v", u, err)
	}
}
