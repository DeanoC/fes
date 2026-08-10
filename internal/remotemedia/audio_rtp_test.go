package remotemedia

import (
	"bytes"
	"testing"
)

func TestAudioRTPPacketizerFiveMillisecondStereoPreservesSamplesAndClock(t *testing.T) {
	format := AudioFormat{SampleRate: AudioSampleRate, Channels: 2, Encoding: AudioEncodingPCM16LE, FrameSamples: DefaultAudioFrameSamples}
	pcm := make([]byte, 2*format.Channels*format.FrameSamples*2)
	for index := range pcm {
		pcm[index] = byte(index)
	}
	packetizer, err := NewAudioRTPPacketizer(1200, 0x01020304, 17, 9000, format)
	if err != nil {
		t.Fatalf("NewAudioRTPPacketizer: %v", err)
	}
	packets, err := packetizer.Packetize(AudioSample{CaptureMonoNS: 1, PCM16: pcm, Frames: format.FrameSamples * 2, Format: format})
	if err != nil {
		t.Fatalf("Packetize: %v", err)
	}
	if len(packets) != 2 {
		t.Fatalf("packet count = %d, want 2", len(packets))
	}
	for index, packet := range packets {
		if len(packet.Marshal()) > 1200 {
			t.Fatalf("packet %d exceeds MTU: %d", index, len(packet.Marshal()))
		}
		if packet.PayloadType != RTPPayloadTypePCM16 || packet.SSRC != 0x01020304 {
			t.Fatalf("packet %d RTP identity = %#v", index, packet)
		}
		if packet.Sequence != uint16(17+index) || packet.Timestamp != uint32(9000+index*DefaultAudioFrameSamples) {
			t.Fatalf("packet %d sequence/timestamp = %d/%d", index, packet.Sequence, packet.Timestamp)
		}
		if packet.Marker != (index == len(packets)-1) {
			t.Fatalf("packet %d marker = %v", index, packet.Marker)
		}
	}
	if !bytes.Equal(append(packets[0].Payload, packets[1].Payload...), pcm) {
		t.Fatal("PCM sample order changed")
	}
}

func TestAudioRTPPacketizerRejectsPartialPCMFrames(t *testing.T) {
	format := AudioFormat{SampleRate: AudioSampleRate, Channels: 2, Encoding: AudioEncodingPCM16LE, FrameSamples: DefaultAudioFrameSamples}
	packetizer, err := NewAudioRTPPacketizer(1200, 1, 0, 0, format)
	if err != nil {
		t.Fatalf("NewAudioRTPPacketizer: %v", err)
	}
	if _, err := packetizer.Packetize(AudioSample{PCM16: make([]byte, 5), Frames: 1, Format: format}); err == nil {
		t.Fatal("partial PCM frame accepted")
	}
}

func TestAudioRTPPacketizerMapsCaptureMonotonicTimeAndClampsDiscontinuities(t *testing.T) {
	format := AudioFormat{SampleRate: AudioSampleRate, Channels: 1, Encoding: AudioEncodingPCM16LE, FrameSamples: DefaultAudioFrameSamples}
	packetizer, err := NewAudioRTPPacketizer(1200, 1, 0, 9000, format)
	if err != nil {
		t.Fatalf("NewAudioRTPPacketizer: %v", err)
	}
	sample := func(mono int64) AudioSample {
		return AudioSample{CaptureMonoNS: mono, PCM16: make([]byte, format.FrameSamples*2), Frames: format.FrameSamples, Format: format}
	}
	first, err := packetizer.Packetize(sample(1_000_000_000))
	if err != nil || first[0].Timestamp != 9000 {
		t.Fatalf("first packet = %#v, %v", first, err)
	}
	second, err := packetizer.Packetize(sample(1_010_000_000))
	if err != nil || second[0].Timestamp != 9480 {
		t.Fatalf("capture-timed second packet = %#v, %v", second, err)
	}
	repeated, err := packetizer.Packetize(sample(1_010_000_000))
	if err != nil || repeated[0].Timestamp != 9720 {
		t.Fatalf("repeated capture time packet = %#v, %v", repeated, err)
	}
	backward, err := packetizer.Packetize(sample(999_000_000))
	if err != nil || backward[0].Timestamp != 9960 {
		t.Fatalf("backward capture time packet = %#v, %v", backward, err)
	}
}
