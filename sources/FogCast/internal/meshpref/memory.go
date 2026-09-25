// Package meshpref remembers two node ids on the host: the household
// display preference, and the DisplaySink of the last play that started.
//
// Empty means unset. The record is process memory. It is not written to
// agent.toml, a kit file, or an SD card, and it has no JSON encoding.
// PlaceOptions copies the ids into meshplace.Options. Override and a
// missing composition slot stay unset for the caller to fill.
// meshplace applies the unsigned Decision 7 strawman; this package only
// supplies the ids that function already accepts.
package meshpref

import (
	"strings"
	"sync"

	"github.com/DeanoC/FogCast/internal/meshplace"
)

// Memory is the host-local record. The zero value is unset.
type Memory struct {
	mu         sync.Mutex
	preference string
	lastSink   string
}

// New returns an empty record.
func New() *Memory {
	return &Memory{}
}

// DisplayPreference returns the household display preference node id.
// Empty means unset.
func (m *Memory) DisplayPreference() string {
	if m == nil {
		return ""
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.preference
}

// SetDisplayPreference stores the household display preference.
// Empty, including blank space, clears it.
func (m *Memory) SetDisplayPreference(nodeID string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.preference = strings.TrimSpace(nodeID)
	m.mu.Unlock()
}

// LastDisplaySink returns the DisplaySink node id recorded when a play
// session started. Empty means unset.
func (m *Memory) LastDisplaySink() string {
	if m == nil {
		return ""
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastSink
}

// NotePlayStarted records nodeID as the last play DisplaySink.
// An empty id is unset and does not replace a remembered sink.
// Callers invoke this when a play session starts. Other session work
// does not.
func (m *Memory) NotePlayStarted(nodeID string) {
	if m == nil {
		return
	}
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return
	}
	m.mu.Lock()
	m.lastSink = nodeID
	m.mu.Unlock()
}

// PlaceOptions copies the stored ids for meshplace.Place.
// Empty fields stay empty. OverrideNodeID and MissingRequiredSlot
// stay at zero.
func (m *Memory) PlaceOptions() meshplace.Options {
	if m == nil {
		return meshplace.Options{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return meshplace.Options{
		DisplayPreference: m.preference,
		LastDisplaySink:   m.lastSink,
	}
}
