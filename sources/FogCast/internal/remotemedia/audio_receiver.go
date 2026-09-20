package remotemedia

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

var ErrAudioReceiverClosed = errors.New("audio receiver is closed")

var audioReceiverMonotonicEpoch = time.Now()

// AudioReceiverConfig describes one private audio stream. ListenAddress and
// ControlAddress are retained for the managed bridge; this transport object
// itself only accepts authenticated control and UDP datagrams supplied by its
// owner.
type AudioReceiverConfig struct {
	ListenAddress           string
	ControlAddress          string
	Session                 string
	Generation              uint64
	Token                   string
	SSRC                    uint32
	PayloadType             uint8
	SampleRate              int
	Channels                int
	FrameSamples            int
	FormatCapabilityVersion uint32
}

type AudioReceiverReport struct {
	Packets          uint64 `json:"packets"`
	Bytes            uint64 `json:"bytes"`
	Frames           uint64 `json:"frames"`
	NonZeroSamples   uint64 `json:"non_zero_samples"`
	SequenceGaps     uint64 `json:"sequence_gaps"`
	MalformedPackets uint64 `json:"malformed_packets"`
	Shutdown         bool   `json:"shutdown"`
}

// AudioReceiver binds RTP packets to the IP authenticated over control. Its
// first accepted UDP source port is pinned using trusted-network TOFU. RTP has
// no per-packet integrity and is therefore not a hostile-network protocol.
type AudioReceiver struct {
	mu sync.Mutex

	config          AudioReceiverConfig
	format          AudioFormat
	authenticated   bool
	controlIP       net.IP
	pinnedPort      int
	haveSequence    bool
	highestSequence uint64
	closed          bool
	report          AudioReceiverReport
	nowMonoNS       func() int64
}

func NewAudioReceiver(config AudioReceiverConfig) (*AudioReceiver, error) {
	if config.ListenAddress == "" || config.ControlAddress == "" || config.Session == "" || config.Generation == 0 || config.Token == "" || config.SSRC == 0 || config.FormatCapabilityVersion == 0 {
		return nil, errors.New("audio receiver configuration is invalid")
	}
	if config.PayloadType == 0 {
		config.PayloadType = RTPPayloadTypePCM16
	}
	if config.PayloadType != RTPPayloadTypePCM16 {
		return nil, errors.New("audio receiver payload type must match the transport contract")
	}
	format := AudioFormat{SampleRate: config.SampleRate, Channels: config.Channels, Encoding: AudioEncodingPCM16LE, FrameSamples: config.FrameSamples}
	if err := ValidateAudioFormat(format); err != nil {
		return nil, err
	}
	if format.FrameSamples != DefaultAudioFrameSamples {
		return nil, errors.New("audio receiver frame samples must match the transport contract")
	}
	return &AudioReceiver{config: config, format: format, nowMonoNS: func() int64 { return time.Since(audioReceiverMonotonicEpoch).Nanoseconds() }}, nil
}

func (r *AudioReceiver) AcceptControl(message ControlMessage, peer net.Addr) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return ErrAudioReceiverClosed
	}
	if err := ValidateControlMessage(message, r.config.Session, r.config.Generation, r.config.Token); err != nil {
		return err
	}
	if message.Type != ControlMediaHello {
		return fmt.Errorf("unexpected audio receiver control message type %q", message.Type)
	}
	ip := addrIP(peer)
	if ip == nil {
		return errors.New("audio control peer has no IP address")
	}
	hello, err := ParseAudioMediaHello(message.Body)
	if err != nil {
		return err
	}
	if err := ValidateAudioMediaHello(hello, r.config); err != nil {
		return err
	}
	if r.authenticated && !r.controlIP.Equal(ip) {
		return errors.New("audio control peer IP changed")
	}
	r.controlIP = append(net.IP(nil), ip...)
	r.authenticated = true
	return nil
}

// Ingest validates one audio RTP datagram and returns a defensive AudioFrame.
func (r *AudioReceiver) Ingest(wire []byte, peer net.Addr) (AudioFrame, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return AudioFrame{}, ErrAudioReceiverClosed
	}
	if !r.authenticated {
		return AudioFrame{}, errors.New("audio receiver control session is not authenticated")
	}
	ip, port := addrIPPort(peer)
	if ip == nil || port <= 0 || !r.controlIP.Equal(ip) {
		r.report.MalformedPackets++
		return AudioFrame{}, errors.New("audio RTP peer does not match authenticated control peer")
	}
	if r.pinnedPort != 0 && r.pinnedPort != port {
		r.report.MalformedPackets++
		return AudioFrame{}, errors.New("audio RTP source port differs from trusted-network TOFU pin")
	}
	packet, err := parseReceiverRTP(wire)
	if err != nil {
		r.report.MalformedPackets++
		return AudioFrame{}, err
	}
	if packet.PayloadType != r.config.PayloadType || packet.SSRC != r.config.SSRC {
		r.report.MalformedPackets++
		return AudioFrame{}, errors.New("audio RTP payload type or SSRC mismatch")
	}
	if len(packet.Payload) != r.format.FrameSamples*r.format.Channels*2 {
		r.report.MalformedPackets++
		return AudioFrame{}, errors.New("audio RTP payload does not contain exactly one configured frame")
	}
	sequence := r.extendSequence(packet.Sequence)
	if !r.haveSequence {
		r.haveSequence = true
		r.highestSequence = sequence
	} else if sequence > r.highestSequence {
		r.report.SequenceGaps += sequence - r.highestSequence - 1
		r.highestSequence = sequence
	}
	if r.pinnedPort == 0 {
		r.pinnedPort = port
	}
	nonZero, err := CountNonZeroPCM16(packet.Payload)
	if err != nil {
		r.report.MalformedPackets++
		return AudioFrame{}, err
	}
	r.report.Packets++
	r.report.Bytes += uint64(len(wire))
	r.report.Frames += uint64(r.format.FrameSamples)
	r.report.NonZeroSamples += nonZero
	return AudioFrame{Sequence: packet.Sequence, Timestamp: packet.Timestamp, ReceiverMonoNS: r.nowMonoNS(), PCM16: append([]byte(nil), packet.Payload...), Frames: r.format.FrameSamples, Format: r.format}, nil
}

func (r *AudioReceiver) extendSequence(sequence uint16) uint64 {
	if !r.haveSequence {
		return uint64(sequence)
	}
	base := uint16(r.highestSequence)
	delta := int64(int16(sequence - base))
	if delta < 0 && uint64(-delta) > r.highestSequence {
		return 0
	}
	return uint64(int64(r.highestSequence) + delta)
}

func (r *AudioReceiver) Report() AudioReceiverReport {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.report
}

func (r *AudioReceiver) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	r.report.Shutdown = true
	r.controlIP = nil
	return nil
}

func addrIP(address net.Addr) net.IP {
	switch address := address.(type) {
	case *net.TCPAddr:
		return address.IP
	case *net.UDPAddr:
		return address.IP
	default:
		return nil
	}
}

func addrIPPort(address net.Addr) (net.IP, int) {
	if udp, ok := address.(*net.UDPAddr); ok {
		return udp.IP, udp.Port
	}
	return nil, 0
}
