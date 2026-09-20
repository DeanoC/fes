package remotemedia

import (
	"context"
	"errors"
	"sync"
	"time"
)

const (
	defaultAudioPlayoutBufferFrames = 32
	maxAudioDriftCorrectionSamples  = 1
)

// AudioPlayoutConfig is platform-neutral policy for bounded PCM playout.
// FrameDuration is derived from the PCM format and must remain five
// milliseconds for the initial 48 kHz, 240-sample contract.
type AudioPlayoutConfig struct {
	Format            AudioFormat
	MaxBufferedFrames int
	StartupPrebuffer  int
	FrameDuration     time.Duration
}

type audioPlayout struct {
	config AudioPlayoutConfig

	mu           sync.Mutex
	frames       map[uint16]AudioFrame
	originSet    bool
	expected     uint16
	expectedRTP  uint32
	expectedMono int64
	stopped      bool
	runCancel    context.CancelFunc
	runDone      chan struct{}
	cleanupErr   error
	stats        AudioPlayoutStats
	notify       chan struct{}
	now          func() time.Time
	waitUntil    func(context.Context, time.Time) error
}

func NewAudioPlayout(config AudioPlayoutConfig) (AudioPlayout, error) {
	if err := ValidateAudioFormat(config.Format); err != nil {
		return nil, err
	}
	if config.Format.FrameSamples != DefaultAudioFrameSamples {
		return nil, errors.New("audio playout frame samples must match the transport contract")
	}
	if config.MaxBufferedFrames <= 0 {
		config.MaxBufferedFrames = defaultAudioPlayoutBufferFrames
	}
	if config.StartupPrebuffer < 0 || config.StartupPrebuffer > config.MaxBufferedFrames {
		return nil, errors.New("audio playout startup prebuffer is invalid")
	}
	frameDuration := time.Duration(config.Format.FrameSamples) * time.Second / time.Duration(config.Format.SampleRate)
	if config.FrameDuration != 0 && config.FrameDuration != frameDuration {
		return nil, errors.New("audio playout cadence must match the audio format")
	}
	config.FrameDuration = frameDuration
	return &audioPlayout{config: config, frames: make(map[uint16]AudioFrame), notify: make(chan struct{}, 1), now: time.Now, waitUntil: waitAudioPlayoutUntil}, nil
}

func (p *audioPlayout) Push(ctx context.Context, frame AudioFrame) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ValidateAudioFrame(frame); err != nil {
		return err
	}
	if frame.Format != p.config.Format {
		return errors.New("audio playout frame format mismatch")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped {
		return errors.New("audio playout is stopped")
	}
	if p.originSet && int16(frame.Sequence-p.expected) < 0 {
		p.stats.DroppedFrames += uint64(frame.Frames)
		return nil
	}
	if _, exists := p.frames[frame.Sequence]; exists {
		p.stats.DroppedFrames += uint64(frame.Frames)
		return nil
	}
	if len(p.frames) >= p.config.MaxBufferedFrames {
		p.stats.DroppedFrames += uint64(frame.Frames)
		p.stats.Overruns++
		return nil
	}
	p.frames[frame.Sequence] = frame.Clone()
	p.stats.PushedFrames += uint64(frame.Frames)
	p.stats.QueueDepth = len(p.frames)
	if p.stats.QueueDepth > p.stats.QueueHighWater {
		p.stats.QueueHighWater = p.stats.QueueDepth
	}
	p.signalLocked()
	return nil
}

func (p *audioPlayout) Run(ctx context.Context, sink AudioSink) (err error) {
	if sink == nil {
		return errors.New("audio playout sink is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	runCtx, cancel := context.WithCancel(ctx)
	p.mu.Lock()
	if p.stopped || p.runCancel != nil {
		p.mu.Unlock()
		cancel()
		return errors.New("audio playout is stopped or already running")
	}
	p.runCancel = cancel
	p.runDone = make(chan struct{})
	p.cleanupErr = nil
	p.mu.Unlock()
	defer func() {
		cancel()
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Second)
		defer cleanupCancel()
		cleanupErr := errors.Join(sink.Mute(cleanupCtx), sink.Drain(cleanupCtx), sink.Reset(cleanupCtx), sink.Close(cleanupCtx))
		p.mu.Lock()
		p.runCancel = nil
		p.cleanupErr = cleanupErr
		close(p.runDone)
		p.mu.Unlock()
		if cleanupErr != nil {
			err = errors.Join(err, cleanupErr)
		}
	}()
	if err := sink.Open(runCtx, AudioSinkConfig{Format: p.config.Format, Capabilities: []AudioSinkCapability{AudioSinkCapabilityPCM16}}); err != nil {
		return err
	}
	if err := sink.Ready(runCtx); err != nil {
		return err
	}
	if err := p.waitStartup(runCtx); err != nil {
		return err
	}
	deadline := p.now()
	for {
		frame, gap, correction := p.nextFrame()
		if err := sink.Write(runCtx, frame); err != nil {
			return err
		}
		p.mu.Lock()
		p.stats.PlayedFrames += uint64(frame.Frames)
		if gap {
			if len(p.frames) == 0 {
				p.stats.Underruns++
			} else {
				p.stats.SequenceGaps++
			}
		}
		p.mu.Unlock()
		deadline = deadline.Add(p.waitDuration(correction))
		if err := p.waitUntil(runCtx, deadline); err != nil {
			return err
		}
	}
}

func (p *audioPlayout) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	p.mu.Lock()
	p.stopped = true
	cancel := p.runCancel
	done := p.runDone
	p.signalLocked()
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.cleanupErr
	}
}

func (p *audioPlayout) Stats() AudioPlayoutStats {
	p.mu.Lock()
	defer p.mu.Unlock()
	stats := p.stats
	stats.QueueDepth = len(p.frames)
	return stats
}

func (p *audioPlayout) waitStartup(ctx context.Context) error {
	for {
		p.mu.Lock()
		// Zero prebuffer may begin with local silence, but it must not establish
		// a network sequence/timestamp origin until a packet actually arrives.
		ready := p.config.StartupPrebuffer == 0 || (len(p.frames) > 0 && len(p.frames) >= p.config.StartupPrebuffer)
		if ready && len(p.frames) > 0 && !p.originSet {
			p.selectOriginLocked()
		}
		stopped := p.stopped
		p.mu.Unlock()
		if ready {
			return nil
		}
		if stopped {
			return errors.New("audio playout is stopped")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-p.notify:
		}
	}
}

func (p *audioPlayout) selectOriginLocked() {
	var origin uint16
	first := true
	for sequence := range p.frames {
		if first || int16(sequence-origin) < 0 {
			origin = sequence
			first = false
		}
	}
	frame := p.frames[origin]
	p.expected = origin
	p.expectedRTP = frame.Timestamp
	p.expectedMono = frame.ReceiverMonoNS
	p.originSet = true
}

func (p *audioPlayout) nextFrame() (AudioFrame, bool, int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.originSet {
		if len(p.frames) == 0 {
			return AudioFrame{PCM16: make([]byte, p.config.Format.FrameSamples*p.config.Format.Channels*2), Frames: p.config.Format.FrameSamples, Format: p.config.Format}, true, 0
		}
		p.selectOriginLocked()
	}
	sequence := p.expected
	p.expected++
	frame, ok := p.frames[sequence]
	if !ok {
		out := AudioFrame{Sequence: sequence, Timestamp: p.expectedRTP, ReceiverMonoNS: p.expectedMono, PCM16: make([]byte, p.config.Format.FrameSamples*p.config.Format.Channels*2), Frames: p.config.Format.FrameSamples, Format: p.config.Format}
		p.expectedRTP += uint32(p.config.Format.FrameSamples)
		p.expectedMono += p.config.FrameDuration.Nanoseconds()
		return out, true, 0
	}
	delete(p.frames, sequence)
	p.stats.QueueDepth = len(p.frames)
	// Small bounded correction combines the packet's RTP cadence and local
	// receive cadence. It never moves the playout clock more than one sample
	// per frame and silence always uses the prior RTP base.
	rtpError := int32(frame.Timestamp - p.expectedRTP)
	arrivalError := samplesForMonoDelta(frame.ReceiverMonoNS - p.expectedMono)
	correction := clampAudioDrift((rtpError + arrivalError) / 2)
	p.expectedRTP += uint32(int32(p.config.Format.FrameSamples) + correction)
	p.expectedMono += p.config.FrameDuration.Nanoseconds()
	return frame.Clone(), false, correction
}

func (p *audioPlayout) waitDuration(correction int32) time.Duration {
	delay := p.config.FrameDuration + time.Duration(correction)*time.Second/RTPAudioClockRate
	if delay < time.Nanosecond {
		return time.Nanosecond
	}
	return delay
}

func waitAudioPlayoutUntil(ctx context.Context, deadline time.Time) error {
	duration := time.Until(deadline)
	if duration <= 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func samplesForMonoDelta(delta int64) int32 {
	if delta == 0 {
		return 0
	}
	if delta > 0 {
		return int32((uint64(delta) * RTPAudioClockRate) / 1_000_000_000)
	}
	return -int32((uint64(-delta) * RTPAudioClockRate) / 1_000_000_000)
}

func clampAudioDrift(value int32) int32 {
	if value > maxAudioDriftCorrectionSamples {
		return maxAudioDriftCorrectionSamples
	}
	if value < -maxAudioDriftCorrectionSamples {
		return -maxAudioDriftCorrectionSamples
	}
	return value
}

func (p *audioPlayout) signalLocked() {
	select {
	case p.notify <- struct{}{}:
	default:
	}
}
