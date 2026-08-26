package remotemedia

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

const maxPreviewJPEGSize = 8 << 20
const previewWriteTimeout = 5 * time.Second

// MJPEGPreview publishes JPEG frames produced from the active local capture.
// It owns no capture device; session lifecycle remains with LocalPreview.
type MJPEGPreview struct {
	mu      sync.Mutex
	owner   *mjpegPreviewDecoder
	frame   []byte
	seq     uint64
	changed chan struct{}
}

func NewMJPEGPreview() *MJPEGPreview { return &MJPEGPreview{changed: make(chan struct{})} }

// Decoder returns a fresh session-owned H.264-to-JPEG converter.
func (p *MJPEGPreview) Decoder(context.Context) (ManagedDecoder, error) {
	if p == nil {
		return nil, errors.New("MJPEG preview is unavailable")
	}
	cmd := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "warning", "-f", "h264", "-i", "pipe:0", "-an", "-fps_mode", "passthrough", "-c:v", "mjpeg", "-q:v", "5", "-flush_packets", "1", "-f", "image2pipe", "pipe:1")
	input, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		_ = input.Close()
		return nil, err
	}
	return &mjpegPreviewDecoder{preview: p, cmd: cmd, input: input, output: output, readDone: make(chan struct{})}, nil
}

func (p *MJPEGPreview) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	sessionDone := p.sessionDone()
	if sessionDone == nil {
		http.Error(w, "session preview is inactive", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=fogcast-frame")
	controller := http.NewResponseController(w)
	flusher, _ := w.(http.Flusher)
	var seq uint64
	for {
		frame, next, changed := p.snapshot(seq)
		if len(frame) == 0 {
			select {
			case <-r.Context().Done():
				return
			case <-sessionDone:
				return
			case <-changed:
				continue
			}
		}
		_ = controller.SetWriteDeadline(time.Now().Add(previewWriteTimeout))
		if _, err := fmt.Fprintf(w, "--fogcast-frame\r\nContent-Type: image/jpeg\r\nContent-Length: %s\r\n\r\n", strconv.Itoa(len(frame))); err != nil {
			return
		}
		if _, err := w.Write(frame); err != nil {
			return
		}
		if _, err := io.WriteString(w, "\r\n"); err != nil {
			return
		}
		if flusher != nil {
			flusher.Flush()
		}
		seq = next
		select {
		case <-r.Context().Done():
			return
		case <-sessionDone:
			return
		case <-changed:
		}
	}
}

func (p *MJPEGPreview) sessionDone() <-chan struct{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.owner == nil {
		return nil
	}
	return p.owner.sessionDone
}

func (p *MJPEGPreview) snapshot(after uint64) ([]byte, uint64, <-chan struct{}) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.seq <= after || len(p.frame) == 0 {
		return nil, p.seq, p.changed
	}
	return append([]byte(nil), p.frame...), p.seq, p.changed
}

func (p *MJPEGPreview) activate(owner *mjpegPreviewDecoder) {
	p.mu.Lock()
	owner.sessionDone = make(chan struct{})
	p.owner = owner
	p.frame = nil
	p.signalLocked()
	p.mu.Unlock()
}

func (p *MJPEGPreview) publish(owner *mjpegPreviewDecoder, frame []byte) {
	p.mu.Lock()
	if p.owner == owner {
		p.frame = append(p.frame[:0], frame...)
		p.seq++
		p.signalLocked()
	}
	p.mu.Unlock()
}

func (p *MJPEGPreview) deactivate(owner *mjpegPreviewDecoder) {
	p.mu.Lock()
	if p.owner == owner {
		p.owner = nil
		p.frame = nil
		close(owner.sessionDone)
		p.signalLocked()
	}
	p.mu.Unlock()
}

func (p *MJPEGPreview) signalLocked() {
	close(p.changed)
	p.changed = make(chan struct{})
}

type mjpegPreviewDecoder struct {
	preview     *MJPEGPreview
	cmd         *exec.Cmd
	input       io.WriteCloser
	output      io.ReadCloser
	readDone    chan struct{}
	sessionDone chan struct{}
	started     bool
}

func (d *mjpegPreviewDecoder) Start() error {
	if d.started {
		return errors.New("MJPEG preview decoder is already started")
	}
	if err := d.cmd.Start(); err != nil {
		_ = d.input.Close()
		_ = d.output.Close()
		close(d.readDone)
		return err
	}
	d.started = true
	d.preview.activate(d)
	go d.readFrames()
	return nil
}

func (d *mjpegPreviewDecoder) Write(p []byte) (int, error) { return d.input.Write(p) }
func (d *mjpegPreviewDecoder) Close() error                { return d.input.Close() }
func (d *mjpegPreviewDecoder) Kill() error {
	if d.cmd.Process == nil {
		return nil
	}
	return d.cmd.Process.Kill()
}
func (d *mjpegPreviewDecoder) Wait() error {
	err := d.cmd.Wait()
	if !d.started {
		return err
	}
	<-d.readDone
	d.preview.deactivate(d)
	return err
}

func (d *mjpegPreviewDecoder) readFrames() {
	defer close(d.readDone)
	defer d.output.Close()
	buffer := make([]byte, 0, 256<<10)
	chunk := make([]byte, 32<<10)
	for {
		n, err := d.output.Read(chunk)
		if n > 0 {
			buffer = append(buffer, chunk[:n]...)
			buffer = d.consumeFrames(buffer)
		}
		if err != nil {
			return
		}
	}
}

func (d *mjpegPreviewDecoder) consumeFrames(buffer []byte) []byte {
	for {
		start := bytes.Index(buffer, []byte{0xff, 0xd8})
		if start < 0 {
			if len(buffer) > 1 {
				return append(buffer[:0], buffer[len(buffer)-1])
			}
			return buffer
		}
		buffer = buffer[start:]
		end := bytes.Index(buffer[2:], []byte{0xff, 0xd9})
		if end < 0 {
			if len(buffer) > maxPreviewJPEGSize {
				return buffer[:0]
			}
			return buffer
		}
		end += 4
		d.preview.publish(d, buffer[:end])
		buffer = buffer[end:]
	}
}

var _ ManagedDecoder = (*mjpegPreviewDecoder)(nil)
var _ http.Handler = (*MJPEGPreview)(nil)
