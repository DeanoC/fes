package remotemedia

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type previewTestResponse struct {
	mu      sync.Mutex
	header  http.Header
	body    bytes.Buffer
	flushed chan struct{}
}

func newPreviewTestResponse() *previewTestResponse {
	return &previewTestResponse{header: make(http.Header), flushed: make(chan struct{}, 1)}
}
func (w *previewTestResponse) Header() http.Header { return w.header }
func (*previewTestResponse) WriteHeader(int)       {}
func (w *previewTestResponse) Write(value []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.body.Write(value)
}
func (w *previewTestResponse) Flush() {
	select {
	case w.flushed <- struct{}{}:
	default:
	}
}
func (w *previewTestResponse) bodyString() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.body.String()
}

type previewTestCapture struct {
	samples chan EncodedSample
	closed  chan struct{}
	once    sync.Once
}

func (c *previewTestCapture) Start() error { return nil }
func (c *previewTestCapture) Next(ctx context.Context) (EncodedSample, error) {
	select {
	case sample := <-c.samples:
		return sample, nil
	case <-c.closed:
		return EncodedSample{}, context.Canceled
	case <-ctx.Done():
		return EncodedSample{}, ctx.Err()
	}
}
func (*previewTestCapture) Stats() CaptureStats { return CaptureStats{} }
func (c *previewTestCapture) Close() error {
	c.once.Do(func() { close(c.closed) })
	return nil
}

type previewTestDecoder struct {
	writes   chan []byte
	done     chan struct{}
	once     sync.Once
	mu       sync.Mutex
	failNext int
}

func (*previewTestDecoder) Start() error { return nil }
func (d *previewTestDecoder) Write(value []byte) (int, error) {
	d.mu.Lock()
	if d.failNext > 0 {
		d.failNext--
		d.mu.Unlock()
		return 0, errors.New("decoder write failed")
	}
	d.mu.Unlock()
	d.writes <- append([]byte(nil), value...)
	return len(value), nil
}
func (d *previewTestDecoder) Close() error { d.once.Do(func() { close(d.done) }); return nil }
func (d *previewTestDecoder) Wait() error  { <-d.done; return nil }
func (d *previewTestDecoder) Kill() error  { d.once.Do(func() { close(d.done) }); return nil }

func TestLocalPreviewFeedsExistingCaptureFramesAsAnnexBAndStops(t *testing.T) {
	capture := &previewTestCapture{samples: make(chan EncodedSample, 1), closed: make(chan struct{})}
	decoder := &previewTestDecoder{writes: make(chan []byte, 1), done: make(chan struct{})}
	preview, err := NewLocalPreview(func() (CaptureSource, error) { return capture, nil }, func(context.Context) (ManagedDecoder, error) { return decoder, nil })
	if err != nil {
		t.Fatal(err)
	}
	handle, err := preview.Start(context.Background(), "actraiser")
	if err != nil {
		t.Fatal(err)
	}
	capture.samples <- EncodedSample{AVCC: []byte{0, 0, 0, 2, 0x65, 0x01}, SPS: []byte{0x67, 0x42}, PPS: []byte{0x68, 0xce}, NALLengthSize: 4, Keyframe: true}
	select {
	case got := <-decoder.writes:
		want := []byte{0, 0, 0, 1, 0x67, 0x42, 0, 0, 0, 1, 0x68, 0xce, 0, 0, 0, 1, 0x65, 0x01}
		if string(got) != string(want) {
			t.Fatalf("Annex-B = %x, want %x", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("capture frame was not delivered")
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := handle.Stop(stopCtx); err != nil {
		t.Fatal(err)
	}
}

func TestLocalPreviewSkipsBadFramesAndKeepsRunning(t *testing.T) {
	capture := &previewTestCapture{samples: make(chan EncodedSample, 3), closed: make(chan struct{})}
	decoder := &previewTestDecoder{writes: make(chan []byte, 1), done: make(chan struct{}), failNext: 1}
	preview, err := NewLocalPreview(func() (CaptureSource, error) { return capture, nil }, func(context.Context) (ManagedDecoder, error) { return decoder, nil })
	if err != nil {
		t.Fatal(err)
	}
	handle, err := preview.Start(context.Background(), "actraiser")
	if err != nil {
		t.Fatal(err)
	}
	good := EncodedSample{AVCC: []byte{0, 0, 0, 2, 0x65, 0x01}, SPS: []byte{0x67, 0x42}, PPS: []byte{0x68, 0xce}, NALLengthSize: 4, Keyframe: true}
	capture.samples <- EncodedSample{AVCC: []byte{0, 0, 0}, NALLengthSize: 4}
	capture.samples <- good
	capture.samples <- good
	select {
	case got := <-decoder.writes:
		want := []byte{0, 0, 0, 1, 0x67, 0x42, 0, 0, 0, 1, 0x68, 0xce, 0, 0, 0, 1, 0x65, 0x01}
		if string(got) != string(want) {
			t.Fatalf("Annex-B = %x, want %x", got, want)
		}
	case <-handle.(interface{ Done() <-chan struct{} }).Done():
		t.Fatal("preview ended after a bad frame")
	case <-time.After(time.Second):
		t.Fatal("good frame was not delivered after skipped failures")
	}
	select {
	case <-handle.(interface{ Done() <-chan struct{} }).Done():
		t.Fatal("preview ended after recovering from a bad frame")
	default:
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := handle.Stop(stopCtx); err != nil {
		t.Fatal(err)
	}
}

func TestMJPEGPreviewStreamsCompleteLatestFrameWithSafeHeaders(t *testing.T) {
	preview := NewMJPEGPreview()
	owner := &mjpegPreviewDecoder{}
	preview.activate(owner)
	frame := []byte{0xff, 0xd8, 1, 2, 0xff, 0xd9}
	preview.publish(owner, frame)
	request := httptest.NewRequest("GET", "/api/v1/session/preview", nil)
	response := newPreviewTestResponse()
	done := make(chan struct{})
	go func() { preview.ServeHTTP(response, request); close(done) }()
	select {
	case <-response.flushed:
	case <-time.After(time.Second):
		t.Fatal("preview frame was not flushed")
	}
	preview.deactivate(owner)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("preview handler did not stop with its viewer")
	}
	if !strings.HasPrefix(response.Header().Get("Content-Type"), "multipart/x-mixed-replace") || response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("headers = %#v", response.Header())
	}
	if body := response.bodyString(); !strings.Contains(body, string(frame)) {
		t.Fatalf("body does not contain frame: %x", []byte(body))
	}
}

func TestMJPEGPreviewApplicationCancellationAllowsBoundedServerShutdown(t *testing.T) {
	preview := NewMJPEGPreview()
	owner := &mjpegPreviewDecoder{}
	preview.activate(owner)
	preview.publish(owner, []byte{0xff, 0xd8, 1, 0xff, 0xd9})
	appCtx, cancelApp := context.WithCancel(context.Background())
	server := httptest.NewUnstartedServer(preview)
	server.Config.BaseContext = func(net.Listener) context.Context { return appCtx }
	server.Start()
	response, err := server.Client().Get(server.URL)
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	cancelApp()
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), time.Second)
	defer cancelShutdown()
	if err := server.Config.Shutdown(shutdownCtx); err != nil {
		response.Body.Close()
		server.Close()
		t.Fatalf("shutdown: %v", err)
	}
	response.Body.Close()
	server.Close()
}

func TestLocalPreviewRejectsMissingFactories(t *testing.T) {
	if _, err := NewLocalPreview(nil, nil); err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("error = %v", err)
	}
	if _, err := (&LocalPreview{newCapture: func() (CaptureSource, error) { return nil, errors.New("private") }, newDecoder: func(context.Context) (ManagedDecoder, error) { return nil, nil }}).Start(context.Background(), "game"); !errors.Is(err, ErrManagedReceiverStart) {
		t.Fatalf("start error = %v", err)
	}
}
