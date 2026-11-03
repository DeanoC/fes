package framefixture

import (
	"crypto/sha256"
	"errors"
	"image"
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
	})
	if err != nil || len(seen) != 12 {
		t.Fatal("tenfoot states did not produce twelve distinct frames", len(seen), err)
	}
	cause := errors.New("presenter failed")
	calls := 0
	err = Render(12, func(index int, _ *image.RGBA) error {
		calls++
		if index == 1 {
			return cause
		}
		return nil
	})
	if !errors.Is(err, cause) || calls != 2 {
		t.Fatal("renderer continued after a failed presentation", calls, err)
	}
}
