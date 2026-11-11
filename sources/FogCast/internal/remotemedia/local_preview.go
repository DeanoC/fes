package remotemedia

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/DeanoC/FogCast/internal/mediasession"
)

type PreviewCaptureFactory func() (CaptureSource, error)
type PreviewDecoderFactory func(context.Context) (ManagedDecoder, error)

// LocalPreview feeds one native capture source directly into a browser
// preview converter and keeps the capture/converter lifetime inside the
// enclosing media session.
type LocalPreview struct {
	newCapture PreviewCaptureFactory
	newDecoder PreviewDecoderFactory
	timeout    time.Duration
}

func NewLocalPreview(newCapture PreviewCaptureFactory, newDecoder PreviewDecoderFactory) (*LocalPreview, error) {
	if newCapture == nil || newDecoder == nil {
		return nil, errors.New("local preview configuration is invalid")
	}
	return &LocalPreview{newCapture: newCapture, newDecoder: newDecoder, timeout: defaultPreviewStopTimeout}, nil
}

func (p *LocalPreview) Start(ctx context.Context, _ string) (mediasession.ComponentHandle, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil, errPreviewStart
	}
	decoder, err := p.newDecoder(ctx)
	if err != nil || decoder == nil {
		return nil, errPreviewStart
	}
	if err := decoder.Start(); err != nil {
		cleanupDecoder(decoder, p.timeout)
		return nil, errPreviewStart
	}
	capture, err := p.newCapture()
	if err != nil || capture == nil {
		cleanupDecoder(decoder, p.timeout)
		return nil, errPreviewStart
	}
	if err := capture.Start(); err != nil {
		_ = capture.Close()
		cleanupDecoder(decoder, p.timeout)
		return nil, errPreviewStart
	}
	runCtx, cancel := context.WithCancel(context.Background())
	h := &localPreviewHandle{capture: capture, decoder: decoder, cancel: cancel, done: make(chan struct{}), stopped: make(chan struct{}), timeout: p.timeout}
	go h.run(runCtx)
	return h, nil
}

type localPreviewHandle struct {
	capture CaptureSource
	decoder ManagedDecoder
	cancel  context.CancelFunc
	done    chan struct{}
	stopped chan struct{}
	timeout time.Duration
	once    sync.Once
}

func (h *localPreviewHandle) run(ctx context.Context) {
	defer close(h.done)
	for {
		sample, err := h.capture.Next(ctx)
		if err != nil {
			return
		}
		annexB, err := (EncodedAccessUnit{AVCC: sample.AVCC, SPS: sample.SPS, PPS: sample.PPS, NALLengthSize: sample.NALLengthSize, Keyframe: sample.Keyframe}).AnnexB()
		if err != nil {
			continue
		}
		// Drop a bad frame instead of ending preview.
		_, _ = h.decoder.Write(annexB)
	}
}

func (h *localPreviewHandle) Done() <-chan struct{} { return h.done }

func (h *localPreviewHandle) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	h.once.Do(func() { go h.cleanup() })
	select {
	case <-h.stopped:
		return nil
	case <-ctx.Done():
		return errPreviewStop
	}
}

func (h *localPreviewHandle) cleanup() {
	defer close(h.stopped)
	h.cancel()
	_ = h.capture.Close()
	_ = h.decoder.Close()
	waitCtx, cancel := context.WithTimeout(context.Background(), h.timeout)
	defer cancel()
	_ = waitDecoder(h.decoder, waitCtx)
}

var _ mediasession.Component = (*LocalPreview)(nil)

const defaultPreviewStopTimeout = 2 * time.Second

var (
	errPreviewStart = errors.New("local preview failed to start")
	errPreviewStop  = errors.New("local preview failed to stop")
)

type ManagedDecoder interface {
	Start() error
	Write([]byte) (int, error)
	Close() error
	Wait() error
	Kill() error
}

func cleanupDecoder(decoder ManagedDecoder, timeout time.Duration) {
	_ = decoder.Close()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	done := make(chan struct{})
	go func() {
		_ = waitDecoder(decoder, ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		// The waiter continues in the background so a process that takes
		// longer than the startup budget is still reaped after Kill.
	}
}
func waitDecoder(decoder ManagedDecoder, ctx context.Context) error {
	done := make(chan error, 1)
	go func() { done <- decoder.Wait() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		_ = decoder.Kill()
		// Reap after killing whenever the decoder honors Kill. Callers that
		// need a bounded wait run this function asynchronously (as startup
		// cleanup does, and Stop's cleanup worker does).
		<-done
		return ctx.Err()
	}
}
