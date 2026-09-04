package attractvideo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	ffmpegMaxW       = 1280
	ffmpegMaxH       = 720
	ffmpegOpenWait   = 8 * time.Second
	ffmpegMaxSeconds = 60
)

var (
	lookPath       = exec.LookPath
	commandContext = exec.CommandContext
)

func ffmpegAvailable() bool {
	_, err := lookPath("ffmpeg")
	return err == nil
}

func openFFmpeg(path string) (Player, error) {
	if path == "" {
		return nil, errors.New("video path is empty")
	}
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	ffmpeg, err := lookPath("ffmpeg")
	if err != nil {
		return nil, ErrUnavailable
	}
	w, h, err := probeVideoSize(path)
	if err != nil {
		w, h = ffmpegMaxW, ffmpegMaxH
	}
	w, h = fitAttractSize(w, h)
	ctx, cancel := context.WithCancel(context.Background())
	args := []string{
		"-hide_banner", "-nostdin", "-loglevel", "error",
		"-re", "-i", path, "-an", "-t", strconv.Itoa(ffmpegMaxSeconds),
		"-vf", fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2,format=rgba", w, h, w, h),
		"-f", "rawvideo", "-pix_fmt", "rgba", "pipe:1",
	}
	cmd := commandContext(ctx, ffmpeg, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stderr := &lockedBuffer{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}
	p := &ffmpegPlayer{
		cmd:    cmd,
		cancel: cancel,
		stdout: stdout,
		stderr: stderr,
		img:    image.NewRGBA(image.Rect(0, 0, w, h)),
		latest: make([]byte, w*h*4),
		ready:  make(chan error, 1),
		frameN: w * h * 4,
	}
	go p.readLoop()
	timer := time.NewTimer(ffmpegOpenWait)
	defer timer.Stop()
	select {
	case err := <-p.ready:
		if err != nil {
			p.Close()
			return nil, err
		}
	case <-timer.C:
		p.Close()
		return nil, errors.New("video open timed out")
	}
	return p, nil
}

func probeVideoSize(path string) (int, int, error) {
	bin, err := lookPath("ffprobe")
	if err != nil {
		return 0, 0, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := commandContext(ctx, bin, "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=width,height", "-of", "csv=p=0:s=x", path)
	out, err := cmd.Output()
	if err != nil {
		return 0, 0, err
	}
	line := strings.TrimSpace(string(out))
	parts := strings.Split(line, "x")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("ffprobe size %q", line)
	}
	w, errW := strconv.Atoi(parts[0])
	h, errH := strconv.Atoi(parts[1])
	if errW != nil || errH != nil || w < 1 || h < 1 {
		return 0, 0, fmt.Errorf("ffprobe size %q", line)
	}
	return w, h, nil
}

func fitAttractSize(w, h int) (int, int) {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	if w <= ffmpegMaxW && h <= ffmpegMaxH {
		return w, h
	}
	sx := float64(ffmpegMaxW) / float64(w)
	sy := float64(ffmpegMaxH) / float64(h)
	scale := sx
	if sy < sx {
		scale = sy
	}
	ow := int(float64(w)*scale + 0.5)
	oh := int(float64(h)*scale + 0.5)
	if ow < 1 {
		ow = 1
	}
	if oh < 1 {
		oh = 1
	}
	return ow, oh
}

type ffmpegPlayer struct {
	mu       sync.Mutex
	cmd      *exec.Cmd
	cancel   context.CancelFunc
	stdout   io.ReadCloser
	stderr   *lockedBuffer
	img      *image.RGBA
	latest   []byte
	seq      int
	seen     int
	ended    bool
	err      error
	closed   bool
	ready    chan error
	frameN   int
	signaled bool
}

func (p *ffmpegPlayer) readLoop() {
	buf := make([]byte, p.frameN)
	for {
		_, err := io.ReadFull(p.stdout, buf)
		if err != nil {
			p.mu.Lock()
			p.ended = true
			if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
				p.err = err
			}
			if msg := strings.TrimSpace(p.stderr.String()); p.err == nil && msg != "" && p.seq == 0 {
				p.err = errors.New(msg)
			}
			if p.seq == 0 && p.err == nil {
				p.err = errors.New("video produced no frames")
			}
			p.signalReadyLocked()
			p.mu.Unlock()
			return
		}
		p.mu.Lock()
		copy(p.latest, buf)
		p.seq++
		p.signalReadyLocked()
		p.mu.Unlock()
	}
}

func (p *ffmpegPlayer) signalReadyLocked() {
	if p.signaled {
		return
	}
	p.signaled = true
	if p.seq == 0 {
		if p.err != nil {
			p.ready <- p.err
			return
		}
		p.ready <- errors.New("video produced no frames")
		return
	}
	p.ready <- nil
}

func (p *ffmpegPlayer) Frame() (*image.RGBA, bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil, false, errors.New("video player is closed")
	}
	if p.seq == p.seen {
		if p.err != nil && p.seq == 0 {
			return nil, true, p.err
		}
		return nil, p.ended, nil
	}
	copy(p.img.Pix, p.latest)
	p.seen = p.seq
	return p.img, p.ended, nil
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	if l == nil {
		return len(p), nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	if l == nil {
		return ""
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func (p *ffmpegPlayer) Close() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	cancel := p.cancel
	cmd := p.cmd
	stdout := p.stdout
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	if stdout != nil {
		_ = stdout.Close()
	}
	if cmd != nil {
		_ = cmd.Wait()
	}
}
