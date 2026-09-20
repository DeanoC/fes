package remotemedia

import (
	"context"
	"strings"
	"testing"
)

func TestAudioAuthorizationErrorsAreActionable(t *testing.T) {
	for _, permission := range []AudioPermission{AudioPermissionMicrophone, AudioPermissionScreenRecording} {
		for _, status := range []AudioAuthorizationStatus{AudioAuthorizationNotDetermined, AudioAuthorizationRestricted, AudioAuthorizationDenied} {
			err := AudioAuthorizationError(permission, status)
			if err == nil || !strings.Contains(err.Error(), "System Settings") || !strings.Contains(err.Error(), permission.String()) {
				t.Fatalf("%s/%s error = %v", permission, status, err)
			}
		}
		if err := AudioAuthorizationError(permission, AudioAuthorizationAuthorized); err != nil {
			t.Fatalf("%s authorized error = %v", permission, err)
		}
	}
}

func TestAudioFloat32ConversionClipsInterleavedAndPlanar(t *testing.T) {
	format := AudioFormat{SampleRate: AudioSampleRate, Channels: 2, Encoding: AudioEncodingPCM16LE, FrameSamples: DefaultAudioFrameSamples}
	interleaved, err := ConvertFloat32PCM([][]float32{{-1, 1, 2, -2}}, false, format, 12)
	if err != nil {
		t.Fatalf("interleaved conversion: %v", err)
	}
	if interleaved.CaptureMonoNS != 12 || interleaved.Frames != 2 || string(interleaved.PCM16) != string([]byte{0, 0x80, 0xff, 0x7f, 0xff, 0x7f, 0, 0x80}) {
		t.Fatalf("interleaved sample = %#v", interleaved)
	}
	planar, err := ConvertFloat32PCM([][]float32{{0, .5}, {-.5, 1}}, true, format, 13)
	if err != nil {
		t.Fatalf("planar conversion: %v", err)
	}
	if planar.Frames != 2 || string(planar.PCM16) != string([]byte{0, 0, 0, 0xc0, 0, 0x40, 0xff, 0x7f}) {
		t.Fatalf("planar sample = %#v", planar)
	}
}

func TestAudioFloat32ConversionRejectsEmptyPlanarAndNonTransportQueueSamples(t *testing.T) {
	format := AudioFormat{SampleRate: AudioSampleRate, Channels: 2, Encoding: AudioEncodingPCM16LE, FrameSamples: DefaultAudioFrameSamples}
	if _, err := ConvertFloat32PCM([][]float32{{}, {}}, true, format, 1); err == nil {
		t.Fatal("empty planar callback accepted")
	}
	queue := newAudioSampleQueue()
	short := AudioSample{CaptureMonoNS: 1, PCM16: make([]byte, 2*format.Channels), Frames: 1, Format: format}
	if _, err := queue.Offer(short); err == nil {
		t.Fatal("non-240-frame transport sample accepted")
	}
}

func TestAudioSampleQueueReplacesOnePendingBufferAndCountsDrops(t *testing.T) {
	format := AudioFormat{SampleRate: AudioSampleRate, Channels: 1, Encoding: AudioEncodingPCM16LE, FrameSamples: DefaultAudioFrameSamples}
	queue := newAudioSampleQueue()
	first := AudioSample{CaptureMonoNS: 1, PCM16: make([]byte, format.FrameSamples*2), Frames: format.FrameSamples, Format: format}
	second := first.Clone()
	second.CaptureMonoNS = 2
	second.PCM16[0] = 1
	if replaced, err := queue.Offer(first); err != nil || replaced {
		t.Fatalf("first offer = %v, %v", replaced, err)
	}
	if replaced, err := queue.Offer(second); err != nil || !replaced {
		t.Fatalf("second offer = %v, %v", replaced, err)
	}
	got, ok := queue.Next(context.Background())
	if !ok || got.CaptureMonoNS != 2 || got.PCM16[0] != 1 || queue.Drops() != uint64(format.FrameSamples) {
		t.Fatalf("queue result = %#v, %v, drops=%d", got, ok, queue.Drops())
	}
}
