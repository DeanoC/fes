package remotemedia

import (
	"context"
	"encoding/json"
	"net"
	"testing"
)

func TestAudioReceiverAuthenticatesHelloBindsPeerIPAndPinsFirstPort(t *testing.T) {
	format := AudioFormat{SampleRate: AudioSampleRate, Channels: 2, Encoding: AudioEncodingPCM16LE, FrameSamples: DefaultAudioFrameSamples}
	receiver, err := NewAudioReceiver(AudioReceiverConfig{ListenAddress: "127.0.0.1:5004", ControlAddress: "127.0.0.1:5005", Session: "session", Generation: 4, Token: "token", SSRC: 5, PayloadType: RTPPayloadTypePCM16, SampleRate: format.SampleRate, Channels: format.Channels, FrameSamples: format.FrameSamples, FormatCapabilityVersion: 1})
	if err != nil {
		t.Fatalf("NewAudioReceiver: %v", err)
	}
	body, err := json.Marshal(AudioMediaHello{MediaKind: "audio", FormatCapabilityVersion: 1, PayloadType: RTPPayloadTypePCM16, ClockRate: RTPAudioClockRate, SSRC: 5, Encoding: AudioEncodingPCM16LE, SampleRate: format.SampleRate, Channels: format.Channels, FrameSamples: format.FrameSamples})
	if err != nil {
		t.Fatalf("marshal hello: %v", err)
	}
	controlPeer := &net.TCPAddr{IP: net.ParseIP("192.0.2.4"), Port: 5005}
	if err := receiver.AcceptControl(ControlMessage{Type: ControlMediaHello, Session: "session", Generation: 4, Token: "token", Body: body}, controlPeer); err != nil {
		t.Fatalf("AcceptControl: %v", err)
	}
	receiver.nowMonoNS = func() int64 { return 77 }
	packet := RTPPacket{PayloadType: RTPPayloadTypePCM16, SSRC: 5, Sequence: 7, Timestamp: 9, Marker: true, Payload: make([]byte, format.FrameSamples*format.Channels*2)}.Marshal()
	packet[len(packet)-1] = 1
	if _, err := receiver.Ingest(packet, &net.UDPAddr{IP: net.ParseIP("192.0.2.5"), Port: 6000}); err == nil {
		t.Fatal("RTP from different IP accepted")
	}
	frame, err := receiver.Ingest(packet, &net.UDPAddr{IP: net.ParseIP("192.0.2.4"), Port: 6000})
	if err != nil {
		t.Fatalf("first TOFU RTP: %v", err)
	}
	if frame.Frames != DefaultAudioFrameSamples || frame.ReceiverMonoNS != 77 || frame.PCM16[len(frame.PCM16)-1] != 1 {
		t.Fatalf("frame = %#v", frame)
	}
	if _, err := receiver.Ingest(packet, &net.UDPAddr{IP: net.ParseIP("192.0.2.4"), Port: 6001}); err == nil {
		t.Fatal("RTP source port changed after TOFU pin")
	}
}

func TestAudioReceiverToPlayoutReordersWrappedPackets(t *testing.T) {
	format := AudioFormat{SampleRate: AudioSampleRate, Channels: 1, Encoding: AudioEncodingPCM16LE, FrameSamples: DefaultAudioFrameSamples}
	receiver, err := NewAudioReceiver(AudioReceiverConfig{ListenAddress: "127.0.0.1:5004", ControlAddress: "127.0.0.1:5005", Session: "s", Generation: 1, Token: "t", SSRC: 1, SampleRate: format.SampleRate, Channels: format.Channels, FrameSamples: format.FrameSamples, FormatCapabilityVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(AudioMediaHello{MediaKind: "audio", FormatCapabilityVersion: 1, PayloadType: RTPPayloadTypePCM16, ClockRate: RTPAudioClockRate, SSRC: 1, Encoding: AudioEncodingPCM16LE, SampleRate: format.SampleRate, Channels: format.Channels, FrameSamples: format.FrameSamples})
	peer := &net.UDPAddr{IP: net.ParseIP("192.0.2.1"), Port: 7}
	if err := receiver.AcceptControl(ControlMessage{Type: ControlMediaHello, Session: "s", Generation: 1, Token: "t", Body: body}, &net.TCPAddr{IP: peer.IP}); err != nil {
		t.Fatal(err)
	}
	playout, err := NewAudioPlayout(AudioPlayoutConfig{Format: format, StartupPrebuffer: 3})
	if err != nil {
		t.Fatal(err)
	}
	for _, sequence := range []uint16{65535, 1, 0} {
		payload := make([]byte, format.FrameSamples*2)
		payload[0] = byte(sequence)
		frame, err := receiver.Ingest(RTPPacket{PayloadType: RTPPayloadTypePCM16, SSRC: 1, Sequence: sequence, Timestamp: 9000 + uint32(sequence-65535)*240, Payload: payload}.Marshal(), peer)
		if err != nil {
			t.Fatalf("Ingest %d: %v", sequence, err)
		}
		if err := playout.Push(context.Background(), frame); err != nil {
			t.Fatalf("Push %d: %v", sequence, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	sink := &recordingAudioSink{cancel: cancel, stopAfter: 3}
	if err := playout.Run(ctx, sink); err == nil {
		t.Fatal("Run returned nil after cancellation")
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.frames) != 3 || sink.frames[0].Sequence != 65535 || sink.frames[1].Sequence != 0 || sink.frames[2].Sequence != 1 {
		t.Fatalf("wrapped playout frames = %#v", sink.frames)
	}
}

func TestAudioReceiverRejectsBadHelloFormatsAndReturnsDefensiveFrames(t *testing.T) {
	format := AudioFormat{SampleRate: AudioSampleRate, Channels: 1, Encoding: AudioEncodingPCM16LE, FrameSamples: DefaultAudioFrameSamples}
	receiver, err := NewAudioReceiver(AudioReceiverConfig{ListenAddress: "127.0.0.1:5004", ControlAddress: "127.0.0.1:5005", Session: "s", Generation: 1, Token: "t", SSRC: 8, SampleRate: format.SampleRate, Channels: format.Channels, FrameSamples: format.FrameSamples, FormatCapabilityVersion: 1})
	if err != nil {
		t.Fatalf("NewAudioReceiver: %v", err)
	}
	bad, _ := json.Marshal(AudioMediaHello{MediaKind: "video", FormatCapabilityVersion: 1, PayloadType: RTPPayloadTypePCM16, ClockRate: RTPAudioClockRate, SSRC: 8, Encoding: AudioEncodingPCM16LE, SampleRate: format.SampleRate, Channels: format.Channels, FrameSamples: format.FrameSamples})
	if err := receiver.AcceptControl(ControlMessage{Type: ControlMediaHello, Session: "s", Generation: 1, Token: "t", Body: bad}, &net.TCPAddr{IP: net.ParseIP("192.0.2.1")}); err == nil {
		t.Fatal("non-audio hello accepted")
	}
	good, _ := json.Marshal(AudioMediaHello{MediaKind: "audio", FormatCapabilityVersion: 1, PayloadType: RTPPayloadTypePCM16, ClockRate: RTPAudioClockRate, SSRC: 8, Encoding: AudioEncodingPCM16LE, SampleRate: format.SampleRate, Channels: format.Channels, FrameSamples: format.FrameSamples})
	if err := receiver.AcceptControl(ControlMessage{Type: ControlMediaHello, Session: "s", Generation: 1, Token: "t", Body: good}, &net.TCPAddr{IP: net.ParseIP("192.0.2.1")}); err != nil {
		t.Fatalf("good hello: %v", err)
	}
	payload := make([]byte, format.FrameSamples*2)
	payload[0] = 3
	frame, err := receiver.Ingest(RTPPacket{PayloadType: RTPPayloadTypePCM16, SSRC: 8, Sequence: 1, Payload: payload}.Marshal(), &net.UDPAddr{IP: net.ParseIP("192.0.2.1"), Port: 7})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	frame.PCM16[0] = 99
	again, err := receiver.Ingest(RTPPacket{PayloadType: RTPPayloadTypePCM16, SSRC: 8, Sequence: 3, Payload: payload}.Marshal(), &net.UDPAddr{IP: net.ParseIP("192.0.2.1"), Port: 7})
	if err != nil {
		t.Fatalf("second Ingest: %v", err)
	}
	if again.PCM16[0] != 3 || receiver.Report().SequenceGaps != 1 {
		t.Fatalf("defensive copy/gaps = %#v/%#v", again.PCM16[:1], receiver.Report())
	}
}

func TestAudioReceiverRequiresControlIdentityAndAllowsOutOfOrderAndWrap(t *testing.T) {
	format := AudioFormat{SampleRate: AudioSampleRate, Channels: 1, Encoding: AudioEncodingPCM16LE, FrameSamples: DefaultAudioFrameSamples}
	invalid := AudioReceiverConfig{ListenAddress: "127.0.0.1:5004", Session: "s", Generation: 1, Token: "t", SSRC: 1, SampleRate: format.SampleRate, Channels: format.Channels, FrameSamples: format.FrameSamples, FormatCapabilityVersion: 1}
	if _, err := NewAudioReceiver(invalid); err == nil {
		t.Fatal("receiver without control address accepted")
	}
	wrongFrame := invalid
	wrongFrame.ControlAddress = "127.0.0.1:5005"
	wrongFrame.FrameSamples = 120
	if _, err := NewAudioReceiver(wrongFrame); err == nil {
		t.Fatal("receiver accepted non-240 audio frame contract")
	}
	wrongPayload := invalid
	wrongPayload.ControlAddress = "127.0.0.1:5005"
	wrongPayload.PayloadType = RTPPayloadTypeH264
	if _, err := NewAudioReceiver(wrongPayload); err == nil {
		t.Fatal("receiver accepted non-PCM16 payload contract")
	}
	config := invalid
	config.ControlAddress = "127.0.0.1:5005"
	receiver, err := NewAudioReceiver(config)
	if err != nil {
		t.Fatalf("NewAudioReceiver: %v", err)
	}
	body, _ := json.Marshal(AudioMediaHello{MediaKind: "audio", FormatCapabilityVersion: 1, PayloadType: RTPPayloadTypePCM16, ClockRate: RTPAudioClockRate, SSRC: 1, Encoding: AudioEncodingPCM16LE, SampleRate: format.SampleRate, Channels: format.Channels, FrameSamples: format.FrameSamples})
	peer := &net.UDPAddr{IP: net.ParseIP("192.0.2.1"), Port: 7}
	if err := receiver.AcceptControl(ControlMessage{Type: ControlMediaHello, Session: "s", Generation: 1, Token: "t", Body: body}, &net.TCPAddr{IP: peer.IP}); err != nil {
		t.Fatalf("AcceptControl: %v", err)
	}
	payload := make([]byte, format.FrameSamples*2)
	for _, sequence := range []uint16{65535, 1, 0} {
		if _, err := receiver.Ingest(RTPPacket{PayloadType: RTPPayloadTypePCM16, SSRC: 1, Sequence: sequence, Timestamp: uint32(sequence) * 240, Payload: payload}.Marshal(), peer); err != nil {
			t.Fatalf("out-of-order/wrapped sequence %d rejected: %v", sequence, err)
		}
	}
}
