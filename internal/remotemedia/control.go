package remotemedia

import (
	"bufio"
	"bytes"
	"crypto/subtle"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const (
	ControlVersion         = 1
	ControlMagic           = "MRV1"
	DefaultControlLimit    = 64 << 10
	ControlMediaHello      = "MEDIA_HELLO"
	ControlMediaWelcome    = "MEDIA_WELCOME"
	ControlKeyframeRequest = "KEYFRAME_REQUEST"
	ControlMediaReport     = "MEDIA_REPORT"
	ControlPing            = "PING"
	ControlPong            = "PONG"
	ControlStop            = "STOP"
)

type ControlMessage struct {
	Type       string          `json:"type"`
	Session    string          `json:"session"`
	Generation uint64          `json:"generation"`
	Token      string          `json:"token,omitempty"`
	Body       json.RawMessage `json:"body,omitempty"`
}

// AudioMediaHello is the private, authenticated audio transport declaration.
// It is deliberately separate from public cast admission: endpoint, device,
// and sink information never crosses this boundary.
type AudioMediaHello struct {
	MediaKind               string `json:"media_kind"`
	FormatCapabilityVersion uint32 `json:"format_capability_version"`
	PayloadType             uint8  `json:"payload_type"`
	ClockRate               int    `json:"clock_rate"`
	SSRC                    uint32 `json:"ssrc"`
	Encoding                string `json:"encoding"`
	SampleRate              int    `json:"sample_rate"`
	Channels                int    `json:"channels"`
	FrameSamples            int    `json:"frame_samples"`
}

// ParseAudioMediaHello decodes the media hello body. Missing media_kind is
// interpreted as video to keep historic video hello bodies valid; callers for
// the audio transport must then explicitly require the audio kind.
func ParseAudioMediaHello(body []byte) (AudioMediaHello, error) {
	if len(body) == 0 {
		return AudioMediaHello{}, errors.New("media hello body is required")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var hello AudioMediaHello
	if err := decoder.Decode(&hello); err != nil {
		return AudioMediaHello{}, fmt.Errorf("decode media hello: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return AudioMediaHello{}, errors.New("media hello has trailing JSON")
		}
		return AudioMediaHello{}, fmt.Errorf("decode trailing media hello data: %w", err)
	}
	if hello.MediaKind == "" {
		hello.MediaKind = "video"
	}
	return hello, nil
}

func ValidateAudioMediaHello(hello AudioMediaHello, expected AudioReceiverConfig) error {
	if hello.MediaKind != "audio" {
		return fmt.Errorf("unexpected media kind %q", hello.MediaKind)
	}
	if hello.FormatCapabilityVersion != expected.FormatCapabilityVersion {
		return errors.New("audio format capability version mismatch")
	}
	if hello.PayloadType != expected.PayloadType || hello.ClockRate != RTPAudioClockRate || hello.SSRC != expected.SSRC {
		return errors.New("audio RTP declaration mismatch")
	}
	if hello.Encoding != AudioEncodingPCM16LE || hello.SampleRate != expected.SampleRate || hello.Channels != expected.Channels || hello.FrameSamples != expected.FrameSamples {
		return errors.New("audio format declaration mismatch")
	}
	return nil
}

func WriteControlMessage(w io.Writer, message ControlMessage) error {
	if w == nil {
		return errors.New("control writer is nil")
	}
	if message.Type == "" || message.Session == "" || message.Token == "" {
		return errors.New("control message requires type, session, and token")
	}
	payload, err := json.Marshal(struct {
		Magic   string `json:"magic"`
		Version int    `json:"version"`
		ControlMessage
	}{Magic: ControlMagic, Version: ControlVersion, ControlMessage: message})
	if err != nil {
		return fmt.Errorf("marshal control message: %w", err)
	}
	if len(payload) > DefaultControlLimit {
		return fmt.Errorf("control message is too large: %d bytes", len(payload))
	}
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(payload)))
	if err := writeFull(w, length[:]); err != nil {
		return fmt.Errorf("write control length: %w", err)
	}
	if err := writeFull(w, payload); err != nil {
		return fmt.Errorf("write control payload: %w", err)
	}
	return nil
}

func writeFull(w io.Writer, payload []byte) error {
	for len(payload) > 0 {
		written, err := w.Write(payload)
		if err != nil {
			return err
		}
		if written <= 0 || written > len(payload) {
			return io.ErrShortWrite
		}
		payload = payload[written:]
	}
	return nil
}

func ReadControlMessage(r io.Reader) (ControlMessage, error) {
	return readControlMessageLimit(r, DefaultControlLimit)
}

func readControlMessageLimit(r io.Reader, limit int) (ControlMessage, error) {
	if r == nil {
		return ControlMessage{}, errors.New("control reader is nil")
	}
	if limit < 1 {
		return ControlMessage{}, errors.New("control limit must be positive")
	}
	var length [4]byte
	if _, err := io.ReadFull(r, length[:]); err != nil {
		return ControlMessage{}, fmt.Errorf("read control length: %w", err)
	}
	size := binary.BigEndian.Uint32(length[:])
	if size == 0 || uint64(size) > uint64(limit) {
		return ControlMessage{}, fmt.Errorf("control frame length %d exceeds limit %d", size, limit)
	}
	payload := make([]byte, size)
	if _, err := io.ReadFull(r, payload); err != nil {
		return ControlMessage{}, fmt.Errorf("read control payload: %w", err)
	}
	var envelope struct {
		Magic   string `json:"magic"`
		Version int    `json:"version"`
		ControlMessage
	}
	decoder := json.NewDecoder(bufio.NewReader(bytes.NewReader(payload)))
	if err := decoder.Decode(&envelope); err != nil {
		return ControlMessage{}, fmt.Errorf("decode control message: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return ControlMessage{}, errors.New("control message has trailing JSON")
		}
		return ControlMessage{}, fmt.Errorf("decode trailing control message data: %w", err)
	}
	if envelope.Magic != ControlMagic || envelope.Version != ControlVersion {
		return ControlMessage{}, errors.New("unsupported control magic or version")
	}
	if envelope.Type == "" || envelope.Session == "" || envelope.Token == "" {
		return ControlMessage{}, errors.New("control message is missing required fields")
	}
	return envelope.ControlMessage, nil
}

func ValidateControlMessage(message ControlMessage, session string, generation uint64, token string) error {
	if message.Session != session {
		return errors.New("control session mismatch")
	}
	if message.Generation != generation {
		return errors.New("control generation mismatch")
	}
	if message.Token == "" || subtle.ConstantTimeCompare([]byte(message.Token), []byte(token)) != 1 {
		return errors.New("control token mismatch")
	}
	return nil
}
