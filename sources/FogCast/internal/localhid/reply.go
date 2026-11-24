package localhid

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"regexp"

	"github.com/DeanoC/FogCast/protocol"
)

const MaxReplyBytes = 16384
const replyVersion = 2

var replyMarker = [4]byte{'F', '2', 'A', '!'}
var tokenPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var ErrReply = errors.New("invalid local control reply")

type Reply struct {
	Version       uint8  `json:"version"`
	Op            string `json:"op"`
	Request       uint64 `json:"request"`
	Token         string `json:"token"`
	Status        string `json:"status"`
	AgentInstance string `json:"agent_instance,omitempty"`
	TargetID      string `json:"target_id,omitempty"`
	BootID        string `json:"boot_id,omitempty"`
}
type Incoming struct {
	Reply *Reply
	Pong  *protocol.InputFrame
}

// ReadIncoming is called only by the one connection reader. It accepts a
// bounded v2 control reply or an existing binary PONG, never a raw key frame.
func ReadIncoming(r io.Reader) (Incoming, error) {
	var prefix [4]byte
	if _, err := io.ReadFull(r, prefix[:]); err != nil {
		return Incoming{}, err
	}
	if prefix == replyMarker {
		var length [4]byte
		if _, err := io.ReadFull(r, length[:]); err != nil {
			return Incoming{}, err
		}
		n := binary.LittleEndian.Uint32(length[:])
		if n == 0 || n > MaxReplyBytes {
			return Incoming{}, ErrReply
		}
		payload := make([]byte, int(n))
		if _, err := io.ReadFull(r, payload); err != nil {
			return Incoming{}, err
		}
		reply, err := decodeReply(payload)
		if err != nil {
			return Incoming{}, err
		}
		return Incoming{Reply: &reply}, nil
	}
	if binary.LittleEndian.Uint32(prefix[:]) != 48 {
		return Incoming{}, ErrReply
	}
	pong, err := protocol.DecodeInputFrame(io.MultiReader(bytes.NewReader(prefix[:]), r), 48)
	if err != nil || pong.Header.Type != protocol.InputTypePong {
		return Incoming{}, ErrReply
	}
	return Incoming{Pong: &pong}, nil
}

// decodeReply accepts exactly one JSON object with each required field once.
func decodeReply(payload []byte) (Reply, error) {
	var reply Reply
	d := json.NewDecoder(bytes.NewReader(payload))
	start, err := d.Token()
	if err != nil || start != json.Delim('{') {
		return Reply{}, ErrReply
	}
	seen := map[string]bool{}
	for d.More() {
		raw, err := d.Token()
		key, ok := raw.(string)
		if err != nil || !ok || seen[key] {
			return Reply{}, ErrReply
		}
		seen[key] = true
		var value json.RawMessage
		if err := d.Decode(&value); err != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return Reply{}, ErrReply
		}
		switch key {
		case "version":
			err = json.Unmarshal(value, &reply.Version)
		case "op":
			err = json.Unmarshal(value, &reply.Op)
		case "request":
			err = json.Unmarshal(value, &reply.Request)
		case "token":
			err = json.Unmarshal(value, &reply.Token)
		case "status":
			err = json.Unmarshal(value, &reply.Status)
		case "agent_instance":
			err = json.Unmarshal(value, &reply.AgentInstance)
		case "target_id":
			err = json.Unmarshal(value, &reply.TargetID)
		case "boot_id":
			err = json.Unmarshal(value, &reply.BootID)
		default:
			return Reply{}, ErrReply
		}
		if err != nil {
			return Reply{}, ErrReply
		}
	}
	end, err := d.Token()
	if err != nil || end != json.Delim('}') || (reply.Op == "recover" && (len(seen) != 8 || !seen["agent_instance"] || !seen["target_id"] || !seen["boot_id"])) || (reply.Op != "recover" && (len(seen) != 5 || seen["agent_instance"] || seen["target_id"] || seen["boot_id"])) {
		return Reply{}, ErrReply
	}
	if _, err = d.Token(); err != io.EOF {
		return Reply{}, ErrReply
	}
	if err := reply.validate(); err != nil {
		return Reply{}, err
	}
	return reply, nil
}
func (r Reply) validate() error {
	if r.Version != replyVersion || r.Request == 0 {
		return ErrReply
	}
	if r.Op != "recover" && (r.AgentInstance != "" || r.TargetID != "" || r.BootID != "") {
		return ErrReply
	}
	switch r.Op {
	case "recover":
		if !nonzeroID(r.AgentInstance, true) || !validRecoveryIdentity(r.TargetID) || !validRecoveryIdentity(r.BootID) || r.Token != "" || (r.Status != "reader-drained" && r.Status != "startup-retired" && r.Status != "unavailable") {
			return ErrReply
		}
	case "begin":
		if r.Status == "ready" {
			if !nonzeroID(r.Token, false) {
				return ErrReply
			}
		} else if r.Status != "quarantined" || r.Token != "" {
			return ErrReply
		}
	case "barrier":
		if !nonzeroID(r.Token, false) || (r.Status != "drained" && r.Status != "quarantined") {
			return ErrReply
		}
	case "edge":
		if !nonzeroID(r.Token, false) || (r.Status != "accepted" && r.Status != "quarantined") {
			return ErrReply
		}
	case "reset":
		if !nonzeroID(r.Token, false) || (r.Status != "neutral-confirmed" && r.Status != "old-generation-retired" && r.Status != "unconfirmed") {
			return ErrReply
		}
	default:
		return ErrReply
	}
	return nil
}

// CorrelateReply must be called by the connection owner before a reply can
// change state. The owner tracks outstanding request IDs and consumes each
// exactly once; begin accepts a newly issued token only on its matching reply.
func CorrelateReply(r Reply, request uint64, op, token string) error {
	if err := r.validate(); err != nil {
		return err
	}
	if r.Request != request || r.Op != op {
		return ErrReply
	}
	if op == "recover" {
		return ErrReply
	}
	if op == "begin" {
		if token != "" {
			return ErrReply
		}
	} else if r.Token != token || !nonzeroID(token, false) {
		return ErrReply
	}
	return nil
}

// CorrelateRecoveryReply binds recovery to the previously observed target,
// boot and agent identity, in addition to the request ID and reply status.
func CorrelateRecoveryReply(r Reply, req Request) error {
	if err := r.validate(); err != nil {
		return err
	}
	if req.Op != "recover" || req.Version != replyVersion || req.Request == 0 || !nonzeroID(req.AgentInstance, true) ||
		!validRecoveryIdentity(req.TargetID) || !validRecoveryIdentity(req.BootID) || r.Op != "recover" || r.Request != req.Request || r.TargetID != req.TargetID || r.BootID != req.BootID ||
		(r.Status == "reader-drained" && r.AgentInstance != req.AgentInstance) || (r.Status == "startup-retired" && r.AgentInstance == req.AgentInstance) {
		return ErrReply
	}
	return nil
}

// EncodeReply emits a bounded v2 reply frame. Only the agent that owns the
// corresponding request and source may use it; encoding is not authorization.
func EncodeReply(reply Reply) ([]byte, error) {
	if err := reply.validate(); err != nil {
		return nil, err
	}
	payload, err := json.Marshal(reply)
	if err != nil || len(payload) == 0 || len(payload) > MaxReplyBytes {
		return nil, ErrReply
	}
	out := make([]byte, 8+len(payload))
	copy(out[:4], replyMarker[:])
	binary.LittleEndian.PutUint32(out[4:8], uint32(len(payload)))
	copy(out[8:], payload)
	return out, nil
}
