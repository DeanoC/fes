//go:build !linux

package bridge

import "github.com/DeanoC/FogCast/protocol"

// FakeSink is used by tests and makes non-Linux builds explicit: no device is opened.
type FakeSink struct {
	Events   []protocol.InputFrame
	Released bool
}

func (s *FakeSink) Apply(f protocol.InputFrame) error { s.Events = append(s.Events, f); return nil }
func (s *FakeSink) ReleaseAll() error                 { s.Released = true; return nil }
func (s *FakeSink) Close() error                      { return nil }

func OpenUInput(_ string) (Sink, error)          { return nil, ErrUnsupportedPlatform }
func CreateUInputGamepad(_ string) (Sink, error) { return nil, ErrUnsupportedPlatform }
