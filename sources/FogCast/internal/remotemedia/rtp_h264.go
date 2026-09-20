package remotemedia

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	RTPPayloadTypeH264 = 96
	RTPClockRate       = 90_000
	RTPHeaderSize      = 12
	DefaultRTPMTU      = 1_200
)

type AccessUnit struct {
	NALs      [][]byte
	Timestamp uint32
	Keyframe  bool
}

type EncodedAccessUnit struct {
	AVCC          []byte
	SPS           []byte
	PPS           []byte
	NALLengthSize int
	Keyframe      bool
}

type RTPPacket struct {
	PayloadType uint8
	SSRC        uint32
	Sequence    uint16
	Timestamp   uint32
	Marker      bool
	Keyframe    bool
	Payload     []byte
}

func (p RTPPacket) Marshal() []byte {
	payloadType := p.PayloadType
	if payloadType == 0 {
		payloadType = RTPPayloadTypeH264
	}
	packet := make([]byte, RTPHeaderSize+len(p.Payload))
	packet[0] = 0x80
	packet[1] = payloadType & 0x7f
	if p.Marker {
		packet[1] |= 0x80
	}
	binary.BigEndian.PutUint16(packet[2:4], p.Sequence)
	binary.BigEndian.PutUint32(packet[4:8], p.Timestamp)
	binary.BigEndian.PutUint32(packet[8:12], p.SSRC)
	copy(packet[RTPHeaderSize:], p.Payload)
	return packet
}

type RTPPacketizer struct {
	mtu         int
	ssrc        uint32
	nextSeq     uint16
	payloadType uint8
}

func NewRTPPacketizer(mtu int, ssrc uint32, initialSequence uint16) *RTPPacketizer {
	if mtu == 0 {
		mtu = DefaultRTPMTU
	}
	return &RTPPacketizer{mtu: mtu, ssrc: ssrc, nextSeq: initialSequence, payloadType: RTPPayloadTypeH264}
}

func (p *RTPPacketizer) Packetize(unit AccessUnit) ([]RTPPacket, error) {
	if p == nil {
		return nil, errors.New("RTP packetizer is nil")
	}
	if p.mtu < RTPHeaderSize+3 {
		return nil, fmt.Errorf("RTP MTU %d is too small", p.mtu)
	}
	if len(unit.NALs) == 0 {
		return nil, errors.New("access unit has no NAL units")
	}
	maxPayload := p.mtu - RTPHeaderSize
	packets := make([]RTPPacket, 0, len(unit.NALs))
	for nalIndex, nal := range unit.NALs {
		if len(nal) == 0 {
			return nil, fmt.Errorf("NAL %d is empty", nalIndex)
		}
		if len(nal) <= maxPayload {
			packets = append(packets, RTPPacket{PayloadType: p.payloadType, SSRC: p.ssrc, Sequence: p.nextSequence(), Timestamp: unit.Timestamp, Keyframe: unit.Keyframe, Payload: append([]byte(nil), nal...)})
			continue
		}
		if maxPayload <= 2 {
			return nil, fmt.Errorf("RTP MTU %d leaves no room for FU-A", p.mtu)
		}
		originalHeader := nal[0]
		fragmentSize := maxPayload - 2
		for offset := 1; offset < len(nal); {
			end := offset + fragmentSize
			if end > len(nal) {
				end = len(nal)
			}
			fuHeader := originalHeader & 0x1f
			if offset == 1 {
				fuHeader |= 0x80
			}
			if end == len(nal) {
				fuHeader |= 0x40
			}
			payload := make([]byte, 2+end-offset)
			payload[0] = (originalHeader & 0xe0) | 28
			payload[1] = fuHeader
			copy(payload[2:], nal[offset:end])
			packets = append(packets, RTPPacket{PayloadType: p.payloadType, SSRC: p.ssrc, Sequence: p.nextSequence(), Timestamp: unit.Timestamp, Keyframe: unit.Keyframe, Payload: payload})
			offset = end
		}
	}
	if len(packets) == 0 {
		return nil, errors.New("access unit produced no packets")
	}
	packets[len(packets)-1].Marker = true
	return packets, nil
}

func (p *RTPPacketizer) nextSequence() uint16 {
	sequence := p.nextSeq
	p.nextSeq++
	return sequence
}

func ParseAnnexBNALs(stream []byte) ([][]byte, error) {
	if len(stream) == 0 {
		return nil, errors.New("Annex-B stream is empty")
	}
	first, ok := annexBStartCodeAt(stream, 0)
	if !ok {
		return nil, errors.New("Annex-B stream does not begin with a start code")
	}
	if first >= len(stream) {
		return nil, errors.New("Annex-B stream ends with a start code")
	}
	nals := make([][]byte, 0, 4)
	start := first
	for start < len(stream) {
		nalStart := start
		next := -1
		for i := start; i < len(stream); i++ {
			if _, ok := annexBStartCodeAt(stream, i); ok {
				next = i
				break
			}
		}
		end := len(stream)
		if next >= 0 {
			end = next
		}
		for end > nalStart && stream[end-1] == 0 {
			end--
		}
		if end <= nalStart {
			return nil, errors.New("Annex-B stream contains an empty NAL unit")
		}
		nals = append(nals, append([]byte(nil), stream[nalStart:end]...))
		if next < 0 {
			break
		}
		codeLen, _ := annexBStartCodeAt(stream, next)
		start = next + codeLen
		if start >= len(stream) {
			return nil, errors.New("Annex-B stream ends with a start code")
		}
	}
	return nals, nil
}

func annexBStartCodeAt(stream []byte, offset int) (int, bool) {
	if offset < 0 || offset+3 > len(stream) {
		return 0, false
	}
	if stream[offset] != 0 || stream[offset+1] != 0 {
		return 0, false
	}
	if stream[offset+2] == 1 {
		return 3, true
	}
	if offset+4 <= len(stream) && stream[offset+2] == 0 && stream[offset+3] == 1 {
		return 4, true
	}
	return 0, false
}

func AVCCToNALs(avcc []byte, nalLengthSize int) ([][]byte, error) {
	if nalLengthSize != 1 && nalLengthSize != 2 && nalLengthSize != 4 {
		return nil, fmt.Errorf("unsupported AVCC NAL length size %d", nalLengthSize)
	}
	if len(avcc) == 0 {
		return nil, errors.New("AVCC block is empty")
	}
	nals := make([][]byte, 0, 4)
	for offset := 0; offset < len(avcc); {
		if len(avcc)-offset < nalLengthSize {
			return nil, errors.New("AVCC block has a truncated NAL length")
		}
		var length uint32
		for i := 0; i < nalLengthSize; i++ {
			length = length<<8 | uint32(avcc[offset+i])
		}
		offset += nalLengthSize
		if length == 0 {
			return nil, errors.New("AVCC block contains a zero-length NAL")
		}
		if uint64(length) > uint64(len(avcc)-offset) {
			return nil, errors.New("AVCC block has a truncated NAL payload")
		}
		nals = append(nals, append([]byte(nil), avcc[offset:offset+int(length)]...))
		offset += int(length)
	}
	return nals, nil
}

func (u EncodedAccessUnit) AnnexB() ([]byte, error) {
	nals, err := AVCCToNALs(u.AVCC, u.NALLengthSize)
	if err != nil {
		return nil, err
	}
	if u.Keyframe {
		nals = parameterSetsForKeyframe(nals, u.SPS, u.PPS)
		if !containsNALType(nals, 7) || !containsNALType(nals, 8) {
			return nil, errors.New("keyframe access unit is missing SPS or PPS")
		}
	}
	var out []byte
	for _, nal := range nals {
		if len(nal) == 0 {
			return nil, errors.New("encoded access unit contains an empty NAL")
		}
		out = append(out, 0, 0, 0, 1)
		out = append(out, nal...)
	}
	return out, nil
}

func parameterSetsForKeyframe(nals [][]byte, sps, pps []byte) [][]byte {
	result := make([][]byte, 0, len(nals)+2)
	if !containsNALType(nals, 7) && len(sps) > 0 {
		result = append(result, append([]byte(nil), sps...))
	}
	if !containsNALType(nals, 8) && len(pps) > 0 {
		result = append(result, append([]byte(nil), pps...))
	}
	return append(result, nals...)
}

func containsNALType(nals [][]byte, nalType byte) bool {
	for _, nal := range nals {
		if len(nal) > 0 && nal[0]&0x1f == nalType {
			return true
		}
	}
	return false
}
