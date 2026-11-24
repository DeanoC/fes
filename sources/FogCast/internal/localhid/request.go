package localhid

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
	"unicode"
)

const MaxRequestBytes = 16384

var shortIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
var ErrRequest = errors.New("invalid local control request")

type Request struct {
	Version         uint8
	Op              string
	Request         uint64
	Publisher       string
	AgentInstance   string
	TargetID        string
	BootID          string
	QueueEpoch      uint64
	LeaseGeneration string
	RuntimeInstance string
	PackageID       string
	CoreGeneration  uint64
	SnapshotSeq     uint64
	Devices         []DeviceSnapshot
	Token           string
	EventSeq        uint64
	Device          DeviceID
	Usage           uint8
	Down            bool
	LastEventSeq    uint64
}

// ReadRequest consumes one bounded newline-terminated v2 control object. It
// uses the caller's shared buffered reader and leaves all following frames.
// A deadline belongs to the connection owner; the parser never authorizes a
// source and must not be used to fall back to a prior protocol.
func ReadRequest(r *bufio.Reader) (Request, error) {
	var buf [MaxRequestBytes]byte
	n := 0
	for {
		ch, err := r.ReadByte()
		if err != nil {
			if err == io.EOF && n > 0 {
				return Request{}, io.ErrUnexpectedEOF
			}
			return Request{}, err
		}
		if ch == '\n' {
			if n == 0 {
				return Request{}, ErrRequest
			}
			return decodeRequest(buf[:n])
		}
		if n == len(buf) {
			return Request{}, ErrRequest
		}
		buf[n] = ch
		n++
	}
}

// object rejects duplicate, unknown and null fields before decoding values.
func object(payload []byte, keys ...string) (map[string]json.RawMessage, error) {
	allowed := make(map[string]bool, len(keys))
	for _, k := range keys {
		allowed[k] = true
	}
	d := json.NewDecoder(bytes.NewReader(payload))
	tok, err := d.Token()
	if err != nil || tok != json.Delim('{') {
		return nil, ErrRequest
	}
	out := make(map[string]json.RawMessage, len(keys))
	for d.More() {
		raw, err := d.Token()
		key, ok := raw.(string)
		if err != nil || !ok || !allowed[key] || out[key] != nil {
			return nil, ErrRequest
		}
		var value json.RawMessage
		if err := d.Decode(&value); err != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, ErrRequest
		}
		out[key] = value
	}
	tok, err = d.Token()
	if err != nil || tok != json.Delim('}') || len(out) != len(keys) {
		return nil, ErrRequest
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, ErrRequest
	}
	return out, nil
}
func field(m map[string]json.RawMessage, key string, v any) error {
	if err := json.Unmarshal(m[key], v); err != nil {
		return ErrRequest
	}
	return nil
}
func validNumber(n uint64) bool     { return n > 0 && n < ^uint64(0) }
func validZeroNumber(n uint64) bool { return n < ^uint64(0) }
func ValidRecoveryIdentity(value string) bool {
	return value != "" && len(value) <= 160 && strings.TrimSpace(value) == value && !strings.ContainsFunc(value, unicode.IsControl)
}
func validRecoveryIdentity(value string) bool { return ValidRecoveryIdentity(value) }
func nonzeroID(s string, short bool) bool {
	if short {
		if !shortIDPattern.MatchString(s) {
			return false
		}
	} else if !tokenPattern.MatchString(s) {
		return false
	}
	for _, ch := range s {
		if ch != '0' {
			return true
		}
	}
	return false
}
func deviceID(s string) (DeviceID, error) {
	var id DeviceID
	if !nonzeroID(s, true) {
		return id, ErrRequest
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return id, ErrRequest
	}
	copy(id[:], b)
	return id, nil
}

func decodeRequest(payload []byte) (Request, error) {
	if len(payload) == 0 || len(payload) > MaxRequestBytes {
		return Request{}, ErrRequest
	}
	// Discover the operation without granting authority; the second strict pass
	// rejects all duplicate/unknown fields, including duplicate op fields.
	var head struct {
		Op string `json:"op"`
	}
	if json.Unmarshal(payload, &head) != nil {
		return Request{}, ErrRequest
	}
	common := []string{"version", "op", "request"}
	var keys []string
	switch head.Op {
	case "begin":
		keys = append(common, "publisher", "agent_instance", "queue_epoch", "lease_generation", "runtime_instance", "package_id", "core_generation", "snapshot_seq", "devices")
	case "recover":
		keys = append(common, "agent_instance", "target_id", "boot_id")
	case "edge":
		keys = append(common, "token", "event_seq", "device", "usage", "down")
	case "barrier", "reset":
		keys = append(common, "token", "last_event_seq")
	default:
		return Request{}, ErrRequest
	}
	m, err := object(payload, keys...)
	if err != nil {
		return Request{}, err
	}
	var r Request
	for key, v := range map[string]any{"version": &r.Version, "op": &r.Op, "request": &r.Request} {
		if err := field(m, key, v); err != nil {
			return Request{}, err
		}
	}
	if r.Version != replyVersion || !validNumber(r.Request) {
		return Request{}, ErrRequest
	}
	switch r.Op {
	case "begin":
		for key, v := range map[string]any{"publisher": &r.Publisher, "agent_instance": &r.AgentInstance, "queue_epoch": &r.QueueEpoch, "lease_generation": &r.LeaseGeneration, "runtime_instance": &r.RuntimeInstance, "package_id": &r.PackageID, "core_generation": &r.CoreGeneration, "snapshot_seq": &r.SnapshotSeq} {
			if err := field(m, key, v); err != nil {
				return Request{}, err
			}
		}
		if !nonzeroID(r.Publisher, true) || !nonzeroID(r.AgentInstance, true) || !validNumber(r.QueueEpoch) || !nonzeroID(r.LeaseGeneration, false) || !nonzeroID(r.RuntimeInstance, true) || (r.PackageID != "" && !nonzeroID(r.PackageID, false)) || !validNumber(r.CoreGeneration) || !validZeroNumber(r.SnapshotSeq) {
			return Request{}, ErrRequest
		}
		var rawDevices []json.RawMessage
		if err := field(m, "devices", &rawDevices); err != nil || len(rawDevices) == 0 || len(rawDevices) > MaxDevices {
			return Request{}, ErrRequest
		}
		seen := map[DeviceID]bool{}
		for _, raw := range rawDevices {
			fields, err := object(raw, "device", "held")
			if err != nil {
				return Request{}, err
			}
			var name string
			var rawHeld []json.RawMessage
			if field(fields, "device", &name) != nil || field(fields, "held", &rawHeld) != nil || rawHeld == nil || len(rawHeld) > 256 {
				return Request{}, ErrRequest
			}
			id, err := deviceID(name)
			if err != nil || seen[id] {
				return Request{}, ErrRequest
			}
			seen[id] = true
			seenUsage := map[uint8]bool{}
			held := make([]uint8, 0, len(rawHeld))
			for _, value := range rawHeld {
				var usage uint8
				if bytes.Equal(bytes.TrimSpace(value), []byte("null")) || json.Unmarshal(value, &usage) != nil {
					return Request{}, ErrRequest
				}
				if !validUsage(usage) || seenUsage[usage] {
					return Request{}, ErrRequest
				}
				seenUsage[usage] = true
				held = append(held, usage)
			}
			r.Devices = append(r.Devices, DeviceSnapshot{ID: id, Held: held})
		}
	case "recover":
		if field(m, "agent_instance", &r.AgentInstance) != nil || field(m, "target_id", &r.TargetID) != nil || field(m, "boot_id", &r.BootID) != nil || !nonzeroID(r.AgentInstance, true) || !validRecoveryIdentity(r.TargetID) || !validRecoveryIdentity(r.BootID) {
			return Request{}, ErrRequest
		}
	case "edge":
		var name string
		for key, v := range map[string]any{"token": &r.Token, "event_seq": &r.EventSeq, "device": &name, "usage": &r.Usage, "down": &r.Down} {
			if err := field(m, key, v); err != nil {
				return Request{}, err
			}
		}
		id, err := deviceID(name)
		if err != nil || !nonzeroID(r.Token, false) || !validNumber(r.EventSeq) || !validUsage(r.Usage) {
			return Request{}, ErrRequest
		}
		r.Device = id
	case "barrier", "reset":
		if field(m, "token", &r.Token) != nil || field(m, "last_event_seq", &r.LastEventSeq) != nil || !nonzeroID(r.Token, false) || !validZeroNumber(r.LastEventSeq) {
			return Request{}, ErrRequest
		}
	}
	return r, nil
}

// EncodeRequest emits exactly one validated v2 control line. It has no I/O
// and confers no source authority. The caller supplies an already registered
// request ID and serializes writes with other local-feed traffic.
func EncodeRequest(r Request) ([]byte, error) {
	m := map[string]any{"version": r.Version, "op": r.Op, "request": r.Request}
	switch r.Op {
	case "begin":
		if len(r.Publisher) != 32 || len(r.AgentInstance) != 32 || len(r.LeaseGeneration) != 64 || len(r.RuntimeInstance) != 32 || (len(r.PackageID) != 0 && len(r.PackageID) != 64) ||
			len(r.Devices) == 0 || len(r.Devices) > MaxDevices {
			return nil, ErrRequest
		}
		devices := make([]map[string]any, 0, len(r.Devices))
		for _, d := range r.Devices {
			if len(d.Held) > 256 {
				return nil, ErrRequest
			}
			held := make([]int, 0, len(d.Held))
			for _, usage := range d.Held {
				held = append(held, int(usage))
			}
			devices = append(devices, map[string]any{"device": hex.EncodeToString(d.ID[:]), "held": held})
		}
		m["publisher"] = r.Publisher
		m["agent_instance"] = r.AgentInstance
		m["queue_epoch"] = r.QueueEpoch
		m["lease_generation"] = r.LeaseGeneration
		m["runtime_instance"] = r.RuntimeInstance
		m["package_id"] = r.PackageID
		m["core_generation"] = r.CoreGeneration
		m["snapshot_seq"] = r.SnapshotSeq
		m["devices"] = devices
	case "recover":
		if len(r.AgentInstance) != 32 || !validRecoveryIdentity(r.TargetID) || !validRecoveryIdentity(r.BootID) {
			return nil, ErrRequest
		}
		m["agent_instance"] = r.AgentInstance
		m["target_id"] = r.TargetID
		m["boot_id"] = r.BootID
	case "edge":
		if len(r.Token) != 64 {
			return nil, ErrRequest
		}
		m["token"] = r.Token
		m["event_seq"] = r.EventSeq
		m["device"] = hex.EncodeToString(r.Device[:])
		m["usage"] = r.Usage
		m["down"] = r.Down
	case "barrier", "reset":
		if len(r.Token) != 64 {
			return nil, ErrRequest
		}
		m["token"] = r.Token
		m["last_event_seq"] = r.LastEventSeq
	default:
		return nil, ErrRequest
	}
	raw, err := json.Marshal(m)
	if err != nil || len(raw) > MaxRequestBytes {
		return nil, ErrRequest
	}
	if _, err := decodeRequest(raw); err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}
