//go:build linux

package menudisplay

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRefusedCommitRetainsClientTimingWithoutCompletion(t *testing.T) {
	for _, mode := range []string{"frame", "mapped-frame", "blit"} {
		t.Run(mode, func(t *testing.T) {
			f, path := newFakeRuntime(t)
			f.mu.Lock()
			f.frameMapping = mode == "mapped-frame"
			f.blitter, f.blitterMinor = mode == "blit", MinBlitterMinor
			f.checkCommit = func() error {
				time.Sleep(5 * time.Millisecond)
				return errors.New("injected scanout fault")
			}
			f.mu.Unlock()
			client := New(path)
			defer client.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var result Result
			var err error
			if mode == "blit" {
				result, err = client.PresentBlit(ctx, 7, BlitFrame{Ops: []BlitOp{{Kind: BlitFill, W: Width, H: Height, Color: 0xff123456}}})
			} else {
				result, err = client.Present(ctx, 7, filled(0x11, 0x22, 0x33))
			}
			if err == nil || !strings.Contains(err.Error(), "injected scanout fault") {
				t.Fatalf("expected original refusal, got result=%+v error=%v", result, err)
			}
			if result.Generation != 0 || result.DisplayedSequence != 0 || result.Underflows != 0 {
				t.Fatalf("refused frame acquired a completion: %+v", result)
			}
			// A tiny blitter list may stage within one microsecond. The
			// controlled refusal still has measurable socket/commit phases.
			if tm := result.Timing; tm.BeginUS <= 0 || tm.StageUS < 0 || tm.CommitUS <= 0 ||
				tm.RuntimeCleanUS != 0 || tm.RuntimeBlitUS != 0 || tm.RuntimeSubmitUS != 0 || tm.RuntimeScanoutWaitUS != 0 {
				t.Fatalf("lost measured client phases or invented runtime phases: %+v", tm)
			}
			if mode == "blit" {
				if result.Timing.Commands == 0 {
					t.Fatal("lost staged blitter command count")
				}
			} else if result.Timing.StagedPixels != Width*Height {
				t.Fatalf("lost staged pixel count: %+v", result.Timing)
			}
			if client.window.held != [Slots]uint64{} || (mode == "mapped-frame" && len(client.window.mapping) != 0) {
				t.Fatal("failed frame retained slot ownership or its per-frame mapping")
			}
		})
	}
}
