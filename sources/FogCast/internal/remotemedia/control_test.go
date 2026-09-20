package remotemedia

import (
	"bytes"
	"strings"
	"testing"
)

type shortControlWriter struct {
	bytes.Buffer
}

func (w *shortControlWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	return w.Buffer.Write(p[:1])
}

func TestControlMessagesAreLengthFramedAndBounded(t *testing.T) {
	var wire bytes.Buffer
	want := ControlMessage{Type: ControlMediaHello, Session: "session", Generation: 3, Token: "secret", Body: []byte(`{"width":1280}`)}
	if err := WriteControlMessage(&wire, want); err != nil {
		t.Fatalf("write control message: %v", err)
	}
	raw := wire.Bytes()
	got, err := ReadControlMessage(&wire)
	if err != nil {
		t.Fatalf("read control message: %v", err)
	}
	if got.Type != want.Type || got.Session != want.Session || got.Generation != want.Generation || got.Token != want.Token || string(got.Body) != string(want.Body) {
		t.Fatalf("message = %#v, want %#v", got, want)
	}
	if !strings.Contains(string(raw), "secret") {
		t.Fatal("authenticated control envelope omitted its token")
	}
}

func TestControlAuthenticationRejectsWrongSessionGenerationOrToken(t *testing.T) {
	want := ControlMessage{Type: ControlKeyframeRequest, Session: "session", Generation: 3, Token: "secret"}
	for _, got := range []ControlMessage{
		{Type: ControlKeyframeRequest, Session: "other", Generation: 3, Token: "secret"},
		{Type: ControlKeyframeRequest, Session: "session", Generation: 4, Token: "secret"},
		{Type: ControlKeyframeRequest, Session: "session", Generation: 3, Token: "wrong"},
	} {
		if err := ValidateControlMessage(got, want.Session, want.Generation, want.Token); err == nil {
			t.Fatalf("invalid control message accepted: %#v", got)
		}
	}
	if err := ValidateControlMessage(want, want.Session, want.Generation, want.Token); err != nil {
		t.Fatalf("valid control message rejected: %v", err)
	}
}

func TestControlReaderRejectsOversizedFrame(t *testing.T) {
	var wire bytes.Buffer
	wire.Write([]byte{0, 0, 0, 0x11})
	wire.WriteString(strings.Repeat("x", 17))
	if _, err := readControlMessageLimit(&wire, 16); err == nil {
		t.Fatal("oversized control frame accepted")
	}
}

func TestControlWriterHandlesShortWrites(t *testing.T) {
	var wire shortControlWriter
	want := ControlMessage{Type: ControlPing, Session: "session", Generation: 1, Token: "token"}
	if err := WriteControlMessage(&wire, want); err != nil {
		t.Fatalf("write control message through short writer: %v", err)
	}
	got, err := ReadControlMessage(&wire.Buffer)
	if err != nil {
		t.Fatalf("read control message after short writes: %v", err)
	}
	if got.Type != want.Type || got.Session != want.Session || got.Generation != want.Generation || got.Token != want.Token {
		t.Fatalf("message = %#v, want %#v", got, want)
	}
}
