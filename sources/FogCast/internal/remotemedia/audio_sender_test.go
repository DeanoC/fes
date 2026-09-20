package remotemedia

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
)

type testAudioSource struct {
	format    AudioFormat
	mu        sync.Mutex
	closed    bool
	closes    int
	starts    int
	failClose bool
	failStart bool
}

func (s *testAudioSource) Start() error {
	s.mu.Lock()
	s.starts++
	s.mu.Unlock()
	if s.failStart {
		return errors.New("test start failure")
	}
	return nil
}
func (s *testAudioSource) Next(ctx context.Context) (AudioSample, error) {
	<-ctx.Done()
	return AudioSample{}, ctx.Err()
}
func (s *testAudioSource) Stats() AudioStats { return AudioStats{SourceFormat: s.format} }
func (s *testAudioSource) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closes++
	if s.failClose {
		s.failClose = false
		return errors.New("test close failure")
	}
	if s.closed {
		return nil
	}
	s.closed = true
	return nil
}

func TestAudioSenderWritesAuthenticatedAudioHello(t *testing.T) {
	format := AudioFormat{SampleRate: AudioSampleRate, Channels: 2, Encoding: AudioEncodingPCM16LE, FrameSamples: DefaultAudioFrameSamples}
	source := &testAudioSource{format: format}
	sender, err := NewAudioSender(AudioSenderConfig{RTPAddress: "127.0.0.1:5004", ControlAddress: "127.0.0.1:5005", Session: "session", Generation: 3, Token: "token", SSRC: 4, SampleRate: format.SampleRate, Channels: format.Channels, FrameSamples: format.FrameSamples, FormatCapabilityVersion: 1}, source)
	if err != nil {
		t.Fatalf("NewAudioSender: %v", err)
	}
	var wire bytes.Buffer
	if err := sender.WriteMediaHello(&wire); err != nil {
		t.Fatalf("WriteMediaHello: %v", err)
	}
	message, err := ReadControlMessage(&wire)
	if err != nil {
		t.Fatalf("ReadControlMessage: %v", err)
	}
	if err := ValidateControlMessage(message, "session", 3, "token"); err != nil {
		t.Fatalf("authentication: %v", err)
	}
	var hello AudioMediaHello
	if err := json.Unmarshal(message.Body, &hello); err != nil {
		t.Fatalf("decode hello: %v", err)
	}
	if hello.MediaKind != "audio" || hello.PayloadType != RTPPayloadTypePCM16 || hello.ClockRate != RTPAudioClockRate || hello.FrameSamples != DefaultAudioFrameSamples {
		t.Fatalf("hello = %#v", hello)
	}
}

func TestAudioSenderRequiresControlAddressAndGeneration(t *testing.T) {
	format := AudioFormat{SampleRate: AudioSampleRate, Channels: 1, Encoding: AudioEncodingPCM16LE, FrameSamples: DefaultAudioFrameSamples}
	for _, config := range []AudioSenderConfig{
		{RTPAddress: "127.0.0.1:5004", Session: "s", Generation: 1, Token: "t", SSRC: 1, SampleRate: format.SampleRate, Channels: format.Channels, FrameSamples: format.FrameSamples, FormatCapabilityVersion: 1},
		{RTPAddress: "127.0.0.1:5004", ControlAddress: "127.0.0.1:5005", Session: "s", Token: "t", SSRC: 1, SampleRate: format.SampleRate, Channels: format.Channels, FrameSamples: format.FrameSamples, FormatCapabilityVersion: 1},
	} {
		if _, err := NewAudioSender(config, &testAudioSource{format: format}); err == nil {
			t.Fatalf("invalid control identity accepted: %#v", config)
		}
	}
}

func TestAudioSenderRetriesFailedSourceClose(t *testing.T) {
	format := AudioFormat{SampleRate: AudioSampleRate, Channels: 1, Encoding: AudioEncodingPCM16LE, FrameSamples: DefaultAudioFrameSamples}
	source := &testAudioSource{format: format, failClose: true}
	sender, err := NewAudioSender(AudioSenderConfig{RTPAddress: "127.0.0.1:5004", ControlAddress: "127.0.0.1:5005", Session: "s", Generation: 1, Token: "t", SSRC: 1, SampleRate: format.SampleRate, Channels: format.Channels, FrameSamples: format.FrameSamples, FormatCapabilityVersion: 1}, source)
	if err != nil {
		t.Fatalf("NewAudioSender: %v", err)
	}
	if err := sender.Close(); err == nil {
		t.Fatal("failed source close was hidden")
	}
	if err := sender.Close(); err != nil {
		t.Fatalf("retry source close: %v", err)
	}
	source.mu.Lock()
	defer source.mu.Unlock()
	if source.closes != 2 || !source.closed {
		t.Fatalf("source close lifecycle = %#v", source)
	}
}

func TestAudioSenderClosesBeforeRunAndFailedStartCleanupCanRetry(t *testing.T) {
	format := AudioFormat{SampleRate: AudioSampleRate, Channels: 1, Encoding: AudioEncodingPCM16LE, FrameSamples: DefaultAudioFrameSamples}
	config := AudioSenderConfig{RTPAddress: "127.0.0.1:5004", ControlAddress: "127.0.0.1:5005", Session: "s", Generation: 1, Token: "t", SSRC: 1, SampleRate: format.SampleRate, Channels: format.Channels, FrameSamples: format.FrameSamples, FormatCapabilityVersion: 1}
	beforeRun := &testAudioSource{format: format, failClose: true}
	sender, err := NewAudioSender(config, beforeRun)
	if err != nil {
		t.Fatal(err)
	}
	if err := sender.Close(); err == nil {
		t.Fatal("close-before-run did not surface source close failure")
	}
	if err := sender.Close(); err != nil {
		t.Fatalf("close-before-run retry: %v", err)
	}
	failingStart := &testAudioSource{format: format, failStart: true, failClose: true}
	sender, err = NewAudioSender(config, failingStart)
	if err != nil {
		t.Fatal(err)
	}
	if err := sender.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "test start failure") || !strings.Contains(err.Error(), "test close failure") {
		t.Fatalf("failed-start joined error = %v", err)
	}
	if err := sender.Close(); err != nil {
		t.Fatalf("failed-start cleanup retry: %v", err)
	}
}

func TestAudioSenderRunReadyFollowsTCPMediaHelloWrite(t *testing.T) {
	format := AudioFormat{SampleRate: AudioSampleRate, Channels: 1, Encoding: AudioEncodingPCM16LE, FrameSamples: DefaultAudioFrameSamples}
	control, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	rtp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer rtp.Close()
	source := &testAudioSource{format: format}
	sender, err := NewAudioSender(AudioSenderConfig{RTPAddress: rtp.LocalAddr().String(), ControlAddress: control.Addr().String(), Session: "s", Generation: 1, Token: "t", SSRC: 1, SampleRate: format.SampleRate, Channels: format.Channels, FrameSamples: format.FrameSamples, FormatCapabilityVersion: 1}, source)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runDone := make(chan error, 1)
	ready := make(chan error, 1)
	go func() { runDone <- sender.RunReady(ctx, func(err error) { ready <- err }) }()
	connection, err := control.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	message, err := ReadControlMessage(connection)
	if err != nil || message.Type != ControlMediaHello || ValidateControlMessage(message, "s", 1, "t") != nil {
		t.Fatalf("control hello = %#v, %v", message, err)
	}
	if err := <-ready; err != nil {
		t.Fatalf("RunReady = %v", err)
	}
	cancel()
	if err := <-runDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run = %v", err)
	}
}

func TestAudioSenderIsOneShotAfterRunTerminates(t *testing.T) {
	format := AudioFormat{SampleRate: AudioSampleRate, Channels: 1, Encoding: AudioEncodingPCM16LE, FrameSamples: DefaultAudioFrameSamples}
	source := &testAudioSource{format: format}
	sender, err := NewAudioSender(AudioSenderConfig{RTPAddress: "%%%", ControlAddress: "127.0.0.1:5005", Session: "s", Generation: 1, Token: "t", SSRC: 1, SampleRate: format.SampleRate, Channels: format.Channels, FrameSamples: format.FrameSamples, FormatCapabilityVersion: 1}, source)
	if err != nil {
		t.Fatal(err)
	}
	if err := sender.Run(context.Background()); err == nil {
		t.Fatal("first sender run unexpectedly succeeded")
	}
	if err := sender.Run(context.Background()); err == nil {
		t.Fatal("sender accepted a second run after source ownership ended")
	}
	source.mu.Lock()
	defer source.mu.Unlock()
	if source.starts != 1 {
		t.Fatalf("source starts = %d, want one", source.starts)
	}
}
