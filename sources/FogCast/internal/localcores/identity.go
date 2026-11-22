package localcores

// AgentIdentity identifies the local agent that answered over the Unix socket.
type AgentIdentity struct {
	TargetID  string
	PeerPID   int
	PeerStart string
}
