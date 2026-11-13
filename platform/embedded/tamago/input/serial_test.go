package input

import (
	"reflect"
	"testing"
	"time"
)

func decode(bytes []byte) []Event {
	var d Decoder
	now := time.Unix(1, 0)
	var events []Event
	for _, b := range bytes {
		if e, ok := d.Feed(b, now); ok {
			events = append(events, e)
		}
	}
	return events
}

func TestArrowAndPlainKeys(t *testing.T) {
	got := decode([]byte("\x1b[A\x1b[B\x1b[C\x1b[D\x1b[H\x1b[ZA x\t\r\n\x7f"))
	want := []Event{{Key: "up"}, {Key: "down"}, {Key: "right"}, {Key: "left"}, {Key: "home"}, {Key: "shift-tab"},
		{Key: "a", Text: "A"}, {Key: "space", Text: " "}, {Key: "x", Text: "x"}, {Key: "tab"}, {Key: "return"}, {Key: "backspace"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestEscapeIsBoundedAndDoesNotSwallowArrow(t *testing.T) {
	var d Decoder
	now := time.Unix(1, 0)
	if _, ok := d.Feed(27, now); ok {
		t.Fatal("escape emitted before ambiguity interval")
	}
	if _, ok := d.Flush(now.Add(EscapeDelay - time.Nanosecond)); ok {
		t.Fatal("escape emitted early")
	}
	if e, ok := d.Flush(now.Add(EscapeDelay)); !ok || e.Key != "escape" {
		t.Fatalf("escape not emitted: %+v %v", e, ok)
	}
	if _, ok := d.Flush(now.Add(time.Second)); ok {
		t.Fatal("escape replayed")
	}
	d.Feed(27, now)
	d.Feed('[', now)
	if e, ok := d.Feed('C', now); !ok || e.Key != "right" {
		t.Fatalf("arrow: %+v %v", e, ok)
	}
	if _, ok := d.Flush(now.Add(time.Second)); ok {
		t.Fatal("arrow left escape pending")
	}
}

func TestUnknownControlDoesNotBecomeInput(t *testing.T) {
	if got := decode([]byte{0, 1, 2, 3, 255}); len(got) != 0 {
		t.Fatalf("control bytes became input: %+v", got)
	}
	if got := decode([]byte("\x1b[Q")); len(got) != 0 {
		t.Fatalf("unknown CSI became input: %+v", got)
	}
}
