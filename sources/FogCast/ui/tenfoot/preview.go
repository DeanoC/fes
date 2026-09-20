package tenfoot

import (
	"context"
	"errors"
	"github.com/DeanoC/FogCast/ui/shared"
	"image"
	"io"
	"time"
)

const (
	previewRetryMin = time.Second
	previewRetryMax = 8 * time.Second
	previewLabel    = "Preview"
)

// PreviewSnapshot is the optional now-playing MJPEG surface.
type PreviewSnapshot struct {
	Label       string
	Image       *image.RGBA
	FrameSeq    int
	Live        bool
	Unavailable bool
}

func previewRetryDelay(fails int) time.Duration {
	if fails < 1 {
		fails = 1
	}
	delay := previewRetryMin
	for i := 1; i < fails && delay < previewRetryMax; i++ {
		delay *= 2
	}
	if delay > previewRetryMax {
		return previewRetryMax
	}
	return delay
}

func (a *App) previewWantedLocked() bool {
	if a.cancel == nil || a.ctx == nil {
		return false
	}
	if a.attractActive {
		return false
	}
	if a.settingsOpen || a.filtersOpen || a.searchOpen || a.detailOpen || a.viewPickerOpen ||
		a.collectionManageOpen || a.collectionConfirmOpen || a.nameEntryOpenLocked() {
		return false
	}
	if a.stopPhase == "stopping" {
		return false
	}
	return a.session.State == "active"
}

func (a *App) syncPreviewLocked() {
	if !a.previewWantedLocked() {
		if a.previewCancel != nil || a.previewLive || a.previewImage != nil {
			a.stopPreviewLocked()
		}
		a.previewUnavailable = false
		a.previewFails = 0
		a.previewNext = time.Time{}
		return
	}
	if a.previewUnavailable {
		return
	}
	if a.previewLive {
		return
	}
	if !a.previewNext.IsZero() && time.Now().Before(a.previewNext) {
		return
	}
	a.startPreviewLocked()
}

func (a *App) startPreviewLocked() {
	a.stopPreviewLocked()
	parent := a.ctx
	if parent == nil {
		return
	}
	ctx, cancel := context.WithCancel(parent)
	a.previewCancel = cancel
	a.previewLive = true
	a.previewUnavailable = false
	gen := a.previewGen
	go a.runPreview(ctx, gen)
}

func (a *App) stopPreviewLocked() {
	a.previewGen++
	if a.previewCancel != nil {
		a.previewCancel()
		a.previewCancel = nil
	}
	a.previewLive = false
	a.previewImage = nil
	a.previewSeq = 0
}

func (a *App) runPreview(ctx context.Context, gen int) {
	defer a.finishPreview(gen)
	if a.client == nil {
		a.notePreviewError(gen, PreviewUnavailable{Message: "session preview is unavailable"})
		return
	}
	stream, err := a.client.OpenSessionPreview(ctx)
	if err != nil {
		a.notePreviewError(gen, err)
		return
	}
	defer stream.Close()
	for {
		if ctx.Err() != nil {
			return
		}
		jpeg, err := stream.NextJPEG()
		if err != nil {
			a.notePreviewError(gen, err)
			return
		}
		img, err := shared.DecodeStill(jpeg)
		if err != nil {
			continue
		}
		a.mu.Lock()
		stale := gen != a.previewGen
		if !stale {
			a.previewImage = img
			a.previewSeq++
			a.previewFails = 0
			a.previewNext = time.Time{}
		}
		a.mu.Unlock()
		if stale {
			return
		}
	}
}

func (a *App) finishPreview(gen int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if gen != a.previewGen {
		return
	}
	a.previewLive = false
}

func (a *App) notePreviewError(gen int, err error) {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		err = PreviewUnavailable{Message: "session preview is unavailable"}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if gen != a.previewGen {
		return
	}
	a.previewLive = false
	if IsPreviewUnavailable(err) && previewUnavailablePermanent(err) {
		a.previewUnavailable = true
		a.previewImage = nil
		a.previewSeq = 0
		return
	}
	a.previewFails++
	a.previewNext = time.Now().Add(previewRetryDelay(a.previewFails))
}

func (a *App) previewSnapshotLocked() PreviewSnapshot {
	wanted := a.previewWantedLocked()
	if !wanted && a.previewImage == nil && !a.previewLive {
		return PreviewSnapshot{}
	}
	return PreviewSnapshot{
		Label:       previewLabel,
		Image:       a.previewImage,
		FrameSeq:    a.previewSeq,
		Live:        a.previewLive,
		Unavailable: a.previewUnavailable,
	}
}
