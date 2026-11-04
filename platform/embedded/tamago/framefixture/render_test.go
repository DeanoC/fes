package framefixture

import (
	"crypto/sha256"
	"errors"
	"image"
	"reflect"
	"strconv"
	"testing"
)

func TestActualTenfootFramesChangeAndStopOnFailure(t *testing.T) {
	seen := map[[32]byte]bool{}
	err := Render(12, func(index int, frame *image.RGBA) error {
		if frame.Rect.Dx() != 1280 || frame.Rect.Dy() != 720 {
			t.Fatal("unexpected tenfoot geometry")
		}
		seen[sha256.Sum256(frame.Pix)] = true
		return nil
	}, nil)
	if err != nil || len(seen) != 12 {
		t.Fatal("tenfoot states did not produce twelve distinct frames", len(seen), err)
	}
	cause := errors.New("presenter failed")
	calls := 0
	var progress []string
	err = Render(12, func(index int, _ *image.RGBA) error {
		calls++
		if index == 1 {
			return cause
		}
		return nil
	}, func(index int, phase string) { progress = append(progress, strconv.Itoa(index)+":"+phase) })
	if !errors.Is(err, cause) || calls != 2 {
		t.Fatal("renderer continued after a failed presentation", calls, err)
	}
	want := []string{"0:render_begin", "0:render_end", "0:present_begin", "0:present_end", "1:render_begin", "1:render_end", "1:present_begin"}
	if !reflect.DeepEqual(progress, want) {
		t.Fatal("progress reported a return after the failed presentation", progress)
	}
}
