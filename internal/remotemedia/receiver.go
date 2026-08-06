package remotemedia

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
)

var ErrReceiverClosed = errors.New("receiver is closed")

// ReceiverConfig describes the authenticated media session expected by a Receiver.
type ReceiverConfig struct {
	Session    string
	Generation uint64
	Token      string
	SSRC       uint32
}

type ReceiverReport struct {
	Packets          uint64 `json:"packets"`
	Bytes            uint64 `json:"bytes"`
	AccessUnits      uint64 `json:"access_units"`
	SequenceGaps     uint64 `json:"sequence_gaps"`
	Malformed        uint64 `json:"malformed_packets"`
	DroppedUnits     uint64 `json:"dropped_access_units"`
	DecodeDrops      uint64 `json:"decode_drops"`
	SPS              uint64 `json:"sps"`
	PPS              uint64 `json:"pps"`
	IDR              uint64 `json:"idr"`
	DecodeConfigured bool   `json:"decode_configured"`
	Shutdown         bool   `json:"shutdown"`
}

// Receiver is the transport/depacketization boundary. It deliberately stops at
// Annex-B access units; a native decoder/display is a separate integration.
type Receiver struct {
	mu               sync.Mutex
	config           ReceiverConfig
	authenticated    bool
	closed           bool
	report           ReceiverReport
	haveSequence     bool
	nextSequence     uint16
	currentTimestamp uint32
	nals             [][]byte
	fu               []byte
	fuTimestamp      uint32
	fuActive         bool
}

func NewReceiver(config ReceiverConfig) (*Receiver, error) {
	if config.Session == "" {
		return nil, errors.New("receiver session is required")
	}
	if config.Token == "" {
		return nil, errors.New("receiver token is required")
	}
	return &Receiver{config: config}, nil
}

func (r *Receiver) AcceptControl(message ControlMessage) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return ErrReceiverClosed
	}
	if err := ValidateControlMessage(message, r.config.Session, r.config.Generation, r.config.Token); err != nil {
		return err
	}
	switch message.Type {
	case ControlMediaHello, ControlMediaWelcome:
		var body struct {
			SSRC uint32 `json:"ssrc"`
		}
		if len(message.Body) == 0 || json.Unmarshal(message.Body, &body) != nil || body.SSRC == 0 {
			return errors.New("media hello is missing a valid SSRC")
		}
		if r.config.SSRC != 0 && r.config.SSRC != body.SSRC {
			return errors.New("media hello SSRC mismatch")
		}
		r.config.SSRC = body.SSRC
		r.authenticated = true
		return nil
	case ControlMediaReport, ControlPing, ControlStop:
		if !r.authenticated {
			return errors.New("receiver control session is not authenticated")
		}
		return nil
	default:
		return fmt.Errorf("unexpected receiver control message type %q", message.Type)
	}
}

// Ingest validates and depacketizes one wire RTP packet. Completed marker-delimited
// access units are returned; incomplete units return an empty slice.
func (r *Receiver) Ingest(wire []byte) ([]AccessUnit, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, ErrReceiverClosed
	}
	if !r.authenticated {
		return nil, errors.New("receiver control session is not authenticated")
	}
	packet, err := parseReceiverRTP(wire)
	if err != nil {
		r.report.Malformed++
		return nil, err
	}
	if packet.PayloadType != RTPPayloadTypeH264 {
		r.report.Malformed++
		return nil, fmt.Errorf("unexpected RTP payload type %d", packet.PayloadType)
	}
	if packet.SSRC != r.config.SSRC {
		r.report.Malformed++
		return nil, errors.New("RTP SSRC mismatch")
	}
	if r.haveSequence && packet.Sequence != r.nextSequence {
		gap := uint16(packet.Sequence - r.nextSequence)
		if gap > 0 && gap < 0x8000 {
			r.report.SequenceGaps += uint64(gap)
			r.report.DroppedUnits++
			r.nals = nil
			r.fu = nil
			r.fuActive = false
		} else if gap >= 0x8000 {
			r.report.Malformed++
			return nil, errors.New("RTP sequence moved backwards")
		}
	}
	r.haveSequence = true
	r.nextSequence = packet.Sequence + 1
	r.report.Packets++
	r.report.Bytes += uint64(len(wire))
	if len(packet.Payload) == 0 {
		r.report.Malformed++
		return nil, errors.New("RTP payload is empty")
	}
	if r.currentTimestamp != 0 && r.currentTimestamp != packet.Timestamp && len(r.nals) > 0 {
		r.report.DroppedUnits++
		r.nals = nil
		r.fu = nil
		r.fuActive = false
	}
	r.currentTimestamp = packet.Timestamp
	nal, err := r.consumePayload(packet.Payload, packet.Timestamp)
	if err != nil {
		r.report.Malformed++
		return nil, err
	}
	if nal != nil {
		r.nals = append(r.nals, nal)
	}
	if !packet.Marker {
		return nil, nil
	}
	if r.fuActive {
		r.report.DroppedUnits++
		r.fu = nil
		r.fuActive = false
		r.nals = nil
		return nil, errors.New("RTP marker ended an incomplete FU-A")
	}
	if len(r.nals) == 0 {
		r.report.Malformed++
		return nil, errors.New("RTP marker ended an empty access unit")
	}
	unit := AccessUnit{NALs: cloneNALs(r.nals), Timestamp: r.currentTimestamp}
	for _, n := range unit.NALs {
		if len(n) == 0 {
			continue
		}
		switch n[0] & 0x1f {
		case 7:
			r.report.SPS++
		case 8:
			r.report.PPS++
		case 5:
			r.report.IDR++
			unit.Keyframe = true
		}
	}
	r.report.AccessUnits++
	r.nals = nil
	return []AccessUnit{unit}, nil
}

func (r *Receiver) consumePayload(payload []byte, timestamp uint32) ([]byte, error) {
	typ := payload[0] & 0x1f
	if typ != 28 {
		if typ == 0 || typ > 23 {
			return nil, fmt.Errorf("unsupported H264 RTP NAL type %d", typ)
		}
		if r.fuActive {
			r.fu = nil
			r.fuActive = false
			return nil, errors.New("single NAL interrupted FU-A")
		}
		return append([]byte(nil), payload...), nil
	}
	if len(payload) < 3 {
		return nil, errors.New("FU-A payload is too short")
	}
	fuHeader := payload[1]
	start := fuHeader&0x80 != 0
	end := fuHeader&0x40 != 0
	nalType := fuHeader & 0x1f
	if nalType == 0 {
		return nil, errors.New("FU-A has invalid NAL type")
	}
	if start {
		if r.fuActive {
			r.fu = nil
			r.fuActive = false
			return nil, errors.New("nested FU-A start")
		}
		r.fu = append([]byte{payload[0]&0xe0 | nalType}, payload[2:]...)
		r.fuTimestamp = timestamp
		r.fuActive = true
		if end {
			r.fuActive = false
			out := r.fu
			r.fu = nil
			return out, nil
		}
		return nil, nil
	}
	if !r.fuActive || r.fuTimestamp != timestamp {
		return nil, errors.New("FU-A continuation without matching start")
	}
	r.fu = append(r.fu, payload[2:]...)
	if end {
		out := r.fu
		r.fu = nil
		r.fuActive = false
		return out, nil
	}
	return nil, nil
}

func (r *Receiver) Report() ReceiverReport {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.report
}

func (r *Receiver) RecordDecodeDrop() {
	r.mu.Lock()
	r.report.DecodeDrops++
	r.mu.Unlock()
}
func (r *Receiver) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	r.report.Shutdown = true
	r.nals = nil
	r.fu = nil
	return nil
}

func cloneNALs(in [][]byte) [][]byte {
	out := make([][]byte, len(in))
	for i := range in {
		out[i] = append([]byte(nil), in[i]...)
	}
	return out
}

type receiverRTP struct {
	PayloadType uint8
	SSRC        uint32
	Sequence    uint16
	Timestamp   uint32
	Marker      bool
	Payload     []byte
}

func parseReceiverRTP(wire []byte) (receiverRTP, error) {
	if len(wire) < RTPHeaderSize {
		return receiverRTP{}, errors.New("RTP packet is shorter than header")
	}
	if wire[0]>>6 != 2 {
		return receiverRTP{}, errors.New("RTP version is not 2")
	}
	cc := int(wire[0] & 0x0f)
	offset := RTPHeaderSize + cc*4
	if len(wire) < offset {
		return receiverRTP{}, errors.New("RTP CSRC list is truncated")
	}
	if wire[0]&0x10 != 0 {
		if len(wire) < offset+4 {
			return receiverRTP{}, errors.New("RTP extension header is truncated")
		}
		words := int(binary.BigEndian.Uint16(wire[offset+2 : offset+4]))
		offset += 4 + words*4
		if len(wire) < offset {
			return receiverRTP{}, errors.New("RTP extension is truncated")
		}
	}
	end := len(wire)
	if wire[0]&0x20 != 0 {
		padding := int(wire[len(wire)-1])
		if padding == 0 || padding > end-offset {
			return receiverRTP{}, errors.New("invalid RTP padding")
		}
		end -= padding
	}
	if end <= offset {
		return receiverRTP{}, errors.New("RTP payload is empty")
	}
	return receiverRTP{PayloadType: wire[1] & 0x7f, Marker: wire[1]&0x80 != 0, Sequence: binary.BigEndian.Uint16(wire[2:4]), Timestamp: binary.BigEndian.Uint32(wire[4:8]), SSRC: binary.BigEndian.Uint32(wire[8:12]), Payload: append([]byte(nil), wire[offset:end]...)}, nil
}
