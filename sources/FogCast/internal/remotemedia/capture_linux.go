//go:build linux

package remotemedia

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	linuxCaptureStartupTimeout = 4 * time.Second
	linuxCaptureStopTimeout    = 2 * time.Second
	linuxCaptureWidth          = 1920
	linuxCaptureHeight         = 1080
	linuxCaptureFPSNumerator   = 30
	linuxCaptureFPSDenominator = 1
	linuxCaptureGOP            = 30
)

// NativeCapture is the Linux V4L2 adapter. FFmpeg reads the UVC device and
// emits low-latency H.264 access units, preserving the CaptureSource contract
// used by both local preview and the existing RTP sender.
type NativeCapture struct {
	mu       sync.Mutex
	cmd      *exec.Cmd
	stdout   io.ReadCloser
	stderr   *bytes.Buffer
	frames   chan EncodedSample
	ready    chan struct{}
	done     chan struct{}
	readyOne sync.Once
	started  bool
	closed   bool
	width    int
	height   int
	fps      FrameRate
	stats    CaptureStats
	runtime  error
	origin   time.Time
}

func ListCaptureDevices() ([]CaptureDevice, error) {
	return nil, errors.New("Linux capture-device enumeration is not implemented; configure a V4L2 path")
}

func OpenNativeCapture(config CaptureConfig) (*NativeCapture, error) {
	if err := validateCaptureConfig(config); err != nil {
		return nil, err
	}
	device := strings.TrimSpace(config.Device)
	if device == "" || !filepath.IsAbs(device) {
		return nil, errors.New("Linux V4L2 capture device must be an absolute path")
	}
	info, err := os.Stat(device)
	if err != nil {
		return nil, fmt.Errorf("stat Linux V4L2 capture device: %w", err)
	}
	if info.Mode()&os.ModeCharDevice == 0 {
		return nil, errors.New("Linux V4L2 capture device is not a character device")
	}
	ffmpeg, err := previewFFmpegPath()
	if err != nil {
		return nil, err
	}

	width, height := config.Width, config.Height
	if width == 0 || height == 0 {
		width, height = linuxCaptureWidth, linuxCaptureHeight
	}
	fps := FrameRate{Numerator: config.FPS.Numerator, Denominator: config.FPS.Denominator}
	if fps.Numerator == 0 {
		fps = FrameRate{Numerator: linuxCaptureFPSNumerator, Denominator: linuxCaptureFPSDenominator}
	}
	gop := config.GOP
	if gop == 0 {
		gop = linuxCaptureGOP
	}
	args := []string{
		"-hide_banner", "-loglevel", "error", "-nostdin",
		"-f", "v4l2", "-input_format", "mjpeg",
		"-video_size", fmt.Sprintf("%dx%d", width, height),
		"-framerate", fmt.Sprintf("%d/%d", fps.Numerator, fps.Denominator),
		"-i", device,
		"-an", "-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency",
		"-pix_fmt", "yuv420p", "-g", strconv.Itoa(gop),
		"-x264-params", fmt.Sprintf("aud=1:repeat-headers=1:keyint=%d:min-keyint=%d:scenecut=0", gop, gop),
		"-f", "h264", "pipe:1",
	}
	cmd := exec.Command(ffmpeg, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("open Linux V4L2 capture output: %w", err)
	}
	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr
	return &NativeCapture{
		cmd: cmd, stdout: stdout, stderr: stderr,
		frames: make(chan EncodedSample, 2), ready: make(chan struct{}), done: make(chan struct{}),
		width: width, height: height, fps: fps,
		stats: CaptureStats{Width: width, Height: height, FPS: fps},
	}, nil
}

func (c *NativeCapture) Start() error {
	if c == nil {
		return errors.New("capture handle is closed")
	}
	c.mu.Lock()
	if c.closed || c.started {
		c.mu.Unlock()
		return errors.New("capture handle is closed or already started")
	}
	if err := c.cmd.Start(); err != nil {
		c.runtime = err
		c.closed = true
		c.mu.Unlock()
		return fmt.Errorf("start Linux V4L2 capture: %w", err)
	}
	c.started = true
	c.origin = time.Now()
	c.mu.Unlock()
	go c.readLoop()

	timer := time.NewTimer(linuxCaptureStartupTimeout)
	defer timer.Stop()
	select {
	case <-c.ready:
		return nil
	case <-c.done:
		return c.startupError()
	case <-timer.C:
		_ = c.Close()
		return errors.New("start Linux V4L2 capture: no frame received before timeout")
	}
}

func (c *NativeCapture) Next(ctx context.Context) (EncodedSample, error) {
	if c == nil {
		return EncodedSample{}, errors.New("capture handle is closed")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		return EncodedSample{}, ctx.Err()
	case sample, ok := <-c.frames:
		if !ok {
			return EncodedSample{}, c.runtimeError()
		}
		c.mu.Lock()
		c.stats.CapturedFrames++
		c.stats.EncodedFrames++
		c.mu.Unlock()
		return sample, nil
	}
}

func (c *NativeCapture) Stats() CaptureStats {
	if c == nil {
		return CaptureStats{RuntimeError: "capture handle is closed"}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	stats := c.stats
	if c.runtime != nil {
		stats.RuntimeError = c.runtime.Error()
	}
	return stats
}

func (c *NativeCapture) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	if c.closed {
		done := c.done
		started := c.started
		c.mu.Unlock()
		if started {
			return waitCaptureDone(done)
		}
		return nil
	}
	c.closed = true
	cmd, started := c.cmd, c.started
	c.mu.Unlock()
	if started && cmd.Process != nil {
		_ = cmd.Process.Kill()
		return waitCaptureDone(c.done)
	}
	return nil
}

func (c *NativeCapture) RequestKeyframe() error {
	return errors.New("FFmpeg V4L2 adapter uses periodic IDR; runtime force-IDR is not supported by this adapter")
}

func (c *NativeCapture) readLoop() {
	defer close(c.done)
	defer close(c.frames)
	parser := newV4L2AccessUnitParser(c.width, c.height, c.fps)
	chunk := make([]byte, 64<<10)
	for {
		n, err := c.stdout.Read(chunk)
		if n > 0 {
			for _, sample := range parser.feed(chunk[:n]) {
				c.offer(sample)
			}
		}
		if err != nil {
			break
		}
	}
	for _, sample := range parser.flush() {
		c.offer(sample)
	}
	waitErr := c.cmd.Wait()
	c.mu.Lock()
	if waitErr != nil && !c.closed {
		message := strings.TrimSpace(c.stderr.String())
		if message != "" {
			c.runtime = fmt.Errorf("FFmpeg capture exited: %s", message)
		} else {
			c.runtime = fmt.Errorf("FFmpeg capture exited: %w", waitErr)
		}
	}
	if !c.started {
		c.runtime = errors.New("Linux V4L2 capture did not start")
	}
	c.mu.Unlock()
}

func (c *NativeCapture) offer(sample EncodedSample) {
	select {
	case c.frames <- sample:
	default:
		select {
		case <-c.frames:
		default:
		}
		c.mu.Lock()
		c.stats.DroppedFrames++
		c.stats.PacketQueueDrops++
		c.mu.Unlock()
		select {
		case c.frames <- sample:
		default:
		}
	}
	c.readyOne.Do(func() { close(c.ready) })
}

func (c *NativeCapture) startupError() error {
	err := c.runtimeError()
	if err == nil {
		return errors.New("start Linux V4L2 capture: stream ended before the first frame")
	}
	return fmt.Errorf("start Linux V4L2 capture: %w", err)
}

func (c *NativeCapture) runtimeError() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.runtime != nil {
		return c.runtime
	}
	if c.closed {
		return errors.New("capture stream closed")
	}
	return errors.New("capture stream ended")
}

func waitCaptureDone(done <-chan struct{}) error {
	timer := time.NewTimer(linuxCaptureStopTimeout)
	defer timer.Stop()
	select {
	case <-done:
		return nil
	case <-timer.C:
		return errors.New("stop Linux V4L2 capture: timeout")
	}
}

type v4l2AccessUnitParser struct {
	buffer  []byte
	current [][]byte
	sps     []byte
	pps     []byte
	width   int
	height  int
	fps     FrameRate
	origin  time.Time
}

func newV4L2AccessUnitParser(width, height int, fps FrameRate) *v4l2AccessUnitParser {
	return &v4l2AccessUnitParser{width: width, height: height, fps: fps, origin: time.Now()}
}

func (p *v4l2AccessUnitParser) feed(data []byte) []EncodedSample {
	p.buffer = append(p.buffer, data...)
	var samples []EncodedSample
	for {
		nal, ok := p.nextNAL()
		if !ok {
			break
		}
		samples = append(samples, p.addNAL(nal)...)
	}
	return samples
}

func (p *v4l2AccessUnitParser) flush() []EncodedSample {
	var samples []EncodedSample
	if start, codeLen, ok := v4l2StartCode(p.buffer, 0); ok {
		payload := trimV4L2TrailingZeros(p.buffer[start+codeLen:])
		if len(payload) > 0 {
			samples = append(samples, p.addNAL(payload)...)
		}
	}
	p.buffer = nil
	if sample, ok := p.makeSample(); ok {
		samples = append(samples, sample)
	}
	p.current = nil
	return samples
}

func (p *v4l2AccessUnitParser) nextNAL() ([]byte, bool) {
	start, codeLen, ok := v4l2StartCode(p.buffer, 0)
	if !ok {
		if len(p.buffer) > 3 {
			p.buffer = append([]byte(nil), p.buffer[len(p.buffer)-3:]...)
		}
		return nil, false
	}
	if start > 0 {
		p.buffer = p.buffer[start:]
		_, codeLen, _ = v4l2StartCode(p.buffer, 0)
	}
	next, _, ok := v4l2StartCode(p.buffer, codeLen)
	if !ok {
		return nil, false
	}
	payload := trimV4L2TrailingZeros(p.buffer[codeLen:next])
	p.buffer = p.buffer[next:]
	if len(payload) == 0 {
		return p.nextNAL()
	}
	return append([]byte(nil), payload...), true
}

func (p *v4l2AccessUnitParser) addNAL(nal []byte) []EncodedSample {
	if len(nal) == 0 {
		return nil
	}
	if nal[0]&0x1f == 9 && len(p.current) > 0 {
		var samples []EncodedSample
		if sample, ok := p.makeSample(); ok {
			samples = append(samples, sample)
		}
		p.current = nil
		p.current = append(p.current, append([]byte(nil), nal...))
		return samples
	}
	p.current = append(p.current, append([]byte(nil), nal...))
	switch nal[0] & 0x1f {
	case 7:
		p.sps = append(p.sps[:0], nal...)
	case 8:
		p.pps = append(p.pps[:0], nal...)
	}
	return nil
}

func (p *v4l2AccessUnitParser) makeSample() (EncodedSample, bool) {
	var avcc []byte
	keyframe := false
	hasVCL := false
	for _, nal := range p.current {
		if len(nal) == 0 {
			continue
		}
		typ := nal[0] & 0x1f
		if typ == 1 || typ == 5 {
			hasVCL = true
		}
		if typ == 5 {
			keyframe = true
		}
		length := len(nal)
		avcc = append(avcc, byte(length>>24), byte(length>>16), byte(length>>8), byte(length))
		avcc = append(avcc, nal...)
	}
	if !hasVCL || len(avcc) == 0 {
		return EncodedSample{}, false
	}
	return EncodedSample{
		CaptureMonoNS: time.Since(p.origin).Nanoseconds(),
		AVCC:          avcc,
		SPS:           append([]byte(nil), p.sps...),
		PPS:           append([]byte(nil), p.pps...),
		NALLengthSize: 4,
		Keyframe:      keyframe,
		Width:         p.width,
		Height:        p.height,
	}, true
}

func v4l2StartCode(data []byte, from int) (int, int, bool) {
	for i := from; i+3 <= len(data); i++ {
		if data[i] != 0 || data[i+1] != 0 {
			continue
		}
		if data[i+2] == 1 {
			return i, 3, true
		}
		if data[i+2] == 0 && i+3 < len(data) && data[i+3] == 1 {
			return i, 4, true
		}
	}
	return 0, 0, false
}

func trimV4L2TrailingZeros(data []byte) []byte {
	for len(data) > 0 && data[len(data)-1] == 0 {
		data = data[:len(data)-1]
	}
	return data
}

var _ CaptureSource = (*NativeCapture)(nil)
