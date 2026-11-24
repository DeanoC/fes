package localhid

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"io"
)

// SourceBackend is the target integration boundary, not an authorization
// itself. Each operation must acquire the current execution-lease admission,
// check source/core/focus/queued-transition authority under lifecycle and
// release admission only after its work drains. Open must not post input or
// retain a lease reference/reservation after it returns, including on cancel.
// Edge must return only after the sink call has returned; nil delta still
// revalidates ownership. Neutral must affect only the old local contribution,
// preserve remote contributors and report success only when confirmed or the
// old generation is proven retired. The caller never retries a failed Edge.
type SourceBackend interface {
	Open(context.Context, Request) (any, error)
	Edge(context.Context, any, *Delta) error
	Barrier(context.Context, any) error
	Neutral(context.Context, any) (string, error)
}

// SourceSession is owned by exactly one serialized local reader. A malformed
// stream is a connection-level error; a syntactically valid wrong-token record
// must be rejected without retiring another source.
type SourceSession struct {
	backend     SourceBackend
	random      io.Reader
	issued      map[string]bool
	token       string
	binding     any
	ledger      *Ledger
	lastRequest uint64
	neutralOnly bool
}

func NewSourceSession(backend SourceBackend) *SourceSession {
	return &SourceSession{backend: backend, random: rand.Reader, issued: make(map[string]bool)}
}

// Active is queried only by the serialized connection owner during cleanup.
func (s *SourceSession) Active() bool { return s != nil && s.token != "" }

func (s *SourceSession) matches(token string) bool {
	return s.token != "" && len(token) == len(s.token) && subtle.ConstantTimeCompare([]byte(token), []byte(s.token)) == 1
}
func (s *SourceSession) reply(req Request, status string) Reply {
	return Reply{Version: 2, Op: req.Op, Request: req.Request, Token: s.token, Status: status}
}

// Handle processes exactly one validated request. Errors must not be treated
// as a successful response. The connection owner decides framing failure
// cleanup, without allowing a stale token to retire an unrelated source.
func (s *SourceSession) Handle(ctx context.Context, req Request) (Reply, error) {
	if s == nil || s.backend == nil || ctx == nil {
		return Reply{}, ErrRequest
	}
	if _, err := EncodeRequest(req); err != nil {
		return Reply{}, err
	}
	if err := ctx.Err(); err != nil {
		return Reply{}, err
	}
	if req.Op == "begin" {
		if s.token != "" {
			return Reply{}, ErrPending
		}
	} else if !s.matches(req.Token) {
		return Reply{}, ErrRequest
	}
	if req.Request <= s.lastRequest {
		return Reply{}, ErrRequest
	}
	if s.neutralOnly && req.Op != "reset" {
		return Reply{}, ErrQuarantined
	}
	switch req.Op {
	case "begin":
		if len(s.issued) >= 1024 {
			return Reply{}, ErrQuarantined
		}
		ledger, err := New(req.SnapshotSeq, req.Devices)
		if err != nil {
			return Reply{}, err
		}
		var random [32]byte
		if _, err = io.ReadFull(s.random, random[:]); err != nil {
			return Reply{}, err
		}
		token := hex.EncodeToString(random[:])
		if !nonzeroID(token, false) || s.issued[token] {
			return Reply{}, ErrRequest
		}
		binding, err := s.backend.Open(ctx, req)
		if err != nil {
			return Reply{}, err
		}
		if binding == nil {
			return Reply{}, ErrRequest
		}
		if ctx.Err() != nil {
			return Reply{}, ctx.Err()
		}
		s.issued[token] = true
		s.token = token
		s.binding = binding
		s.ledger = ledger
		s.lastRequest = req.Request
		return s.reply(req, "ready"), nil
	case "edge":
		// Ledger effects remain provisional until the backend's exact sink call
		// has drained and the matching transaction is confirmed.
		deltas, err := s.ledger.Edge(req.EventSeq, req.Device, req.Usage, req.Down)
		if err != nil {
			// A valid token with an invalid source sequence is quarantined;
			// the reader may request only a whole-source neutral afterward.
			s.neutralOnly = true
			s.lastRequest = req.Request
			return s.reply(req, "quarantined"), nil
		}
		var delta *Delta
		if len(deltas) == 1 {
			delta = &deltas[0]
		}
		err = s.backend.Edge(ctx, s.binding, delta)
		// A successful, drained backend post is the commit point. A later
		// cancellation cannot turn it into an ambiguous write; if the reply
		// cannot be sent, connection cleanup neutralizes the owned source.
		if err != nil {
			if id, ok := s.ledger.Pending(); ok {
				_ = s.ledger.Fail(id)
			} else {
				_, _ = s.ledger.Quarantine()
			}
			s.neutralOnly = true
			s.lastRequest = req.Request
			return s.reply(req, "quarantined"), nil
		}
		if id, ok := s.ledger.Pending(); ok {
			if err := s.ledger.Confirm(id); err != nil {
				return Reply{}, err
			}
		}
		s.lastRequest = req.Request
		return s.reply(req, "accepted"), nil
	case "barrier":
		if req.LastEventSeq != s.ledger.Sequence() {
			return Reply{}, ErrRequest
		}
		if !s.ledger.Ready() {
			s.neutralOnly = true
			s.lastRequest = req.Request
			return s.reply(req, "quarantined"), nil
		}
		if err := s.backend.Barrier(ctx, s.binding); err != nil {
			_, _ = s.ledger.Quarantine()
			s.neutralOnly = true
			s.lastRequest = req.Request
			return s.reply(req, "quarantined"), nil
		}
		s.lastRequest = req.Request
		return s.reply(req, "drained"), nil
	case "reset":
		// Once an edge has quarantined the source, the sender may know only
		// the attempted sequence, not the target's last accounted sequence.
		// Same-token RESET neutralizes the whole source; it does not replay or
		// assert that the missing edge was applied.
		if !s.neutralOnly && req.LastEventSeq != s.ledger.Sequence() {
			return Reply{}, ErrRequest
		}
		if _, err := s.ledger.Quarantine(); err != nil && !errors.Is(err, ErrQuarantined) {
			return Reply{}, err
		}
		status, err := s.backend.Neutral(ctx, s.binding)
		if err != nil || ctx.Err() != nil || (status != "neutral-confirmed" && status != "old-generation-retired") {
			s.neutralOnly = true
			s.lastRequest = req.Request
			return s.reply(req, "unconfirmed"), nil
		}
		result := s.reply(req, status)
		s.lastRequest = req.Request
		s.token = ""
		s.binding = nil
		s.ledger = nil
		s.neutralOnly = false
		return result, nil
	}
	return Reply{}, ErrRequest
}

// Abort is called by the same owner after any current Handle/sink call has
// returned, e.g. after EOF, malformed framing, or an uncertain reply write.
// It performs only idempotent local-source neutralization, not input replay.
// An unconfirmed result retains the source so cleanup can be retried or the
// execution-lease manager can take ownership on revocation.
func (s *SourceSession) Abort(ctx context.Context) (string, error) {
	if s == nil || s.backend == nil || ctx == nil {
		return "", ErrRequest
	}
	if s.token == "" {
		return "neutral-confirmed", nil
	}
	if _, err := s.ledger.Quarantine(); err != nil && !errors.Is(err, ErrQuarantined) {
		return "unconfirmed", err
	}
	s.neutralOnly = true
	status, err := s.backend.Neutral(ctx, s.binding)
	if err != nil || ctx.Err() != nil || (status != "neutral-confirmed" && status != "old-generation-retired") {
		return "unconfirmed", errors.Join(err, ctx.Err())
	}
	s.token = ""
	s.binding = nil
	s.ledger = nil
	s.neutralOnly = false
	return status, nil
}
