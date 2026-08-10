package remotemedia

import (
	"errors"
	"fmt"
)

const (
	// RTPPayloadTypePCM16 is the private dynamic RTP payload type for FogCast
	// interleaved little-endian PCM16 audio.
	RTPPayloadTypePCM16 = 97
	RTPAudioClockRate   = 48_000

	// DefaultAudioFrameSamples is five milliseconds at 48 kHz.
	DefaultAudioFrameSamples = 240
)

// AudioRTPPacketizer owns the sequence and 48 kHz timestamp state for one
// audio SSRC. It intentionally does not serialize capture time: that remains
// host-local diagnostic evidence.
type AudioRTPPacketizer struct {
	mtu          int
	ssrc         uint32
	nextSeq      uint16
	nextStamp    uint32
	originStamp  uint32
	originMonoNS int64
	haveOrigin   bool
	format       AudioFormat
}

func NewAudioRTPPacketizer(mtu int, ssrc uint32, initialSequence uint16, initialTimestamp uint32, format AudioFormat) (*AudioRTPPacketizer, error) {
	if ssrc == 0 {
		return nil, errors.New("audio RTP SSRC must be nonzero")
	}
	if err := ValidateAudioFormat(format); err != nil {
		return nil, err
	}
	if mtu == 0 {
		mtu = DefaultRTPMTU
	}
	if format.FrameSamples != DefaultAudioFrameSamples {
		return nil, fmt.Errorf("audio RTP frame samples must be %d", DefaultAudioFrameSamples)
	}
	bytesPerPacket := RTPHeaderSize + format.FrameSamples*format.Channels*2
	if mtu < bytesPerPacket {
		return nil, fmt.Errorf("audio RTP MTU %d cannot carry one %d-frame audio packet", mtu, format.FrameSamples)
	}
	return &AudioRTPPacketizer{mtu: mtu, ssrc: ssrc, nextSeq: initialSequence, nextStamp: initialTimestamp, originStamp: initialTimestamp, format: format}, nil
}

// Packetize splits a sample into whole configured-duration PCM frames. A
// sample cannot end on a partial transport frame because that would make the
// 48 kHz timestamp cadence ambiguous.
func (p *AudioRTPPacketizer) Packetize(sample AudioSample) ([]RTPPacket, error) {
	if p == nil {
		return nil, errors.New("audio RTP packetizer is nil")
	}
	if err := ValidateAudioSample(sample); err != nil {
		return nil, err
	}
	if sample.Format != p.format {
		return nil, errors.New("audio sample format does not match RTP packetizer format")
	}
	if sample.Frames%p.format.FrameSamples != 0 {
		return nil, fmt.Errorf("audio sample frames %d do not align to transport frame size %d", sample.Frames, p.format.FrameSamples)
	}
	bytesPerPacket := p.format.FrameSamples * p.format.Channels * 2
	if RTPHeaderSize+bytesPerPacket > p.mtu {
		return nil, errors.New("audio RTP frame exceeds MTU")
	}
	count := sample.Frames / p.format.FrameSamples
	packets := make([]RTPPacket, 0, count)
	if !p.haveOrigin {
		p.originMonoNS = sample.CaptureMonoNS
		p.haveOrigin = true
	} else {
		candidate := p.timestampForCapture(sample.CaptureMonoNS)
		if timestampAfter(candidate, p.nextStamp) {
			p.nextStamp = candidate
		}
	}
	for offset := 0; offset < len(sample.PCM16); offset += bytesPerPacket {
		packets = append(packets, RTPPacket{
			PayloadType: RTPPayloadTypePCM16,
			SSRC:        p.ssrc,
			Sequence:    p.nextSeq,
			Timestamp:   p.nextStamp,
			Payload:     append([]byte(nil), sample.PCM16[offset:offset+bytesPerPacket]...),
		})
		p.nextSeq++
		p.nextStamp += uint32(p.format.FrameSamples)
	}
	packets[len(packets)-1].Marker = true
	return packets, nil
}

func (p *AudioRTPPacketizer) timestampForCapture(captureMonoNS int64) uint32 {
	if captureMonoNS <= p.originMonoNS {
		return p.nextStamp
	}
	delta := uint64(captureMonoNS - p.originMonoNS)
	ticks := delta/1_000_000_000*RTPAudioClockRate + (delta%1_000_000_000)*RTPAudioClockRate/1_000_000_000
	return p.originStamp + uint32(ticks) // uint32 addition deliberately wraps RTP time.
}

func timestampAfter(candidate, current uint32) bool {
	return int32(candidate-current) > 0
}
