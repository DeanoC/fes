package protocol

import (
	"bytes"
	"testing"
)

func TestMouseWireAndStrictSequence(t *testing.T) {
	f := InputFrame{Header: InputHeader{Type: InputTypeInput, Session: 7}, Seq: 10, Device: 2, Kind: 4, Action: 3, Code: 3, Value: -2147450881}
	b, err := EncodeInputFrame(f)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeInputFrame(bytes.NewReader(b), 1024)
	if err != nil || got != f {
		t.Fatalf("%+v %v", got, err)
	}
	for _, mutate := range []func(*InputFrame){func(f *InputFrame) { f.Code = 4 }, func(f *InputFrame) { f.Player = 1 }, func(f *InputFrame) { f.Action = 1 }, func(f *InputFrame) { f.Kind = 3 }} {
		bad := f
		mutate(&bad)
		if _, err := EncodeInputFrame(bad); err == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
	var seq SequenceTracker
	if ok, _ := seq.Observe(10); !ok {
		t.Fatal("first")
	}
	if ok, _ := seq.ObserveTransient(10); ok {
		t.Fatal("duplicate motion")
	}
	if ok, _ := seq.ObserveTransient(9); ok {
		t.Fatal("stale motion")
	}
	if ok, _ := seq.ObserveTransient(11); !ok {
		t.Fatal("next motion")
	}
	if ok, _ := seq.Observe(11); !ok {
		t.Fatal("idempotent legacy duplicate")
	}
}
