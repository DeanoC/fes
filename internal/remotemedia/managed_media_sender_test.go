package remotemedia

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast-POC/internal/mediasession"
)

type managedMediaTestComponent struct {
	mu       sync.Mutex
	starts   int
	stops    int
	startErr error
	stopErrs []error
	done     chan struct{}
	name     string
	events   *managedMediaEventLog
}

type managedMediaEventLog struct {
	mu     sync.Mutex
	events []string
}

func (l *managedMediaEventLog) add(event string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.events = append(l.events, event)
	l.mu.Unlock()
}

func (c *managedMediaTestComponent) Start(context.Context, string) (mediasession.ComponentHandle, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.starts++
	if c.events != nil {
		c.events.add(c.name + ":start")
	}
	if c.startErr != nil {
		return nil, c.startErr
	}
	return &managedMediaTestHandle{component: c, done: c.done}, nil
}

func (c *managedMediaTestComponent) counts() (int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.starts, c.stops
}

type managedMediaTestHandle struct {
	component *managedMediaTestComponent
	done      <-chan struct{}
}

func (h *managedMediaTestHandle) Stop(context.Context) error {
	h.component.mu.Lock()
	defer h.component.mu.Unlock()
	h.component.stops++
	if h.component.events != nil {
		h.component.events.add(h.component.name + ":stop")
	}
	if len(h.component.stopErrs) == 0 {
		return nil
	}
	err := h.component.stopErrs[0]
	h.component.stopErrs = h.component.stopErrs[1:]
	return err
}

func (h *managedMediaTestHandle) Done() <-chan struct{} { return h.done }

func TestManagedMediaSenderStartsVideoThenAudioAndStopsAudioThenVideo(t *testing.T) {
	events := &managedMediaEventLog{}
	video := &managedMediaTestComponent{name: "video", events: events}
	audio := &managedMediaTestComponent{name: "audio", events: events}
	media := newManagedMediaSenderForComponents(video, audio, 50*time.Millisecond)
	handle, err := media.Start(context.Background(), "game")
	if err != nil {
		t.Fatal(err)
	}
	if starts, _ := video.counts(); starts != 1 {
		t.Fatalf("video starts = %d, want 1", starts)
	}
	if starts, _ := audio.counts(); starts != 1 {
		t.Fatalf("audio starts = %d, want 1", starts)
	}
	if err := handle.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, stops := audio.counts(); stops != 1 {
		t.Fatalf("audio stops = %d, want 1", stops)
	}
	if _, stops := video.counts(); stops != 1 {
		t.Fatalf("video stops = %d, want 1", stops)
	}
	events.mu.Lock()
	got := append([]string(nil), events.events...)
	events.mu.Unlock()
	want := []string{"video:start", "audio:start", "audio:stop", "video:stop"}
	if len(got) != len(want) {
		t.Fatalf("lifecycle events = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("lifecycle events = %v, want %v", got, want)
		}
	}
	if err := handle.Stop(context.Background()); err != nil {
		t.Fatalf("idempotent Stop: %v", err)
	}
	if _, stops := audio.counts(); stops != 1 {
		t.Fatalf("audio stops after idempotent Stop = %d, want 1", stops)
	}
	if _, stops := video.counts(); stops != 1 {
		t.Fatalf("video stops after idempotent Stop = %d, want 1", stops)
	}
}

func TestManagedMediaSenderRollsBackVideoWhenAudioStartFails(t *testing.T) {
	video := &managedMediaTestComponent{}
	audio := &managedMediaTestComponent{startErr: errors.New("private audio start detail")}
	media := newManagedMediaSenderForComponents(video, audio, 50*time.Millisecond)
	handle, err := media.Start(context.Background(), "game")
	if !errors.Is(err, ErrManagedMediaSenderStart) {
		t.Fatalf("Start error = %v", err)
	}
	if handle != nil {
		t.Fatal("fully cleaned startup failure returned a handle")
	}
	if _, stops := video.counts(); stops != 1 {
		t.Fatalf("video rollback stops = %d, want 1", stops)
	}
}

func TestManagedMediaSenderReturnsRetryablePartialHandle(t *testing.T) {
	video := &managedMediaTestComponent{stopErrs: []error{errors.New("close once")}}
	audio := &managedMediaTestComponent{startErr: errors.New("audio start")}
	media := newManagedMediaSenderForComponents(video, audio, 50*time.Millisecond)
	handle, err := media.Start(context.Background(), "game")
	if !errors.Is(err, ErrManagedMediaSenderStart) || handle == nil {
		t.Fatalf("Start = %T %v, want retryable partial", handle, err)
	}
	if err := handle.Stop(context.Background()); err != nil {
		t.Fatalf("retry Stop: %v", err)
	}
	if _, stops := video.counts(); stops != 2 {
		t.Fatalf("video stops = %d, want 2", stops)
	}
}

func TestManagedMediaSenderDoneWhenEitherWorkerEndsAndReapsSibling(t *testing.T) {
	videoDone := make(chan struct{})
	video := &managedMediaTestComponent{done: videoDone}
	audio := &managedMediaTestComponent{}
	media := newManagedMediaSenderForComponents(video, audio, 50*time.Millisecond)
	handle, err := media.Start(context.Background(), "game")
	if err != nil {
		t.Fatal(err)
	}
	terminal, ok := handle.(interface{ Done() <-chan struct{} })
	if !ok {
		t.Fatal("combined handle has no Done")
	}
	close(videoDone)
	select {
	case <-terminal.Done():
	case <-time.After(time.Second):
		t.Fatal("combined Done did not close")
	}
	deadline := time.Now().Add(time.Second)
	for {
		_, stops := audio.counts()
		if stops == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("audio sibling was not reaped")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestManagedMediaSenderAudioTerminalReapsVideo(t *testing.T) {
	audioDone := make(chan struct{})
	video := &managedMediaTestComponent{}
	audio := &managedMediaTestComponent{done: audioDone}
	media := newManagedMediaSenderForComponents(video, audio, 50*time.Millisecond)
	handle, err := media.Start(context.Background(), "game")
	if err != nil {
		t.Fatal(err)
	}
	terminal := handle.(interface{ Done() <-chan struct{} })
	close(audioDone)
	select {
	case <-terminal.Done():
	case <-time.After(time.Second):
		t.Fatal("audio terminal state did not close combined Done")
	}
	deadline := time.Now().Add(time.Second)
	for {
		_, stops := video.counts()
		if stops == 1 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("video sibling was not reaped")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestManagedMediaSenderRejectsMismatchedWorkerIdentity(t *testing.T) {
	format := AudioFormat{SampleRate: AudioSampleRate, Channels: 1, Encoding: AudioEncodingPCM16LE, FrameSamples: DefaultAudioFrameSamples}
	video, err := NewManagedSender(ManagedSenderConfig{
		Session: "session", Generation: 4, Token: "token", RTPAddress: "127.0.0.1:5000", ControlAddress: "127.0.0.1:5001", SSRC: 7,
	}, &managedSenderFakeSource{})
	if err != nil {
		t.Fatal(err)
	}
	audio, err := NewManagedAudioSender(AudioSenderConfig{
		RTPAddress: "127.0.0.1:5002", ControlAddress: "127.0.0.1:5003", Session: "other", Generation: 4, Token: "token", SSRC: 8,
		SampleRate: format.SampleRate, Channels: format.Channels, FrameSamples: format.FrameSamples, FormatCapabilityVersion: 1,
	}, &testAudioSource{format: format})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewManagedMediaSender(video, audio); !errors.Is(err, ErrManagedMediaSenderStart) {
		t.Fatalf("mismatched identity accepted: %v", err)
	}
}

func TestManagedMediaSenderRejectsEndpointCollision(t *testing.T) {
	format := AudioFormat{SampleRate: AudioSampleRate, Channels: 1, Encoding: AudioEncodingPCM16LE, FrameSamples: DefaultAudioFrameSamples}
	video, err := NewManagedSender(ManagedSenderConfig{
		Session: "session", Generation: 4, Token: "token", RTPAddress: "127.0.0.1:5000", ControlAddress: "127.0.0.1:5001", SSRC: 7,
	}, &managedSenderFakeSource{})
	if err != nil {
		t.Fatal(err)
	}
	audio, err := NewManagedAudioSender(AudioSenderConfig{
		RTPAddress: "127.0.0.1:5000", ControlAddress: "127.0.0.1:5003", Session: "session", Generation: 4, Token: "token", SSRC: 8,
		SampleRate: format.SampleRate, Channels: format.Channels, FrameSamples: format.FrameSamples, FormatCapabilityVersion: 1,
	}, &testAudioSource{format: format})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewManagedMediaSender(video, audio); !errors.Is(err, ErrManagedMediaSenderStart) {
		t.Fatalf("endpoint collision accepted: %v", err)
	}
}

func TestManagedMediaSenderRejectsCanonicalEndpointAliasCollision(t *testing.T) {
	format := AudioFormat{SampleRate: AudioSampleRate, Channels: 1, Encoding: AudioEncodingPCM16LE, FrameSamples: DefaultAudioFrameSamples}
	video, err := NewManagedSender(ManagedSenderConfig{
		Session: "session", Generation: 4, Token: "token", RTPAddress: "127.0.0.1:5000", ControlAddress: "127.0.0.1:5001", SSRC: 7,
	}, &managedSenderFakeSource{})
	if err != nil {
		t.Fatal(err)
	}
	audio, err := NewManagedAudioSender(AudioSenderConfig{
		RTPAddress: "[::ffff:127.0.0.1]:05000", ControlAddress: "127.0.0.1:5003", Session: "session", Generation: 4, Token: "token", SSRC: 8,
		SampleRate: format.SampleRate, Channels: format.Channels, FrameSamples: format.FrameSamples, FormatCapabilityVersion: 1,
	}, &testAudioSource{format: format})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewManagedMediaSender(video, audio); !errors.Is(err, ErrManagedMediaSenderStart) {
		t.Fatalf("canonical endpoint alias collision accepted: %v", err)
	}
}

type managedMediaFlakyCapture struct {
	mu       sync.Mutex
	closes   int
	failOnce bool
}

func (*managedMediaFlakyCapture) Start() error { return nil }
func (*managedMediaFlakyCapture) Next(context.Context) (EncodedSample, error) {
	return EncodedSample{}, context.Canceled
}
func (*managedMediaFlakyCapture) Stats() CaptureStats { return CaptureStats{} }
func (s *managedMediaFlakyCapture) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closes++
	if s.failOnce {
		s.failOnce = false
		return errors.New("capture close failed")
	}
	return nil
}

func (s *managedMediaFlakyCapture) closeCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closes
}

func TestManagedMediaSenderCleansUnstartedAudioWithVideoFailureAndRetry(t *testing.T) {
	format := AudioFormat{SampleRate: AudioSampleRate, Channels: 1, Encoding: AudioEncodingPCM16LE, FrameSamples: DefaultAudioFrameSamples}
	capture := &managedMediaFlakyCapture{failOnce: true}
	audioSource := &testAudioSource{format: format}
	video, err := NewManagedSender(ManagedSenderConfig{
		Session: "session", Generation: 4, Token: "token", RTPAddress: "127.0.0.1:5000", SSRC: 7,
	}, capture, WithManagedSenderFactory(func(SenderConfig, CaptureSource) (ManagedSenderRunner, error) {
		return nil, errors.New("video startup failed")
	}))
	if err != nil {
		t.Fatal(err)
	}
	audio, err := NewManagedAudioSender(AudioSenderConfig{
		RTPAddress: "127.0.0.1:5002", ControlAddress: "127.0.0.1:5003", Session: "session", Generation: 4, Token: "token", SSRC: 8,
		SampleRate: format.SampleRate, Channels: format.Channels, FrameSamples: format.FrameSamples, FormatCapabilityVersion: 1,
	}, audioSource)
	if err != nil {
		t.Fatal(err)
	}
	media, err := NewManagedMediaSender(video, audio)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := media.Start(context.Background(), "game")
	if !errors.Is(err, ErrManagedMediaSenderStart) || handle == nil {
		t.Fatalf("Start = %T %v, want retryable cleanup", handle, err)
	}
	if capture.closeCount() != 1 {
		t.Fatalf("initial capture closes = %d, want 1", capture.closeCount())
	}
	audioSource.mu.Lock()
	initialAudioCloses := audioSource.closes
	audioSource.mu.Unlock()
	if initialAudioCloses != 1 {
		t.Fatalf("initial audio closes = %d, want 1", initialAudioCloses)
	}
	if err := handle.Stop(context.Background()); err != nil {
		t.Fatalf("retry cleanup: %v", err)
	}
	if capture.closeCount() != 2 {
		t.Fatalf("capture closes after retry = %d, want 2", capture.closeCount())
	}
	audioSource.mu.Lock()
	defer audioSource.mu.Unlock()
	if audioSource.closes != 1 {
		t.Fatalf("audio closes after retry = %d, want 1", audioSource.closes)
	}
}

func TestManagedMediaSenderTreatsLegacyPartialRunnerAsCallerOwnedOnError(t *testing.T) {
	format := AudioFormat{SampleRate: AudioSampleRate, Channels: 1, Encoding: AudioEncodingPCM16LE, FrameSamples: DefaultAudioFrameSamples}
	capture := &managedMediaFlakyCapture{}
	audioSource := &testAudioSource{format: format}
	video, err := NewManagedSender(ManagedSenderConfig{
		Session: "session", Generation: 4, Token: "token", RTPAddress: "127.0.0.1:5000", SSRC: 7,
	}, capture, WithManagedSenderFactory(func(SenderConfig, CaptureSource) (ManagedSenderRunner, error) {
		return &managedMediaTestRunner{}, errors.New("partial video construction")
	}))
	if err != nil {
		t.Fatal(err)
	}
	audio, err := NewManagedAudioSender(AudioSenderConfig{
		RTPAddress: "127.0.0.1:5002", ControlAddress: "127.0.0.1:5003", Session: "session", Generation: 4, Token: "token", SSRC: 8,
		SampleRate: format.SampleRate, Channels: format.Channels, FrameSamples: format.FrameSamples, FormatCapabilityVersion: 1,
	}, audioSource)
	if err != nil {
		t.Fatal(err)
	}
	media, err := NewManagedMediaSender(video, audio)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := media.Start(context.Background(), "game")
	if !errors.Is(err, ErrManagedMediaSenderStart) || handle != nil {
		t.Fatalf("Start = %T %v, want fully cleaned failure", handle, err)
	}
	if capture.closeCount() != 1 {
		t.Fatalf("capture closes = %d, want 1", capture.closeCount())
	}
	audioSource.mu.Lock()
	closes := audioSource.closes
	audioSource.mu.Unlock()
	if closes != 1 {
		t.Fatalf("audio source closes = %d, want 1", closes)
	}
}

func TestManagedMediaSenderHonorsExplicitPartialRunnerOwnership(t *testing.T) {
	capture := &managedMediaFlakyCapture{}
	video, err := NewManagedSender(ManagedSenderConfig{
		Session: "session", Generation: 4, Token: "token", RTPAddress: "127.0.0.1:5000", SSRC: 7,
	}, capture, WithManagedSenderFactoryOwnership(func(_ SenderConfig, source CaptureSource) (ManagedSenderRunner, bool, error) {
		return &managedSourceClosingRunner{source: source}, true, errors.New("partial video construction")
	}))
	if err != nil {
		t.Fatal(err)
	}
	media, err := NewManagedMediaSender(video, nil)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := media.Start(context.Background(), "game")
	if !errors.Is(err, ErrManagedMediaSenderStart) || handle != nil {
		t.Fatalf("Start = %T %v, want fully cleaned failure", handle, err)
	}
	if capture.closeCount() != 1 {
		t.Fatalf("explicitly owned capture closes = %d, want 1", capture.closeCount())
	}
}

type managedSourceClosingRunner struct{ source interface{ Close() error } }

func (*managedSourceClosingRunner) Run(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }
func (r *managedSourceClosingRunner) Close() error                { return r.source.Close() }

func TestManagedAudioSenderPassesTheConfiguredAudioWorkerIdentity(t *testing.T) {
	format := AudioFormat{SampleRate: AudioSampleRate, Channels: 1, Encoding: AudioEncodingPCM16LE, FrameSamples: DefaultAudioFrameSamples}
	source := &testAudioSource{format: format}
	var got AudioSenderConfig
	sender, err := NewManagedAudioSender(AudioSenderConfig{
		RTPAddress: "127.0.0.1:5004", ControlAddress: "127.0.0.1:5005", Session: "session", Generation: 4, Token: "token", SSRC: 9,
		SampleRate: format.SampleRate, Channels: format.Channels, FrameSamples: format.FrameSamples, FormatCapabilityVersion: 1,
	}, source, WithManagedAudioSenderFactory(func(config AudioSenderConfig, source AudioSource) (ManagedAudioSenderRunner, error) {
		got = config
		return &managedAudioTestRunner{source: source}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	handle, err := sender.Start(context.Background(), "game")
	if err != nil {
		t.Fatal(err)
	}
	if got.Session != "session" || got.Generation != 4 || got.Token != "token" || got.SSRC != 9 || got.RTPAddress == got.ControlAddress {
		t.Fatalf("audio config = %#v", got)
	}
	if err := handle.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

type managedOrderedAudioSource struct {
	format AudioFormat
	events *managedMediaEventLog
}

func (*managedOrderedAudioSource) Start() error { return nil }
func (*managedOrderedAudioSource) Next(context.Context) (AudioSample, error) {
	return AudioSample{}, context.Canceled
}
func (s *managedOrderedAudioSource) Stats() AudioStats { return AudioStats{SourceFormat: s.format} }
func (s *managedOrderedAudioSource) Close() error {
	s.events.add("audio-source:close")
	return nil
}

func TestManagedAudioSenderLegacyFactoryFailureStopsRunnerBeforeSource(t *testing.T) {
	format := AudioFormat{SampleRate: AudioSampleRate, Channels: 1, Encoding: AudioEncodingPCM16LE, FrameSamples: DefaultAudioFrameSamples}
	events := &managedMediaEventLog{}
	component, err := NewManagedAudioSender(AudioSenderConfig{
		RTPAddress: "127.0.0.1:5004", ControlAddress: "127.0.0.1:5005", Session: "session", Generation: 4, Token: "token", SSRC: 9,
		SampleRate: format.SampleRate, Channels: format.Channels, FrameSamples: format.FrameSamples, FormatCapabilityVersion: 1,
	}, &managedOrderedAudioSource{format: format, events: events}, WithManagedAudioSenderFactory(func(AudioSenderConfig, AudioSource) (ManagedAudioSenderRunner, error) {
		return &managedOrderedRunner{events: events}, errors.New("partial construction")
	}))
	if err != nil {
		t.Fatal(err)
	}
	if handle, startErr := component.Start(context.Background(), "game"); startErr == nil || handle != nil {
		t.Fatalf("Start = %v, %v; want fully cleaned failure", handle, startErr)
	}
	events.mu.Lock()
	got := append([]string(nil), events.events...)
	events.mu.Unlock()
	want := []string{"runner:close", "audio-source:close"}
	if len(got) != len(want) {
		t.Fatalf("cleanup events = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("cleanup events = %v, want %v", got, want)
		}
	}
}

func TestManagedAudioSenderOwnershipFactoryNormalizesMalformedResults(t *testing.T) {
	format := AudioFormat{SampleRate: AudioSampleRate, Channels: 1, Encoding: AudioEncodingPCM16LE, FrameSamples: DefaultAudioFrameSamples}
	for name, factory := range map[string]managedAudioSenderFactoryWithOwnership{
		"nil runner claims ownership": func(AudioSenderConfig, AudioSource) (ManagedAudioSenderRunner, bool, error) {
			return nil, true, errors.New("construction failed")
		},
		"successful runner declines ownership": func(AudioSenderConfig, AudioSource) (ManagedAudioSenderRunner, bool, error) {
			return &managedOrderedRunner{}, false, nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			source := &testAudioSource{format: format}
			component, err := NewManagedAudioSender(AudioSenderConfig{
				RTPAddress: "127.0.0.1:5004", ControlAddress: "127.0.0.1:5005", Session: "session", Generation: 4, Token: "token", SSRC: 9,
				SampleRate: format.SampleRate, Channels: format.Channels, FrameSamples: format.FrameSamples, FormatCapabilityVersion: 1,
			}, source, WithManagedAudioSenderFactoryOwnership(factory))
			if err != nil {
				t.Fatal(err)
			}
			handle, startErr := component.Start(context.Background(), "game")
			if startErr == nil || !errors.Is(startErr, ErrManagedAudioSenderStart) {
				t.Fatalf("Start = %v, %v; want managed startup failure", handle, startErr)
			}
			if handle != nil {
				if err := handle.Stop(context.Background()); err != nil {
					t.Fatalf("partial cleanup: %v", err)
				}
			}
			source.mu.Lock()
			closes := source.closes
			source.mu.Unlock()
			if closes != 1 {
				t.Fatalf("source close count = %d, want 1", closes)
			}
		})
	}
}

func TestManagedMediaSenderDoesNotTerminateAHealthyManagedVideoWorker(t *testing.T) {
	source := &managedSenderFakeSource{}
	video, err := NewManagedSender(ManagedSenderConfig{Session: "session", Generation: 1, Token: "token", RTPAddress: "127.0.0.1:5000", SSRC: 7}, source,
		WithManagedSenderFactory(func(SenderConfig, CaptureSource) (ManagedSenderRunner, error) {
			return &managedMediaTestRunner{}, nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := NewManagedMediaSender(video, nil)
	if err != nil {
		t.Fatal(err)
	}
	started, err := handle.Start(context.Background(), "game")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started.(interface{ Done() <-chan struct{} }).Done():
		t.Fatal("healthy video worker terminated at startup")
	default:
	}
	if err := started.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

type managedAudioTestRunner struct{ source AudioSource }

func (r *managedAudioTestRunner) Run(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }
func (r *managedAudioTestRunner) Close() error                  { return r.source.Close() }

type managedMediaTestRunner struct{}

func (*managedMediaTestRunner) Run(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }
func (*managedMediaTestRunner) Close() error                  { return nil }
