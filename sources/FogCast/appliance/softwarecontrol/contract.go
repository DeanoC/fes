// Package softwarecontrol defines the persistent kit software control API.
package softwarecontrol

type Request struct {
	ID         string `json:"id"`
	Candidate  string `json:"candidate"`
	Requester  string `json:"requester"`
	Generation uint64 `json:"expected_generation"`
}

type Operation struct {
	Request
	Previous string `json:"previous"`
	Phase    string `json:"phase"` // queued, applying, recovering, blocked
	Reason   string `json:"reason,omitempty"`
}

type Receipt struct {
	Request
	Result string `json:"result"`
	Reason string `json:"reason,omitempty"`
}

type State struct {
	Version    int                `json:"version"`
	Generation uint64             `json:"generation"`
	Selected   string             `json:"selected"`
	Previous   string             `json:"previous,omitempty"`
	Pending    *Operation         `json:"pending,omitempty"`
	Unreleased *Request           `json:"unreleased,omitempty"`
	Receipts   map[string]Receipt `json:"receipts"`
}

type CancelRequest struct {
	ID         string `json:"id"`
	Generation uint64 `json:"expected_generation"`
}

type Status struct {
	State
	ActiveBundle string `json:"active_bundle"`
	BaseIdentity string `json:"base_identity"`
	ServiceFault string `json:"service_fault,omitempty"`
	StorageError string `json:"storage_error,omitempty"`
	// HeldBack is a selected bundle this base image cannot run, and
	// HeldBackReason says why. The image's factory software runs in its place
	// while the selection stays recorded.
	HeldBack       string `json:"held_back,omitempty"`
	HeldBackReason string `json:"held_back_reason,omitempty"`
}
