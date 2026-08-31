package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/remotemedia"
)

func TestParseBridgeArgsRequiresPrivateTransportAndTokenFile(t *testing.T) {
	for _, args := range [][]string{
		{"-control", "127.0.0.1:5001", "-session", "s", "-generation", "1", "-token-file", "token"},
		{"-rtp", "127.0.0.1:5000", "-session", "s", "-generation", "1", "-token-file", "token"},
		{"-rtp", "127.0.0.1:5000", "-control", "127.0.0.1:5001", "-session", "s", "-token-file", "token"},
		{"-rtp", "127.0.0.1:5000", "-control", "127.0.0.1:5001", "-session", "s", "-generation", "1"},
	} {
		if _, err := parseBridgeArgs(args, nil); err == nil {
			t.Fatalf("accepted incomplete bridge arguments: %v", args)
		}
	}
}

func TestParseBridgeArgsRejectsHardwareSinkAndInvalidTransport(t *testing.T) {
	base := []string{"-rtp", "127.0.0.1:5000", "-control", "127.0.0.1:5001", "-session", "s", "-generation", "1", "-token-file", "token"}
	for _, extra := range [][]string{
		{"-audio-device", "alsa"},
		{"-audio-device", "dump"},
		{"-audio-device", "null", "-dump-pcm", "/tmp/audio.pcm"},
		{"-rtp", "not-an-address"},
		{"-rtp", "0.0.0.0:5000"},
		{"-rtp", "example.com:5000"},
		{"-rtp", "127.0.0.1:5000", "-control", "127.0.0.1:5000"},
	} {
		args := append([]string(nil), base...)
		args = append(args, extra...)
		if _, err := parseBridgeArgs(args, nil); err == nil {
			t.Fatalf("accepted invalid bridge arguments: %v", args)
		}
	}
	config, err := parseBridgeArgs(append(base, "-audio-device", "dump", "-dump-pcm", "capture.pcm"), nil)
	if err != nil || config.audioDevice != "dump" {
		t.Fatalf("dump sink config = %#v, %v", config, err)
	}
	privateArgs := append([]string(nil), base...)
	privateArgs[1], privateArgs[3] = "192.168.1.20:5000", "192.168.1.20:5001"
	if _, err := parseBridgeArgs(privateArgs, nil); err != nil {
		t.Fatalf("private target address rejected: %v", err)
	}
}

func TestReadTokenFileClosesAndRejectsEmptyFiles(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "token")
	if err := os.WriteFile(path, []byte("token-value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	token, err := readTokenFile(path)
	if err != nil || token != "token-value" {
		t.Fatalf("readTokenFile = %q, %v", token, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("token file remained open: %v", err)
	}
	if err := os.WriteFile(path, []byte("\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readTokenFile(path); err == nil {
		t.Fatal("empty token file accepted")
	}
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 16<<10+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readTokenFile(path); err == nil {
		t.Fatal("oversized token file accepted")
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readTokenFile(path); err == nil {
		t.Fatal("world-readable token file accepted")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, "token-link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readTokenFile(link); err == nil {
		t.Fatal("symlink token file accepted")
	}
}

func TestAudioBridgeAuthenticatesHelloAndConsumesNonZeroPCM(t *testing.T) {
	config := bridgeConfig{
		rtpAddress: "127.0.0.1:0", controlAddress: "127.0.0.1:0", session: "session", generation: 4,
		tokenFile: "token", sampleRate: bridgeDefaultRate, channels: 1, ssrc: 7, audioDevice: "null",
	}
	sink := newNullAudioSink()
	bridge, err := newAudioBridge(config, "token", sink)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() { runErr <- bridge.Run(ctx) }()
	var udpAddress, controlAddress string
	deadline := time.Now().Add(time.Second)
	for udpAddress == "" || controlAddress == "" {
		udpAddress, controlAddress = bridge.addresses()
		if time.Now().After(deadline) {
			t.Fatal("bridge did not publish bound addresses")
		}
		time.Sleep(time.Millisecond)
	}
	control, err := net.Dial("tcp", controlAddress)
	if err != nil {
		t.Fatal(err)
	}
	format := remotemedia.AudioFormat{SampleRate: bridgeDefaultRate, Channels: 1, Encoding: remotemedia.AudioEncodingPCM16LE, FrameSamples: remotemedia.DefaultAudioFrameSamples}
	body, err := json.Marshal(remotemedia.AudioMediaHello{MediaKind: "audio", FormatCapabilityVersion: 1, PayloadType: remotemedia.RTPPayloadTypePCM16, ClockRate: remotemedia.RTPAudioClockRate, SSRC: 7, Encoding: remotemedia.AudioEncodingPCM16LE, SampleRate: format.SampleRate, Channels: format.Channels, FrameSamples: format.FrameSamples})
	if err != nil {
		t.Fatal(err)
	}
	if err := remotemedia.WriteControlMessage(control, remotemedia.ControlMessage{Type: remotemedia.ControlMediaHello, Session: "session", Generation: 4, Token: "token", Body: body}); err != nil {
		t.Fatal(err)
	}
	udp, err := net.Dial("udp", udpAddress)
	if err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, format.FrameSamples*format.Channels*2)
	payload[0], payload[1] = 1, 0
	packet := remotemedia.RTPPacket{PayloadType: remotemedia.RTPPayloadTypePCM16, SSRC: 7, Sequence: 1, Timestamp: 100, Marker: true, Payload: payload}
	if _, err := udp.Write(packet.Marshal()); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(time.Second)
	for sink.Stats().WrittenFrames == 0 {
		if time.Now().After(deadline) {
			t.Fatal("bridge did not consume a non-zero PCM frame")
		}
		time.Sleep(time.Millisecond)
	}
	_ = control.Close()
	_ = udp.Close()
	select {
	case err := <-runErr:
		if err == nil || !strings.Contains(err.Error(), "read") {
			t.Fatalf("unexpected bridge termination result = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("bridge did not stop after control disconnect")
	}
	cancel()
}

func TestAudioBridgeRejectsUnauthenticatedHello(t *testing.T) {
	config := bridgeConfig{rtpAddress: "127.0.0.1:0", controlAddress: "127.0.0.1:0", session: "session", generation: 4, tokenFile: "token", sampleRate: bridgeDefaultRate, channels: 1, ssrc: 7, audioDevice: "null"}
	bridge, err := newAudioBridge(config, "token", newNullAudioSink())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runErr := make(chan error, 1)
	go func() { runErr <- bridge.Run(ctx) }()
	var controlAddress string
	deadline := time.Now().Add(time.Second)
	for controlAddress == "" {
		_, controlAddress = bridge.addresses()
		if time.Now().After(deadline) {
			t.Fatal("bridge did not publish control address")
		}
		time.Sleep(time.Millisecond)
	}
	control, err := net.Dial("tcp", controlAddress)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(remotemedia.AudioMediaHello{MediaKind: "audio", FormatCapabilityVersion: 1, PayloadType: remotemedia.RTPPayloadTypePCM16, ClockRate: remotemedia.RTPAudioClockRate, SSRC: 7, Encoding: remotemedia.AudioEncodingPCM16LE, SampleRate: bridgeDefaultRate, Channels: 1, FrameSamples: remotemedia.DefaultAudioFrameSamples})
	if err := remotemedia.WriteControlMessage(control, remotemedia.ControlMessage{Type: remotemedia.ControlMediaHello, Session: "session", Generation: 4, Token: "wrong", Body: body}); err != nil {
		t.Fatal(err)
	}
	_ = control.Close()
	select {
	case err := <-runErr:
		if err == nil || !strings.Contains(err.Error(), "control") {
			t.Fatalf("unauthenticated bridge result = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("unauthenticated bridge did not stop")
	}
}

func TestAudioBridgeStopsWhenContextIsCanceledBeforeControl(t *testing.T) {
	config := bridgeConfig{rtpAddress: "127.0.0.1:0", controlAddress: "127.0.0.1:0", session: "session", generation: 4, tokenFile: "token", sampleRate: bridgeDefaultRate, channels: 1, ssrc: 7, audioDevice: "null"}
	bridge, err := newAudioBridge(config, "token", newNullAudioSink())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() { runErr <- bridge.Run(ctx) }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-runErr:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled bridge result = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("bridge did not stop after context cancellation")
	}
}

func TestNullAudioSinkRejectsInvalidFramesAndTracksWrites(t *testing.T) {
	sink := newNullAudioSink()
	format := remotemedia.AudioFormat{SampleRate: bridgeDefaultRate, Channels: 1, Encoding: remotemedia.AudioEncodingPCM16LE, FrameSamples: remotemedia.DefaultAudioFrameSamples}
	if err := sink.Open(context.Background(), remotemedia.AudioSinkConfig{Format: format, Capabilities: []remotemedia.AudioSinkCapability{remotemedia.AudioSinkCapabilityPCM16}}); err != nil {
		t.Fatal(err)
	}
	frame := remotemedia.AudioFrame{Sequence: 1, Timestamp: 1, PCM16: make([]byte, format.FrameSamples*2), Frames: format.FrameSamples, Format: format}
	if err := sink.Write(context.Background(), frame); err != nil {
		t.Fatal(err)
	}
	if sink.Stats().WrittenFrames != uint64(format.FrameSamples) {
		t.Fatalf("sink stats = %#v", sink.Stats())
	}
	if err := sink.Write(context.Background(), remotemedia.AudioFrame{}); err == nil {
		t.Fatal("invalid frame accepted")
	}
}

type blockingBridgeSink struct {
	opened       chan struct{}
	closeStarted chan struct{}
}

type failingCleanupBridgeSink struct {
	opened     chan struct{}
	openedOnce sync.Once
}

func (s *failingCleanupBridgeSink) Open(context.Context, remotemedia.AudioSinkConfig) error {
	s.openedOnce.Do(func() { close(s.opened) })
	return nil
}
func (*failingCleanupBridgeSink) Ready(context.Context) error { return nil }
func (*failingCleanupBridgeSink) Write(context.Context, remotemedia.AudioFrame) error {
	return nil
}
func (*failingCleanupBridgeSink) Drain(context.Context) error { return nil }
func (*failingCleanupBridgeSink) Mute(context.Context) error  { return nil }
func (*failingCleanupBridgeSink) Reset(context.Context) error { return nil }
func (*failingCleanupBridgeSink) Stats() remotemedia.AudioSinkStats {
	return remotemedia.AudioSinkStats{}
}
func (*failingCleanupBridgeSink) Close(context.Context) error {
	return errors.New("sink close failure")
}

func (s *blockingBridgeSink) Open(context.Context, remotemedia.AudioSinkConfig) error {
	close(s.opened)
	return nil
}
func (s *blockingBridgeSink) Ready(context.Context) error                         { return nil }
func (s *blockingBridgeSink) Write(context.Context, remotemedia.AudioFrame) error { return nil }
func (s *blockingBridgeSink) Drain(context.Context) error                         { return nil }
func (s *blockingBridgeSink) Mute(context.Context) error                          { return nil }
func (s *blockingBridgeSink) Reset(context.Context) error                         { return nil }
func (*blockingBridgeSink) Stats() remotemedia.AudioSinkStats                     { return remotemedia.AudioSinkStats{} }
func (s *blockingBridgeSink) Close(ctx context.Context) error {
	close(s.closeStarted)
	<-ctx.Done()
	return ctx.Err()
}

func TestAudioPlayoutBoundsDiagnosticSinkCleanup(t *testing.T) {
	format := remotemedia.AudioFormat{SampleRate: bridgeDefaultRate, Channels: 1, Encoding: remotemedia.AudioEncodingPCM16LE, FrameSamples: remotemedia.DefaultAudioFrameSamples}
	playout, err := remotemedia.NewAudioPlayout(remotemedia.AudioPlayoutConfig{Format: format, StartupPrebuffer: 1})
	if err != nil {
		t.Fatal(err)
	}
	frame := remotemedia.AudioFrame{Sequence: 1, Timestamp: 1, PCM16: make([]byte, format.FrameSamples*2), Frames: format.FrameSamples, Format: format}
	if err := playout.Push(context.Background(), frame); err != nil {
		t.Fatal(err)
	}
	sink := &blockingBridgeSink{opened: make(chan struct{}), closeStarted: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- playout.Run(ctx, sink) }()
	select {
	case <-sink.opened:
	case <-time.After(time.Second):
		t.Fatal("diagnostic sink did not open")
	}
	started := time.Now()
	cancel()
	select {
	case <-sink.closeStarted:
	case <-time.After(1500 * time.Millisecond):
		t.Fatal("diagnostic sink cleanup did not begin")
	}
	select {
	case <-done:
		if elapsed := time.Since(started); elapsed > 1500*time.Millisecond {
			t.Fatalf("bounded sink cleanup took %v", elapsed)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("diagnostic sink cleanup was unbounded")
	}
}

type bridgeCleanupProbePlayout struct {
	stopCalled chan struct{}
}

func (*bridgeCleanupProbePlayout) Push(context.Context, remotemedia.AudioFrame) error { return nil }
func (*bridgeCleanupProbePlayout) Run(context.Context, remotemedia.AudioSink) error   { return nil }
func (p *bridgeCleanupProbePlayout) Stop(context.Context) error {
	close(p.stopCalled)
	return nil
}
func (*bridgeCleanupProbePlayout) Stats() remotemedia.AudioPlayoutStats {
	return remotemedia.AudioPlayoutStats{}
}

func TestAudioBridgeUsesIndependentPlayoutCleanupDeadline(t *testing.T) {
	networkCtx, networkCancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer networkCancel()
	<-networkCtx.Done()
	playoutDone := make(chan struct{})
	close(playoutDone)
	probe := &bridgeCleanupProbePlayout{stopCalled: make(chan struct{})}
	if err := stopAudioBridgePlayout(probe, playoutDone, 100*time.Millisecond); err != nil {
		t.Fatalf("playout cleanup after network deadline: %v", err)
	}
	select {
	case <-probe.stopCalled:
	default:
		t.Fatal("playout cleanup was not attempted after network deadline")
	}
}

func TestAudioBridgeCleanupFailureIsNotHiddenByCancellation(t *testing.T) {
	runErr := error(context.Canceled)
	bridgeRecordCleanupFailure(&runErr, errors.New("cleanup failed"))
	if runErr == nil || errors.Is(runErr, context.Canceled) || !strings.Contains(runErr.Error(), "cleanup failed") {
		t.Fatalf("cleanup result = %v", runErr)
	}
}

func TestAudioBridgeReportsSinkCleanupFailureDuringCancellation(t *testing.T) {
	config := bridgeConfig{rtpAddress: "127.0.0.1:0", controlAddress: "127.0.0.1:0", session: "session", generation: 4, tokenFile: "token", sampleRate: bridgeDefaultRate, channels: 1, ssrc: 7, audioDevice: "null"}
	sink := &failingCleanupBridgeSink{opened: make(chan struct{})}
	bridge, err := newAudioBridge(config, "token", sink)
	if err != nil {
		t.Fatal(err)
	}
	runErr := make(chan error, 1)
	go func() { runErr <- bridge.Run(context.Background()) }()
	var controlAddress string
	deadline := time.Now().Add(time.Second)
	for controlAddress == "" {
		_, controlAddress = bridge.addresses()
		if time.Now().After(deadline) {
			t.Fatal("bridge did not publish control address")
		}
		time.Sleep(time.Millisecond)
	}
	control, err := net.Dial("tcp", controlAddress)
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	body, err := json.Marshal(remotemedia.AudioMediaHello{MediaKind: "audio", FormatCapabilityVersion: 1, PayloadType: remotemedia.RTPPayloadTypePCM16, ClockRate: remotemedia.RTPAudioClockRate, SSRC: 7, Encoding: remotemedia.AudioEncodingPCM16LE, SampleRate: bridgeDefaultRate, Channels: 1, FrameSamples: remotemedia.DefaultAudioFrameSamples})
	if err != nil {
		t.Fatal(err)
	}
	if err := remotemedia.WriteControlMessage(control, remotemedia.ControlMessage{Type: remotemedia.ControlMediaHello, Session: "session", Generation: 4, Token: "token", Body: body}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-sink.opened:
	case <-time.After(time.Second):
		t.Fatal("bridge sink did not open")
	}
	if err := remotemedia.WriteControlMessage(control, remotemedia.ControlMessage{Type: remotemedia.ControlStop, Session: "session", Generation: 4, Token: "token"}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-runErr:
		if err == nil || !strings.Contains(err.Error(), "playout cleanup") {
			t.Fatalf("sink cleanup result = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("bridge did not report sink cleanup failure")
	}
}

func TestPCMDumpSinkRejectsExistingOrSymlinkTargets(t *testing.T) {
	format := remotemedia.AudioFormat{SampleRate: bridgeDefaultRate, Channels: 1, Encoding: remotemedia.AudioEncodingPCM16LE, FrameSamples: remotemedia.DefaultAudioFrameSamples}
	config := remotemedia.AudioSinkConfig{Format: format, Capabilities: []remotemedia.AudioSinkCapability{remotemedia.AudioSinkCapabilityPCM16}}
	directory := t.TempDir()
	existing := filepath.Join(directory, "existing.pcm")
	if err := os.WriteFile(existing, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := newPCMDumpAudioSink(existing).Open(context.Background(), config); err == nil {
		t.Fatal("existing PCM dump target accepted")
	}
	target := filepath.Join(directory, "target.pcm")
	link := filepath.Join(directory, "link.pcm")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := newPCMDumpAudioSink(link).Open(context.Background(), config); err == nil {
		t.Fatal("symlink PCM dump target accepted")
	}
}
