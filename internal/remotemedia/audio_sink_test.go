package remotemedia

import (
	"context"
	"reflect"
	"testing"
	"time"
)

var _ AudioSink = (*boundedAudioSink)(nil)

func TestAudioSinkRequiresBoundedReadyDrainMuteReset(t *testing.T) {
	sink := &boundedAudioSink{}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	config := AudioSinkConfig{Format: AudioFormat{SampleRate: 48_000, Channels: 2, Encoding: AudioEncodingPCM16LE, FrameSamples: 240}, Capabilities: []AudioSinkCapability{AudioSinkCapabilityPCM16}}
	if err := sink.Open(ctx, config); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sink.config, config) {
		t.Fatalf("Open config = %+v, want %+v", sink.config, config)
	}
	if err := sink.Write(ctx, AudioFrame{Format: config.Format, Frames: 1, PCM16: make([]byte, 4)}); err != nil {
		t.Fatal(err)
	}
	if err := sink.Ready(ctx); err != nil {
		t.Fatal(err)
	}
	if err := sink.Drain(ctx); err != nil {
		t.Fatal(err)
	}
	if err := sink.Mute(ctx); err != nil {
		t.Fatal(err)
	}
	if err := sink.Reset(ctx); err != nil {
		t.Fatal(err)
	}
	if err := sink.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := sink.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if sink.calls != 8 {
		t.Fatalf("bounded lifecycle calls = %d, want 8", sink.calls)
	}
}

func TestAudioSinkConfigRejectsInvalidFormat(t *testing.T) {
	if err := ValidateAudioSinkConfig(AudioSinkConfig{Format: AudioFormat{SampleRate: 44_100, Channels: 2, Encoding: AudioEncodingPCM16LE, FrameSamples: 240}}); err == nil {
		t.Fatal("invalid sink format accepted")
	}
}

type boundedAudioSink struct {
	calls  int
	config AudioSinkConfig
}

func (s *boundedAudioSink) bounded(ctx context.Context) error {
	s.calls++
	_, ok := ctx.Deadline()
	if !ok {
		return context.DeadlineExceeded
	}
	return nil
}
func (s *boundedAudioSink) Open(ctx context.Context, config AudioSinkConfig) error {
	if err := ValidateAudioSinkConfig(config); err != nil {
		return err
	}
	s.config = config
	return s.bounded(ctx)
}
func (s *boundedAudioSink) Ready(ctx context.Context) error               { return s.bounded(ctx) }
func (s *boundedAudioSink) Write(ctx context.Context, _ AudioFrame) error { return s.bounded(ctx) }
func (s *boundedAudioSink) Drain(ctx context.Context) error               { return s.bounded(ctx) }
func (s *boundedAudioSink) Mute(ctx context.Context) error                { return s.bounded(ctx) }
func (s *boundedAudioSink) Reset(ctx context.Context) error               { return s.bounded(ctx) }
func (s *boundedAudioSink) Stats() AudioSinkStats                         { return AudioSinkStats{} }
func (s *boundedAudioSink) Close(ctx context.Context) error               { return s.bounded(ctx) }
