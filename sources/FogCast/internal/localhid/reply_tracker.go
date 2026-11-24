package localhid

import "sync"

// ReplyTracker binds at most one outstanding control request to its reply.
// The connection owner must keep this state quarantined across EOF until the
// old source is safely retired; a new connection alone is not a reset.
type ReplyTracker struct {
	mu          sync.Mutex
	next        uint64
	waiting     bool
	request     uint64
	op          string
	token       string
	source      string
	quarantined bool
	neutralOnly bool
	recovery    Request
}

// Start registers a request before it is written. Waiting for its response
// must not hold the connection's raw-input write mutex.
func (t *ReplyTracker) Start(op, token string) (uint64, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.quarantined {
		return 0, ErrQuarantined
	}
	if t.neutralOnly && op != "reset" {
		return 0, ErrQuarantined
	}
	if t.waiting {
		return 0, ErrPending
	}
	if (op == "begin" && (token != "" || t.source != "")) ||
		((op == "edge" || op == "barrier" || op == "reset") && (!nonzeroID(token, false) || token != t.source)) ||
		(op != "begin" && op != "edge" && op != "barrier" && op != "reset") {
		return 0, ErrReply
	}
	if t.next == ^uint64(0) {
		t.quarantined = true
		return 0, ErrReply
	}
	t.next++
	t.request = t.next
	t.op = op
	t.token = token
	t.waiting = true
	return t.request, nil
}

// StartRecovery reserves one correlated request and its full prior identity.
func (t *ReplyTracker) StartRecovery(agentInstance, targetID, bootID string) (uint64, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.quarantined || t.neutralOnly {
		return 0, ErrQuarantined
	}
	if t.waiting {
		return 0, ErrPending
	}
	if t.source != "" || !nonzeroID(agentInstance, true) || !validRecoveryIdentity(targetID) || !validRecoveryIdentity(bootID) {
		return 0, ErrReply
	}
	if t.next == ^uint64(0) {
		t.quarantined = true
		return 0, ErrReply
	}
	t.next++
	t.request, t.op, t.token, t.waiting = t.next, "recover", "", true
	t.recovery = Request{Version: replyVersion, Op: "recover", Request: t.request, AgentInstance: agentInstance, TargetID: targetID, BootID: bootID}
	return t.request, nil
}

// Accept consumes a correlated reply once. Any unsolicited, duplicate, stale
// or mismatched response permanently quarantines this connection/source.
func (t *ReplyTracker) Accept(r Reply) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.quarantined {
		return ErrQuarantined
	}
	var correlation error
	if t.op == "recover" {
		correlation = CorrelateRecoveryReply(r, t.recovery)
	} else {
		correlation = CorrelateReply(r, t.request, t.op, t.token)
	}
	if !t.waiting || correlation != nil {
		t.quarantined = true
		return ErrReply
	}
	t.waiting = false
	if r.Status == "quarantined" && r.Op == "edge" {
		// This correlated response proves the sink call has drained, but not
		// that its outcome was neutral. Permit only same-token reset.
		t.neutralOnly = true
		return ErrQuarantined
	}
	if r.Status == "quarantined" || r.Status == "unconfirmed" || r.Status == "unavailable" {
		t.quarantined = true
		return ErrQuarantined
	}
	if r.Op == "begin" {
		t.source = r.Token
	} else if r.Op == "reset" {
		t.source = ""
		t.neutralOnly = false
	}
	return nil
}

// Disconnect never counts as neutral or a successful response.
func (t *ReplyTracker) Disconnect() { t.mu.Lock(); defer t.mu.Unlock(); t.quarantined = true }
