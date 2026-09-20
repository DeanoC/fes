package remotemedia

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

var errTestSinkCleanup = errors.New("test sink cleanup")

type blockingCleanupAudioSink struct{ started chan struct{} }

func (s *blockingCleanupAudioSink) Open(context.Context, AudioSinkConfig) error { return nil }
func (s *blockingCleanupAudioSink) Ready(context.Context) error                 { return nil }
func (s *blockingCleanupAudioSink) Write(ctx context.Context, _ AudioFrame) error {
	select {
	case <-s.started:
	default:
		close(s.started)
	}
	<-ctx.Done()
	return ctx.Err()
}
func (s *blockingCleanupAudioSink) Drain(context.Context) error { return errTestSinkCleanup }
func (s *blockingCleanupAudioSink) Mute(context.Context) error  { return nil }
func (s *blockingCleanupAudioSink) Reset(context.Context) error { return nil }
func (s *blockingCleanupAudioSink) Stats() AudioSinkStats       { return AudioSinkStats{} }
func (s *blockingCleanupAudioSink) Close(context.Context) error { return nil }

type advancingAudioSink struct{ advance func() }

func (s *advancingAudioSink) Open(context.Context, AudioSinkConfig) error { return nil }
func (s *advancingAudioSink) Ready(context.Context) error                 { return nil }
func (s *advancingAudioSink) Write(context.Context, AudioFrame) error {
	s.advance()
	return nil
}
func (s *advancingAudioSink) Drain(context.Context) error { return nil }
func (s *advancingAudioSink) Mute(context.Context) error  { return nil }
func (s *advancingAudioSink) Reset(context.Context) error { return nil }
func (s *advancingAudioSink) Stats() AudioSinkStats       { return AudioSinkStats{} }
func (s *advancingAudioSink) Close(context.Context) error { return nil }

type recordingAudioSink struct {
	mu        sync.Mutex
	frames    []AudioFrame
	cancel    context.CancelFunc
	stopAfter int
	started   chan struct{}
}

func (s *recordingAudioSink) Open(context.Context, AudioSinkConfig) error { return nil }
func (s *recordingAudioSink) Ready(context.Context) error                 { return nil }
func (s *recordingAudioSink) Write(_ context.Context, frame AudioFrame) error {
	s.mu.Lock()
	s.frames = append(s.frames, frame.Clone())
	if s.started != nil {
		select {
		case <-s.started:
		default:
			close(s.started)
		}
	}
	if s.stopAfter > 0 && len(s.frames) >= s.stopAfter && s.cancel != nil {
		s.cancel()
	}
	s.mu.Unlock()
	return nil
}
func (s *recordingAudioSink) Drain(context.Context) error { return nil }
func (s *recordingAudioSink) Mute(context.Context) error  { return nil }
func (s *recordingAudioSink) Reset(context.Context) error { return nil }
func (s *recordingAudioSink) Stats() AudioSinkStats       { return AudioSinkStats{} }
func (s *recordingAudioSink) Close(context.Context) error { return nil }

func TestAudioPlayoutReordersAndConvertsSequenceGapToSilence(t *testing.T) {
	format := AudioFormat{SampleRate: AudioSampleRate, Channels: 1, Encoding: AudioEncodingPCM16LE, FrameSamples: DefaultAudioFrameSamples}
	playout, err := NewAudioPlayout(AudioPlayoutConfig{Format: format, MaxBufferedFrames: 4, StartupPrebuffer: 1})
	if err != nil {
		t.Fatalf("NewAudioPlayout: %v", err)
	}
	frame := func(sequence uint16, value byte) AudioFrame {
		pcm := make([]byte, format.FrameSamples*2)
		pcm[0] = value
		return AudioFrame{Sequence: sequence, Timestamp: uint32(sequence) * DefaultAudioFrameSamples, PCM16: pcm, Frames: format.FrameSamples, Format: format}
	}
	if err := playout.Push(context.Background(), frame(10, 10)); err != nil {
		t.Fatalf("push 10: %v", err)
	}
	if err := playout.Push(context.Background(), frame(12, 12)); err != nil {
		t.Fatalf("push 12: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	sink := &recordingAudioSink{cancel: cancel, stopAfter: 3}
	if err := playout.Run(ctx, sink); err == nil {
		t.Fatal("Run returned nil after context cancellation")
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.frames) != 3 || sink.frames[0].PCM16[0] != 10 || sink.frames[1].PCM16[0] != 0 || sink.frames[2].PCM16[0] != 12 {
		t.Fatalf("playout frames = %#v", sink.frames)
	}
	stats := playout.Stats()
	if stats.SequenceGaps != 1 || stats.Underruns != 0 || stats.Overruns != 0 {
		t.Fatalf("playout stats = %#v", stats)
	}
}

func TestAudioPlayoutOwnsStartupUnderrunMetric(t *testing.T) {
	format := AudioFormat{SampleRate: AudioSampleRate, Channels: 1, Encoding: AudioEncodingPCM16LE, FrameSamples: DefaultAudioFrameSamples}
	playout, err := NewAudioPlayout(AudioPlayoutConfig{Format: format, MaxBufferedFrames: 2})
	if err != nil {
		t.Fatalf("NewAudioPlayout: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	sink := &recordingAudioSink{cancel: cancel, stopAfter: 1}
	if err := playout.Run(ctx, sink); err == nil {
		t.Fatal("Run returned nil after context cancellation")
	}
	stats := playout.Stats()
	if stats.Underruns != 1 || stats.PlayedFrames != uint64(DefaultAudioFrameSamples) {
		t.Fatalf("playout stats = %#v", stats)
	}
}

func TestAudioPlayoutSelectsLowestSequenceDuringPrebufferAndPreservesRTPBaseForSilence(t *testing.T) {
	format := AudioFormat{SampleRate: AudioSampleRate, Channels: 1, Encoding: AudioEncodingPCM16LE, FrameSamples: DefaultAudioFrameSamples}
	playout, err := NewAudioPlayout(AudioPlayoutConfig{Format: format, MaxBufferedFrames: 4, StartupPrebuffer: 2})
	if err != nil {
		t.Fatalf("NewAudioPlayout: %v", err)
	}
	frame := func(sequence uint16, timestamp uint32, value byte) AudioFrame {
		pcm := make([]byte, format.FrameSamples*2)
		pcm[0] = value
		return AudioFrame{Sequence: sequence, Timestamp: timestamp, ReceiverMonoNS: int64(sequence) * 5_000_000, PCM16: pcm, Frames: format.FrameSamples, Format: format}
	}
	for _, value := range []AudioFrame{frame(12, 9480, 12), frame(10, 9000, 10), frame(11, 9240, 11)} {
		if err := playout.Push(context.Background(), value); err != nil {
			t.Fatalf("push: %v", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	sink := &recordingAudioSink{cancel: cancel, stopAfter: 4}
	if err := playout.Run(ctx, sink); err == nil {
		t.Fatal("Run returned nil after cancellation")
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.frames) != 4 || sink.frames[0].Sequence != 10 || sink.frames[1].Sequence != 11 || sink.frames[2].Sequence != 12 || sink.frames[3].Timestamp != 9720 {
		t.Fatalf("prebuffer order/timestamp = %#v", sink.frames)
	}
}

func TestAudioPlayoutZeroPrebufferWaitsForNetworkOrigin(t *testing.T) {
	format := AudioFormat{SampleRate: AudioSampleRate, Channels: 1, Encoding: AudioEncodingPCM16LE, FrameSamples: DefaultAudioFrameSamples}
	playout, err := NewAudioPlayout(AudioPlayoutConfig{Format: format, MaxBufferedFrames: 2})
	if err != nil {
		t.Fatalf("NewAudioPlayout: %v", err)
	}
	pcm := make([]byte, format.FrameSamples*2)
	pcm[0] = 1
	if err := playout.Push(context.Background(), AudioFrame{Sequence: 65535, Timestamp: 9000, PCM16: pcm, Frames: format.FrameSamples, Format: format}); err != nil {
		t.Fatalf("push high sequence: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	sink := &recordingAudioSink{cancel: cancel, stopAfter: 2}
	if err := playout.Run(ctx, sink); err == nil {
		t.Fatal("Run returned nil after cancellation")
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.frames) != 2 || sink.frames[0].Sequence != 65535 || sink.frames[0].Timestamp != 9000 || sink.frames[1].Sequence != 0 || sink.frames[1].Timestamp != 9240 {
		t.Fatalf("zero-prebuffer origin = %#v", sink.frames)
	}
}

func TestAudioPlayoutZeroPrebufferDoesNotAnchorBeforeFirstNetworkFrame(t *testing.T) {
	format := AudioFormat{SampleRate: AudioSampleRate, Channels: 1, Encoding: AudioEncodingPCM16LE, FrameSamples: DefaultAudioFrameSamples}
	playout, err := NewAudioPlayout(AudioPlayoutConfig{Format: format, MaxBufferedFrames: 2})
	if err != nil {
		t.Fatalf("NewAudioPlayout: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	sink := &recordingAudioSink{cancel: cancel, stopAfter: 2, started: make(chan struct{})}
	runDone := make(chan error, 1)
	go func() { runDone <- playout.Run(ctx, sink) }()
	<-sink.started
	pcm := make([]byte, format.FrameSamples*2)
	pcm[0] = 7
	if err := playout.Push(context.Background(), AudioFrame{Sequence: 65535, Timestamp: 9000, PCM16: pcm, Frames: format.FrameSamples, Format: format}); err != nil {
		t.Fatalf("push high sequence after local silence: %v", err)
	}
	if err := <-runDone; err == nil {
		t.Fatal("Run returned nil after cancellation")
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.frames) != 2 || sink.frames[0].Sequence != 0 || sink.frames[1].Sequence != 65535 || sink.frames[1].Timestamp != 9000 || sink.frames[1].PCM16[0] != 7 {
		t.Fatalf("unanchored local silence/network origin = %#v", sink.frames)
	}
}

func TestAudioPlayoutStopWaitsForAndReturnsSinkCleanupError(t *testing.T) {
	format := AudioFormat{SampleRate: AudioSampleRate, Channels: 1, Encoding: AudioEncodingPCM16LE, FrameSamples: DefaultAudioFrameSamples}
	playout, err := NewAudioPlayout(AudioPlayoutConfig{Format: format})
	if err != nil {
		t.Fatalf("NewAudioPlayout: %v", err)
	}
	sink := &blockingCleanupAudioSink{started: make(chan struct{})}
	runDone := make(chan error, 1)
	go func() { runDone <- playout.Run(context.Background(), sink) }()
	<-sink.started
	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := playout.Stop(stopCtx); !errors.Is(err, errTestSinkCleanup) {
		t.Fatalf("Stop error = %v", err)
	}
	if err := <-runDone; !errors.Is(err, errTestSinkCleanup) {
		t.Fatalf("Run error = %v", err)
	}
}

func TestAudioPlayoutAppliesBoundedPositiveAndNegativeDriftToActualWait(t *testing.T) {
	format := AudioFormat{SampleRate: AudioSampleRate, Channels: 1, Encoding: AudioEncodingPCM16LE, FrameSamples: DefaultAudioFrameSamples}
	for _, skew := range []int32{2, -2} {
		playout, err := NewAudioPlayout(AudioPlayoutConfig{Format: format, StartupPrebuffer: 2})
		if err != nil {
			t.Fatalf("NewAudioPlayout: %v", err)
		}
		inner := playout.(*audioPlayout)
		base := time.Unix(0, 0)
		now := base
		var deadlines []time.Time
		inner.now = func() time.Time { return now }
		inner.waitUntil = func(ctx context.Context, deadline time.Time) error {
			deadlines = append(deadlines, deadline)
			now = deadline
			if len(deadlines) == 2 {
				return context.Canceled
			}
			return nil
		}
		frame := func(sequence uint16, timestamp uint32, mono int64) AudioFrame {
			return AudioFrame{Sequence: sequence, Timestamp: timestamp, ReceiverMonoNS: mono, PCM16: make([]byte, format.FrameSamples*2), Frames: format.FrameSamples, Format: format}
		}
		if err := playout.Push(context.Background(), frame(10, 9000, 0)); err != nil {
			t.Fatal(err)
		}
		if err := playout.Push(context.Background(), frame(11, uint32(int64(9240)+int64(skew)), 5_000_000)); err != nil {
			t.Fatal(err)
		}
		sink := &recordingAudioSink{}
		err = playout.Run(context.Background(), sink)
		if !errors.Is(err, context.Canceled) || len(deadlines) != 2 {
			t.Fatalf("skew %d Run/deadlines = %v/%v", skew, err, deadlines)
		}
		nominal := 5 * time.Millisecond
		oneSample := time.Second / RTPAudioClockRate
		if !deadlines[0].Equal(base.Add(nominal)) || !deadlines[1].Equal(base.Add(2*nominal+time.Duration(skew/2)*oneSample)) {
			t.Fatalf("skew %d deadlines = %v, want %v then %v", skew, deadlines, base.Add(nominal), base.Add(2*nominal+time.Duration(skew/2)*oneSample))
		}
	}
}

func TestAudioPlayoutUsesAbsoluteDeadlineIncludingSinkWriteTime(t *testing.T) {
	format := AudioFormat{SampleRate: AudioSampleRate, Channels: 1, Encoding: AudioEncodingPCM16LE, FrameSamples: DefaultAudioFrameSamples}
	playout, err := NewAudioPlayout(AudioPlayoutConfig{Format: format, StartupPrebuffer: 1})
	if err != nil {
		t.Fatal(err)
	}
	frame := AudioFrame{Sequence: 1, Timestamp: 9000, PCM16: make([]byte, format.FrameSamples*2), Frames: format.FrameSamples, Format: format}
	if err := playout.Push(context.Background(), frame); err != nil {
		t.Fatal(err)
	}
	base := time.Unix(0, 0)
	now := base
	inner := playout.(*audioPlayout)
	inner.now = func() time.Time { return now }
	var remaining time.Duration
	inner.waitUntil = func(_ context.Context, deadline time.Time) error {
		remaining = deadline.Sub(now)
		return context.Canceled
	}
	sink := &advancingAudioSink{advance: func() { now = now.Add(2 * time.Millisecond) }}
	if err := playout.Run(context.Background(), sink); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run = %v", err)
	}
	if remaining != 3*time.Millisecond {
		t.Fatalf("remaining deadline wait = %s, want 3ms", remaining)
	}
}
