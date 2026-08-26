package remotemedia

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/DeanoC/FogCast-POC/internal/mediasession"
)

type PreviewCaptureFactory func() (CaptureSource, error)
type PreviewDecoderFactory func(context.Context) (ManagedDecoder, error)

// LocalPreview feeds one native capture source directly into a browser
// preview converter. It avoids claiming the target cast surface and keeps the
// capture/converter lifetime inside the enclosing media session.
type LocalPreview struct {
	newCapture PreviewCaptureFactory
	newDecoder PreviewDecoderFactory
	timeout    time.Duration
}

func NewLocalPreview(newCapture PreviewCaptureFactory, newDecoder PreviewDecoderFactory) (*LocalPreview, error) {
	if newCapture == nil || newDecoder == nil {
		return nil, errors.New("local preview configuration is invalid")
	}
	return &LocalPreview{newCapture: newCapture, newDecoder: newDecoder, timeout: defaultManagedReceiverStopTimeout}, nil
}

func (p *LocalPreview) Start(ctx context.Context, _ string) (mediasession.ComponentHandle, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil, ErrManagedReceiverStart
	}
	decoder, err := p.newDecoder(ctx)
	if err != nil || decoder == nil {
		return nil, ErrManagedReceiverStart
	}
	if err := decoder.Start(); err != nil {
		cleanupDecoder(decoder, p.timeout)
		return nil, ErrManagedReceiverStart
	}
	capture, err := p.newCapture()
	if err != nil || capture == nil {
		cleanupDecoder(decoder, p.timeout)
		return nil, ErrManagedReceiverStart
	}
	if err := capture.Start(); err != nil {
		_ = capture.Close()
		cleanupDecoder(decoder, p.timeout)
		return nil, ErrManagedReceiverStart
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
		// Match managed RTP receiver: drop a bad frame instead of ending preview.
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
		return ErrManagedReceiverStop
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
